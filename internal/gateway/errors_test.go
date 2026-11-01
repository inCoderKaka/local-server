package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// wantEnvelope checks that r is an OpenAI error envelope with the four members and nothing
// that quotes the markers.
func wantEnvelope(t *testing.T, r resp, status int, markers ...string) map[string]any {
	t.Helper()
	statusIs(t, r, status)
	if ct := r.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", ct)
	}
	var env struct {
		Error map[string]json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &env); err != nil || env.Error == nil {
		t.Fatalf("not an error envelope: %s", r.Body.String())
	}
	for _, k := range []string{"message", "type", "param", "code"} {
		if _, ok := env.Error[k]; !ok {
			t.Fatalf("envelope lacks %q: %s", k, r.Body.String())
		}
	}
	if r.Header().Get("X-Request-Id") == "" {
		t.Error("no X-Request-Id")
	}
	for _, m := range markers {
		if strings.Contains(r.Body.String(), m) {
			t.Errorf("the answer quotes request bytes %q: %s", m, r.Body.String())
		}
	}
	return r.errBody()
}

func TestErrorEnvelopeForDecodeErrorsNeverQuotesTheRequest(t *testing.T) {
	e := newEnv(t)
	pre := `{"model":"` + chatModel + `",`
	cases := []struct {
		name, body string
		status     int
		code       any
		param      any
	}{
		{"bad type in a known field", pre + `"messages":[{"role":"user","content":"x"}],"max_tokens":"SECRETMARKER"}`, 400, nil, "max_tokens"},
		{"unknown role", pre + `"messages":[{"role":"SECRETMARKER","content":"x"}]}`, 400, nil, "messages"},
		{"truncated JSON", pre + `"messages":[{"role":"user","content":"SECRETMARKER"}`, 400, nil, "messages"},
		{"not JSON", `SECRETMARKER`, 400, nil, nil},
		{"out of range", pre + `"messages":[{"role":"user","content":"SECRETMARKER"}],"temperature":9}`, 400, nil, "temperature"},
		{"empty messages", pre + `"messages":[]}`, 400, nil, "messages"},
		{"unknown field with a hostile name", pre + `"messages":[{"role":"user","content":"x"}],"SECRET MARKER\"<script>":1}`, 400, "unsupported_parameter", nil},
		{"unknown field with a plain name", pre + `"messages":[{"role":"user","content":"x"}],"n":2}`, 400, "unsupported_parameter", "n"},
		{"remote image url", pre + `"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://SECRETMARKER.example/a.png"}}]}]}`, 400, nil, "messages"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := e.chat(tc.body)
			eb := wantEnvelope(t, r, tc.status, "SECRETMARKER", "<script>")
			if eb["code"] != tc.code || eb["param"] != tc.param {
				t.Fatalf("error = %v, want code %v param %v", eb, tc.code, tc.param)
			}
			if strings.Contains(e.tel.logText(), "SECRETMARKER") {
				t.Fatal("a log line quotes request bytes")
			}
		})
	}
	t.Run("wrong content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("SECRETMARKER"))
		req.Header.Set("Content-Type", "text/SECRETMARKER")
		req.Header.Set("Authorization", e.key)
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		wantEnvelope(t, resp{rec}, http.StatusUnsupportedMediaType, "SECRETMARKER")
	})
	t.Run("embeddings", func(t *testing.T) {
		r := e.embed(`{"model":"` + embedModel + `","input":{"SECRETMARKER":1}}`)
		wantEnvelope(t, r, http.StatusBadRequest, "SECRETMARKER")
	})
	t.Run("bad path parameter", func(t *testing.T) {
		r := e.do(http.MethodGet, "/v1/usage/SECRETMARKER/proof", "")
		eb := wantEnvelope(t, r, http.StatusBadRequest, "SECRETMARKER")
		if eb["param"] != "usage_id" {
			t.Fatalf("error = %v", eb)
		}
	})
	t.Run("bad query parameter", func(t *testing.T) {
		r := e.do(http.MethodGet, "/v1/ledger/checkpoint?from_tree_size=SECRETMARKER", "")
		eb := wantEnvelope(t, r, http.StatusBadRequest, "SECRETMARKER")
		if eb["param"] != "from_tree_size" {
			t.Fatalf("error = %v", eb)
		}
	})
}

func TestErrorEnvelopeForNotFoundMethodAndNotImplemented(t *testing.T) {
	e := newEnv(t)
	t.Run("unknown path", func(t *testing.T) {
		wantEnvelope(t, e.do(http.MethodGet, "/v1/nothing-here", ""), http.StatusNotFound)
	})
	t.Run("wrong method", func(t *testing.T) {
		r := e.do(http.MethodGet, "/v1/chat/completions", "")
		wantEnvelope(t, r, http.StatusMethodNotAllowed)
		if r.Header().Get("Allow") == "" {
			t.Error("no Allow header")
		}
	})
	t.Run("an operation whose ticket has not landed", func(t *testing.T) {
		wantEnvelope(t, e.do(http.MethodGet, "/v1/resource-requests", ""), http.StatusNotImplemented)
	})
	t.Run("no key", func(t *testing.T) {
		r := e.doAuth("", http.MethodPost, "/v1/chat/completions", okChat)
		if eb := wantEnvelope(t, r, http.StatusUnauthorized); eb["code"] != "invalid_api_key" {
			t.Fatalf("error = %v", eb)
		}
	})
	t.Run("body too large", func(t *testing.T) {
		e.set("limits.max_body_bytes", "64")
		r := e.chat(`{"model":"` + chatModel + `","messages":[{"role":"user","content":"` + strings.Repeat("x", 200) + `"}]}`)
		eb := wantEnvelope(t, r, http.StatusRequestEntityTooLarge, "xxxxxxxx")
		if eb["code"] != nil {
			t.Fatalf("error = %v, want a null code", eb)
		}
		// A chunked body, whose size the header cannot tell, is cut off the same way.
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"`+chatModel+`","messages":[{"role":"user","content":"`+strings.Repeat("x", 200)+`"}]}`))
		req.ContentLength = -1
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", e.key)
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		wantEnvelope(t, resp{rec}, http.StatusRequestEntityTooLarge, "xxxxxxxx")
	})
	t.Run("body too large on a route that needs no key", func(t *testing.T) {
		e.set("limits.max_body_bytes", "64")
		body := `{"username":"` + strings.Repeat("x", 200) + `","password":"y"}`
		for _, chunked := range []bool{false, true} {
			req := httptest.NewRequest(http.MethodPost, "/v1/session/password", strings.NewReader(body))
			if chunked {
				req.ContentLength = -1
			}
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			e.h.ServeHTTP(rec, req)
			wantEnvelope(t, resp{rec}, http.StatusRequestEntityTooLarge, "xxxxxxxx")
		}
	})
}

// PLT-B-005: the consumer listener serves /v1 only.
func TestListenerPrefixServesOnlyV1(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{
		"/admin/v1/host", "/admin/v1/setup", "/admin/v1/settings", "/admin/v1/session",
		"/admin/v1/", "/admin/v1", "/v1", "/", "/metrics", "/health",
		"/v1/../admin/v1/host", "/%2e%2e/admin/v1/host", "/v1%2f..%2fadmin%2fv1%2fhost",
		"//admin/v1/host", "/ADMIN/V1/HOST",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(method+" "+path, func(t *testing.T) {
				req := httptest.NewRequest(method, "http://consumer.test"+path, strings.NewReader("{}"))
				req.Header.Set("Authorization", e.key)
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				e.h.ServeHTTP(rec, req)
				r := resp{rec}
				if r.Code != http.StatusNotFound && r.Code != http.StatusMethodNotAllowed {
					t.Fatalf("status = %d, want 404; body: %s", r.Code, r.Body.String())
				}
				var env struct {
					Error struct {
						Type string `json:"type"`
					} `json:"error"`
				}
				if err := json.Unmarshal(r.Body.Bytes(), &env); err != nil || env.Error.Type == "" {
					t.Fatalf("not the OpenAI envelope: %s", r.Body.String())
				}
			})
		}
	}
	// The same host serves /v1.
	statusIs(t, e.do(http.MethodGet, "/v1/keys", ""), http.StatusOK)
	statusIs(t, e.chat(okChat), http.StatusOK)
}

func TestListenerPrefixAdminPathsAreNotAuthenticatedEither(t *testing.T) {
	e := newEnv(t)
	// No key, and a key: the answer is the same 404, so the listener says nothing about admin.
	for _, auth := range []string{"", e.key} {
		r := e.doAuth(auth, http.MethodGet, "/admin/v1/host", "")
		wantEnvelope(t, r, http.StatusNotFound)
	}
}
