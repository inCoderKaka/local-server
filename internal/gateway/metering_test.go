package gateway_test

import (
	"context"
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

// D-38 interactive-request machine: a client disconnect during the engine call is metered as
// aborted with the tokens so far.
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
	if outcome != string(ledger.OutcomeAborted) || prompt != 0 || completion != 3 {
		t.Fatalf("row = %s, %d prompt, %d completion; want aborted, 0, 3", outcome, prompt, completion)
	}
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
	if outcome != string(ledger.OutcomeAborted) || prompt != 0 || completion != 0 {
		t.Fatalf("row = %s, %d prompt, %d completion; want aborted, 0, 0", outcome, prompt, completion)
	}
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
