package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ogen-go/ogen"
	"github.com/ogen-go/ogen/ogenerrors"
)

// The contract test guards properties of api/openapi.yaml that the generated
// server must keep: D-32 says unsupported OpenAI fields are rejected, never
// silently ignored, and D-33 says the receipt travels in LS-Receipt.

type allowAll struct{}

func (allowAll) HandleAccessKeyAuth(ctx context.Context, _ OperationName, _ AccessKeyAuth) (context.Context, error) {
	return ctx, nil
}

func (allowAll) HandleAdminSessionAuth(ctx context.Context, _ OperationName, _ AdminSessionAuth) (context.Context, error) {
	return ctx, nil
}

func (allowAll) HandleConsumerSessionAuth(ctx context.Context, _ OperationName, _ ConsumerSessionAuth) (context.Context, error) {
	return ctx, nil
}

type chatStub struct {
	UnimplementedHandler
	called bool
}

func (h *chatStub) CreateChatCompletion(_ context.Context, req *ChatCompletionRequest, _ CreateChatCompletionParams) (CreateChatCompletionRes, error) {
	h.called = true
	return &ChatCompletionHeaders{
		LSReceipt: NewOptString("aGVhZGVy.cGF5bG9hZA.c2ln"),
		Response: ChatCompletion{
			ID:      "c1",
			Object:  "chat.completion",
			Model:   req.Model,
			Choices: []ChatCompletionChoice{{Message: ChatCompletionMessage{Role: ChatCompletionMessageRoleAssistant}, FinishReason: FinishReasonStop}},
		},
	}, nil
}

func newTestServer(t *testing.T, h Handler) (*Server, *error) {
	t.Helper()
	var decodeErr error
	srv, err := NewServer(h, allowAll{}, WithErrorHandler(func(_ context.Context, w http.ResponseWriter, _ *http.Request, err error) {
		decodeErr = err
		w.WriteHeader(http.StatusBadRequest)
	}))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv, &decodeErr
}

func post(srv http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer ls_abc_secret")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestUnsupportedOpenAIFieldsAreRejected(t *testing.T) {
	cases := []struct {
		name, path, body, field string
	}{
		{"chat n", "/v1/chat/completions", `{"model":"m","messages":[{"role":"user","content":"hi"}],"n":2}`, "n"},
		{"chat logit_bias", "/v1/chat/completions", `{"model":"m","messages":[{"role":"user","content":"hi"}],"logit_bias":{}}`, "logit_bias"},
		{"chat tool_choice", "/v1/chat/completions", `{"model":"m","messages":[{"role":"user","content":"hi"}],"tool_choice":"auto"}`, "tool_choice"},
		{"message name", "/v1/chat/completions", `{"model":"m","messages":[{"role":"user","content":"hi","name":"x"}]}`, "name"},
		{"embeddings user", "/v1/embeddings", `{"model":"m","input":"hi","user":"u"}`, "user"},
		{"job n", "/v1/jobs", `{"model":"m","messages":[{"role":"user","content":"hi"}],"n":2}`, "n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &chatStub{}
			srv, decodeErr := newTestServer(t, h)
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer ls_abc_secret")
			req.Header.Set("Idempotency-Key", "k1")
			srv.ServeHTTP(httptest.NewRecorder(), req)
			if h.called {
				t.Fatal("handler ran; an unsupported field must be rejected before it")
			}
			var de *ogenerrors.DecodeRequestError
			if !errors.As(*decodeErr, &de) {
				t.Fatalf("want a request decode error, got %v", *decodeErr)
			}
			if !strings.Contains((*decodeErr).Error(), `"`+tc.field+`"`) {
				t.Fatalf("error %q does not name field %q", *decodeErr, tc.field)
			}
		})
	}
}

func TestSupportedChatRequestReachesHandlerAndCarriesReceipt(t *testing.T) {
	h := &chatStub{}
	srv, decodeErr := newTestServer(t, h)
	rec := post(srv, "/v1/chat/completions",
		`{"model":"m","messages":[{"role":"system","content":"be brief"},{"role":"user","content":[{"type":"text","text":"hi"}]}],"stream_options":{"include_usage":true},"max_tokens":16}`)
	if *decodeErr != nil {
		t.Fatalf("unexpected decode error: %v", *decodeErr)
	}
	if !h.called || rec.Code != http.StatusOK {
		t.Fatalf("handler called=%v status=%d", h.called, rec.Code)
	}
	if got := rec.Header().Get("LS-Receipt"); got == "" {
		t.Fatal("LS-Receipt header missing")
	}
}

func TestSupportedChatRequestAcceptsDataImageURL(t *testing.T) {
	h := &chatStub{}
	srv, decodeErr := newTestServer(t, h)
	rec := post(srv, "/v1/chat/completions",
		`{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`)
	if *decodeErr != nil {
		t.Fatalf("unexpected decode error: %v", *decodeErr)
	}
	if !h.called || rec.Code != http.StatusOK {
		t.Fatalf("handler called=%v status=%d", h.called, rec.Code)
	}
}

func TestRemoteImageURLRejected(t *testing.T) {
	h := &chatStub{}
	srv, decodeErr := newTestServer(t, h)
	post(srv, "/v1/chat/completions",
		`{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`)
	if h.called {
		t.Fatal("handler ran; a remote image URL must be rejected before it")
	}
	if *decodeErr == nil {
		t.Fatal("want a decode error for a remote image URL")
	}
}

// streamStub answers a request like the gateway does for a stream: no receipt header,
// because the receipt travels in the SSE comment line before `[DONE]` (D-27, D-39, D-78, PLT-B-007).
type streamStub struct{ chatStub }

func (h *streamStub) CreateChatCompletion(_ context.Context, _ *ChatCompletionRequest, _ CreateChatCompletionParams) (CreateChatCompletionRes, error) {
	return &CreateChatCompletionOKTextEventStreamHeaders{Response: CreateChatCompletionOKTextEventStream{Data: strings.NewReader("data: [DONE]\n\n")}}, nil
}

func TestStreamedChatHasNoReceiptHeader(t *testing.T) {
	srv, decodeErr := newTestServer(t, &streamStub{})
	rec := post(srv, "/v1/chat/completions", `{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if *decodeErr != nil {
		t.Fatalf("unexpected decode error: %v", *decodeErr)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q; want text/event-stream", ct)
	}
	if v := rec.Header().Values("LS-Receipt"); len(v) != 0 {
		t.Fatalf("a streamed response must not carry LS-Receipt, got %q", v)
	}
}

// getJobResult keeps a required receipt header: a job result is never streamed (D-78).
func TestJobResultReceiptHeaderIsRequired(t *testing.T) {
	var _ func(JobResultHeaders) string = func(h JobResultHeaders) string { return h.LSReceipt }
}

// The JobResult description promises "the same shape as ChatCompletion". The schema is a copy
// rather than a $ref because of I-136, so this test keeps the two from drifting apart.
func TestJobResultMatchesChatCompletion(t *testing.T) {
	b, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ogen.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	cc, jr := spec.Components.Schemas["ChatCompletion"], spec.Components.Schemas["JobResult"]
	if cc == nil || jr == nil {
		t.Fatalf("ChatCompletion or JobResult missing from the spec (got %v, %v)", cc != nil, jr != nil)
	}
	ccp, err := json.Marshal(cc.Properties)
	if err != nil {
		t.Fatal(err)
	}
	jrp, err := json.Marshal(jr.Properties)
	if err != nil {
		t.Fatal(err)
	}
	if string(ccp) != string(jrp) {
		t.Errorf("JobResult properties differ from ChatCompletion:\n got  %s\n want %s", jrp, ccp)
	}
	ccr, jrr := slices.Clone(cc.Required), slices.Clone(jr.Required)
	slices.Sort(ccr)
	slices.Sort(jrr)
	if !slices.Equal(ccr, jrr) {
		t.Errorf("JobResult required = %v; ChatCompletion required = %v", jrr, ccr)
	}
}

// The input schemas for a quota hold what storage holds (D-25, D-98): one token figure and one
// request figure, with the window length a host setting, not a client choice. Only the read-only
// QuotaStatus reports the window and when it resets.
func TestQuotaInputShapeMatchesStorage(t *testing.T) {
	b, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ogen.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	schemas := spec.Components.Schemas

	quota := schemas["Quota"]
	if quota == nil {
		t.Fatal("Quota missing from the spec")
	}
	if _, ok := findProperty(quota, "window_seconds"); ok {
		t.Error("Quota has a window_seconds property; the window is the host setting limits.quota_window")
	}
	if slices.Contains(quota.Required, "window_seconds") {
		t.Error("Quota requires window_seconds")
	}
	for _, name := range []string{"tokens", "requests"} {
		if _, ok := findProperty(quota, name); !ok {
			t.Errorf("Quota has no %s property", name)
		}
	}

	for _, name := range []string{"GrantLimits", "AccessKeyNarrowing", "ResourceRequestAsk"} {
		s := schemas[name]
		if s == nil {
			t.Errorf("%s missing from the spec", name)
			continue
		}
		if _, ok := findProperty(s, "quota"); !ok {
			t.Errorf("%s has no quota property", name)
		}
		if _, ok := findProperty(s, "quotas"); ok {
			t.Errorf("%s still has a quotas list; storage holds one quota", name)
		}
	}

	status := schemas["QuotaStatus"]
	if status == nil {
		t.Fatal("QuotaStatus missing from the spec")
	}
	for _, name := range []string{"window_seconds", "resets_at"} {
		if _, ok := findProperty(status, name); !ok {
			t.Errorf("QuotaStatus has no %s property", name)
		}
		if !slices.Contains(status.Required, name) {
			t.Errorf("QuotaStatus does not require %s", name)
		}
	}
}

func findProperty(s *ogen.Schema, name string) (*ogen.Schema, bool) {
	for _, p := range s.Properties {
		if p.Name == name {
			return p.Schema, true
		}
	}
	return nil, false
}
