package credential

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// guarded runs one request through Guard and reports the status and the refusal reason.
func guarded(t *testing.T, names func(context.Context) ([]string, error), method, host string, hdr map[string]string, tlsOn bool) (int, Reason) {
	t.Helper()
	var why Reason
	g := Guard(GuardConfig{
		Names: names, SessionCookies: []string{AdminSessionCookie},
		Reject: func(w http.ResponseWriter, _ *http.Request, r Reason) { why = r; w.WriteHeader(http.StatusForbidden) },
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	req := httptest.NewRequest(method, "/x", nil)
	req.Host = host
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if tlsOn {
		req.TLS = &tls.ConnectionState{}
	}
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	return rec.Code, why
}

func TestGuardHost(t *testing.T) {
	tailnet := func(context.Context) ([]string, error) { return []string{"Host-1.Tail-Net.ts.net"}, nil }
	for _, c := range []struct {
		host string
		ok   bool
	}{
		{"localhost", true}, {"localhost:8485", true}, {"LOCALHOST:8485", true}, {"localhost.", true}, {"localhost.:8485", true},
		{"127.0.0.1", true}, {"127.0.0.1:8485", true}, {"127.9.9.9:1", true},
		{"[::1]", true}, {"[::1]:8485", true}, {"::1", true},
		{"host-1.tail-net.ts.net", true}, {"HOST-1.tail-net.ts.net:8485", true}, {"host-1.tail-net.ts.net.", true},
		{"", false}, {"evil.example", false}, {"evil.example:8485", false}, {"localhost.evil.example", false},
		{"127.0.0.1.evil.example", false}, {"localhost:abc", false}, {"localhost:99999", false}, {"user@localhost", false},
		{"128.0.0.1", false}, {"[::2]", false}, {"0.0.0.0", false}, {"foo.localhost", false}, {"10.0.0.1:8485", false},
		{"evil.example/localhost", false}, {"localhost evil", false},
	} {
		code, why := guarded(t, tailnet, "GET", c.host, nil, false)
		if c.ok && code != http.StatusNoContent {
			t.Errorf("Host %q = %d (%s), want it accepted", c.host, code, why)
		}
		if !c.ok && (code != http.StatusForbidden || why != ReasonForeignHost) {
			t.Errorf("Host %q = %d (%s), want foreign_host", c.host, code, why)
		}
	}
}

func TestGuardHostWithoutExtraNames(t *testing.T) {
	if code, _ := guarded(t, nil, "GET", "host-1.tail-net.ts.net", nil, false); code != http.StatusForbidden {
		t.Errorf("a name nobody configured = %d, want 403", code)
	}
}

func TestGuardFailsClosedWhenTheNamesCannotBeRead(t *testing.T) {
	broken := func(context.Context) ([]string, error) { return nil, errors.New("database is down") }
	code, why := guarded(t, broken, "GET", "evil.example", nil, false)
	if code != http.StatusForbidden || why != ReasonHostUnavailable {
		t.Errorf("foreign host, names unreadable = %d (%s)", code, why)
	}
	// A loopback host needs no setting, so it still works.
	if code, _ := guarded(t, broken, "GET", "127.0.0.1:1", nil, false); code != http.StatusNoContent {
		t.Errorf("loopback host with unreadable names = %d", code)
	}
}

func TestGuardOrigin(t *testing.T) {
	cookie := map[string]string{"Cookie": AdminSessionCookie + "=abc"}
	with := func(base map[string]string, k, v string) map[string]string {
		out := map[string]string{}
		for a, b := range base {
			out[a] = b
		}
		out[k] = v
		return out
	}
	for _, c := range []struct {
		name   string
		method string
		host   string
		hdr    map[string]string
		tls    bool
		want   Reason // "" means accepted
	}{
		{"cookie post, same origin", "POST", "127.0.0.1:8485", with(cookie, "Origin", "http://127.0.0.1:8485"), false, ""},
		{"cookie post, no origin", "POST", "127.0.0.1:8485", cookie, false, ReasonOriginRequired},
		{"cookie delete, no origin", "DELETE", "127.0.0.1:8485", cookie, false, ReasonOriginRequired},
		{"cookie patch, no origin", "PATCH", "127.0.0.1:8485", cookie, false, ReasonOriginRequired},
		{"cookie post, null origin", "POST", "127.0.0.1:8485", with(cookie, "Origin", "null"), false, ReasonForeignOrigin},
		{"cookie post, foreign host", "POST", "127.0.0.1:8485", with(cookie, "Origin", "http://evil.example"), false, ReasonForeignOrigin},
		{"cookie post, foreign port", "POST", "127.0.0.1:8485", with(cookie, "Origin", "http://127.0.0.1:8484"), false, ReasonForeignOrigin},
		{"cookie post, other loopback name", "POST", "127.0.0.1:8485", with(cookie, "Origin", "http://localhost:8485"), false, ReasonForeignOrigin},
		{"cookie post, scheme mismatch", "POST", "127.0.0.1:8485", with(cookie, "Origin", "https://127.0.0.1:8485"), false, ReasonForeignOrigin},
		{"cookie post over tls, http origin", "POST", "127.0.0.1:8485", with(cookie, "Origin", "http://127.0.0.1:8485"), true, ReasonForeignOrigin},
		{"cookie post over tls, https origin", "POST", "127.0.0.1:8485", with(cookie, "Origin", "https://127.0.0.1:8485"), true, ""},
		{"origin with a path", "POST", "127.0.0.1:8485", with(cookie, "Origin", "http://127.0.0.1:8485/x"), false, ReasonForeignOrigin},
		{"origin with userinfo", "POST", "127.0.0.1:8485", with(cookie, "Origin", "http://a@127.0.0.1:8485"), false, ReasonForeignOrigin},
		{"origin not a url", "POST", "127.0.0.1:8485", with(cookie, "Origin", "%%%"), false, ReasonForeignOrigin},
		{"origin uppercase host", "POST", "LOCALHOST:8485", with(cookie, "Origin", "http://localhost:8485"), false, ""},
		{"ipv6 same origin", "POST", "[::1]:8485", with(cookie, "Origin", "http://[::1]:8485"), false, ""},
		{"default port omitted on both", "POST", "localhost", with(cookie, "Origin", "http://localhost:80"), false, ""},
		{"no cookie, no origin (curl)", "POST", "127.0.0.1:8485", nil, false, ""},
		{"no cookie, foreign origin (login CSRF)", "POST", "127.0.0.1:8485", map[string]string{"Origin": "http://evil.example"}, false, ReasonForeignOrigin},
		{"no cookie, null origin", "POST", "127.0.0.1:8485", map[string]string{"Origin": "null"}, false, ReasonForeignOrigin},
		{"get, foreign origin", "GET", "127.0.0.1:8485", with(cookie, "Origin", "http://evil.example"), false, ""},
		{"head, no origin, cookie", "HEAD", "127.0.0.1:8485", cookie, false, ""},
		{"options, no origin, cookie", "OPTIONS", "127.0.0.1:8485", cookie, false, ""},
		{"get, no origin, cookie", "GET", "127.0.0.1:8485", cookie, false, ""},
		{"another cookie is not a session", "POST", "127.0.0.1:8485", map[string]string{"Cookie": "other=1"}, false, ""},
	} {
		code, why := guarded(t, nil, c.method, c.host, c.hdr, c.tls)
		if c.want == "" && code != http.StatusNoContent {
			t.Errorf("%s: %d (%s), want it accepted", c.name, code, why)
		}
		if c.want != "" && (code != http.StatusForbidden || why != c.want) {
			t.Errorf("%s: %d (%s), want %s", c.name, code, why, c.want)
		}
	}
}

func TestGuardTwoOriginHeadersAreRefused(t *testing.T) {
	g := Guard(GuardConfig{Reject: func(w http.ResponseWriter, _ *http.Request, _ Reason) { w.WriteHeader(http.StatusForbidden) }})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	req := httptest.NewRequest("POST", "/x", nil)
	req.Host = "127.0.0.1:1"
	req.Header.Add("Origin", "http://127.0.0.1:1")
	req.Header.Add("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("two Origin headers = %d, want 403", rec.Code)
	}
}

// The refusal reasons are fixed codes: nothing a client sent can be in them.
func TestReasonsCarryNoRequestBytes(t *testing.T) {
	for _, r := range []Reason{ReasonForeignHost, ReasonHostUnavailable, ReasonForeignOrigin, ReasonOriginRequired} {
		if strings.ContainsAny(string(r), ".:/ ") {
			t.Errorf("reason %q", r)
		}
	}
}

func TestIsLoopbackHost(t *testing.T) {
	for host, want := range map[string]bool{"localhost": true, "127.0.0.1:11434": true, "[::1]:1": true, "example.com": false, "": false, "evil:80": false} {
		if got := IsLoopbackHost(host); got != want {
			t.Errorf("IsLoopbackHost(%q) = %v", host, got)
		}
	}
}
