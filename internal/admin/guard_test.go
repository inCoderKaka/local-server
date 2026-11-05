package admin_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/admin"
	"github.com/inCoderKaka/local-server/internal/settings"
)

// A request whose Host header is not a loopback name or the configured tailnet name is refused on
// every route, including first-run admin creation, login and /metrics (req_001 D-10).
func TestForeignHostIsRefusedEverywhere(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	for _, host := range []string{"evil.example", "evil.example:8485", "127.0.0.1.evil.example", "10.0.0.5:8485"} {
		for _, rt := range []struct{ method, path, body string }{
			{"GET", "/admin/v1/setup", ""},
			{"POST", "/admin/v1/setup", creds("late", testPass)},
			{"POST", "/admin/v1/session", creds(testUser, testPass)},
			{"GET", "/admin/v1/session", ""},
			{"GET", "/admin/v1/host", ""},
			{"GET", "/metrics", ""},
			{"GET", "/v1/health", ""},
			{"GET", "/nothing", ""},
		} {
			r := e.do(req{method: rt.method, path: rt.path, body: rt.body, host: host, cookie: token})
			if r.Code != http.StatusForbidden {
				t.Errorf("Host %q %s %s = %d, want 403", host, rt.method, rt.path, r.Code)
				continue
			}
			r.errBody(t)
			if strings.Contains(r.Body.String(), "evil") || strings.Contains(r.Body.String(), "10.0.0.5") {
				t.Errorf("the refusal echoes the Host header: %s", r.Body.String())
			}
			if r.Header().Get("X-Request-Id") == "" {
				t.Errorf("Host %q %s %s: no request id on the refusal", host, rt.method, rt.path)
			}
		}
	}
	if n := e.count(`SELECT count(*) FROM idn_admin_user`); n != 1 {
		t.Fatalf("admins = %d", n)
	}
	// The foreign host was not logged back.
	if strings.Contains(e.tel.buf.String(), "evil.example") {
		t.Error("the foreign Host header reached the log")
	}
}

// A request with no Host header (HTTP/1.0) is refused.
func TestMissingHostIsRefused(t *testing.T) {
	e := newEnv(t)
	hr := httptest.NewRequest("GET", "/metrics", nil)
	hr.Host = ""
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, hr)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("empty Host = %d, want 403", rec.Code)
	}
}

func TestLoopbackHostsAreAccepted(t *testing.T) {
	e := newEnv(t)
	for _, host := range []string{"127.0.0.1:8485", "localhost:8485", "LOCALHOST", "localhost.:8485", "[::1]:8485", "127.5.5.5"} {
		r := e.do(req{path: "/metrics", host: host})
		if r.Code != http.StatusOK || r.Body.String() != "metrics_ok 1\n" {
			t.Errorf("Host %q: /metrics = %d %q", host, r.Code, r.Body.String())
		}
	}
}

func TestTailnetNameSettingAppliesPerRequest(t *testing.T) {
	e := newEnv(t)
	const name = "my-host.example.ts.net"
	status(t, e.do(req{path: "/metrics", host: name + ":8485"}), http.StatusForbidden)
	e.set(admin.KeyTailnetName, name)
	status(t, e.do(req{path: "/metrics", host: name + ":8485"}), http.StatusOK)
	status(t, e.do(req{path: "/metrics", host: "MY-HOST.example.ts.net."}), http.StatusOK)
	status(t, e.do(req{path: "/metrics", host: "other.example.ts.net"}), http.StatusForbidden)
	e.set(admin.KeyTailnetName, "")
	status(t, e.do(req{path: "/metrics", host: name}), http.StatusForbidden)
}

func TestTailnetNameValidator(t *testing.T) {
	e := newEnv(t)
	for _, bad := range []string{"http://x.ts.net", "x.ts.net:8485", "x/y", "a b", ".leading", "-leading", "über.ts.net", strings.Repeat("a", 254)} {
		if err := e.st.Set(t.Context(), settings.Host, admin.KeyTailnetName, "", bad); err == nil {
			t.Errorf("%q was accepted as a tailnet name", bad)
		}
	}
	e.set(admin.KeyTailnetName, "ok-host.tail1234.ts.net")
}

// The guard cannot be talked round by a lookalike Host that carries a loopback name in it.
func TestLookalikeHostsAreRefused(t *testing.T) {
	e := newEnv(t)
	for _, host := range []string{"localhost.evil.example", "evil.example#localhost", "user@127.0.0.1", "127.0.0.1@evil.example", "localhost:8485@evil.example"} {
		if r := e.do(req{path: "/metrics", host: host}); r.Code != http.StatusForbidden {
			t.Errorf("Host %q = %d, want 403", host, r.Code)
		}
	}
}

// A state-changing request that carries the session cookie must carry a same-origin Origin
// header (req_001 D-10).
func TestCookiePostNeedsASameOriginOrigin(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	pause := func(origin string) resp {
		return e.do(req{method: "POST", path: "/admin/v1/host/pause", cookie: token, origin: origin})
	}
	for name, origin := range map[string]string{
		"foreign host":        "http://evil.example",
		"foreign port":        "http://127.0.0.1:9999",
		"null":                "null",
		"other scheme":        "https://127.0.0.1:8485",
		"other loopback name": "http://localhost:8485",
	} {
		r := pause(origin)
		if r.Code != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", name, r.Code)
		}
		r.errBody(t)
		if strings.Contains(r.Body.String(), "evil") {
			t.Errorf("%s: the refusal echoes the Origin", name)
		}
	}
	// No Origin at all, with the cookie: refused.
	status(t, pause(""), http.StatusForbidden)
	// Nothing happened.
	if n := len(e.auditRows("host.paused")); n != 0 {
		t.Fatalf("a refused request paused the host (%d audit rows)", n)
	}
	// Same origin: accepted.
	status(t, pause(adminOrigin), http.StatusOK)
	if n := len(e.auditRows("host.paused")); n != 1 {
		t.Fatalf("host.paused rows = %d", n)
	}
	// A safe method needs no Origin.
	status(t, e.do(req{path: "/admin/v1/host", cookie: token}), http.StatusOK)
	// PATCH and DELETE are state-changing too.
	status(t, e.do(req{method: "PATCH", path: "/admin/v1/settings", body: `{"values":{}}`, cookie: token}), http.StatusForbidden)
	status(t, e.do(req{method: "DELETE", path: "/admin/v1/session", cookie: token}), http.StatusForbidden)
	status(t, e.do(req{method: "DELETE", path: "/admin/v1/session", cookie: token, origin: "http://evil.example"}), http.StatusForbidden)
	status(t, e.call(token, "GET", "/admin/v1/session", ""), http.StatusOK)
}

// The login POST has no cookie yet, but a foreign Origin on it is still refused, which stops a
// login-CSRF; a client that sends no Origin (curl) is not a browser and passes.
func TestLoginPostRefusesAForeignOrigin(t *testing.T) {
	e := newEnv(t)
	e.setup()
	status(t, e.do(req{method: "POST", path: "/admin/v1/session", body: creds(testUser, testPass), origin: "http://evil.example"}), http.StatusForbidden)
	status(t, e.do(req{method: "POST", path: "/admin/v1/session", body: creds(testUser, testPass), origin: "null"}), http.StatusForbidden)
	status(t, e.do(req{method: "POST", path: "/admin/v1/session", body: creds(testUser, testPass)}), http.StatusOK)
	status(t, e.do(req{method: "POST", path: "/admin/v1/setup", body: creds("x", testPass), origin: "http://evil.example"}), http.StatusForbidden)
}

// The admin listener serves no consumer route and the consumer API serves no admin route
// (admin side here; the consumer listener is covered in cmd/local-server).
func TestConsumerRoutesAreNotOnTheAdminListener(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	for _, p := range []string{"/v1/usage?granularity=day", "/v1/health", "/v1/models", "/v1/chat/completions", "/v1/", "/", "/admin", "/admin/", "/admin/v1", "/%61dmin/v1/setup", "/admin%2Fv1/setup"} {
		r := e.do(req{path: p, cookie: token})
		if r.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, r.Code)
			continue
		}
		r.errBody(t)
	}
	// A path that decodes into /admin/v1 only through an escape is not an admin route.
	r := e.do(req{path: "/%2e%2e/admin/v1/setup"})
	if r.Code != http.StatusNotFound {
		t.Errorf("escaped path = %d, want 404", r.Code)
	}
}
