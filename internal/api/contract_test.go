package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
		LSReceipt: "aGVhZGVy.cGF5bG9hZA.c2ln",
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
