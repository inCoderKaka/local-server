package gateway_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/engine/fake"
	"github.com/inCoderKaka/local-server/internal/gateway"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func TestQueueFullIs429WithRetryAfter(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyTotalConcurrency, "1")
	e.set(scheduler.KeyOllamaNumParallel, "1")
	e.set(scheduler.KeyQueueLength, "0")
	e.set(scheduler.KeyRetryAfter, "7s")
	held, err := e.sched.Admit(context.Background(), scheduler.Request{GrantID: "other", Class: scheduler.ClassInteractive, Model: chatModel, EstimatedCtxTokens: 8})
	if err != nil {
		t.Fatal(err)
	}
	r := e.chat(okChat)
	statusIs(t, r, http.StatusTooManyRequests)
	if eb := r.errBody(); eb["code"] != "queue_full" || eb["type"] != "server_error" {
		t.Fatalf("error = %v", eb)
	}
	if got := r.Header().Get("Retry-After"); got != "7" {
		t.Fatalf("Retry-After = %q, want 7", got)
	}
	held.Release()
	statusIs(t, e.chat(okChat), http.StatusOK)
}

func TestHostPausedIs503(t *testing.T) {
	e := newEnv(t)
	if _, err := e.share.Pause(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	r := e.chat(okChat)
	statusIs(t, r, http.StatusServiceUnavailable)
	if eb := r.errBody(); eb["code"] != "host_paused" || eb["type"] != "server_error" {
		t.Fatalf("error = %v", eb)
	}
	if n := e.useRecords(); n != 0 {
		t.Fatalf("a refused call wrote %d records", n)
	}
	if _, err := e.share.Resume(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	statusIs(t, e.chat(okChat), http.StatusOK)
}

func TestOverBudgetIs422WithoutRetryAfter(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyMemoryBudgetBytes, "1024")
	r := e.chat(okChat)
	statusIs(t, r, http.StatusUnprocessableEntity)
	if eb := r.errBody(); eb["code"] != "over_budget" || eb["type"] != "invalid_request_error" {
		t.Fatalf("error = %v", eb)
	}
	if r.Header().Get("Retry-After") != "" {
		t.Fatalf("over_budget must not carry Retry-After, got %q", r.Header().Get("Retry-After"))
	}
}

func TestGrantConcurrencyReachesTheScheduler(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			return grantsNewVersion{Models: models, MaxConcurrency: i64(3)}
		}
	})
	// The scheduler setting default is 1; a grant limit of 3 must be what Admit receives. Two
	// held slots of this grant leave room for the call only if 3 reached the scheduler.
	e.set(scheduler.KeyTotalConcurrency, "10")
	e.set(scheduler.KeyOllamaNumParallel, "10")
	e.set(scheduler.KeyQueueLength, "0")
	var held []*scheduler.Ticket
	for i := 0; i < 2; i++ {
		tk, err := e.sched.Admit(context.Background(), scheduler.Request{GrantID: e.au0().GrantID, GrantConcurrency: 3, Class: scheduler.ClassInteractive, Model: chatModel, EstimatedCtxTokens: 8})
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, tk)
	}
	statusIs(t, e.chat(okChat), http.StatusOK)
	for _, tk := range held {
		tk.Release()
	}
}

func (e *env) au0() *auth.Identity {
	id, err := e.au.Authenticate(context.Background(), e.key, "100.64.0.1:1")
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func TestTokenQuotaIs429QuotaExceeded(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			return grantsNewVersion{Models: models, Quotas: dayQuotas(grants.MetricTokens, 20)}
		}
	})
	statusIs(t, e.chat(okChat), http.StatusOK) // 11 tokens
	statusIs(t, e.chat(okChat), http.StatusOK) // 22 tokens: now over
	r := e.chat(okChat)
	statusIs(t, r, http.StatusTooManyRequests)
	if eb := r.errBody(); eb["code"] != "quota_exceeded" || eb["type"] != "insufficient_quota" {
		t.Fatalf("error = %v", eb)
	}
	if r.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	// A used-up quota does not stop the consumer from verifying what it was billed.
	statusIs(t, e.do(http.MethodGet, "/v1/keys", ""), http.StatusOK)
}

func TestRequestQuotaCountsEveryAdmittedCall(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			return grantsNewVersion{Models: models, Quotas: dayQuotas(grants.MetricRequests, 3)}
		}
	})
	e.meter.set(-1) // metering fails for good: every call is answered unmetered, no use_record
	for i := 0; i < 3; i++ {
		statusIs(t, e.chat(okChat), http.StatusOK)
	}
	if n := e.useRecords(); n != 0 {
		t.Fatalf("use_record rows = %d, want 0 (all unmetered)", n)
	}
	r := e.chat(okChat)
	statusIs(t, r, http.StatusTooManyRequests)
	eb := r.errBody()
	if eb["code"] != "quota_exceeded" || !strings.Contains(eb["message"].(string), "request quota") {
		t.Fatalf("error = %v, want quota_exceeded for the request quota", eb)
	}
	if r.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
}

// D-100: a call is counted once it is admitted, so a call refused before admission is not
// counted and a call that fails after admission is.
func TestRequestQuotaSkipsRefusedCallsAndCountsFailedOnes(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			return grantsNewVersion{Models: models, Quotas: dayQuotas(grants.MetricRequests, 2)}
		}
	})
	statusIs(t, e.chat(`{"model":"nope","messages":[{"role":"user","content":"hi"}]}`), http.StatusForbidden)
	e.eng.InjectError(fake.OpChat, engineDown(), 1)
	statusIs(t, e.chat(okChat), http.StatusServiceUnavailable) // counted: 1 of 2
	statusIs(t, e.chat(okChat), http.StatusOK)                 // counted: 2 of 2
	r := e.chat(okChat)
	statusIs(t, r, http.StatusTooManyRequests)
	eb := r.errBody()
	if msg, _ := eb["message"].(string); eb["code"] != "quota_exceeded" || !strings.Contains(msg, "request quota") {
		t.Fatalf("error = %v, want quota_exceeded for the request quota", eb)
	}
	if r.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
}

// countFake is the real limiter with CountAdmitted replaced by a fixed answer.
type countFake struct {
	gateway.Limiter
	err error
}

func (f countFake) CountAdmitted(context.Context, *auth.Identity) error { return f.err }

// req_001 D-107: when counting the admitted call finds a request quota used up, the call is
// refused with 429 quota_exceeded and the scheduler slot it took is freed.
func TestCountAdmittedQuotaRefusalIs429AndFreesTheSlot(t *testing.T) {
	qe := &limits.QuotaError{Scope: limits.ScopeAccessKey, Unit: limits.UnitRequests, Limit: 1, Used: 1, Window: time.Hour,
		ResetsAt: time.Now().Add(time.Hour), RetryAfter: time.Hour}
	e := newEnv(t, func(o *envOpts) {
		o.limiter = func(real gateway.Limiter) gateway.Limiter { return countFake{Limiter: real, err: qe} }
	})
	r := e.chat(okChat)
	statusIs(t, r, http.StatusTooManyRequests)
	if eb := r.errBody(); eb["code"] != "quota_exceeded" || eb["type"] != "insufficient_quota" {
		t.Fatalf("error = %v", eb)
	}
	if r.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	if st := e.sched.Stats(); st.Running != 0 || st.Queued != 0 {
		t.Fatalf("scheduler stats = %+v, want nothing running or queued", st)
	}
	if n := e.useRecords(); n != 0 {
		t.Fatalf("a refused call wrote %d records", n)
	}
}

// Any other counting error fails open: the call is served and the failure is logged.
func TestCountAdmittedOtherErrorFailsOpen(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.limiter = func(real gateway.Limiter) gateway.Limiter {
			return countFake{Limiter: real, err: errors.New("counter store down")}
		}
	})
	statusIs(t, e.chat(okChat), http.StatusOK)
	if st := e.sched.Stats(); st.Running != 0 {
		t.Fatalf("scheduler stats = %+v, want nothing running", st)
	}
}

func TestRateLimitIs429RateLimitExceeded(t *testing.T) {
	e := newEnv(t)
	e.set(limits.KeyRateEnabled, "true")
	e.set(limits.KeyRateCapacity, "2")
	e.set(limits.KeyRateWindow, "1h")
	statusIs(t, e.chat(okChat), http.StatusOK)
	statusIs(t, e.chat(okChat), http.StatusOK)
	r := e.chat(okChat)
	statusIs(t, r, http.StatusTooManyRequests)
	if eb := r.errBody(); eb["code"] != "rate_limit_exceeded" || eb["type"] != "requests" {
		t.Fatalf("error = %v", eb)
	}
	if r.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
}

func TestAuthFailuresAreOneAnswerAndLockOut(t *testing.T) {
	e := newEnv(t)
	bad := "Bearer ls_" + strings.Repeat("a", 12) + "_" + strings.Repeat("b", 64)
	for _, hdr := range []string{"", "Bearer nonsense", bad} {
		r := e.doAuth(hdr, http.MethodPost, "/v1/chat/completions", okChat)
		statusIs(t, r, http.StatusUnauthorized)
		if eb := r.errBody(); eb["code"] != "invalid_api_key" {
			t.Fatalf("auth %q: error = %v", hdr, eb)
		}
	}
	// Three more failed checks at a threshold of 3 lock the address out, even for a good key.
	e.set(limits.KeyLockoutFailures, "3")
	for i := 0; i < 3; i++ {
		e.doAuth(bad, http.MethodPost, "/v1/chat/completions", okChat)
	}
	r := e.chat(okChat) // a good key from a locked-out address
	statusIs(t, r, http.StatusTooManyRequests)
	if eb := r.errBody(); eb["code"] != "rate_limit_exceeded" {
		t.Fatalf("error = %v", eb)
	}
	if r.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After on a lockout")
	}
}

// A revoked key gets the same answer as an unknown one.
func TestRevokedKeyIsInvalidAPIKey(t *testing.T) {
	e := newEnv(t)
	hdr, id := e.issue(auth.Narrowing{})
	err := e.bus.Write(context.Background(), e.db, func(tx *storage.Tx) error { return e.au.Revoke(tx, id, "") })
	if err != nil {
		t.Fatal(err)
	}
	r := e.doAuth(hdr, http.MethodPost, "/v1/chat/completions", okChat)
	statusIs(t, r, http.StatusUnauthorized)
	eb := r.errBody()
	if eb["code"] != "invalid_api_key" {
		t.Fatalf("error = %v", eb)
	}
	bad := "Bearer ls_" + strings.Repeat("a", 12) + "_" + strings.Repeat("b", 64)
	u := e.doAuth(bad, http.MethodPost, "/v1/chat/completions", okChat)
	statusIs(t, u, http.StatusUnauthorized)
	if ub := u.errBody(); ub["message"] != eb["message"] {
		t.Fatalf("revoked message %v differs from unknown-key message %v", eb["message"], ub["message"])
	}
}
