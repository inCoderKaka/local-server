package webui_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/inCoderKaka/local-server/internal/webui"
)

const wantCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; " +
	"connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

const indexBody = `<!doctype html><html><head><script type="module" src="/assets/index-abc.js"></script></head><body><div id="root"></div></body></html>`

func built() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte(indexBody)},
		"assets/index-abc.js":  {Data: []byte("console.log(1)")},
		"assets/index-abc.css": {Data: []byte("body{}")},
		"assets/logo.svg":      {Data: []byte("<svg/>")},
		"assets/data.bin":      {Data: []byte{1, 2, 3}},
		".gitkeep":             {Data: []byte{}},
		"assets/.secret":       {Data: []byte("x")},
	}
}

func serve(t *testing.T, h http.Handler, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://127.0.0.1:8485/", nil)
	// Set the path by hand so a test can send raw, unclean paths the client would normalise.
	if err := setTarget(r, target); err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func setTarget(r *http.Request, target string) error {
	// ParseRequestURI keeps dot segments and a leading "//" as the server receives them.
	u, err := url.ParseRequestURI(target)
	if err != nil {
		return err
	}
	r.URL = u
	r.RequestURI = target
	return nil
}

func checkSecurityHeaders(t *testing.T, name string, w *httptest.ResponseRecorder) {
	t.Helper()
	h := w.Header()
	if got := h.Get("Content-Security-Policy"); got != wantCSP {
		t.Errorf("%s: CSP = %q, want %q", name, got, wantCSP)
	}
	for k, want := range map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"Referrer-Policy":              "no-referrer",
		"Cross-Origin-Resource-Policy": "same-origin",
	} {
		if got := h.Get(k); got != want {
			t.Errorf("%s: %s = %q, want %q", name, k, got, want)
		}
	}
	for k := range h {
		if strings.HasPrefix(strings.ToLower(k), "access-control-") {
			t.Errorf("%s: CORS header %s set", name, k)
		}
	}
	if strings.Contains(h.Get("Cache-Control"), "max-age") {
		t.Errorf("%s: Cache-Control %q names a lifetime", name, h.Get("Cache-Control"))
	}
}

func TestCSPConstantIsExact(t *testing.T) {
	if webui.CSP != wantCSP {
		t.Fatalf("CSP = %q", webui.CSP)
	}
}

func TestServesFilesAndPages(t *testing.T) {
	h := webui.Handler(webui.Config{Files: built()})
	for _, tc := range []struct {
		name, path string
		status     int
		ctype      string
		body       string
	}{
		{"root", "/", 200, "text/html; charset=utf-8", indexBody},
		{"index", "/index.html", 200, "text/html; charset=utf-8", indexBody},
		{"script", "/assets/index-abc.js", 200, "text/javascript; charset=utf-8", "console.log(1)"},
		{"style", "/assets/index-abc.css", 200, "text/css; charset=utf-8", "body{}"},
		{"svg", "/assets/logo.svg", 200, "image/svg+xml", "<svg/>"},
		{"unknown type", "/assets/data.bin", 200, "application/octet-stream", "\x01\x02\x03"},
		{"page route", "/consumers", 200, "text/html; charset=utf-8", indexBody},
		{"nested page route", "/consumers/abc/keys", 200, "text/html; charset=utf-8", indexBody},
		{"directory", "/assets", 200, "text/html; charset=utf-8", indexBody},
		{"directory slash", "/assets/", 200, "text/html; charset=utf-8", indexBody},
		{"missing asset", "/assets/index-old.js", 404, "text/plain; charset=utf-8", ""},
		{"missing extension file", "/favicon.ico", 404, "text/plain; charset=utf-8", ""},
		{"dot file", "/.gitkeep", 404, "text/plain; charset=utf-8", ""},
		{"nested dot file", "/assets/.secret", 404, "text/plain; charset=utf-8", ""},
		{"dot segment", "/.git/config", 404, "text/plain; charset=utf-8", ""},
		{"traversal", "/../etc/passwd", 404, "text/plain; charset=utf-8", ""},
		{"traversal inside", "/assets/../../etc/passwd", 404, "text/plain; charset=utf-8", ""},
		{"encoded traversal", "/%2e%2e/etc/passwd", 404, "text/plain; charset=utf-8", ""},
		{"encoded traversal mixed", "/assets/%2E%2e/index.html", 404, "text/plain; charset=utf-8", ""},
		{"encoded slash traversal", "/assets%2f..%2f..%2fetc%2fpasswd", 404, "text/plain; charset=utf-8", ""},
		{"backslash", "/assets\\..\\index.html", 404, "text/plain; charset=utf-8", ""},
		{"encoded backslash", "/assets%5c..%5cindex.html", 404, "text/plain; charset=utf-8", ""},
		{"nul", "/index.html%00.js", 404, "text/plain; charset=utf-8", ""},
		{"double slash", "//assets//index-abc.js", 200, "text/javascript; charset=utf-8", "console.log(1)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := serve(t, h, http.MethodGet, tc.path, nil)
			if w.Code != tc.status {
				t.Fatalf("GET %s = %d, want %d (%q)", tc.path, w.Code, tc.status, w.Body.String())
			}
			checkSecurityHeaders(t, tc.path, w)
			if got := w.Header().Get("Content-Type"); got != tc.ctype {
				t.Errorf("Content-Type = %q, want %q", got, tc.ctype)
			}
			if tc.status == 200 {
				if w.Body.String() != tc.body {
					t.Errorf("body = %q, want %q", w.Body.String(), tc.body)
				}
				if w.Header().Get("Cache-Control") != "no-cache" || !strings.HasPrefix(w.Header().Get("ETag"), `"`) {
					t.Errorf("cache headers = %q / %q", w.Header().Get("Cache-Control"), w.Header().Get("ETag"))
				}
			} else if strings.Contains(w.Body.String(), "root") || strings.Contains(w.Body.String(), "console") {
				t.Errorf("404 body leaks a file: %q", w.Body.String())
			}
		})
	}
}

func TestOnlyGetAndHead(t *testing.T) {
	h := webui.Handler(webui.Config{Files: built()})
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, "PROPFIND"} {
		w := serve(t, h, m, "/", nil)
		if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("%s / = %d Allow %q", m, w.Code, w.Header().Get("Allow"))
		}
		checkSecurityHeaders(t, m, w)
	}
	w := serve(t, h, http.MethodHead, "/assets/index-abc.js", nil)
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("HEAD = %d, %d body bytes, type %q", w.Code, w.Body.Len(), w.Header().Get("Content-Type"))
	}
	checkSecurityHeaders(t, "HEAD", w)
}

func TestETagRevalidation(t *testing.T) {
	h := webui.Handler(webui.Config{Files: built()})
	first := serve(t, h, http.MethodGet, "/assets/index-abc.js", nil)
	tag := first.Header().Get("ETag")
	if tag == "" {
		t.Fatal("no ETag")
	}
	if other := serve(t, h, http.MethodGet, "/assets/index-abc.css", nil).Header().Get("ETag"); other == tag {
		t.Fatal("two different files share an ETag")
	}
	w := serve(t, h, http.MethodGet, "/assets/index-abc.js", map[string]string{"If-None-Match": tag})
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatalf("If-None-Match = %d, %d body bytes", w.Code, w.Body.Len())
	}
	checkSecurityHeaders(t, "304", w)
	w = serve(t, h, http.MethodGet, "/assets/index-abc.js", map[string]string{"If-None-Match": `"other"`})
	if w.Code != http.StatusOK {
		t.Fatalf("stale If-None-Match = %d", w.Code)
	}
	// A page route revalidates against index.html's tag.
	idx := serve(t, h, http.MethodGet, "/", nil).Header().Get("ETag")
	if w := serve(t, h, http.MethodGet, "/usage", map[string]string{"If-None-Match": idx}); w.Code != http.StatusNotModified {
		t.Fatalf("page route If-None-Match = %d", w.Code)
	}
}

func TestStubWhenNotBuilt(t *testing.T) {
	for name, files := range map[string]fstest.MapFS{
		"nil":            nil,
		"only gitkeep":   {".gitkeep": {Data: []byte{}}},
		"assets, no idx": {"assets/a.js": {Data: []byte("x")}},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := webui.Config{}
			if files != nil {
				cfg.Files = files
			}
			h := webui.Handler(cfg)
			for _, p := range []string{"/", "/index.html", "/consumers"} {
				w := serve(t, h, http.MethodGet, p, nil)
				if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
					t.Fatalf("GET %s = %d %q", p, w.Code, w.Header().Get("Content-Type"))
				}
				checkSecurityHeaders(t, "stub "+p, w)
				body := w.Body.String()
				if !strings.Contains(body, "make web-admin-build") || !strings.Contains(body, "not built") {
					t.Fatalf("stub does not say how to build: %s", body)
				}
				for _, bad := range []string{"<script", "<style", "style=", "<link"} {
					if strings.Contains(strings.ToLower(body), bad) {
						t.Errorf("stub holds %q", bad)
					}
				}
			}
			w := serve(t, h, http.MethodGet, "/assets/missing.js", nil)
			if w.Code != http.StatusNotFound {
				t.Fatalf("missing asset with stub = %d", w.Code)
			}
			checkSecurityHeaders(t, "stub 404", w)
		})
	}
}
