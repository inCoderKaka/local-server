package admin_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/admin"
)

// Every error the admin listener answers is the OpenAI error envelope, whoever raised it, and
// none quotes request bytes (PLT-B-003).
func TestErrorEnvelopeEveryFailureShape(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	e.set(admin.KeyMaxBodyBytes, "2048")
	hostile := "ZZhostile-marker-7731"

	for _, c := range []struct {
		name   string
		r      req
		status int
		code   any // the expected code, nil for null
	}{
		{"unknown admin path", req{path: "/admin/v1/nothing-here", cookie: token}, 404, nil},
		{"unknown path", req{path: "/nowhere", cookie: token}, 404, nil},
		{"consumer path", req{path: "/v1/models", cookie: token}, 404, nil},
		{"wrong method", req{method: "PUT", path: "/admin/v1/host", cookie: token, origin: adminOrigin}, 405, nil},
		{"wrong method on setup", req{method: "DELETE", path: "/admin/v1/setup", origin: adminOrigin}, 405, nil},
		{"wrong method on metrics", req{method: "POST", path: "/metrics", origin: adminOrigin}, 405, nil},
		{"malformed json", req{method: "POST", path: "/admin/v1/session", body: `{"username": ` + hostile, origin: adminOrigin}, 400, nil},
		{"json of the wrong type", req{method: "POST", path: "/admin/v1/session", body: `{"username": 5, "password": "` + hostile + `"}`, origin: adminOrigin}, 400, nil},
		{"missing field", req{method: "POST", path: "/admin/v1/session", body: `{"username": "` + hostile + `"}`, origin: adminOrigin}, 400, nil},
		{"empty body", req{method: "POST", path: "/admin/v1/session", origin: adminOrigin}, 400, nil},
		{"wrong content type", req{method: "POST", path: "/admin/v1/session", body: creds("a", "b"), origin: adminOrigin, header: map[string]string{"Content-Type": "text/plain"}}, 415, nil},
		{"no content type", req{method: "POST", path: "/admin/v1/session", body: creds("a", "b"), origin: adminOrigin, noContentType: true}, 400, nil},
		{"oversized body", req{method: "POST", path: "/admin/v1/session", body: creds(strings.Repeat("x", 4096), hostile), origin: adminOrigin}, 413, nil},
		{"bad query parameter", req{path: "/admin/v1/audit-events?limit=" + hostile, cookie: token}, 400, nil},
		{"bad date parameter", req{path: "/admin/v1/host-metrics/history?from=" + hostile, cookie: token}, 400, nil},
		{"bad granularity", req{path: "/admin/v1/usage?granularity=" + hostile, cookie: token}, 400, nil},
		{"not authenticated", req{path: "/admin/v1/host"}, 401, nil},
		{"wrong login", req{method: "POST", path: "/admin/v1/session", body: creds("nobody", hostile), origin: adminOrigin}, 401, nil},
		{"foreign host", req{path: "/admin/v1/host", cookie: token, host: "evil.example"}, 403, nil},
		{"foreign origin", req{method: "POST", path: "/admin/v1/host/pause", cookie: token, origin: "http://evil.example"}, 403, nil},
		{"not implemented", req{path: "/admin/v1/consumers", cookie: token}, 501, nil},
		{"not implemented write", req{method: "POST", path: "/admin/v1/invites", body: `{}`, cookie: token, origin: adminOrigin}, 501, nil},
		{"setup done", req{method: "POST", path: "/admin/v1/setup", body: creds("x", testPass), origin: adminOrigin}, 409, nil},
	} {
		r := e.do(c.r)
		if r.Code != c.status {
			t.Errorf("%s: status %d, want %d; body %s", c.name, r.Code, c.status, r.Body.String())
			continue
		}
		inner := r.errBody(t)
		if inner["code"] != c.code {
			t.Errorf("%s: code = %v, want %v", c.name, inner["code"], c.code)
		}
		if ct := r.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("%s: Content-Type = %q", c.name, ct)
		}
		if r.Header().Get("X-Request-Id") == "" {
			t.Errorf("%s: no request id", c.name)
		}
		if strings.Contains(r.Body.String(), hostile) {
			t.Errorf("%s: the answer quotes the request: %s", c.name, r.Body.String())
		}
		if c.status == 405 && r.Header().Get("Allow") == "" {
			t.Errorf("%s: 405 without Allow", c.name)
		}
	}
	if strings.Contains(e.tel.buf.String(), hostile) {
		t.Error("request bytes reached the log")
	}
}

// An unimplemented admin operation answers 501 to a logged-in caller and 401 to anyone else:
// security runs first.
func TestErrorEnvelopeUnimplementedOperation(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	r := e.call(token, "GET", "/admin/v1/consumers", "")
	status(t, r, http.StatusNotImplemented)
	if inner := r.errBody(t); inner["type"] != "server_error" {
		t.Errorf("error = %v", inner)
	}
	status(t, e.do(req{path: "/admin/v1/consumers"}), http.StatusUnauthorized)
}

// An unknown field in a login body: reported if ogen refuses it, and never quoted.
func TestErrorEnvelopeUnknownField(t *testing.T) {
	e := newEnv(t)
	e.setup()
	r := e.do(req{method: "POST", path: "/admin/v1/session", body: `{"username":"` + testUser + `","password":"` + testPass + `","ZZextra":1}`, origin: adminOrigin})
	if r.Code != http.StatusOK && r.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", r.Code, r.Body.String())
	}
	if r.Code == http.StatusBadRequest {
		r.errBody(t)
	}
}

func TestErrorEnvelopeRetryAfterOnLockout(t *testing.T) {
	e := newEnv(t)
	e.setup()
	e.set("auth.login_lockout.failures", "1")
	status(t, e.do(req{method: "POST", path: "/admin/v1/session", body: creds(testUser, "wrong password!!"), origin: adminOrigin}), http.StatusUnauthorized)
	r := e.do(req{method: "POST", path: "/admin/v1/session", body: creds(testUser, testPass), origin: adminOrigin})
	status(t, r, http.StatusTooManyRequests)
	if r.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After on a lockout")
	}
	inner := r.errBody(t)
	if inner["code"] != "rate_limit_exceeded" || inner["type"] != "requests" {
		t.Errorf("error = %v", inner)
	}
}

// A handler that panics is answered 500 in the envelope, and the log holds the panic's type, not
// its message.
func TestErrorEnvelopeHandlerPanic(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.metrics = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("secret-prompt-text") })
	})
	r := e.do(req{path: "/metrics"})
	status(t, r, http.StatusInternalServerError)
	r.errBody(t)
	logs := e.tel.buf.String()
	if strings.Contains(logs, "secret-prompt-text") || !strings.Contains(logs, "panic_type") {
		t.Errorf("log = %s", logs)
	}
}

func TestMetricsServesBehindTheGuard(t *testing.T) {
	e := newEnv(t)
	r := e.do(req{path: "/metrics"})
	if r.Code != http.StatusOK || r.Body.String() != "metrics_ok 1\n" {
		t.Fatalf("/metrics = %d %q", r.Code, r.Body.String())
	}
	if r.Header().Get("X-Request-Id") == "" {
		t.Error("no request id on /metrics")
	}
	// /metrics?x and the escaped form are the same route or no route, never an admin route.
	status(t, e.do(req{path: "/metrics/"}), http.StatusNotFound)
}

func TestNewRequiresItsDependencies(t *testing.T) {
	if _, err := admin.New(admin.Config{}); err == nil {
		t.Fatal("New(Config{}) succeeded")
	}
}

func TestNewIsRepeatableOnOneStore(t *testing.T) {
	e := newEnv(t)
	// A second admin on the same store (the recovery-code command builds its own authenticator
	// beside a running server's) registers identical settings without error.
	if _, err := admin.NewAuthenticator(admin.AuthConfig{DB: e.db, Settings: e.st, Identity: e.ir}); err != nil {
		t.Fatal(err)
	}
}

// A body of unknown length (chunked) is cut off by the reader limit, not by Content-Length, and
// is still a 413 in the envelope.
func TestErrorEnvelopeOversizedChunkedBody(t *testing.T) {
	e := newEnv(t)
	e.setup()
	e.set(admin.KeyMaxBodyBytes, "512")
	hr := httptest.NewRequest("POST", "http://"+adminHost+"/admin/v1/session", strings.NewReader(creds(strings.Repeat("x", 4096), "p")))
	hr.ContentLength = -1
	hr.Host = adminHost
	hr.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, hr)
	r := resp{rec}
	status(t, r, http.StatusRequestEntityTooLarge)
	r.errBody(t)
}
