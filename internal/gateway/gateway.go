package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	ht "github.com/ogen-go/ogen/http"
	"github.com/ogen-go/ogen/ogenerrors"
	"golang.org/x/mod/sumdb/tlog"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/gateway/stream"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/ledger/checkpoint"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/policy"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// Authenticator is the part of auth.Service the gateway uses.
type Authenticator interface {
	Authenticate(ctx context.Context, header, remoteAddr string) (*auth.Identity, error)
}

// Limiter is the part of limits.Limiter the gateway uses, in the order it calls it.
type Limiter interface {
	CheckLockout(ctx context.Context, remoteAddr, header string) error
	AuthResult(ctx context.Context, remoteAddr, header string, authErr error)
	LimitBody(ctx context.Context, r *http.Request) error
	Check(ctx context.Context, id *auth.Identity) error
	CountAdmitted(ctx context.Context, id *auth.Identity) error
	Record(ctx context.Context, id *auth.Identity) error
	StatusOf(ctx context.Context, id *auth.Identity) (limits.Status, error)
}

// Admitter is the part of scheduler.Scheduler the gateway uses.
type Admitter interface {
	Admit(ctx context.Context, req scheduler.Request) (*scheduler.Ticket, error)
	Stats() scheduler.Stats
}

// Sharing is the part of host.SharingState the gateway uses.
type Sharing interface {
	Effective(ctx context.Context) (host.State, error)
}

// Engine is the part of the engine port the gateway uses.
type Engine interface {
	Chat(ctx context.Context, req engine.ChatRequest) (engine.ChatStream, error)
	Embed(ctx context.Context, req engine.EmbedRequest) (engine.EmbedResult, error)
	Loaded(ctx context.Context) ([]engine.LoadedModel, error)
}

// Meter is the part of ledger.Ledger the gateway uses.
type Meter interface {
	Record(ctx context.Context, u ledger.Usage) (ledger.Recorded, error)
	SignUnmetered(u ledger.Usage) (ledger.Signed, error)
}

// Checkpoints is the part of checkpoint.Service the gateway serves.
type Checkpoints interface {
	KeySet(ctx context.Context) (checkpoint.KeySet, error)
	Latest(ctx context.Context) (checkpoint.Checkpoint, error)
	Consistency(ctx context.Context, from int64) (checkpoint.Checkpoint, tlog.TreeProof, error)
	ProofForUsage(ctx context.Context, usageID string) (checkpoint.UsageProof, error)
}

// Config is what New needs. All fields except Telemetry and Clock are required.
type Config struct {
	DB       *storage.DB
	Settings *settings.Store
	Events   events.Notifier
	// Grants resolves the grant's model ids to engine model names.
	Grants    *grants.Repos
	Auth      Authenticator
	Limits    Limiter
	Policy    policy.Policy
	Scheduler Admitter
	// Sharing reports paused and quiet hours for /v1/health.
	Sharing     Sharing
	Engine      Engine
	Ledger      Meter
	Checkpoints Checkpoints
	// Telemetry nil means telemetry.Nop().
	Telemetry telemetry.Telemetry
	// Clock nil means time.Now.
	Clock func() time.Time
}

// Gateway serves the consumer API. Build it with New and mount Handler on the consumer
// listener (PLT-F-020). It is safe for concurrent use.
type Gateway struct {
	api.UnimplementedHandler

	cfg Config
	now func() time.Time
	log *slog.Logger
	srv http.Handler

	meteringFailures telemetry.Counter // by operation
	ruleFailuresCnt  telemetry.Counter // by code
	sloBreaches      telemetry.Counter // by slo
	overheadSeconds  telemetry.Histogram
	admissionSeconds telemetry.Histogram
	signingSeconds   telemetry.Histogram

	mu          sync.Mutex
	ruleFails   map[string]int64     // failed evaluations per rule id
	ruleEventAt map[string]time.Time // last policy.rule_invalid event per rule id
}

var (
	_ api.Handler         = (*Gateway)(nil)
	_ api.SecurityHandler = (*Gateway)(nil)
)

// New registers the gateway settings and builds the gateway.
func New(cfg Config) (*Gateway, error) {
	if cfg.DB == nil || cfg.Settings == nil || cfg.Events == nil || cfg.Grants == nil || cfg.Auth == nil || cfg.Limits == nil ||
		cfg.Policy == nil || cfg.Scheduler == nil || cfg.Sharing == nil || cfg.Engine == nil || cfg.Ledger == nil || cfg.Checkpoints == nil {
		return nil, errors.New("gateway: DB, Settings, Events, Grants, Auth, Limits, Policy, Scheduler, Sharing, Engine, Ledger and Checkpoints are required")
	}
	if cfg.Telemetry == nil {
		cfg.Telemetry = telemetry.Nop()
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if err := cfg.Settings.Register(Defs()...); err != nil {
		return nil, err
	}
	if err := setValidators(cfg.Settings); err != nil {
		return nil, err
	}
	t := cfg.Telemetry
	g := &Gateway{cfg: cfg, now: cfg.Clock, log: t.Logger(),
		meteringFailures: t.Counter("gateway_metering_failures_total", "operation"),
		ruleFailuresCnt:  t.Counter("gateway_policy_rule_failures_total", "code"),
		sloBreaches:      t.Counter("gateway_slo_breaches_total", "slo"),
		overheadSeconds:  t.Histogram("gateway_overhead_seconds", "operation"),
		admissionSeconds: t.Histogram("gateway_admission_seconds"),
		signingSeconds:   t.Histogram("gateway_receipt_signing_seconds"),
		ruleFails:        map[string]int64{}, ruleEventAt: map[string]time.Time{},
	}
	srv, err := api.NewServer(g, g,
		api.WithErrorHandler(g.errorHandler),
		api.WithNotFound(g.notFound),
		api.WithMethodNotAllowed(g.methodNotAllowed),
	)
	if err != nil {
		return nil, err
	}
	g.srv = srv
	return g, nil
}

// Handler returns the consumer API handler: /v1 only, every error in the OpenAI envelope, a
// request id on every response. The listener binds to localhost until the tailnet listeners
// exist (PLT-F-038); binding is the caller's job.
func (g *Gateway) Handler() http.Handler {
	return telemetry.Middleware(http.HandlerFunc(g.serve))
}

type stateKey struct{}
type identityKey struct{}

// reqState is the per-request state the ogen security handler needs: the request itself (it
// cannot reach it otherwise) and when the gateway received it, on the real clock; used only to
// measure overhead.
type reqState struct {
	r       *http.Request
	started time.Time
}

func stateOf(ctx context.Context) *reqState {
	st, _ := ctx.Value(stateKey{}).(*reqState)
	return st
}

func identityOf(ctx context.Context) *auth.Identity {
	id, _ := ctx.Value(identityKey{}).(*auth.Identity)
	return id
}

// onConsumerPath reports whether a request path belongs to the consumer API. Both the decoded
// and the escaped form must be under /v1/, so no encoding reaches an /admin/v1 route (PLT-B-005).
func onConsumerPath(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/v1/") {
		return false
	}
	return r.URL.RawPath == "" || strings.HasPrefix(r.URL.RawPath, "/v1/")
}

func (g *Gateway) serve(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if p := recover(); p != nil {
			// A panic value can hold request bytes; log its type only.
			g.log.ErrorContext(r.Context(), "gateway: handler panic", "panic_type", typeName(p))
			writeError(w, internalError())
		}
	}()
	if !onConsumerPath(r) {
		writeError(w, newErr(http.StatusNotFound, typeInvalidRequest, "", "", "No such endpoint."))
		return
	}
	st := &reqState{started: time.Now()}
	r = r.WithContext(context.WithValue(r.Context(), stateKey{}, st))
	st.r = r
	if err := g.cfg.Limits.LimitBody(r.Context(), r); err != nil {
		e, internal := classify(err, g.now())
		if internal {
			g.log.ErrorContext(r.Context(), "gateway: request failed", "err", err)
		}
		writeError(w, e)
		return
	}
	g.srv.ServeHTTP(stream.NewFlushWriter(w), r)
}

func (g *Gateway) notFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, newErr(http.StatusNotFound, typeInvalidRequest, "", "", "No such endpoint."))
}

func (g *Gateway) methodNotAllowed(w http.ResponseWriter, _ *http.Request, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, newErr(http.StatusMethodNotAllowed, typeInvalidRequest, "", "", "This endpoint does not accept this method."))
}

// errorHandler answers what ogen sends it: request decoding errors, the not-implemented error,
// and a response it could not write. A refusal is logged with its status and code, never the
// error text: ogen's messages can quote the request.
func (g *Gateway) errorHandler(ctx context.Context, w http.ResponseWriter, _ *http.Request, err error) {
	var de *ogenerrors.DecodeRequestError
	var dp *ogenerrors.DecodeParamsError
	var e *apiError
	switch {
	case errors.Is(err, ht.ErrNotImplemented):
		e = newErr(http.StatusNotImplemented, typeServer, "", "", "This endpoint is not available on this host yet.")
	case errors.As(err, &de), errors.As(err, &dp), errors.Is(err, limits.ErrTooLarge):
		e = decodeError(err)
		g.log.InfoContext(ctx, "gateway: request refused at decoding", "status", e.status, "code", e.code)
	default:
		// A response ogen could not encode or write. If the status line already went out,
		// net/http ignores the second write.
		g.log.WarnContext(ctx, "gateway: response could not be written")
		e = internalError()
	}
	writeError(w, e)
}

// NewError implements api.Handler: it maps every error a handler or the security handler
// returns to the envelope. Expected refusals are not logged; internal failures are, with
// the error text of the modules (which never hold content, req_001 D-31). An unreachable,
// too-old or timed-out model engine is logged at Warn with the same content-free text.
func (g *Gateway) NewError(ctx context.Context, err error) *api.ErrorStatusCodeWithHeaders {
	e, internal := classify(err, g.now())
	var ee *engine.Error
	var te *stream.TimeoutError
	switch {
	case internal:
		g.log.ErrorContext(ctx, "gateway: request failed", "err", err)
	case errors.As(err, &ee) && (e.status == http.StatusServiceUnavailable || e.status == http.StatusGatewayTimeout):
		g.log.WarnContext(ctx, "gateway: model engine call failed", "status", e.status, "code", string(ee.Code), "err", err)
	case errors.As(err, &te):
		g.log.WarnContext(ctx, "gateway: streamed engine call timed out", "kind", te.Kind)
	}
	return e.response()
}

// HandleAccessKeyAuth is auth and limits: F1 step 2 (req_001 D-39), with the limits check. It
// runs before ogen reads the body.
func (g *Gateway) HandleAccessKeyAuth(ctx context.Context, op api.OperationName, t api.AccessKeyAuth) (context.Context, error) {
	st := stateOf(ctx)
	if st == nil {
		return nil, errors.New("gateway: request state missing")
	}
	header := "Bearer " + t.Token
	remote := st.r.RemoteAddr
	if err := g.cfg.Limits.CheckLockout(ctx, remote, header); err != nil {
		return nil, err
	}
	id, err := g.cfg.Auth.Authenticate(ctx, header, remote)
	g.cfg.Limits.AuthResult(ctx, remote, header, err)
	if err != nil {
		return nil, err
	}
	// A used-up quota stops inference. It must not stop a consumer from fetching the keys,
	// checkpoint and proofs it needs to verify what it was billed.
	if err := g.cfg.Limits.Check(ctx, id); err != nil && (isInference(op) || !errors.Is(err, limits.ErrQuotaExceeded)) {
		return nil, err
	}
	return context.WithValue(ctx, identityKey{}, id), nil
}

// HandleAdminSessionAuth never authenticates: admin operations are not served here.
func (g *Gateway) HandleAdminSessionAuth(context.Context, api.OperationName, api.AdminSessionAuth) (context.Context, error) {
	return nil, ogenerrors.ErrSkipServerSecurity
}

// HandleConsumerSessionAuth never authenticates yet: consumer sessions come with PLT-F-027.
func (g *Gateway) HandleConsumerSessionAuth(context.Context, api.OperationName, api.ConsumerSessionAuth) (context.Context, error) {
	return nil, ogenerrors.ErrSkipServerSecurity
}

func isInference(op api.OperationName) bool {
	return op == api.CreateChatCompletionOperation || op == api.CreateEmbeddingOperation
}
