package admin_test

import (
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/inCoderKaka/local-server/internal/admin"
	"github.com/inCoderKaka/local-server/internal/webui"
)

const uiIndex = `<!doctype html><html><head><script type="module" src="/assets/app-1.js"></script></head><body><div id="root"></div></body></html>`

func withUI(o *envOpts) {
	o.ui = webui.Handler(webui.Config{Files: fstest.MapFS{
		"index.html":      {Data: []byte(uiIndex)},
		"assets/app-1.js": {Data: []byte("export {}")},
	}})
}

// The admin listener serves the UI on GET and HEAD of page and asset paths, with the CSP, behind
// the same guard as the API (req_001 D-10, D-151).
func TestUIIsServedOnTheAdminListener(t *testing.T) {
	e := newEnv(t, withUI)
	for _, p := range []string{"/", "/consumers", "/consumers/abc"} {
		r := e.do(req{path: p})
		status(t, r, http.StatusOK)
		if r.Body.String() != uiIndex {
			t.Fatalf("GET %s body = %q", p, r.Body.String())
		}
		if got := r.Header().Get("Content-Security-Policy"); got != webui.CSP {
			t.Fatalf("GET %s CSP = %q", p, got)
		}
		if r.Header().Get("X-Request-Id") == "" {
			t.Fatalf("GET %s: no request id", p)
		}
	}
	r := e.do(req{path: "/assets/app-1.js"})
	status(t, r, http.StatusOK)
	if r.Header().Get("Content-Type") != "text/javascript; charset=utf-8" || r.Body.String() != "export {}" {
		t.Fatalf("asset = %q %q", r.Header().Get("Content-Type"), r.Body.String())
	}
	status(t, e.do(req{method: http.MethodHead, path: "/"}), http.StatusOK)
	// A tailnet peer reaches the UI on the remote admin listener, behind the admin login.
	e2 := newEnv(t, withUI, func(o *envOpts) { o.tailnetHosts = []string{"my-host.example.ts.net"} })
	status(t, e2.do(req{path: "/", host: "my-host.example.ts.net", remote: "100.64.0.9:4444"}), http.StatusOK)
}

// API paths never get the UI's page: an unknown admin API path, a consumer route and an escaped
// spelling of either keep answering the 404 envelope.
func TestUIDoesNotShadowAPIPaths(t *testing.T) {
	e := newEnv(t, withUI)
	for _, p := range []string{
		"/admin/v1/nope", "/admin/v1/", "/admin", "/admin/", "/admin/v2/host", "/admin%2fv1/nope", "/%61dmin/v1/nope",
		"/v1/health", "/v1", "/v1%2fhealth", "/metrics/x", "/%6detrics", "/ADMIN/v1/nope",
	} {
		r := e.do(req{path: p})
		if r.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404; body %s", p, r.Code, r.Body.String())
			continue
		}
		r.errBody(t)
		if r.Header().Get("Content-Type") != "application/json" && !strings.HasPrefix(r.Header().Get("Content-Type"), "application/json") {
			t.Errorf("GET %s Content-Type = %q", p, r.Header().Get("Content-Type"))
		}
	}
	// /metrics itself is unchanged.
	r := e.do(req{path: "/metrics"})
	status(t, r, http.StatusOK)
	if r.Body.String() != "metrics_ok 1\n" {
		t.Fatalf("/metrics = %q", r.Body.String())
	}
	// A real admin route still works.
	status(t, e.do(req{path: "/admin/v1/setup"}), http.StatusOK)
}

// Only GET and HEAD reach the UI; any other method on a page path keeps the 404 envelope.
func TestUIOnlyForGetAndHead(t *testing.T) {
	e := newEnv(t, withUI)
	token := e.setup()
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		r := e.call(token, m, "/", `{}`)
		if r.Code != http.StatusNotFound || strings.Contains(r.Body.String(), "root") {
			t.Errorf("%s / = %d %s", m, r.Code, r.Body.String())
			continue
		}
		r.errBody(t)
	}
}

// The DNS-rebinding guard still wraps the UI.
func TestUIForeignHostRefused(t *testing.T) {
	e := newEnv(t, withUI)
	for _, p := range []string{"/", "/assets/app-1.js", "/consumers"} {
		r := e.do(req{path: p, host: "evil.example"})
		if r.Code != http.StatusForbidden || strings.Contains(r.Body.String(), "root") {
			t.Errorf("foreign Host GET %s = %d %s", p, r.Code, r.Body.String())
			continue
		}
		r.errBody(t)
	}
}

// Without Config.UI the listener answers a page path with the 404 envelope, as before.
func TestNoUIKeepsEnvelope(t *testing.T) {
	e := newEnv(t)
	r := e.do(req{path: "/"})
	status(t, r, http.StatusNotFound)
	r.errBody(t)
}

// admin.ui_poll_interval is a host-owned duration with a default of 30s, refused when not a
// positive duration (req_001 D-38, D-157).
func TestUIPollIntervalSetting(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	r := e.call(token, "GET", "/admin/v1/settings", "")
	status(t, r, http.StatusOK)
	var found bool
	for _, s := range r.json()["data"].([]any) {
		m := s.(map[string]any)
		if m["key"] == admin.KeyUIPollInterval {
			found = true
			if m["value"] != "30s" || m["default_value"] != "30s" || m["owner"] != "host" {
				t.Fatalf("setting = %v", m)
			}
		}
	}
	if !found {
		t.Fatal("admin.ui_poll_interval is not listed")
	}
	for _, bad := range []string{`"0s"`, `"-5s"`, `"soon"`, `30`} {
		r := e.call(token, "PATCH", "/admin/v1/settings", `{"values":{"admin.ui_poll_interval":`+bad+`}}`)
		if r.Code != http.StatusBadRequest {
			t.Errorf("value %s = %d", bad, r.Code)
		}
	}
	status(t, e.call(token, "PATCH", "/admin/v1/settings", `{"values":{"admin.ui_poll_interval":"1m"}}`), http.StatusOK)
}
