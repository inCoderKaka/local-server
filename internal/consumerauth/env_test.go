package consumerauth_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/auth/credential"
	"github.com/inCoderKaka/local-server/internal/consumerauth"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/requests"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
	"github.com/inCoderKaka/local-server/internal/usage"
)

const (
	pageHost   = "host.example-tailnet.ts.net"
	pageOrigin = "http://" + pageHost
	goodPass   = "correct horse battery staple"
	// maxBody is the body cap of the test page listener.
	maxBody = 4096
)

var t0 = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

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

type env struct {
	t    *testing.T
	db   *storage.DB
	st   *settings.Store
	ir   *identity.Repos
	gr   *grants.Repos
	reqs *requests.Service
	clk  *clock
	tel  *logTel
	svc  *consumerauth.Service
	h    http.Handler
	// admin is the principal that issues invites.
	admin identity.Principal
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"),
		settings.Module(), host.Module(), identity.Module(), grants.Module(), requests.Module(), audit.Module(), events.Module(), usage.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{t: t, db: db, st: settings.New(db), clk: &clock{t: t0}, tel: newLogTel()}
	e.ir, e.gr = identity.NewRepos(e.clk.now), grants.NewRepos(e.clk.now)
	// The usage settings (usage.page_size) that GET /v1/usage reads.
	if _, err := usage.NewRollups(db, e.st, e.tel); err != nil {
		t.Fatal(err)
	}
	// Two consumer-owned settings with one value per consumer.
	if err := e.st.Register(
		settings.Def{Key: "x.pref", Kind: settings.Duration, Default: "24h", Owner: settings.Consumer, Scoped: true, Description: "A consumer preference."},
		settings.Def{Key: "x.count", Kind: settings.Int, Default: "3", Owner: settings.Consumer, Scoped: true, Description: "A consumer number."},
	); err != nil {
		t.Fatal(err)
	}
	e.reqs, err = requests.New(requests.Config{DB: db, Settings: e.st, Identity: e.ir, Events: events.New(), Clock: e.clk.now, Logger: e.tel.Logger()})
	if err != nil {
		t.Fatal(err)
	}
	e.svc, err = consumerauth.New(consumerauth.Config{DB: db, Settings: e.st, Identity: e.ir, Grants: e.gr, Telemetry: e.tel, Clock: e.clk.now, Requests: e.reqs,
		Keys: func(ctx context.Context, id uuid.UUID) (*api.AccessKeyList, error) {
			keys, err := e.ir.AccessKeys.ListByConsumer(ctx, db.Reader(), id.String())
			if err != nil {
				return nil, err
			}
			out := &api.AccessKeyList{Data: []api.AccessKey{}}
			for _, k := range keys {
				kid, _ := uuid.Parse(k.ID)
				out.Data = append(out.Data, api.AccessKey{ID: kid, ConsumerID: id, Prefix: k.Prefix, Status: api.AccessKeyStatusActive, CreatedAt: k.CreatedAt})
			}
			return out, nil
		},
		Limits: func(_ context.Context, id uuid.UUID) (*api.GrantVersion, error) {
			return &api.GrantVersion{ConsumerID: id, Version: 1}, nil
		},
		LimitBody: func(_ context.Context, r *http.Request) error {
			if r.ContentLength > maxBody {
				return limits.ErrTooLarge
			}
			return nil
		},
		HostNames: func(context.Context) ([]string, error) { return []string{pageHost}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	e.h = e.svc.Handler()
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) {
		e.admin, err = e.ir.Principals.Create(tx, "host_admin", nil, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) set(key, value string) {
	e.t.Helper()
	if err := e.st.Set(context.Background(), settings.Host, key, "", value); err != nil {
		e.t.Fatal(err)
	}
}

// invite creates a pending invite and returns its token and id.
func (e *env) invite(ttl time.Duration) (token, id string) {
	e.t.Helper()
	prefix, secret, err := auth.NewInviteToken()
	if err != nil {
		e.t.Fatal(err)
	}
	err = e.db.Write(context.Background(), func(tx *storage.Tx) error {
		inv, err := e.ir.Invites.Create(tx, prefix, auth.HashSecret(secret), e.admin.ID, nil, e.clk.now().Add(ttl))
		id = inv.ID
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return auth.FormatInviteToken(prefix, secret), id
}

// req is one request to the page listener.
type req struct {
	method, path, body string
	host, origin       string // host "" means pageHost; origin "" means none, "-" means pageOrigin
	cookie             string // the session token
	remote             string // "" means 100.64.0.7:4000
	tls                bool   // the request arrived over TLS
	header             map[string]string
}

type resp struct{ *httptest.ResponseRecorder }

func (e *env) do(r req) resp {
	var rd io.Reader
	if r.body != "" {
		rd = strings.NewReader(r.body)
	}
	hr := httptest.NewRequest(r.method, r.path, rd)
	hr.Host = pageHost
	if r.host != "" {
		hr.Host = r.host
	}
	hr.RemoteAddr = "100.64.0.7:4000"
	if r.tls {
		hr.TLS = &tls.ConnectionState{}
	}
	if r.remote != "" {
		hr.RemoteAddr = r.remote
	}
	if r.body != "" {
		hr.Header.Set("Content-Type", "application/json")
	}
	switch r.origin {
	case "":
	case "-":
		hr.Header.Set("Origin", pageOrigin)
	default:
		hr.Header.Set("Origin", r.origin)
	}
	if r.cookie != "" {
		hr.AddCookie(&http.Cookie{Name: credential.ConsumerSessionCookie, Value: r.cookie})
	}
	for k, v := range r.header {
		hr.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, hr)
	return resp{rec}
}

// get is a GET with the session token, from the page's own origin.
func (e *env) get(token, path string) resp {
	return e.do(req{method: "GET", path: path, cookie: token})
}

// send is a state-changing request with the session token, from the page's own origin.
func (e *env) send(token, method, path, body string) resp {
	return e.do(req{method: method, path: path, body: body, cookie: token, origin: "-"})
}

func status(t *testing.T, r resp, want int) {
	t.Helper()
	if r.Code != want {
		t.Fatalf("status = %d, want %d; body %s", r.Code, want, r.Body.String())
	}
}

// token returns the session token in a response's Set-Cookie header.
func (r resp) token() string {
	for _, c := range r.Result().Cookies() {
		if c.Name == credential.ConsumerSessionCookie {
			return c.Value
		}
	}
	return ""
}

func redeemBody(token, username, password string) string {
	return `{"invite_token":"` + token + `","display_name":"Example Consumer","username":"` + username + `","email":"user@example.invalid","password":"` + password +
		`","consent":{"terms_version":"` + consumerauth.TermsVersion + `","privacy_version":"` + consumerauth.PrivacyVersion + `"}}`
}

func loginBody(user, pass string) string {
	return `{"username":"` + user + `","password":"` + pass + `"}`
}

// signUp redeems a fresh invite and returns the session token.
func (e *env) signUp(username, password string) (token string) {
	e.t.Helper()
	inv, _ := e.invite(time.Hour)
	r := e.do(req{method: "POST", path: "/v1/invite-redemptions/password", body: redeemBody(inv, username, password)})
	status(e.t, r, http.StatusCreated)
	return r.token()
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.Reader().QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}
