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

	"github.com/go-faster/jx"
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

// A quota is a list of windowed entries (D-107). The old single-figure Quota object stays as a
// deprecated alias beside the list, because /v1 and /admin/v1 change additively only (D-105).
// Only the read-only QuotaStatus reports when a window resets.
func TestQuotaInputShapeHoldsWindowedEntries(t *testing.T) {
	b, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ogen.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	schemas := spec.Components.Schemas

	entry := schemas["QuotaEntry"]
	if entry == nil {
		t.Fatal("QuotaEntry missing from the spec")
	}
	for _, name := range []string{"metric", "limit", "window_seconds"} {
		if _, ok := findProperty(entry, name); !ok {
			t.Errorf("QuotaEntry has no %s property", name)
		}
		if !slices.Contains(entry.Required, name) {
			t.Errorf("QuotaEntry does not require %s", name)
		}
	}
	if w, _ := findProperty(entry, "window_seconds"); w == nil || string(w.Minimum) != "1" {
		t.Error("QuotaEntry window_seconds has no minimum of 1")
	}

	quota := schemas["Quota"]
	if quota == nil {
		t.Fatal("Quota missing from the spec")
	}
	if !quota.Deprecated {
		t.Error("Quota is not deprecated")
	}
	if _, ok := findProperty(quota, "window_seconds"); ok {
		t.Error("Quota has a window_seconds property; the alias has the host's quota_window")
	}
	for _, name := range []string{"tokens", "requests"} {
		if _, ok := findProperty(quota, name); !ok {
			t.Errorf("Quota has no %s property", name)
		}
	}

	for _, name := range []string{"Quota", "QuotaEntry", "GrantLimits", "AccessKeyNarrowing", "ResourceRequestAsk"} {
		s := schemas[name]
		if s == nil {
			t.Errorf("%s missing from the spec", name)
			continue
		}
		if ap := s.AdditionalProperties; ap == nil || ap.Bool == nil || *ap.Bool {
			t.Errorf("%s is not strict (additionalProperties: false); that changes only with PLT-B-053 (D-105)", name)
		}
		if name == "Quota" || name == "QuotaEntry" {
			continue
		}
		q, ok := findProperty(s, "quota")
		if !ok {
			t.Errorf("%s dropped the deprecated quota property; removing a field is not additive (D-105)", name)
		} else if !q.Deprecated {
			t.Errorf("%s quota is not marked deprecated", name)
		}
		l, ok := findProperty(s, "quotas")
		if !ok || l.Type != "array" {
			t.Errorf("%s has no quotas list", name)
		}
		if slices.Contains(s.Required, "quotas") {
			t.Errorf("%s requires quotas; an older client that sends only quota must keep working", name)
		}
	}
	// Loosened, never tightened (D-105): a client that sends only quotas is a valid grant body.
	if slices.Contains(schemas["GrantLimits"].Required, "quota") {
		t.Error("GrantLimits requires the deprecated quota")
	}

	if props, ok := findProperty(schemas["AccessKey"], "warnings"); !ok || props.Type != "array" {
		t.Error("AccessKey has no warnings list")
	}
	for _, name := range []string{"Consumer", "ConsumerCreate", "ConsumerUpdate"} {
		if _, ok := findProperty(schemas[name], "billing_mode"); !ok {
			t.Errorf("%s has no billing_mode", name)
		}
	}
	if bm := schemas["BillingMode"]; bm == nil || len(bm.Enum) != 3 {
		t.Error("BillingMode is not none, prepaid or postpaid")
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

// A body may carry the deprecated quota, the quotas list or both (the host decides which wins and
// warns); an entry needs its three fields, and a window below one second is refused (D-107).
func TestQuotaRequestBodiesDecode(t *testing.T) {
	entry := `{"metric":"tokens","limit":5,"window_seconds":60}`

	var ask ResourceRequestAsk
	if err := ask.Decode(jx.DecodeStr(`{"quota":{"tokens":5},"quotas":[` + entry + `]}`)); err != nil {
		t.Errorf("ResourceRequestAsk refused quota beside quotas: %v", err)
	}
	if len(ask.Quotas) != 1 || ask.Quotas[0].WindowSeconds != 60 {
		t.Errorf("ask quotas = %+v", ask.Quotas)
	}
	for _, bad := range []string{`{"quotas":[{"tokens":5}]}`, `{"quotas":[{"metric":"tokens","limit":5,"window_seconds":0}]}`,
		`{"quotas":[{"metric":"jobs","limit":5,"window_seconds":60}]}`, `{"quotas":[{"metric":"tokens","limit":-1,"window_seconds":60}]}`} {
		var a ResourceRequestAsk
		err := a.Decode(jx.DecodeStr(bad))
		if err == nil {
			err = a.Validate()
		}
		if err == nil {
			t.Errorf("ResourceRequestAsk accepted %s", bad)
		}
	}

	var limits GrantLimits
	body := `{"models":[],"concurrency":1,"on_context_overflow":"refuse","allowed_request_properties":[],"quotas":[` + entry + `]}`
	if err := limits.Decode(jx.DecodeStr(body)); err != nil {
		t.Errorf("GrantLimits refused a body with only quotas: %v", err)
	}
	old := `{"models":[],"quota":null,"concurrency":1,"on_context_overflow":"refuse","allowed_request_properties":[]}`
	if err := new(GrantLimits).Decode(jx.DecodeStr(old)); err != nil {
		t.Errorf("GrantLimits refused an older client's body: %v", err)
	}
}
