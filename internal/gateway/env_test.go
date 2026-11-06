package gateway_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/engine/fake"
	"github.com/inCoderKaka/local-server/internal/gateway"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/jobs"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/ledger/checkpoint"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/policy"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
	"github.com/inCoderKaka/local-server/internal/usage"
)

const (
	chatModel  = "fake-chat"
	embedModel = "fake-embed"
)

func i64(v int64) *int64 { return &v }

// testTelemetry keeps counters and logs in memory.
type testTelemetry struct {
	mu       sync.Mutex
	counters map[string]float64
	logs     bytes.Buffer
	log      *slog.Logger
}

func newTestTelemetry() *testTelemetry {
	t := &testTelemetry{counters: map[string]float64{}}
	t.log = slog.New(slog.NewTextHandler(&lockedBuf{t: t}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return t
}

type lockedBuf struct{ t *testTelemetry }

func (b *lockedBuf) Write(p []byte) (int, error) {
	b.t.mu.Lock()
	defer b.t.mu.Unlock()
	return b.t.logs.Write(p)
}

func (t *testTelemetry) Logger() *slog.Logger { return t.log }
func (t *testTelemetry) Counter(name string, _ ...string) telemetry.Counter {
	return counter{t: t, name: name}
}
func (t *testTelemetry) Gauge(string, ...string) telemetry.Gauge { return telemetry.Nop().Gauge("") }
func (t *testTelemetry) Histogram(string, ...string) telemetry.Histogram {
	return telemetry.Nop().Histogram("")
}
func (t *testTelemetry) StartSpan(ctx context.Context, _ string) (context.Context, telemetry.Span) {
	return telemetry.Nop().StartSpan(ctx, "")
}

func (t *testTelemetry) count(name string, labels ...string) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.counters[name+"|"+strings.Join(labels, ",")]
}

func (t *testTelemetry) logText() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.logs.String()
}

type counter struct {
	t    *testTelemetry
	name string
}

func (c counter) Inc(l ...string) { c.Add(1, l...) }
func (c counter) Add(d float64, l ...string) {
	c.t.mu.Lock()
	c.t.counters[c.name+"|"+strings.Join(l, ",")] += d
	c.t.mu.Unlock()
}

// recEngine records the requests the engine saw.
type recEngine struct {
	*fake.Engine
	mu    sync.Mutex
	chats []engine.ChatRequest
	embed []engine.EmbedRequest
	// nanos is the time spent inside Chat and Embed, for the SLO measurements.
	nanos atomic.Int64
	// wrapChat, when set, wraps each chat stream; embedHook, when set, runs before each Embed.
	wrapChat func(engine.ChatStream) engine.ChatStream
	// lastCtx is the context of the latest Chat call, to see whether the gateway cancelled it.
	lastCtx   context.Context
	embedHook func()
}

func (e *recEngine) Chat(ctx context.Context, r engine.ChatRequest) (engine.ChatStream, error) {
	t0 := time.Now()
	defer func() { e.nanos.Add(int64(time.Since(t0))) }()
	e.mu.Lock()
	e.chats = append(e.chats, r)
	e.lastCtx = ctx
	e.mu.Unlock()
	s, err := e.Engine.Chat(ctx, r)
	if err == nil && e.wrapChat != nil {
		s = e.wrapChat(s)
	}
	return s, err
}

func (e *recEngine) chatCtx() context.Context {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastCtx
}

func (e *recEngine) Embed(ctx context.Context, r engine.EmbedRequest) (engine.EmbedResult, error) {
	t0 := time.Now()
	defer func() { e.nanos.Add(int64(time.Since(t0))) }()
	e.mu.Lock()
	e.embed = append(e.embed, r)
	e.mu.Unlock()
	if e.embedHook != nil {
		e.embedHook()
	}
	return e.Engine.Embed(ctx, r)
}

// timedSched adds up the time spent inside Admit.
type timedSched struct {
	s     *scheduler.Scheduler
	nanos atomic.Int64
	mu    sync.Mutex
	last  scheduler.Request
	// refuse, when set, decides before the scheduler does: a non-nil result is Admit's error.
	refuse func(scheduler.Request) error
}

func (t *timedSched) Admit(ctx context.Context, r scheduler.Request) (*scheduler.Ticket, error) {
	t0 := time.Now()
	defer func() { t.nanos.Add(int64(time.Since(t0))) }()
	t.mu.Lock()
	t.last = r
	refuse := t.refuse
	t.mu.Unlock()
	if refuse != nil {
		if err := refuse(r); err != nil {
			return nil, err
		}
	}
	return t.s.Admit(ctx, r)
}

func (t *timedSched) Stats() scheduler.Stats { return t.s.Stats() }

// lastRequest is the request of the latest Admit.
func (t *timedSched) lastRequest() scheduler.Request {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.last
}

// flakyMeter fails Record the first failures times (all when failures < 0).
type flakyMeter struct {
	*ledger.Ledger
	mu       sync.Mutex
	failures int
	calls    int
	// signFails makes SignUnmetered fail.
	signFails bool
}

func (m *flakyMeter) SignUnmetered(u ledger.Usage) (ledger.Signed, error) {
	m.mu.Lock()
	fail := m.signFails
	m.mu.Unlock()
	if fail {
		return ledger.Signed{}, io.ErrClosedPipe
	}
	return m.Ledger.SignUnmetered(u)
}

func (m *flakyMeter) Record(ctx context.Context, u ledger.Usage) (ledger.Recorded, error) {
	m.mu.Lock()
	m.calls++
	fail := m.failures != 0
	if m.failures > 0 {
		m.failures--
	}
	m.mu.Unlock()
	if fail {
		return ledger.Recorded{}, io.ErrUnexpectedEOF
	}
	return m.Ledger.Record(ctx, u)
}

func (m *flakyMeter) set(n int) { m.mu.Lock(); m.failures = n; m.mu.Unlock() }
func (m *flakyMeter) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

type env struct {
	t     testing.TB
	db    *storage.DB
	st    *settings.Store
	bus   *events.Bus
	keys  *hostkeys.Keys
	led   *ledger.Ledger
	meter *flakyMeter
	cps   *checkpoint.Service
	eng   *recEngine
	sched *scheduler.Scheduler
	timed *timedSched
	share *host.SharingState
	lim   *limits.Limiter
	au    *auth.Service
	gr    *grants.Repos
	ir    *identity.Repos
	jobs  *jobs.Service
	gw    *gateway.Gateway
	h     http.Handler
	tel   *testTelemetry

	cons  identity.Consumer
	grant grants.Grant
	key   string // "Bearer ls_..."
	keyID string

	evMu sync.Mutex
	evs  []events.Event
}

type envOpts struct {
	version func(models []grants.VersionModel) grants.NewVersion
	policy  policy.Policy // replaces the real policy service
	engine  []fake.Option // added to the fake engine's options
	noJobs  bool          // build the gateway without a jobs service
}

func newEnv(t testing.TB, mod ...func(*envOpts)) *env {
	t.Helper()
	var o envOpts
	for _, m := range mod {
		m(&o)
	}
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"),
		settings.Module(), host.Module(), identity.Module(), grants.Module(), audit.Module(), events.Module(), usage.Module(), ledger.Module(), scheduler.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{t: t, db: db, st: settings.New(db), bus: events.New(), ir: identity.NewRepos(nil), gr: grants.NewRepos(nil), tel: newTestTelemetry()}
	master := make([]byte, hostkeys.KeySize)
	if _, err := rand.Read(master); err != nil {
		t.Fatal(err)
	}
	if e.keys, _, err = hostkeys.Open(ctx, db, e.st, master); err != nil {
		t.Fatal(err)
	}
	e.led = ledger.New(db, e.keys)
	e.meter = &flakyMeter{Ledger: e.led}
	if e.cps, err = checkpoint.New(db, e.keys, e.st); err != nil {
		t.Fatal(err)
	}
	e.eng = &recEngine{Engine: fake.New(append([]fake.Option{fake.WithModels(
		fake.ModelSpec{Name: chatModel, ContextLength: 8192},
		fake.ModelSpec{Name: embedModel, ContextLength: 8192, Embedding: true, Dim: 4})}, o.engine...)...)}
	if e.share, err = host.NewSharing(db, e.st, e.bus, nil); err != nil {
		t.Fatal(err)
	}
	if e.sched, err = scheduler.New(e.eng, e.st, e.share, scheduler.Options{}); err != nil {
		t.Fatal(err)
	}
	e.timed = &timedSched{s: e.sched}
	if e.jobs, err = jobs.New(jobs.Config{DB: db, Settings: e.st, Events: e.bus, Keys: e.keys, Telemetry: e.tel}); err != nil {
		t.Fatal(err)
	}
	if e.au, err = auth.New(auth.Config{DB: db, Identity: e.ir, Grants: e.gr, Events: e.bus, Settings: e.st}); err != nil {
		t.Fatal(err)
	}
	if e.lim, err = limits.New(limits.Config{DB: db, Identity: e.ir, Grants: e.gr, Events: e.bus, Settings: e.st, Logger: e.tel.Logger()}); err != nil {
		t.Fatal(err)
	}
	pol := o.policy
	if pol == nil {
		if pol, err = policy.New(e.st, gateway.NewRuleSource(db, e.gr)); err != nil {
			t.Fatal(err)
		}
	}
	for _, ty := range []events.Type{events.MeteringFailed, events.PolicyRuleInvalid, events.QuotaNearLimit, events.JobDone} {
		if err := e.bus.Subscribe("test."+string(ty), func(_ context.Context, ev events.Event) error {
			e.evMu.Lock()
			e.evs = append(e.evs, ev)
			e.evMu.Unlock()
			return nil
		}, ty); err != nil {
			t.Fatal(err)
		}
	}
	var jobsSvc gateway.Jobs
	if !o.noJobs {
		jobsSvc = e.jobs
	}
	e.gw, err = gateway.New(gateway.Config{DB: db, Settings: e.st, Events: e.bus, Grants: e.gr, Auth: e.au, Limits: e.lim, Policy: pol,
		Scheduler: e.timed, Sharing: e.share, Engine: e.eng, Ledger: e.meter, Checkpoints: e.cps, Jobs: jobsSvc, Telemetry: e.tel})
	if err != nil {
		t.Fatal(err)
	}
	e.h = e.gw.Handler()
	// Roomy defaults for the throughput tests; a test narrows what it tests.
	e.set(limits.KeyRateEnabled, "false")
	e.set(gateway.KeyMeteringRetryDelay, "1ms")

	if err := db.Write(ctx, func(tx *storage.Tx) error {
		p, err := e.ir.Principals.Create(tx, "consumer", nil, nil)
		if err != nil {
			return err
		}
		if e.cons, err = e.ir.Consumers.Create(tx, p.ID, identity.ConsumerApplication, nil, nil); err != nil {
			return err
		}
		if e.grant, err = e.gr.Grants.Create(tx, e.cons.ID); err != nil {
			return err
		}
		var models []grants.VersionModel
		for _, name := range []string{chatModel, embedModel} {
			m, err := e.gr.Models.Create(tx, name)
			if err != nil {
				return err
			}
			models = append(models, grants.VersionModel{ModelID: m.ID})
		}
		nv := grants.NewVersion{Models: models}
		if o.version != nil {
			nv = o.version(models)
		}
		_, err = e.gr.Versions.Create(tx, e.grant.ID, nv)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e.key, e.keyID = e.issue(auth.Narrowing{})
	return e
}

func (e *env) set(key, value string) {
	e.t.Helper()
	if err := e.st.Set(context.Background(), settings.Host, key, "", value); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) issue(n auth.Narrowing) (header, keyID string) {
	e.t.Helper()
	var is auth.Issued
	err := e.bus.Write(context.Background(), e.db, func(tx *storage.Tx) (err error) {
		is, err = e.au.Issue(tx, auth.IssueParams{ConsumerID: e.cons.ID, Narrowing: n})
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return "Bearer " + is.Key.Reveal(), is.AccessKey.ID
}

func (e *env) events(ty events.Type) []events.Event {
	e.evMu.Lock()
	defer e.evMu.Unlock()
	var out []events.Event
	for _, ev := range e.evs {
		if ev.Type == ty {
			out = append(out, ev)
		}
	}
	return out
}

// resp is a recorded response.
type resp struct {
	*httptest.ResponseRecorder
}

func (r resp) json() map[string]any {
	var m map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
		panic("response is not JSON: " + r.Body.String())
	}
	return m
}

// errBody returns the error object of an envelope response.
func (r resp) errBody() map[string]any {
	e, _ := r.json()["error"].(map[string]any)
	return e
}

func (e *env) do(method, path, body string, hdr ...string) resp {
	return e.doAuth(e.key, method, path, body, hdr...)
}

func (e *env) doAuth(auth, method, path, body string, hdr ...string) resp {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.RemoteAddr = "100.64.0.1:5555"
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Add(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return resp{rec}
}

// doCtx is do with POST and a request context the test can cancel.
func (e *env) doCtx(ctx context.Context, path, body string) resp {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
	req.RemoteAddr = "100.64.0.1:5555"
	req.Header.Set("Authorization", e.key)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return resp{rec}
}

func (e *env) chat(body string, hdr ...string) resp {
	return e.do(http.MethodPost, "/v1/chat/completions", body, hdr...)
}

func (e *env) embed(body string, hdr ...string) resp {
	return e.do(http.MethodPost, "/v1/embeddings", body, hdr...)
}

const okChat = `{"model":"` + chatModel + `","messages":[{"role":"user","content":"hello there"}]}`

func (e *env) verify(jws string) ledger.Receipt {
	e.t.Helper()
	ks, err := e.cps.KeySet(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	r, err := ks.VerifyReceipt(jws)
	if err != nil {
		e.t.Fatalf("receipt does not verify: %v", err)
	}
	return r
}

func (e *env) useRecords() int64 {
	e.t.Helper()
	n, err := usage.Count(context.Background(), e.db.Reader(), usage.ByConsumer, e.cons.ID, time.Unix(0, 0), time.Now().Add(time.Hour))
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

type (
	grantsModel      = grants.VersionModel
	grantsNewVersion = grants.NewVersion
	storageTx        = *storage.Tx
)

func engineDown() error { return engine.Errorf(engine.CodeEngineUnavailable, nil, "down") }

// dayQuotas is one quota entry over 24 hours, the default limits.quota_window.
func dayQuotas(metric string, limit int64) []grants.QuotaEntry {
	return []grants.QuotaEntry{{Metric: metric, Limit: limit, Window: 24 * time.Hour}}
}
