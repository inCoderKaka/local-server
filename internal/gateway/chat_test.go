package gateway_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/engine/fake"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/scheduler"
)

func TestGoldenChatCompletion(t *testing.T) {
	e := newEnv(t)
	r := e.chat(okChat)
	statusIs(t, r, http.StatusOK)
	golden(t, "chat_completion.json", stable(r.Body.Bytes()))
	if ct := r.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	if r.Header().Get("X-Request-Id") == "" {
		t.Error("no X-Request-Id")
	}
}

func TestGoldenEmbeddings(t *testing.T) {
	e := newEnv(t)
	r := e.embed(`{"model":"` + embedModel + `","input":["alpha","beta gamma"]}`)
	statusIs(t, r, http.StatusOK)
	golden(t, "embeddings.json", stable(r.Body.Bytes()))
	r = e.embed(`{"model":"` + embedModel + `","input":"alpha","encoding_format":"base64"}`)
	statusIs(t, r, http.StatusOK)
	golden(t, "embeddings_base64.json", stable(r.Body.Bytes()))
}

func TestChatReceiptHeaderIsPresentAndVerifies(t *testing.T) {
	e := newEnv(t)
	r := e.chat(okChat)
	statusIs(t, r, http.StatusOK)
	jws := r.Header().Get("LS-Receipt")
	if jws == "" {
		t.Fatal("LS-Receipt header missing")
	}
	rc := e.verify(jws)
	if rc.Metering != ledger.Metered || rc.LeafIndex == nil || rc.TreeSize == nil || *rc.TreeSize != *rc.LeafIndex+1 {
		t.Fatalf("receipt = %+v, want a metered receipt with a leaf", rc)
	}
	if rc.Operation != ledger.OpChatCompletions || rc.Model != chatModel || rc.ConsumerID != e.cons.ID || rc.AccessKeyID != e.keyID || rc.Outcome != ledger.OutcomeCompleted {
		t.Fatalf("receipt fields = %+v", rc)
	}
	body := r.json()
	usage := body["usage"].(map[string]any)
	if int64(usage["prompt_tokens"].(float64)) != rc.PromptTokens || int64(usage["completion_tokens"].(float64)) != rc.CompletionTokens || rc.PromptTokens != 2 {
		t.Fatalf("usage %v and receipt %+v disagree", usage, rc)
	}
	if rc.TotalTokens != rc.PromptTokens+rc.CompletionTokens || !strings.HasSuffix(body["id"].(string), rc.UsageID) {
		t.Fatalf("receipt %+v, body id %v", rc, body["id"])
	}
	if n := e.useRecords(); n != 1 {
		t.Fatalf("use_record rows = %d, want 1", n)
	}
}

func TestEmbeddingsReceiptHeaderIsPresentAndVerifies(t *testing.T) {
	e := newEnv(t)
	r := e.embed(`{"model":"` + embedModel + `","input":"alpha beta"}`)
	statusIs(t, r, http.StatusOK)
	rc := e.verify(r.Header().Get("LS-Receipt"))
	if rc.Operation != ledger.OpEmbeddings || rc.PromptTokens != 2 || rc.CompletionTokens != 0 || rc.Metering != ledger.Metered {
		t.Fatalf("receipt = %+v", rc)
	}
}

func TestUnsupportedFieldsAreRejectedWithATypedError(t *testing.T) {
	e := newEnv(t)
	for _, field := range []string{`"n":2`, `"logit_bias":{}`, `"tool_choice":"auto"`, `"user":"u"`, `"logprobs":true`} {
		name := strings.Split(strings.Trim(field, `"`), `"`)[0]
		t.Run(name, func(t *testing.T) {
			r := e.chat(`{"model":"` + chatModel + `","messages":[{"role":"user","content":"hi"}],` + field + `}`)
			statusIs(t, r, http.StatusBadRequest)
			eb := r.errBody()
			if eb["code"] != "unsupported_parameter" || eb["param"] != name || eb["type"] != "invalid_request_error" {
				t.Fatalf("error = %v", eb)
			}
		})
	}
	r := e.embed(`{"model":"` + embedModel + `","input":"hi","user":"u"}`)
	statusIs(t, r, http.StatusBadRequest)
	if eb := r.errBody(); eb["code"] != "unsupported_parameter" || eb["param"] != "user" {
		t.Fatalf("error = %v", eb)
	}
	if n := e.eng.Calls(fake.OpChat) + e.eng.Calls(fake.OpEmbed); n != 0 {
		t.Fatalf("engine was called %d times for rejected requests", n)
	}
	if n := e.useRecords(); n != 0 {
		t.Fatalf("rejected requests wrote %d usage records", n)
	}
}

// Fields the spec lists but the engine port cannot carry yet are refused, never dropped (D-32).
func TestFieldsTheEngineCannotCarryAreRefusedNotIgnored(t *testing.T) {
	e := newEnv(t)
	pre := `{"model":"` + chatModel + `","messages":[{"role":"user","content":"hi"}],`
	for _, tc := range []struct{ name, body string }{
		{"temperature", pre + `"temperature":0.2}`},
		{"top_p", pre + `"top_p":0.5}`},
		{"seed", pre + `"seed":1}`},
		{"stop", pre + `"stop":"x"}`},
		{"frequency_penalty", pre + `"frequency_penalty":1}`},
		{"presence_penalty", pre + `"presence_penalty":1}`},
		{"tools", pre + `"tools":[{"type":"function","function":{"name":"f"}}]}`},
		{"response_format", pre + `"response_format":{"type":"json_object"}}`},
		{"stream", pre + `"stream":true}`},
		{"messages", `{"model":"` + chatModel + `","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`},
		{"messages", `{"model":"` + chatModel + `","messages":[{"role":"tool","content":"x","tool_call_id":"1"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := e.chat(tc.body)
			statusIs(t, r, http.StatusBadRequest)
			if eb := r.errBody(); eb["code"] != "unsupported_parameter" || eb["param"] != tc.name {
				t.Fatalf("error = %v", eb)
			}
		})
	}
	// A null value and the default response format are not a request for anything.
	statusIs(t, e.chat(pre+`"temperature":null,"response_format":{"type":"text"}}`), http.StatusOK)
	r := e.embed(`{"model":"` + embedModel + `","input":"hi","dimensions":2}`)
	statusIs(t, r, http.StatusBadRequest)
	if eb := r.errBody(); eb["code"] != "unsupported_parameter" || eb["param"] != "dimensions" {
		t.Fatalf("error = %v", eb)
	}
}

func TestMaxTokensIsCapped(t *testing.T) {
	cap100 := func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			for i := range models {
				models[i].MaxOutputTokens = i64(100)
			}
			return grantsNewVersion{Models: models}
		}
	}
	e := newEnv(t, cap100)
	last := func() int {
		e.eng.mu.Lock()
		defer e.eng.mu.Unlock()
		return e.eng.chats[len(e.eng.chats)-1].NumPredict
	}
	pre := `{"model":"` + chatModel + `","messages":[{"role":"user","content":"hi"}]`
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"above the grant cap", pre + `,"max_tokens":5000}`, 100},
		{"max_completion_tokens", pre + `,"max_completion_tokens":5000}`, 100},
		{"below the cap", pre + `,"max_tokens":7}`, 7},
		{"absent uses the default", pre + `}`, 100}, // default 1024, capped to 100
	} {
		t.Run(tc.name, func(t *testing.T) {
			statusIs(t, e.chat(tc.body), http.StatusOK)
			if got := last(); got != tc.want {
				t.Fatalf("num_predict = %d, want %d", got, tc.want)
			}
		})
	}
	r := e.chat(pre + `,"max_tokens":5,"max_completion_tokens":5}`)
	statusIs(t, r, http.StatusBadRequest)
	if eb := r.errBody(); eb["param"] != "max_completion_tokens" {
		t.Fatalf("error = %v", eb)
	}
}

func TestHostCeilingAppliesWhenTheGrantSetsNoCap(t *testing.T) {
	e := newEnv(t)
	e.set("gateway.max_tokens_ceiling", "50")
	e.set("gateway.default_max_tokens", "30")
	statusIs(t, e.chat(strings.Replace(okChat, `}]}`, `}],"max_tokens":9999}`, 1)), http.StatusOK)
	statusIs(t, e.chat(okChat), http.StatusOK)
	e.eng.mu.Lock()
	defer e.eng.mu.Unlock()
	if a, b := e.eng.chats[0].NumPredict, e.eng.chats[1].NumPredict; a != 50 || b != 30 {
		t.Fatalf("num_predict = %d and %d, want 50 and 30", a, b)
	}
}

func TestPromptSizePrecheckAndOverflowPolicy(t *testing.T) {
	long := strings.Repeat("word ", 400) // 2000 bytes: about 500 tokens at 4 bytes per token
	body := `{"model":"` + chatModel + `","messages":[{"role":"user","content":"` + long + `"}]}`
	small := func(overflow string) func(*envOpts) {
		return func(o *envOpts) {
			o.version = func(models []grantsModel) grantsNewVersion {
				for i := range models {
					models[i].ContextCap = i64(100)
				}
				return grantsNewVersion{Models: models, OnContextOverflow: overflow}
			}
		}
	}
	e := newEnv(t, small("refuse"))
	r := e.chat(body)
	statusIs(t, r, http.StatusBadRequest)
	if eb := r.errBody(); eb["code"] != "context_too_long" || eb["param"] != "messages" {
		t.Fatalf("error = %v", eb)
	}
	if e.eng.Calls(fake.OpChat) != 0 {
		t.Fatal("the pre-check should refuse before the engine runs")
	}
	// A short prompt passes, and the context cap is what the engine is told.
	statusIs(t, e.chat(okChat), http.StatusOK)
	if got := e.eng.chats[0].NumCtx; got != 100 {
		t.Fatalf("num_ctx = %d, want the grant's cap 100", got)
	}
	if got := e.eng.chats[0].OnContextOverflow; got != "refuse" {
		t.Fatalf("overflow = %q", got)
	}
	// Embeddings use the same check.
	r = e.embed(`{"model":"` + embedModel + `","input":"` + long + `"}`)
	statusIs(t, r, http.StatusBadRequest)
	if eb := r.errBody(); eb["code"] != "context_too_long" || eb["param"] != "input" {
		t.Fatalf("error = %v", eb)
	}

	// With truncate the engine may drop old messages, so nothing is refused up front.
	e2 := newEnv(t, small("truncate"))
	statusIs(t, e2.chat(body), http.StatusOK)
	if got := e2.eng.chats[0].OnContextOverflow; got != "truncate" {
		t.Fatalf("overflow = %q, want truncate", got)
	}
}

func TestEngineRefusalOfAnOversizedPromptIsContextTooLong(t *testing.T) {
	e := newEnv(t)
	e.set("gateway.default_context_tokens", "4")
	// 5 words fit the byte estimate (4*4 bytes of context) only if the estimate is generous; the
	// fake engine counts words, so a prompt of 5 short words is over its 4-token context.
	r := e.chat(`{"model":"` + chatModel + `","messages":[{"role":"user","content":"a b c d e"}]}`)
	statusIs(t, r, http.StatusBadRequest)
	if eb := r.errBody(); eb["code"] != "context_too_long" {
		t.Fatalf("error = %v", eb)
	}
}

func TestEngineDownIs503EngineUnavailable(t *testing.T) {
	e := newEnv(t)
	e.eng.InjectError(fake.OpChat, engineDown(), 1)
	r := e.chat(okChat)
	statusIs(t, r, http.StatusServiceUnavailable)
	if eb := r.errBody(); eb["code"] != "engine_unavailable" || eb["type"] != "server_error" {
		t.Fatalf("error = %v", eb)
	}
	if r.Header().Get("Retry-After") != "" {
		t.Error("engine_unavailable has no Retry-After")
	}
	if n := e.useRecords(); n != 0 {
		t.Fatalf("a failed call wrote %d usage records", n)
	}
	// The slot was released: the next call works.
	statusIs(t, e.chat(okChat), http.StatusOK)
}

func TestModelOutsideTheGrantIs403ModelNotGranted(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {})
	if err := e.db.Write(context.Background(), func(tx storageTx) error {
		_, err := e.gr.Models.Create(tx, "not-installed")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// The model exists in the catalogue but is not in the grant: model_not_granted.
	r := e.chat(`{"model":"not-installed","messages":[{"role":"user","content":"hi"}]}`)
	statusIs(t, r, http.StatusForbidden)
	if eb := r.errBody(); eb["code"] != "model_not_granted" || eb["param"] != "model" {
		t.Fatalf("error = %v", eb)
	}
}

func TestEngineErrorsMapToTheirStatuses(t *testing.T) {
	e := newEnv(t)
	for _, tc := range []struct {
		code   engine.Code
		status int
		api    any
	}{
		{engine.CodeEngineUnavailable, http.StatusServiceUnavailable, "engine_unavailable"},
		{engine.CodeUnsupportedVersion, http.StatusServiceUnavailable, "engine_unavailable"},
		{engine.CodeContextTooLong, http.StatusBadRequest, "context_too_long"},
		{engine.CodeModelNotFound, http.StatusNotFound, nil},
		{engine.CodeInvalidRequest, http.StatusBadRequest, nil},
		{engine.CodeTimeout, http.StatusGatewayTimeout, nil},
		{engine.CodeEngineError, http.StatusBadGateway, nil},
	} {
		t.Run(string(tc.code), func(t *testing.T) {
			e.eng.InjectError(fake.OpChat, engine.Errorf(tc.code, nil, "injected SECRETMARKER"), 1)
			r := e.chat(okChat)
			eb := wantEnvelope(t, r, tc.status, "SECRETMARKER")
			if eb["code"] != tc.api {
				t.Fatalf("error = %v, want code %v", eb, tc.api)
			}
			e.eng.InjectError(fake.OpEmbed, engine.Errorf(tc.code, nil, "injected"), 1)
			statusIs(t, e.embed(`{"model":"`+embedModel+`","input":"x"}`), tc.status)
		})
	}
	if n := e.useRecords(); n != 0 {
		t.Fatalf("failed calls wrote %d usage records", n)
	}
	// A stream the engine breaks half way is an engine error, not a short answer.
	e.eng.InjectError(fake.OpChatStream, engine.Errorf(engine.CodeEngineError, nil, "broke"), 1)
	statusIs(t, e.chat(okChat), http.StatusBadGateway)
}

func TestConcurrentCallsAreAllMeteredInOrder(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyTotalConcurrency, "8")
	e.set(scheduler.KeyOllamaNumParallel, "8")
	e.set(scheduler.KeyGrantConcurrency, "8")
	const n = 16
	var wg sync.WaitGroup
	leaves := make(chan int64, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := e.chat(okChat)
			if r.Code != http.StatusOK {
				t.Errorf("status %d: %s", r.Code, r.Body.String())
				return
			}
			leaves <- *e.verify(r.Header().Get("LS-Receipt")).LeafIndex
		}()
	}
	wg.Wait()
	close(leaves)
	seen := map[int64]bool{}
	for l := range leaves {
		seen[l] = true
	}
	if len(seen) != n {
		t.Fatalf("distinct leaf indexes = %d, want %d", len(seen), n)
	}
	if got := e.useRecords(); got != n {
		t.Fatalf("use_record rows = %d, want %d", got, n)
	}
	secret := strings.TrimPrefix(e.key, "Bearer ")
	if strings.Contains(e.tel.logText(), secret[len(secret)-40:]) {
		t.Fatal("a log line holds the access key secret")
	}
}
