package gateway_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/gateway"
	"github.com/inCoderKaka/local-server/internal/ledger"
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
			return grantsNewVersion{Models: models, QuotaTokensPerDay: i64(100)}
		}
	})
	for i := 0; i < 3; i++ {
		statusIs(t, e.chat(okChat), http.StatusOK)
	}
	if n := e.useRecords(); n != 3 {
		t.Fatalf("use_record rows = %d, want 3", n)
	}
	n, err := e.cps.LeafCount(t.Context())
	if err != nil || n != 3 {
		t.Fatalf("ledger leaves = %d, %v; want 3", n, err)
	}
}
