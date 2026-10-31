package telemetry

import (
	"encoding/hex"
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
	if (Redacted{}).Reveal() != "" {
		t.Fatal("zero Redacted must reveal empty text")
	}

	// A Redacted in another struct's unexported field: fmt cannot call its
	// methods there, so only the pointer keeps the text out.
	type holder struct{ prompt Redacted }
	h := holder{prompt: Redact("secret prompt")}
	hexSecret := hex.EncodeToString([]byte("secret"))
	for _, v := range []any{h, &h, []holder{h}} {
		s := fmt.Sprintf("%v %+v %#v %s %q %x", v, v, v, v, v, v)
		if strings.Contains(s, "secret") || strings.Contains(s, hexSecret) {
			t.Fatalf("leak through unexported field: %s", s)
		}
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

	long := strings.Repeat("a", 64)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, long)
	h.ServeHTTP(rec, req)
	if seen != long || rec.Header().Get(RequestIDHeader) != long {
		t.Fatalf("64-char id must be kept: seen=%q", seen)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, long+"a")
	h.ServeHTTP(rec, req)
	if len(seen) != 32 || seen == long+"a" || seen != rec.Header().Get(RequestIDHeader) {
		t.Fatalf("65-char id must be replaced: seen=%q", seen)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)
	if len(seen) != 32 || seen != rec.Header().Get(RequestIDHeader) {
		t.Fatalf("missing id must be generated: seen=%q header=%q", seen, rec.Header().Get(RequestIDHeader))
	}
}

func TestNop(t *testing.T) {
	n := Nop()
	n.Counter("a").Inc()
	n.Logger().Info("x")
	_, sp := n.StartSpan(t.Context(), "s")
	sp.End()
}
