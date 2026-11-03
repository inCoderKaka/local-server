package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/admin"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/auth/credential"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/engine/fake"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/hostmetrics"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
	"github.com/inCoderKaka/local-server/internal/usage"
)

const (
	adminHost   = "127.0.0.1:8485"
	adminOrigin = "http://" + adminHost
	testPass    = "correct horse battery staple"
	testUser    = "example-admin"
	hostID      = "ZXhhbXBsZS1ob3N0LWlkZW50aXR5LWtleQ"
)

var t0 = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

// clock is a settable fake clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// logTel is a Telemetry that keeps its log text.
type logTel struct {
	telemetry.Telemetry
	buf *lockedBuffer
	log *slog.Logger
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuffer) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func newLogTel() *logTel {
	b := &lockedBuffer{}
	return &logTel{Telemetry: telemetry.Nop(), buf: b, log: slog.New(slog.NewTextHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug}))}
}

func (l *logTel) Logger() *slog.Logger { return l.log }

// fakeSampler serves a settable reading and stored minutes.
type fakeSampler struct {
	mu      sync.Mutex
	current *hostmetrics.Reading
	read    hostmetrics.Reading
	minutes []hostmetrics.Minute
	reads   int
	ranges  [][2]time.Time
}

func (s *fakeSampler) Current() (hostmetrics.Reading, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return hostmetrics.Reading{}, false
	}
	return *s.current, true
}

func (s *fakeSampler) Read(context.Context) (hostmetrics.Reading, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	return s.read, nil
}

func (s *fakeSampler) GetRange(_ context.Context, from, to time.Time) ([]hostmetrics.Minute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ranges = append(s.ranges, [2]time.Time{from, to})
	var out []hostmetrics.Minute
	for _, m := range s.minutes {
		if !m.Start.Before(from) && m.Start.Before(to) {
			out = append(out, m)
		}
	}
	return out, nil
}

type fakeKeys struct{}

func (fakeKeys) HostID() string { return hostID }

// versionedEngine is an engine that also reports a version, like the Ollama adapter.
type versionedEngine struct {
	*fake.Engine
	version string
}

func (v versionedEngine) Version(context.Context) (string, error) { return v.version, nil }

// downEngine fails every Loaded call.
type downEngine struct{}

func (downEngine) Loaded(context.Context) ([]engine.LoadedModel, error) {
	return nil, errors.New("connection refused")
}

type env struct {
	t       *testing.T
	db      *storage.DB
	st      *settings.Store
	ir      *identity.Repos
	clk     *clock
	tel     *logTel
	sampler *fakeSampler
	share   *host.SharingState
	admin   *admin.Admin
	h       http.Handler
	eng     admin.Engine
}

type envOpts struct {
	engine  admin.Engine
	metrics http.Handler
}

func newEnv(t *testing.T, mod ...func(*envOpts)) *env {
	t.Helper()
	o := &envOpts{engine: fake.New(), metrics: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "metrics_ok 1\n") })}
	for _, m := range mod {
		m(o)
	}
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"),
		settings.Module(), host.Module(), identity.Module(), audit.Module(), events.Module(), usage.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{t: t, db: db, st: settings.New(db), clk: &clock{t: t0}, tel: newLogTel(), sampler: &fakeSampler{}, eng: o.engine}
	e.ir = identity.NewRepos(e.clk.now)
	if err := db.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.Exec(`INSERT INTO hst_host (id, created_at, data_key_sealed) VALUES ('h1', 1, x'00')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e.share, err = host.NewSharing(db, e.st, events.New(), e.clk.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := usage.NewRollups(db, e.st, telemetry.Nop()); err != nil { // registers usage.page_size, as wire does
		t.Fatal(err)
	}
	log, err := audit.NewLog(e.st)
	if err != nil {
		t.Fatal(err)
	}
	e.admin, err = admin.New(admin.Config{DB: db, Settings: e.st, Identity: e.ir, Audit: log, Sharing: e.share, Sampler: e.sampler,
		Keys: fakeKeys{}, Engine: o.engine, Metrics: o.metrics, Version: "1.2.3-test", Telemetry: e.tel, Clock: e.clk.now})
	if err != nil {
		t.Fatal(err)
	}
	e.h = e.admin.Handler()
	return e
}

func (e *env) set(key, value string) {
	e.t.Helper()
	if err := e.st.Set(context.Background(), settings.Host, key, "", value); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) count(q string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.Reader().QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// resp is a recorded response.
type resp struct {
	*httptest.ResponseRecorder
}

func (r resp) json() map[string]any {
	var m map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
		panic("body is not a JSON object: " + r.Body.String())
	}
	return m
}

// errBody returns the envelope's error object and fails unless it has the four envelope keys.
func (r resp) errBody(t *testing.T) map[string]any {
	t.Helper()
	m := r.json()
	if len(m) != 1 {
		t.Fatalf("body %s is not the error envelope", r.Body.String())
	}
	inner, _ := m["error"].(map[string]any)
	for _, k := range []string{"message", "type", "param", "code"} {
		if _, ok := inner[k]; !ok {
			t.Fatalf("error envelope %s lacks %q", r.Body.String(), k)
		}
	}
	return inner
}

// req describes one request. The defaults are a local browser page of the admin listener.
type req struct {
	method, path, body string
	host               string // default adminHost
	remote             string // default 127.0.0.1:5555
	origin             string // "" sends none; "-" is also none; default none
	cookie             string // the session token, sent as the cookie
	header             map[string]string
	noContentType      bool
}

func (e *env) do(r req) resp {
	e.t.Helper()
	if r.method == "" {
		r.method = http.MethodGet
	}
	var body io.Reader
	if r.body != "" {
		body = strings.NewReader(r.body)
	}
	hr := httptest.NewRequest(r.method, "http://"+adminHost+r.path, body)
	hr.Host = adminHost
	if r.host != "" {
		hr.Host = r.host
	}
	hr.RemoteAddr = "127.0.0.1:5555"
	if r.remote != "" {
		hr.RemoteAddr = r.remote
	}
	if r.body != "" && !r.noContentType {
		hr.Header.Set("Content-Type", "application/json")
	}
	if r.origin != "" {
		hr.Header.Set("Origin", r.origin)
	}
	if r.cookie != "" {
		hr.AddCookie(&http.Cookie{Name: credential.AdminSessionCookie, Value: r.cookie})
	}
	for k, v := range r.header {
		hr.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, hr)
	return resp{rec}
}

// call is a request from the admin page: same-origin, with the session cookie.
func (e *env) call(token, method, path, body string) resp {
	e.t.Helper()
	return e.do(req{method: method, path: path, body: body, cookie: token, origin: adminOrigin})
}

func creds(user, pass string) string {
	b, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	return string(b)
}

// tokenOf returns the session token from a response's Set-Cookie.
func tokenOf(t *testing.T, r resp) string {
	t.Helper()
	for _, c := range r.Result().Cookies() {
		if c.Name == credential.AdminSessionCookie {
			return c.Value
		}
	}
	t.Fatalf("no %s cookie in %v", credential.AdminSessionCookie, r.Header())
	return ""
}

func status(t *testing.T, r resp, want int) {
	t.Helper()
	if r.Code != want {
		t.Fatalf("status = %d, want %d; body %s", r.Code, want, r.Body.String())
	}
}

// setup creates the first admin and returns its session token.
func (e *env) setup() string {
	e.t.Helper()
	r := e.do(req{method: "POST", path: "/admin/v1/setup", body: creds(testUser, testPass), origin: adminOrigin})
	status(e.t, r, http.StatusCreated)
	return tokenOf(e.t, r)
}

// login logs in and returns the session token.
func (e *env) login() string {
	e.t.Helper()
	r := e.do(req{method: "POST", path: "/admin/v1/session", body: creds(testUser, testPass), origin: adminOrigin})
	status(e.t, r, http.StatusOK)
	return tokenOf(e.t, r)
}

func q(params map[string]string) string {
	v := url.Values{}
	for k, s := range params {
		v.Set(k, s)
	}
	return "?" + v.Encode()
}

// auditRows returns the (action, actor, target, detail) of the audit rows of an action, oldest first.
func (e *env) auditRows(action string) []auditRow {
	e.t.Helper()
	rows, err := e.db.Reader().Query(context.Background(),
		`SELECT action, COALESCE(actor_id, ''), COALESCE(target_id, ''), detail FROM aud_event WHERE action = ? ORDER BY created_at, id`, action)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		var d string
		if err := rows.Scan(&r.action, &r.actor, &r.target, &d); err != nil {
			e.t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(d), &r.detail); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

type auditRow struct {
	action, actor, target string
	detail                map[string]any
}

func (e *env) adminPrincipal() string {
	e.t.Helper()
	var id string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT principal_id FROM idn_admin_user`).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}
