package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ogen-go/ogen/ogenerrors"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	admingrants "github.com/inCoderKaka/local-server/internal/admin/grants"
	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/auth/credential"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/hostmetrics"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// Sharing is the part of host.SharingState the admin API uses.
type Sharing interface {
	Effective(ctx context.Context) (host.State, error)
	PausedAt(ctx context.Context) (time.Time, bool, error)
	Pause(ctx context.Context, by string) (changed bool, err error)
	Resume(ctx context.Context, by string) (changed bool, err error)
}

// Sampler is the part of hostmetrics.Sampler the admin API uses.
type Sampler interface {
	Current() (hostmetrics.Reading, bool)
	Read(ctx context.Context) (hostmetrics.Reading, error)
	GetRange(ctx context.Context, from, to time.Time) ([]hostmetrics.Minute, error)
}

// HostKeys is the part of hostkeys.Keys the admin API uses: the host's global id, which is the
// host identity key's kid that receipts and checkpoints name too.
type HostKeys interface {
	HostID() string
}

// Engine is the part of the engine port the admin API uses: whether it answers, and which models
// it has. An engine that also has `Version(ctx) (string, error)` (the Ollama adapter does) has its
// version shown; the engine port itself does not carry one.
type Engine interface {
	Loaded(ctx context.Context) ([]engine.LoadedModel, error)
	ListModels(ctx context.Context) ([]engine.Model, error)
}

// RuleFailures is the part of the gateway the policy rule operations use: how many evaluations a
// rule has failed by not parsing, and forgetting them (*gateway.Gateway). The admin module does
// not import the gateway.
type RuleFailures = admingrants.RuleFailures

// Config is what New needs. All fields except Telemetry, Version and Clock are required.
type Config struct {
	DB       *storage.DB
	Settings *settings.Store
	// Identity is built with the same clock as Clock, so a session's created_at and expires_at
	// agree.
	Identity *identity.Repos
	// Grants is read to check a key's narrowing against its consumer's grant and to name models.
	Grants *grants.Repos
	// Auth issues and revokes access keys and forgets cached keys after a consumer or key changes
	// (*auth.Service). Build it with the same Clock.
	Auth consumers.Auth
	// Events runs the unit of work of every consumer, key and invite write, so that revoking a key
	// emits access_key.revoked in the same unit.
	Events  events.Notifier
	Audit   *audit.Log
	Sharing Sharing
	Sampler Sampler
	Keys    HostKeys
	Engine  Engine
	// RuleFailures lets the host switch off a broken policy rule after
	// policy.rule.max_failures failed evaluations (req_001 D-73); nil leaves that off. Pass the
	// *gateway.Gateway.
	RuleFailures RuleFailures
	// Metrics serves GET /metrics (telemetry.MetricsHandler), behind the same guard as the API.
	Metrics http.Handler
	// Version is the software version shown in /admin/v1/host. Empty means "dev".
	Version string
	// Telemetry nil means telemetry.Nop().
	Telemetry telemetry.Telemetry
	// Clock nil means time.Now.
	Clock func() time.Time
}

// Admin serves the admin API. Build it with New and mount Handler on the admin listener. It is
// safe for concurrent use.
type Admin struct {
	api.UnimplementedHandler

	cfg  Config
	now  func() time.Time
	log  *slog.Logger
	auth *Authenticator
	cons *consumers.Service
	grs  *admingrants.Service
	srv  http.Handler
}

var (
	_ api.Handler         = (*Admin)(nil)
	_ api.SecurityHandler = (*Admin)(nil)
)

// New registers the admin and credential settings and builds the admin API.
func New(cfg Config) (*Admin, error) {
	if cfg.DB == nil || cfg.Settings == nil || cfg.Identity == nil || cfg.Grants == nil || cfg.Auth == nil || cfg.Events == nil ||
		cfg.Audit == nil || cfg.Sharing == nil || cfg.Sampler == nil || cfg.Keys == nil || cfg.Engine == nil || cfg.Metrics == nil {
		return nil, errors.New("admin: DB, Settings, Identity, Grants, Auth, Events, Audit, Sharing, Sampler, Keys, Engine and Metrics are required")
	}
	if cfg.Telemetry == nil {
		cfg.Telemetry = telemetry.Nop()
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if err := registerSettings(cfg.Settings); err != nil {
		return nil, err
	}
	au, err := NewAuthenticator(AuthConfig{DB: cfg.DB, Settings: cfg.Settings, Identity: cfg.Identity, Clock: cfg.Clock})
	if err != nil {
		return nil, err
	}
	cons, err := consumers.New(consumers.Config{DB: cfg.DB, Settings: cfg.Settings, Identity: cfg.Identity, Grants: cfg.Grants,
		Auth: cfg.Auth, Events: cfg.Events, Clock: cfg.Clock, Logger: cfg.Telemetry.Logger()})
	if err != nil {
		return nil, err
	}
	grs, err := admingrants.New(admingrants.Config{DB: cfg.DB, Settings: cfg.Settings, Identity: cfg.Identity, Grants: cfg.Grants,
		Auth: cfg.Auth, Events: cfg.Events, Engine: cfg.Engine, RuleFailures: cfg.RuleFailures, Clock: cfg.Clock, Logger: cfg.Telemetry.Logger()})
	if err != nil {
		return nil, err
	}
	a := &Admin{cfg: cfg, now: cfg.Clock, log: cfg.Telemetry.Logger(), auth: au, cons: cons, grs: grs}
	srv, err := api.NewServer(a, a,
		api.WithErrorHandler(a.errorHandler),
		api.WithMiddleware(ignoredMiddleware),
		api.WithNotFound(a.notFound),
		api.WithMethodNotAllowed(a.methodNotAllowed),
	)
	if err != nil {
		return nil, err
	}
	a.srv = srv
	return a, nil
}

// RunRuleSweeper switches off policy rules that failed too many times, every
// policy.rule.check_interval, until ctx is done (req_001 D-73). It does nothing useful without
// Config.RuleFailures. Run it as a background worker.
func (a *Admin) RunRuleSweeper(ctx context.Context) { a.grs.RunRuleSweeper(ctx) }

// RunConsumerPurge purges the consumers deleted longer ago than consumers.deleted_retention, now
// and then every retention.interval, until ctx is done (req_001 D-106, D-134, D-136). Run it as a
// background worker.
func (a *Admin) RunConsumerPurge(ctx context.Context) { a.cons.RunPurge(ctx) }

// Authenticate checks a login name and password with the login lockout (req_001 D-54). See
// Authenticator.Authenticate.
func (a *Admin) Authenticate(ctx context.Context, username, password, remoteAddr string) (Authenticated, error) {
	return a.auth.Authenticate(ctx, username, password, remoteAddr)
}

// Handler returns the admin listener's handler: a request id on every response, the credential
// guard, then GET /metrics, the /admin/v1 API, and a 404 envelope for everything else. A panic
// in a handler is answered 500 and logged by type only.
func (a *Admin) Handler() http.Handler {
	guard := credential.Guard(credential.GuardConfig{
		Names:          a.hostNames,
		SessionCookies: []string{credential.AdminSessionCookie},
		Reject:         a.reject,
	})
	return telemetry.Middleware(a.recoverPanics(guard(http.HandlerFunc(a.serve))))
}

// hostNames returns the extra Host name the guard accepts: admin.tailnet_name, read per request.
func (a *Admin) hostNames(ctx context.Context) ([]string, error) {
	n, err := a.cfg.Settings.String(ctx, KeyTailnetName, "")
	if err != nil {
		a.log.ErrorContext(ctx, "admin: read setting", "key", KeyTailnetName, "err", err)
		return nil, err
	}
	if n == "" {
		return nil, nil
	}
	return []string{n}, nil
}

// reject answers a request the guard refused. The reason is a fixed code; the Host or Origin that
// caused it is neither answered nor logged. A failed read of the accepted host names is a server
// fault, so it is answered 500 and logged at Error level; the request is still refused.
func (a *Admin) reject(w http.ResponseWriter, r *http.Request, why credential.Reason) {
	if why == credential.ReasonHostUnavailable {
		a.log.ErrorContext(r.Context(), "admin: guard could not read the accepted host names", "reason", string(why))
		writeError(w, internalError())
		return
	}
	a.log.InfoContext(r.Context(), "admin: request refused by the guard", "reason", string(why))
	msg := "This listener answers only to localhost, a loopback address or the host's configured name."
	switch why {
	case credential.ReasonForeignOrigin, credential.ReasonOriginRequired:
		msg = "A state-changing request that carries a session must come from this page's own origin."
	}
	writeError(w, forbidden(msg))
}

func (a *Admin) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				// A panic value can hold request bytes; log its type only.
				a.log.ErrorContext(r.Context(), "admin: handler panic", "panic_type", typeName(p))
				writeError(w, internalError())
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func typeName(v any) string { return fmt.Sprintf("%T", v) }

// onAdminPath reports whether a request path belongs to the admin API. Both the decoded and the
// escaped form must be under /admin/v1/, so no encoding reaches a consumer route (PLT-B-005).
func onAdminPath(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/admin/v1/") {
		return false
	}
	return r.URL.RawPath == "" || strings.HasPrefix(r.URL.RawPath, "/admin/v1/")
}

func (a *Admin) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/metrics" && r.URL.RawPath == "":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, newErr(http.StatusMethodNotAllowed, typeInvalidRequest, "", "", "This endpoint does not accept this method."))
			return
		}
		a.cfg.Metrics.ServeHTTP(w, r)
	case onAdminPath(r):
		max, err := a.intSetting(r.Context(), KeyMaxBodyBytes)
		if err != nil {
			a.log.ErrorContext(r.Context(), "admin: read settings", "err", err)
			writeError(w, internalError())
			return
		}
		if r.ContentLength > max {
			writeError(w, tooLarge())
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, max)
		r = r.WithContext(context.WithValue(r.Context(), remoteKey{}, r.RemoteAddr))
		a.srv.ServeHTTP(w, r)
	default:
		writeError(w, notFound())
	}
}

// HandleAccessKeyAuth never authenticates: access keys belong to the consumer API.
func (a *Admin) HandleAccessKeyAuth(context.Context, api.OperationName, api.AccessKeyAuth) (context.Context, error) {
	return nil, ogenerrors.ErrSkipServerSecurity
}

// HandleConsumerSessionAuth never authenticates: the consumer session belongs to the consumer
// web page (PLT-F-027).
func (a *Admin) HandleConsumerSessionAuth(context.Context, api.OperationName, api.ConsumerSessionAuth) (context.Context, error) {
	return nil, ogenerrors.ErrSkipServerSecurity
}
