package gateway_test

import (
	"context"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/gateway"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/scheduler"
)

// The service-level targets of req_001 D-31 and D-55 are the settings gateway.slo_overhead,
// gateway.slo_admission and gateway.slo_receipt_signing. The benchmarks below measure the
// three figures on the fake engine; the defaults in settings.go are their 95th percentile with
// headroom. The SLO tests read the targets from the settings, so a host that changes a target
// changes what the tests hold the gateway to.
//
// What is measured, on the fake engine (instant), with a real SQLite database in a temporary
// directory and the full pipeline (auth, limits, policy, validate, admission, metering with
// its signed receipt):
//   - overhead: the time of a whole HTTP call, minus the time spent inside the engine call
//     and inside Scheduler.Admit; it includes the metering transaction.
//   - admission: one Scheduler.Admit with a free slot, and its Release.
//   - receipt signing: Ledger.SignUnmetered, which is the signing path of every receipt
//     (the metered receipt is signed by the same function inside the metering transaction).

func p95(d []time.Duration) time.Duration {
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d[len(d)*95/100]
}

func p50(d []time.Duration) time.Duration { return d[len(d)/2] }

// overheadSamples runs n chat calls and returns the gateway's own time for each.
func overheadSamples(tb testing.TB, e *env, n int) []time.Duration {
	tb.Helper()
	// Admit is measured apart (admissionSamples), so it is taken out here with the engine.
	out := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		before, adm := e.eng.nanos.Load(), e.timed.nanos.Load()
		t0 := time.Now()
		r := e.chat(okChat)
		total := time.Since(t0)
		if r.Code != http.StatusOK {
			tb.Fatalf("status %d: %s", r.Code, r.Body.String())
		}
		out = append(out, total-time.Duration(e.eng.nanos.Load()-before)-time.Duration(e.timed.nanos.Load()-adm))
	}
	return out
}

func admissionSamples(tb testing.TB, e *env, n int) []time.Duration {
	tb.Helper()
	out := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		t0 := time.Now()
		tk, err := e.sched.Admit(context.Background(), scheduler.Request{GrantID: "g", Class: scheduler.ClassInteractive, Model: chatModel, EstimatedCtxTokens: 1024})
		d := time.Since(t0)
		if err != nil {
			tb.Fatal(err)
		}
		tk.Release()
		out = append(out, d)
	}
	return out
}

func signingSamples(tb testing.TB, e *env, n int) []time.Duration {
	tb.Helper()
	u := ledger.Usage{UsageID: ledger.NewUsageID(), ConsumerID: e.cons.ID, AccessKeyID: e.keyID, GrantVersionID: e.grant.ID,
		Operation: ledger.OpChatCompletions, Model: chatModel, Outcome: ledger.OutcomeCompleted,
		PromptTokens: 100, CompletionTokens: 200, StartedAt: time.Now().Add(-time.Second), FinishedAt: time.Now()}
	out := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		t0 := time.Now()
		if _, err := e.led.SignUnmetered(u); err != nil {
			tb.Fatal(err)
		}
		out = append(out, time.Since(t0))
	}
	return out
}

func report(b *testing.B, d []time.Duration) {
	b.ReportMetric(float64(p50(d).Nanoseconds()), "p50-ns")
	b.ReportMetric(float64(p95(d).Nanoseconds()), "p95-ns")
}

func BenchmarkGatewayOverhead(b *testing.B) {
	e := newEnv(b)
	e.set(gateway.KeyMeteringRetryDelay, "1ms")
	b.ResetTimer()
	d := overheadSamples(b, e, b.N+20)
	b.StopTimer()
	report(b, d)
}

func BenchmarkAdmission(b *testing.B) {
	e := newEnv(b)
	b.ResetTimer()
	d := admissionSamples(b, e, b.N+20)
	b.StopTimer()
	report(b, d)
}

// BenchmarkMeteringWrite is the largest part of the overhead: one metering transaction.
func BenchmarkMeteringWrite(b *testing.B) {
	e := newEnv(b)
	u := ledger.Usage{ConsumerID: e.cons.ID, AccessKeyID: e.keyID, GrantVersionID: e.grant.ID,
		Operation: ledger.OpChatCompletions, Model: chatModel, Outcome: ledger.OutcomeCompleted,
		PromptTokens: 100, CompletionTokens: 200, StartedAt: time.Now().Add(-time.Second), FinishedAt: time.Now()}
	d := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.UsageID = ledger.NewUsageID()
		t0 := time.Now()
		if _, err := e.led.Record(context.Background(), u); err != nil {
			b.Fatal(err)
		}
		d = append(d, time.Since(t0))
	}
	b.StopTimer()
	report(b, d)
}

func BenchmarkReceiptSigning(b *testing.B) {
	e := newEnv(b)
	b.ResetTimer()
	d := signingSamples(b, e, b.N+20)
	b.StopTimer()
	report(b, d)
}

// sloTarget reads a target from the settings and scales it for the race detector.
func sloTarget(t *testing.T, e *env, key string) time.Duration {
	t.Helper()
	d, err := e.st.Duration(context.Background(), key, "")
	if err != nil {
		t.Fatal(err)
	}
	if raceEnabled {
		d *= raceSlowdown
	}
	return d
}

func TestSLOGatewayOverhead(t *testing.T) {
	if testing.Short() {
		t.Skip("timing check")
	}
	e := newEnv(t)
	overheadSamples(t, e, 20) // warm up
	d := overheadSamples(t, e, 200)
	if got, want := p95(d), sloTarget(t, e, gateway.KeySLOOverhead); got > want {
		t.Errorf("gateway overhead p95 = %v, target %v (%s)", got, want, gateway.KeySLOOverhead)
	}
}

func TestSLOAdmissionLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("timing check")
	}
	e := newEnv(t)
	admissionSamples(t, e, 20)
	d := admissionSamples(t, e, 500)
	if got, want := p95(d), sloTarget(t, e, gateway.KeySLOAdmission); got > want {
		t.Errorf("admission p95 = %v, target %v (%s)", got, want, gateway.KeySLOAdmission)
	}
}

func TestSLOReceiptSigning(t *testing.T) {
	if testing.Short() {
		t.Skip("timing check")
	}
	e := newEnv(t)
	signingSamples(t, e, 20)
	d := signingSamples(t, e, 500)
	if got, want := p95(d), sloTarget(t, e, gateway.KeySLOReceiptSigning); got > want {
		t.Errorf("receipt signing p95 = %v, target %v (%s)", got, want, gateway.KeySLOReceiptSigning)
	}
}

// The targets are host-owned settings with a default and a validator (req_001 D-38).
func TestSLOTargetsAreHostSettings(t *testing.T) {
	e := newEnv(t)
	for _, tc := range []struct{ key, def string }{
		{gateway.KeySLOOverhead, gateway.DefaultSLOOverhead},
		{gateway.KeySLOAdmission, gateway.DefaultSLOAdmission},
		{gateway.KeySLOReceiptSigning, gateway.DefaultSLOReceiptSigning},
	} {
		def, ok := e.st.Definition(tc.key)
		if !ok || def.Owner != "host" || def.Default != tc.def {
			t.Errorf("%s = %+v, %v", tc.key, def, ok)
		}
		got, err := e.st.Duration(context.Background(), tc.key, "")
		if err != nil || got <= 0 {
			t.Errorf("%s: %v, %v", tc.key, got, err)
		}
		if err := e.st.Set(context.Background(), "host", tc.key, "", "0s"); err == nil {
			t.Errorf("%s accepted 0s", tc.key)
		}
	}
}

// A call slower than the overhead target is counted.
func TestSLOBreachIsCounted(t *testing.T) {
	e := newEnv(t)
	e.set(gateway.KeySLOOverhead, "1ns")
	statusIs(t, e.chat(okChat), http.StatusOK)
	if got := e.tel.count("gateway_slo_breaches_total", "overhead"); got != 1 {
		t.Fatalf("breaches = %v, want 1", got)
	}
}
