package consumerauth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/requests"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// Config is what New needs. Everything except UI, Telemetry and Clock is required.
type Config struct {
	DB       *storage.DB
	Settings *settings.Store
	// Identity is built with the same clock as Clock, so a session's created_at and expires_at
	// agree.
	Identity *identity.Repos
	// Grants is read to tell a consumer that has no grant yet from one that has.
	Grants *grants.Repos
	// Keys lists a consumer's access keys in the API's shape, by prefix and never a secret. The
	// admin API's list already has that mapping, so wire passes it here (*admin.Admin
	// AdminListAccessKeys) instead of copying it.
	Keys func(ctx context.Context, consumerID uuid.UUID) (*api.AccessKeyList, error)
	// Limits returns a consumer's current grant version in the API's shape, with model names
	// (*admin.Admin AdminGetGrant). It is called only for a consumer that has a grant.
	Limits func(ctx context.Context, consumerID uuid.UUID) (*api.GrantVersion, error)
	// Requests serves the resource request routes (PLT-F-028). Optional: nil leaves them
	// answering 501.
	Requests *requests.Service
	// LimitBody caps the request body on the page listener, with the same setting as the
	// consumer API (limits.Limiter.LimitBody).
	LimitBody func(ctx context.Context, r *http.Request) error
	// HostNames returns the Host names the page listener accepts besides localhost and loopback
	// addresses: the host's own tailnet names. It is read for each request. An error refuses the
	// request (fail closed).
	HostNames func(ctx context.Context) ([]string, error)
	// UI serves the consumer page for GET and HEAD on every path outside /v1 (PLT-F-036). Nil
	// answers those paths 404, as before the page exists.
	UI http.Handler
	// Telemetry nil means telemetry.Nop().
	Telemetry telemetry.Telemetry
	// Clock nil means time.Now.
	Clock func() time.Time
}

// Service is the consumer accounts logic. Build it with New. It is safe for concurrent use.
type Service struct {
	cfg       Config
	now       func() time.Time
	log       *slog.Logger
	password  *PasswordAdapter
	passkey   *PasskeyAdapter
	adapters  map[string]Adapter
	srv       http.Handler // the page listener's ogen server
	pageGuard func(http.Handler) http.Handler
}

// New registers the settings of this module, builds the password and passkey adapters and the
// page listener's server.
func New(cfg Config) (*Service, error) {
	if cfg.DB == nil || cfg.Settings == nil || cfg.Identity == nil || cfg.Grants == nil || cfg.Keys == nil ||
		cfg.Limits == nil || cfg.LimitBody == nil || cfg.HostNames == nil {
		return nil, errors.New("consumerauth: DB, Settings, Identity, Grants, Keys, Limits, LimitBody and HostNames are required")
	}
	if cfg.Telemetry == nil {
		cfg.Telemetry = telemetry.Nop()
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if err := registerSettings(cfg.Settings); err != nil {
		return nil, err
	}
	pw, err := NewPasswordAdapter(cfg.DB, cfg.Settings, cfg.Identity, cfg.Clock)
	if err != nil {
		return nil, err
	}
	// One lockout tracker for both ways to log in: failed passkey assertions count against the
	// same address as failed passwords.
	pk, err := NewPasskeyAdapter(cfg.DB, cfg.Settings, cfg.Identity, pw.lock, cfg.Clock, cfg.Telemetry.Logger())
	if err != nil {
		return nil, err
	}
	s := &Service{cfg: cfg, now: cfg.Clock, log: cfg.Telemetry.Logger(), password: pw, passkey: pk,
		adapters: map[string]Adapter{pw.Kind(): pw, pk.Kind(): pk}}
	if err := s.buildPage(); err != nil {
		return nil, err
	}
	return s, nil
}

// Caller is who a call is for, as a consumer. On the consumer page it is the logged-in consumer
// of the session; on the consumer API it is the consumer behind the access key. Everything a
// consumer route does is scoped to ConsumerID and never to an id taken from the request.
type Caller struct {
	ConsumerID  string
	PrincipalID string
	// SessionID is the session of a call made with the page's cookie; empty for an access key.
	SessionID string
	// KeyID is the access key of a call made with a key; empty for a session.
	KeyID string
}

type callerKey struct{}

// WithCaller returns ctx carrying c. The security handler of the page listener and the gateway's
// key authentication both set it.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// CallerOf returns the consumer a call is for. ok is false for a call that passed no consumer
// authentication.
func CallerOf(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok && c.ConsumerID != ""
}

type remoteKey struct{}

func remoteOf(ctx context.Context) string {
	s, _ := ctx.Value(remoteKey{}).(string)
	return s
}

type userAgentKey struct{}

func userAgentOf(ctx context.Context) string {
	s, _ := ctx.Value(userAgentKey{}).(string)
	return s
}

func isNotFound(err error) bool { return errors.Is(err, identity.ErrNotFound) }
