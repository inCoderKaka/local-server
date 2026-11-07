package gateway_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/gateway"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
)

// D-39 step 9, D-51: persistent metering failure still answers, with the signed unmetered receipt.
func TestMeteringFailureStillAnswersWithAnUnmeteredReceipt(t *testing.T) {
	e := newEnv(t)
	e.meter.set(-1)
	r := e.chat(okChat)
	statusIs(t, r, http.StatusOK)
	jws := r.Header().Get("LS-Receipt")
	if jws == "" {
		t.Fatal("LS-Receipt header missing on an unmetered answer")
	}
	rc := e.verify(jws)
	if rc.Metering != ledger.Unmetered || rc.LeafIndex != nil || rc.TreeSize != nil {
		t.Fatalf("receipt = %+v, want unmetered with no leaf_index or tree_size", rc)
	}
	if rc.PromptTokens != 2 || rc.CompletionTokens != 9 || rc.Operation != ledger.OpChatCompletions {
		t.Fatalf("receipt = %+v, want the engine's token counts", rc)
	}
	if !strings.HasSuffix(r.json()["id"].(string), rc.UsageID) {
		t.Fatalf("completion id %v does not carry the receipt's usage id %s", r.json()["id"], rc.UsageID)
	}
	if got := e.meter.count(); got != 3 {
		t.Fatalf("metering attempts = %d, want the default 3", got)
	}
	if n := e.useRecords(); n != 0 {
		t.Fatalf("use_record rows = %d, want 0", n)
	}
	if got := e.tel.count("gateway_metering_failures_total", ledger.OpChatCompletions); got != 1 {
		t.Fatalf("metering failure counter = %v, want 1", got)
	}
	if !strings.Contains(e.tel.logText(), "metering failed") || !strings.Contains(e.tel.logText(), rc.UsageID) {
		t.Fatalf("the failure is not logged with the usage id:\n%s", e.tel.logText())
	}
	if strings.Contains(e.tel.logText(), "hello there") || strings.Contains(e.tel.logText(), "deterministic reply") {
		t.Fatal("a log line holds content")
	}
	evs := e.events("metering.failed")
	if len(evs) != 1 {
		t.Fatalf("metering.failed events = %d, want 1", len(evs))
	}
	if p := string(evs[0].Payload); !strings.Contains(p, rc.UsageID) || strings.Contains(p, "hello") {
		t.Fatalf("payload = %s", p)
	}
	// The unmetered usage id has no ledger leaf, so there is no proof (404).
	statusIs(t, e.do(http.MethodGet, "/v1/usage/"+rc.UsageID+"/proof", ""), http.StatusNotFound)

	// Embeddings answer the same way.
	er := e.embed(`{"model":"` + embedModel + `","input":"alpha"}`)
	statusIs(t, er, http.StatusOK)
	if rc := e.verify(er.Header().Get("LS-Receipt")); rc.Metering != ledger.Unmetered {
		t.Fatalf("embeddings receipt = %+v", rc)
	}
	if len(e.events("metering.failed")) != 2 {
		t.Fatalf("metering.failed events = %d, want 2", len(e.events("metering.failed")))
	}
}

func TestMeteringRetriesInMemoryBeforeGivingUp(t *testing.T) {
	e := newEnv(t)
	e.meter.set(2) // two failures, then the write works
	r := e.chat(okChat)
	statusIs(t, r, http.StatusOK)
	if rc := e.verify(r.Header().Get("LS-Receipt")); rc.Metering != ledger.Metered || rc.LeafIndex == nil {
		t.Fatalf("receipt = %+v, want a metered receipt after the retries", rc)
	}
	if got := e.meter.count(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
	if len(e.events("metering.failed")) != 0 || e.tel.count("gateway_metering_failures_total", ledger.OpChatCompletions) != 0 {
		t.Fatal("a recovered write must not alert")
	}

	// The attempt count is a setting.
	e.set(gateway.KeyMeteringAttempts, "1")
	e.meter.set(-1)
	before := e.meter.count()
	r = e.chat(okChat)
	statusIs(t, r, http.StatusOK)
	if got := e.meter.count() - before; got != 1 {
		t.Fatalf("attempts = %d, want 1 after setting the attempts to 1", got)
	}
	if rc := e.verify(r.Header().Get("LS-Receipt")); rc.Metering != ledger.Unmetered {
		t.Fatalf("receipt = %+v", rc)
	}
}

func TestMeteredCallsAreInTheLedgerAndTheNearLimitCheckRuns(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			return grantsNewVersion{Models: models, Quotas: dayQuotas(grants.MetricTokens, 100)}
		}
	})
	e.set(limits.KeyNearLimitPercent, "30")
	for i := 0; i < 3; i++ {
		statusIs(t, e.chat(okChat), http.StatusOK) // 11 tokens each
		want := 0
		if i == 2 {
			want = 1 // 33 of 100 tokens is past 30 percent; 22 was not
		}
		if got := len(e.events(events.QuotaNearLimit)); got != want {
			t.Fatalf("after call %d: quota.near_limit events = %d, want %d", i+1, got, want)
		}
	}
	if n := e.useRecords(); n != 3 {
		t.Fatalf("use_record rows = %d, want 3", n)
	}
	n, err := e.cps.LeafCount(t.Context())
	if err != nil || n != 3 {
		t.Fatalf("ledger leaves = %d, %v; want 3", n, err)
	}
}

// req_001 D-16, D-51: no answer goes out without a signed receipt, so when the unmetered receipt
// cannot be signed either the call answers 500 (D-51 overrides D-39 step 9's "still returned"
// here); the failure is still alerted once.
func TestUnsignableReceiptStillEmitsMeteringFailed(t *testing.T) {
	e := newEnv(t)
	e.meter.set(-1)
	e.meter.mu.Lock()
	e.meter.signFails = true
	e.meter.mu.Unlock()
	r := e.chat(okChat)
	statusIs(t, r, http.StatusInternalServerError)
	if got := len(e.events("metering.failed")); got != 1 {
		t.Fatalf("metering.failed events = %d, want 1", got)
	}
	if got := e.tel.count("gateway_metering_failures_total", ledger.OpChatCompletions); got != 1 {
		t.Fatalf("metering failure counter = %v, want 1", got)
	}
}

// cancelAfter cancels the request once n content chunks were read.
type cancelAfter struct {
	engine.ChatStream
	n      int
	cancel context.CancelFunc
	seen   int
}

func (c *cancelAfter) Recv() (engine.ChatChunk, error) {
	ch, err := c.ChatStream.Recv()
	if err == nil && ch.Content != "" {
		c.seen++
		if c.seen == c.n {
			c.cancel()
		}
	}
	return ch, err
}

// slowStream waits before each Recv, so a short engine timeout expires mid-call.
type slowStream struct{ engine.ChatStream }

func (s slowStream) Recv() (engine.ChatChunk, error) {
	time.Sleep(50 * time.Millisecond)
	return s.ChatStream.Recv()
}

// D-38 interactive-request machine, D-108: a client disconnect during the engine call is metered
// as aborted with the completion tokens so far and an estimated prompt (okChat's prompt is 11
// bytes, 3 tokens at the default 4 bytes per token); the receipt marks the prompt as estimated.
func TestClientDisconnectMetersAbortedChat(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.eng.wrapChat = func(s engine.ChatStream) engine.ChatStream { return &cancelAfter{ChatStream: s, n: 3, cancel: cancel} }
	r := e.doCtx(ctx, "/v1/chat/completions", okChat)
	statusIs(t, r, 499)
	if n := e.useRecords(); n != 1 {
		t.Fatalf("use_record rows = %d, want 1", n)
	}
	var outcome string
	var prompt, completion int64
	err := e.db.Reader().QueryRow(context.Background(),
		`SELECT outcome, prompt_tokens, completion_tokens FROM use_record`).Scan(&outcome, &prompt, &completion)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != string(ledger.OutcomeAborted) || prompt != 3 || completion != 3 {
		t.Fatalf("row = %s, %d prompt, %d completion; want aborted, 3, 3", outcome, prompt, completion)
	}
	e.wantMarked(t, true)
}

// wantMarked checks the latest committed record's usage and receipt: the prompt count is marked
// as estimated or not, on the Usage and on the signed receipt.
func (e *env) wantMarked(t *testing.T, want bool) {
	t.Helper()
	// The meter appends to its in-memory record after the row is committed, and some callers meter
	// on another goroutine, so wait until the meter has seen every committed row.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if n := e.meter.recordedLen(); n > 0 && int64(n) >= e.useRecords() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the committed record did not reach the test meter")
		}
		time.Sleep(10 * time.Millisecond)
	}
	u, jws, ok := e.meter.lastRecorded()
	if !ok {
		t.Fatal("no record was committed")
	}
	if u.PromptTokensEstimated != want {
		t.Fatalf("usage marker = %v, want %v", u.PromptTokensEstimated, want)
	}
	rc := e.verify(jws)
	if rc.PromptTokensEstimated != want {
		t.Fatalf("receipt marker = %v, want %v", rc.PromptTokensEstimated, want)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.Split(jws, ".")[1])
	if err != nil {
		t.Fatal(err)
	}
	if has := strings.Contains(string(raw), "prompt_tokens_estimated"); has != want {
		t.Fatalf("receipt payload names the marker = %v, want %v: %s", has, want, raw)
	}
}

// The estimate follows gateway.chars_per_token (req_001 D-108, D-38): 11 bytes are 11 tokens at 1
// byte per token, 6 at 2, 3 at 4 (the default) and 1 at 20; the setting is read for each call.
func TestAbortedPromptEstimateFollowsTheSetting(t *testing.T) {
	e := newEnv(t)
	for i, c := range []struct {
		perToken string
		want     int64
	}{{"1", 11}, {"2", 6}, {"20", 1}} {
		e.set(gateway.KeyCharsPerToken, c.perToken)
		ctx, cancel := context.WithCancel(context.Background())
		e.eng.wrapChat = func(s engine.ChatStream) engine.ChatStream { return &cancelAfter{ChatStream: s, n: 2, cancel: cancel} }
		statusIs(t, e.doCtx(ctx, "/v1/chat/completions", okChat), 499)
		cancel()
		rows := e.useRows()
		if len(rows) != i+1 || rows[i] != (useRow{ledger.OutcomeAborted, c.want, 2}) {
			t.Fatalf("chars_per_token %s: rows = %+v, want an aborted row with %d prompt and 2 completion tokens", c.perToken, rows, c.want)
		}
		e.wantMarked(t, true)
	}
	// Only a byte count reaches the metering: no prompt text in the logs (req_001 D-31, D-40).
	if logs := e.tel.logText(); strings.Contains(logs, "hello there") {
		t.Fatalf("the prompt text is in the logs: %s", logs)
	}
}

// With on_context_overflow truncate the pre-check is skipped and the engine drops what does not fit,
// so an aborted call bills no more than the context the engine ran with (req_001 D-108): a 1000
// byte prompt is about 250 estimated tokens, capped at the grant's context cap of 100.
func TestAbortedPromptEstimateIsCappedAtTheContext(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			for i := range models {
				models[i].ContextCap = i64(100)
			}
			return grantsNewVersion{Models: models, OnContextOverflow: "truncate"}
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.eng.wrapChat = func(s engine.ChatStream) engine.ChatStream { return &cancelAfter{ChatStream: s, n: 2, cancel: cancel} }
	body := `{"model":"` + chatModel + `","messages":[{"role":"user","content":"` + strings.Repeat("a", 1000) + `"}]}`
	statusIs(t, e.doCtx(ctx, "/v1/chat/completions", body), 499)
	if rows := e.useRows(); len(rows) != 1 || rows[0] != (useRow{ledger.OutcomeAborted, 100, 2}) {
		t.Fatalf("rows = %+v, want one aborted row with 100 prompt and 2 completion tokens", rows)
	}
	e.wantMarked(t, true)
}

// A completed call bills the engine's exact count and its receipt carries no marker.
func TestCompletedCallReceiptHasNoEstimatedMarker(t *testing.T) {
	e := newEnv(t)
	statusIs(t, e.chat(okChat), http.StatusOK)
	e.wantMarked(t, false)
	if rows := e.useRows(); len(rows) != 1 || rows[0].outcome != ledger.OutcomeCompleted {
		t.Fatalf("rows = %+v", rows)
	}
	statusIs(t, e.chat(streamChatBody), http.StatusOK)
	e.wantMarked(t, false)
	statusIs(t, e.embed(`{"model":"`+embedModel+`","input":"alpha"}`), http.StatusOK)
	e.wantMarked(t, false)
}

func TestClientDisconnectMetersAbortedEmbeddings(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.eng.embedHook = cancel
	r := e.doCtx(ctx, "/v1/embeddings", `{"model":"`+embedModel+`","input":"alpha"}`)
	statusIs(t, r, 499)
	var outcome string
	var prompt, completion int64
	err := e.db.Reader().QueryRow(context.Background(),
		`SELECT outcome, prompt_tokens, completion_tokens FROM use_record`).Scan(&outcome, &prompt, &completion)
	if err != nil {
		t.Fatal(err)
	}
	// "alpha" is 5 bytes: 2 tokens at the default 4 bytes per token (req_001 D-108).
	if outcome != string(ledger.OutcomeAborted) || prompt != 2 || completion != 0 {
		t.Fatalf("row = %s, %d prompt, %d completion; want aborted, 2, 0", outcome, prompt, completion)
	}
	e.wantMarked(t, true)
}

func TestEngineTimeoutWritesNoUsageRecord(t *testing.T) {
	e := newEnv(t)
	e.set(gateway.KeyEngineTimeout, "10ms")
	e.eng.wrapChat = func(s engine.ChatStream) engine.ChatStream { return slowStream{s} }
	r := e.chat(okChat)
	statusIs(t, r, http.StatusGatewayTimeout)
	if eb := r.errBody(); eb["type"] != "server_error" {
		t.Fatalf("error = %v", eb)
	}
	if n := e.useRecords(); n != 0 {
		t.Fatalf("use_record rows = %d, want 0", n)
	}
}
