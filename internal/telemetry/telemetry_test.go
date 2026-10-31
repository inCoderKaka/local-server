package telemetry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRedactedNeverRenders(t *testing.T) {
	r := Redact("secret prompt")
	for _, s := range []string{fmt.Sprint(r), fmt.Sprintf("%v %+v %#v %s %q", r, r, r, r, r)} {
		if strings.Contains(s, "secret") {
			t.Fatalf("leak: %s", s)
		}
	}
	b, _ := json.Marshal(map[string]any{"p": r})
	if strings.Contains(string(b), "secret") {
		t.Fatalf("json leak: %s", b)
	}
	if r.Reveal() != "secret prompt" {
		t.Fatal("Reveal must return the text")
	}
}

func TestMiddlewareRequestID(t *testing.T) {
	var seen string
	h := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = RequestID(r.Context()) }))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "abc-123")
	h.ServeHTTP(rec, req)
	if seen != "abc-123" || rec.Header().Get(RequestIDHeader) != "abc-123" {
		t.Fatalf("kept id: seen=%q header=%q", seen, rec.Header().Get(RequestIDHeader))
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "bad id\n")
	h.ServeHTTP(rec, req)
	if len(seen) != 32 || seen != rec.Header().Get(RequestIDHeader) {
		t.Fatalf("generated id: %q", seen)
	}
}

func TestNop(t *testing.T) {
	n := Nop()
	n.Counter("a").Inc()
	n.Logger().Info("x")
	_, sp := n.StartSpan(t.Context(), "s")
	sp.End()
}
