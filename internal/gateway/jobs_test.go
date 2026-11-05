package gateway_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/engine/fake"
	"github.com/inCoderKaka/local-server/internal/gateway"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/jobs"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/policy"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/storage"
)

const jobBody = `{"model":"` + chatModel + `","messages":[{"role":"user","content":"hello there from a job"}]}`

func (e *env) postJob(idem, body string, hdr ...string) resp {
	e.t.Helper()
	h := append([]string{"Idempotency-Key", idem}, hdr...)
	return e.do(http.MethodPost, "/v1/jobs", body, h...)
}

func (e *env) getJob(id string) resp { return e.do(http.MethodGet, "/v1/jobs/"+id, "") }

func (e *env) getResult(id string) resp { return e.do(http.MethodGet, "/v1/jobs/"+id+"/result", "") }

func (e *env) cancelJob(id string) resp { return e.do(http.MethodPost, "/v1/jobs/"+id+"/cancel", "") }

// runJobs starts the job worker the way startWorkers does, with a short poll interval.
func (e *env) runJobs() {
	e.t.Helper()
	e.set(jobs.KeyPollInterval, "5ms")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); e.jobs.Run(ctx, e.gw.JobExecutor()) }()
	var once sync.Once
	e.t.Cleanup(func() { once.Do(func() { cancel(); <-done }) })
}

func (e *env) waitJob(id, status string) map[string]any {
	e.t.Helper()
	var m map[string]any
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r := e.getJob(id)
		if r.Code != http.StatusOK {
			e.t.Fatalf("GET job = %d: %s", r.Code, r.Body.String())
		}
		if m = r.json(); m["status"] == status {
			return m
		}
		time.Sleep(3 * time.Millisecond)
	}
	e.t.Fatalf("job %s did not reach %s; last: %v", id, status, m)
	return nil
}

func (e *env) sqlInt(query string, args ...any) int64 {
	e.t.Helper()
	var n int64
	if err := e.db.Reader().QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) execSQL(query string, args ...any) {
	e.t.Helper()
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error { _, err := tx.Exec(query, args...); return err })
	if err != nil {
		e.t.Fatal(err)
	}
}

func idOf(t *testing.T, r resp) string {
	t.Helper()
	id, _ := r.json()["id"].(string)
	if _, err := uuid.Parse(id); err != nil {
		t.Fatalf("no job id in %s", r.Body.String())
	}
	return id
}

// otherConsumer returns the Authorization header of a second consumer with its own grant.
func (e *env) otherConsumer() string {
	e.t.Helper()
	var key auth.Issued
	err := e.bus.Write(context.Background(), e.db, func(tx *storage.Tx) error {
		p, err := e.ir.Principals.Create(tx, "consumer", nil, nil)
		if err != nil {
			return err
		}
		c, err := e.ir.Consumers.Create(tx, p.ID, identity.ConsumerApplication, nil, nil)
		if err != nil {
			return err
		}
		g, err := e.gr.Grants.Create(tx, c.ID)
		if err != nil {
			return err
		}
		models, err := e.gr.Models.List(context.Background(), e.db.Reader(), false)
		if err != nil {
			return err
		}
		var vm []grants.VersionModel
		for _, m := range models {
			vm = append(vm, grants.VersionModel{ModelID: m.ID})
		}
		if _, err = e.gr.Versions.Create(tx, g.ID, grants.NewVersion{Models: vm}); err != nil {
			return err
		}
		key, err = e.au.Issue(tx, auth.IssueParams{ConsumerID: c.ID})
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return "Bearer " + key.Key.Reveal()
}

// ---- create and read ----

func TestCreateJobIsQueuedThenADuplicateReturnsTheSameJob(t *testing.T) {
	e := newEnv(t)
	r := e.postJob("idem-1", jobBody)
	statusIs(t, r, http.StatusAccepted)
	m := r.json()
	if m["status"] != "queued" || m["model"] != chatModel || m["idempotency_key"] != "idem-1" || m["result_available"] != false ||
		m["failure_reason"] != nil || m["started_at"] != nil || m["finished_at"] != nil || m["result_expires_at"] != nil {
		t.Fatalf("job = %v", m)
	}
	created, err := time.Parse(time.RFC3339, m["created_at"].(string))
	if err != nil {
		t.Fatal(err)
	}
	deadline, err := time.Parse(time.RFC3339, m["deadline_at"].(string))
	if err != nil || deadline.Sub(created) != 24*time.Hour {
		t.Fatalf("deadline_at %v, created_at %v: want the default 24h deadline (%v)", m["deadline_at"], created, err)
	}
	id := idOf(t, r)

	again := e.postJob("idem-1", jobBody)
	statusIs(t, again, http.StatusOK)
	if idOf(t, again) != id {
		t.Fatalf("duplicate returned job %s, want %s", idOf(t, again), id)
	}
	// "Unchanged": a duplicate with another body still returns the first job.
	other := e.postJob("idem-1", `{"model":"`+chatModel+`","messages":[{"role":"user","content":"different"}]}`)
	statusIs(t, other, http.StatusOK)
	if idOf(t, other) != id {
		t.Fatal("a duplicate key with another body started a new job")
	}
	if n := e.sqlInt(`SELECT count(*) FROM sch_job`); n != 1 {
		t.Fatalf("%d jobs", n)
	}
	got := e.getJob(id)
	statusIs(t, got, http.StatusOK)
	if got.json()["status"] != "queued" {
		t.Fatalf("job = %s", got.Body.String())
	}
	if e.eng.Calls(fake.OpChat) != 0 {
		t.Fatal("POST /v1/jobs ran the engine")
	}
}

func TestCreateJobNeedsAnIdempotencyKeyAndAKey(t *testing.T) {
	e := newEnv(t)
	statusIs(t, e.do(http.MethodPost, "/v1/jobs", jobBody), http.StatusBadRequest)
	statusIs(t, e.postJob("", jobBody), http.StatusBadRequest)
	r := e.doAuth("", http.MethodPost, "/v1/jobs", jobBody, "Idempotency-Key", "k")
	statusIs(t, r, http.StatusUnauthorized)
	statusIs(t, e.doAuth("", http.MethodGet, "/v1/jobs/"+uuid.NewString(), ""), http.StatusUnauthorized)
	if n := e.sqlInt(`SELECT count(*) FROM sch_job`); n != 0 {
		t.Fatalf("%d jobs", n)
	}
}

func TestCreateJobValidatesLikeChat(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name, body string
		status     int
		param      string
	}{
		{"stream", `{"model":"` + chatModel + `","stream":true,"messages":[{"role":"user","content":"hi"}]}`, http.StatusBadRequest, "stream"},
		{"model not granted", `{"model":"nope","messages":[{"role":"user","content":"hi"}]}`, http.StatusForbidden, "model"},
		{"no messages", `{"model":"` + chatModel + `","messages":[]}`, http.StatusBadRequest, "messages"},
		{"unsupported field", `{"model":"` + chatModel + `","temperature":0.5,"messages":[{"role":"user","content":"hi"}]}`, http.StatusBadRequest, "temperature"},
		{"unknown field", `{"model":"` + chatModel + `","n":2,"messages":[{"role":"user","content":"hi"}]}`, http.StatusBadRequest, "n"},
	}
	for _, c := range cases {
		r := e.postJob("idem-"+c.name, c.body)
		statusIs(t, r, c.status)
		if eb := r.errBody(); eb["param"] != c.param {
			t.Errorf("%s: error = %v, want param %s", c.name, eb, c.param)
		}
	}
	// A prompt that can never fit the context is refused at POST time, as for chat.
	e.set(gateway.KeyDefaultContextTokens, "4")
	big := `{"model":"` + chatModel + `","messages":[{"role":"user","content":"` + strings.Repeat("word ", 100) + `"}]}`
	r := e.postJob("idem-big", big)
	statusIs(t, r, http.StatusBadRequest)
	if r.errBody()["code"] != "context_too_long" {
		t.Fatalf("error = %v", r.errBody())
	}
	if n := e.sqlInt(`SELECT count(*) FROM sch_job`); n != 0 {
		t.Fatalf("%d jobs were queued by refused requests", n)
	}
}

func TestJobEndpointsAnswer501WithoutAJobsService(t *testing.T) {
	e := newEnv(t, func(o *envOpts) { o.noJobs = true })
	statusIs(t, e.postJob("k", jobBody), http.StatusNotImplemented)
	id := uuid.NewString()
	statusIs(t, e.getJob(id), http.StatusNotImplemented)
	statusIs(t, e.cancelJob(id), http.StatusNotImplemented)
	statusIs(t, e.getResult(id), http.StatusNotImplemented)
}

// ---- the whole flow ----

func TestJobRunsMetersAndIsFetchedOnce(t *testing.T) {
	e := newEnv(t)
	r := e.postJob("idem-1", jobBody, "LS-Request-Properties", "background")
	statusIs(t, r, http.StatusAccepted)
	id := idOf(t, r)
	statusIs(t, e.getResult(id), http.StatusConflict) // not succeeded yet

	e.runJobs()
	m := e.waitJob(id, "succeeded")
	if m["result_available"] != true || m["result_expires_at"] == nil || m["finished_at"] == nil || m["started_at"] == nil {
		t.Fatalf("job = %v", m)
	}
	// F1 steps 5 to 8: background class, job request, metered under the job id, signed receipt.
	last := e.timed.lastRequest()
	if !last.Job || last.Class != scheduler.ClassBackground || last.Model != chatModel {
		t.Fatalf("scheduler request = %+v", last)
	}
	if n := e.sqlInt(`SELECT count(*) FROM use_record WHERE job_id = ? AND outcome = 'completed'`, id); n != 1 {
		t.Fatalf("%d usage records for the job, want 1", n)
	}
	if e.sqlInt(`SELECT count(*) FROM sch_job_payload`) != 0 {
		t.Fatal("payload kept after the job succeeded")
	}
	evs := e.events("job.done")
	if len(evs) != 1 {
		t.Fatalf("%d job.done events", len(evs))
	}
	if strings.Contains(string(evs[0].Payload), "hello there") || strings.Contains(string(evs[0].Payload), fake.DefaultReply) {
		t.Fatalf("job.done payload carries content: %s", evs[0].Payload)
	}

	res := e.getResult(id)
	statusIs(t, res, http.StatusOK)
	rc := e.verify(res.Header().Get("LS-Receipt"))
	if rc.JobID == nil || *rc.JobID != id || rc.Metering != ledger.Metered || rc.Outcome != ledger.OutcomeCompleted ||
		rc.ConsumerID != e.cons.ID || rc.Operation != ledger.OpChatCompletions {
		t.Fatalf("receipt = %+v", rc)
	}
	body := res.json()
	choice := body["choices"].([]any)[0].(map[string]any)
	if choice["message"].(map[string]any)["content"] != fake.DefaultReply || !strings.HasSuffix(body["id"].(string), rc.UsageID) {
		t.Fatalf("result = %v", body)
	}

	// The body was written whole, so the result is deleted: the next fetch is 410.
	gone := e.getResult(id)
	statusIs(t, gone, http.StatusGone)
	if e.sqlInt(`SELECT count(*) FROM sch_job_result`) != 0 {
		t.Fatal("result kept after a complete fetch")
	}
	statusIs(t, e.getResult(id), http.StatusGone)
	after := e.getJob(id).json()
	if after["status"] != "succeeded" || after["result_available"] != false {
		t.Fatalf("job after the fetch = %v", after)
	}
}

// PLT-B-009 (req_001 D-85): the result is deleted after the body was written whole, not when the
// fetch begins; a dropped connection leaves it fetchable.
func TestResultSurvivesAConnectionDroppedMidBody(t *testing.T) {
	e := newEnv(t)
	id := idOf(t, e.postJob("idem-1", jobBody))
	e.runJobs()
	e.waitJob(id, "succeeded")

	for _, cut := range []int{1, 40, 150} {
		w := &droppingWriter{ResponseRecorder: httptest.NewRecorder(), after: cut}
		e.serveTo(w, http.MethodGet, "/v1/jobs/"+id+"/result")
		if w.Code != http.StatusOK {
			t.Fatalf("cut %d: status %d", cut, w.Code)
		}
		if !w.dropped {
			t.Fatalf("cut %d: the connection never dropped; the result is smaller than the cut", cut)
		}
		if n := e.sqlInt(`SELECT count(*) FROM sch_job_result`); n != 1 {
			t.Fatalf("cut %d: the result was deleted although the body did not reach the consumer", cut)
		}
	}
	if got := e.getJob(id).json(); got["result_available"] != true {
		t.Fatalf("job = %v", got)
	}

	// The retry gets the whole result, with its receipt; only then it is gone.
	res := e.getResult(id)
	statusIs(t, res, http.StatusOK)
	if e.verify(res.Header().Get("LS-Receipt")).JobID == nil {
		t.Fatal("the retried result has no receipt for the job")
	}
	if res.json()["choices"] == nil {
		t.Fatalf("result = %s", res.Body.String())
	}
	statusIs(t, e.getResult(id), http.StatusGone)

	// A failed flush at the end is a dropped connection too.
	id2 := idOf(t, e.postJob("idem-2", jobBody))
	e.waitJob(id2, "succeeded")
	w := &droppingWriter{ResponseRecorder: httptest.NewRecorder(), after: -1, flushErr: io.ErrClosedPipe}
	e.serveTo(w, http.MethodGet, "/v1/jobs/"+id2+"/result")
	if n := e.sqlInt(`SELECT count(*) FROM sch_job_result WHERE job_id = ?`, id2); n != 1 {
		t.Fatal("the result was deleted although the final flush failed")
	}
	statusIs(t, e.getResult(id2), http.StatusOK)
}

// The same over a real listener: net/http's own writer, flushed after the handler.
func TestResultFetchedOverARealConnectionIsDeletedAfterwards(t *testing.T) {
	e := newEnv(t)
	id := idOf(t, e.postJob("idem-1", jobBody))
	e.runJobs()
	e.waitJob(id, "succeeded")
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	get := func() (int, string, string) {
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/jobs/"+id+"/result", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", e.key)
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, res.Header.Get("LS-Receipt"), string(b)
	}
	code, receipt, body := get()
	if code != http.StatusOK || receipt == "" || !strings.Contains(body, fake.DefaultReply) {
		t.Fatalf("first fetch = %d, receipt %q, body %s", code, receipt, body)
	}
	waitFor(t, func() bool { return e.sqlInt(`SELECT count(*) FROM sch_job_result`) == 0 }, "the delivered result to be deleted")
	if code, _, _ := get(); code != http.StatusGone {
		t.Fatalf("second fetch = %d, want 410", code)
	}
}

func TestResultPastItsTTLIs410AndAJobThatDidNotSucceedIs409(t *testing.T) {
	e := newEnv(t)
	id := idOf(t, e.postJob("idem-1", jobBody))
	e.runJobs()
	e.waitJob(id, "succeeded")
	// The TTL passes: the stored expiry is in the past. The sweep deletes the row; before it does,
	// the endpoint already treats the result as gone.
	e.execSQL(`UPDATE sch_job_result SET expires_at = ? WHERE job_id = ?`, time.Now().Add(-time.Second).UnixMilli(), id)
	statusIs(t, e.getResult(id), http.StatusGone)
	if e.getJob(id).json()["result_available"] != false {
		t.Fatal("job reports an expired result as available")
	}
	// A failed job has no result.
	e.eng.InjectError(fake.OpChat, engineDown(), 1)
	failed := idOf(t, e.postJob("idem-2", jobBody))
	e.waitJob(failed, "failed")
	statusIs(t, e.getResult(failed), http.StatusConflict)
}

func TestJobsAreVisibleOnlyToTheirConsumer(t *testing.T) {
	e := newEnv(t)
	id := idOf(t, e.postJob("idem-1", jobBody))
	other := e.otherConsumer()
	for name, r := range map[string]resp{
		"get":    e.doAuth(other, http.MethodGet, "/v1/jobs/"+id, ""),
		"cancel": e.doAuth(other, http.MethodPost, "/v1/jobs/"+id+"/cancel", ""),
		"result": e.doAuth(other, http.MethodGet, "/v1/jobs/"+id+"/result", ""),
	} {
		statusIs(t, r, http.StatusNotFound)
		if eb := r.errBody(); eb["type"] != "invalid_request_error" || eb["param"] != "job_id" {
			t.Errorf("%s: error = %v", name, eb)
		}
	}
	// The key is per access key: another consumer using the same Idempotency-Key gets its own job.
	r := e.doAuth(other, http.MethodPost, "/v1/jobs", jobBody, "Idempotency-Key", "idem-1")
	statusIs(t, r, http.StatusAccepted)
	if idOf(t, r) == id {
		t.Fatal("another consumer was handed this consumer's job")
	}
	// And a second key of the same consumer is another access key: its own namespace too.
	second, _ := e.issue(auth.Narrowing{})
	r = e.doAuth(second, http.MethodPost, "/v1/jobs", jobBody, "Idempotency-Key", "idem-1")
	statusIs(t, r, http.StatusAccepted)
	if idOf(t, r) == id {
		t.Fatal("a duplicate key from another access key returned the first key's job")
	}
	// The same consumer sees the job through either key.
	statusIs(t, e.doAuth(second, http.MethodGet, "/v1/jobs/"+id, ""), http.StatusOK)
	statusIs(t, e.getJob(uuid.NewString()), http.StatusNotFound)
	statusIs(t, e.getJob("not-a-uuid"), http.StatusBadRequest)
}

// ---- cancel ----

func TestCancelQueuedJobEndpoint(t *testing.T) {
	e := newEnv(t)
	id := idOf(t, e.postJob("idem-1", jobBody))
	r := e.cancelJob(id)
	statusIs(t, r, http.StatusOK)
	m := r.json()
	if m["status"] != "cancelled" || m["finished_at"] == nil || m["id"] != id {
		t.Fatalf("job = %v", m)
	}
	if e.sqlInt(`SELECT count(*) FROM sch_job_payload`) != 0 {
		t.Fatal("payload kept after cancel")
	}
	// A job in a terminal state is returned unchanged with 409.
	again := e.cancelJob(id)
	statusIs(t, again, http.StatusConflict)
	if m := again.json(); m["status"] != "cancelled" || m["id"] != id {
		t.Fatalf("second cancel = %v", m)
	}
	statusIs(t, e.cancelJob(uuid.NewString()), http.StatusNotFound)
	// The Idempotency-Key still names the cancelled job.
	dup := e.postJob("idem-1", jobBody)
	statusIs(t, dup, http.StatusOK)
	if dup.json()["status"] != "cancelled" {
		t.Fatalf("duplicate = %s", dup.Body.String())
	}
	e.runJobs()
	time.Sleep(50 * time.Millisecond)
	if e.eng.Calls(fake.OpChat) != 0 {
		t.Fatal("a cancelled job ran")
	}
}

func TestCancelRunningJobStopsTheEngineAndMetersTheTokensSoFar(t *testing.T) {
	e := newEnv(t, withEngine(fake.WithChatTimeouts(time.Hour, time.Hour), fake.WithChunkGap(2, time.Hour)))
	id := idOf(t, e.postJob("idem-1", jobBody))
	e.runJobs()
	waitFor(t, func() bool { return e.eng.chatCtx() != nil }, "the engine call to start")
	if got := e.getJob(id).json(); got["status"] != "running" {
		t.Fatalf("job = %v", got)
	}
	ctx := e.eng.chatCtx()
	r := e.cancelJob(id)
	statusIs(t, r, http.StatusOK)
	if r.json()["status"] != "cancelled" {
		t.Fatalf("job = %s", r.Body.String())
	}
	waitFor(t, func() bool { return ctx.Err() != nil }, "the engine call to be cancelled")
	waitFor(t, func() bool {
		return e.sqlInt(`SELECT count(*) FROM use_record WHERE job_id = ? AND outcome = 'aborted'`, id) == 1
	}, "the tokens used so far to be metered")
	if n := e.sqlInt(`SELECT completion_tokens FROM use_record WHERE job_id = ?`, id); n > 2 {
		t.Fatalf("metered %d completion tokens, the engine had sent at most 2", n)
	}
	time.Sleep(30 * time.Millisecond)
	if got := e.getJob(id).json(); got["status"] != "cancelled" || got["failure_reason"] != nil {
		t.Fatalf("job = %v", got)
	}
	if e.sqlInt(`SELECT count(*) FROM sch_job_result`) != 0 || e.sqlInt(`SELECT count(*) FROM sch_job_payload`) != 0 {
		t.Fatal("content kept for a cancelled job")
	}
	if len(e.events("job.done")) != 0 {
		t.Fatal("job.done for a cancelled job")
	}
}

// ---- failure ----

func TestEngineFailureFailsTheJobAndIsMetered(t *testing.T) {
	e := newEnv(t)
	e.eng.InjectError(fake.OpChat, engineDown(), 1)
	id := idOf(t, e.postJob("idem-1", jobBody))
	e.runJobs()
	m := e.waitJob(id, "failed")
	if m["failure_reason"] != "engine_unavailable" || m["result_available"] != false {
		t.Fatalf("job = %v", m)
	}
	if n := e.sqlInt(`SELECT count(*) FROM use_record WHERE job_id = ? AND outcome = 'failed'`, id); n != 1 {
		t.Fatalf("%d failed usage records for the job, want 1", n)
	}
	if e.sqlInt(`SELECT count(*) FROM sch_job_payload`) != 0 {
		t.Fatal("payload kept after the job failed")
	}
	// The key is held by the failed job: only host_restart and payload_minimized free it.
	dup := e.postJob("idem-1", jobBody)
	statusIs(t, dup, http.StatusOK)
	if idOf(t, dup) != id {
		t.Fatal("a job that failed with an engine error released its Idempotency-Key")
	}
}

func TestResubmitAfterHostRestartMakesANewAttempt(t *testing.T) {
	e := newEnv(t)
	id := idOf(t, e.postJob("idem-1", jobBody))
	e.execSQL(`UPDATE sch_job SET status = 'running', started_at = ? WHERE id = ?`, time.Now().UnixMilli(), id)
	if n, err := e.jobs.FailInterrupted(context.Background()); err != nil || n != 1 {
		t.Fatalf("FailInterrupted = %d, %v", n, err)
	}
	if got := e.getJob(id).json(); got["status"] != "failed" || got["failure_reason"] != "host_restart" {
		t.Fatalf("job = %v", got)
	}
	r := e.postJob("idem-1", jobBody)
	statusIs(t, r, http.StatusAccepted)
	if idOf(t, r) == id {
		t.Fatal("the new attempt reused the failed job")
	}
	statusIs(t, e.postJob("idem-1", jobBody), http.StatusOK)
	// And payload_minimized, set by a restore (req_001 D-68).
	e.execSQL(`UPDATE sch_job SET status = 'failed', failure_reason = 'payload_minimized' WHERE id = ?`, idOf(t, r))
	r2 := e.postJob("idem-1", jobBody)
	statusIs(t, r2, http.StatusAccepted)
	if idOf(t, r2) == idOf(t, r) {
		t.Fatal("payload_minimized did not release the key")
	}
}

// ---- policy at POST and at admission (req_001 D-38 kept by D-54) ----

func TestPolicyDenyRefusesTheJobAtPostTime(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			allowed := `["background","purpose"]`
			return grantsNewVersion{Models: models, AllowedProperties: &allowed}
		}
	})
	rule := e.addRule("no-secret", false, 0, `{"format_version":1,"when":{"properties":{"purpose":"secret"}},"deny":true}`)
	r := e.postJob("idem-1", jobBody, "LS-Request-Properties", "purpose=secret")
	statusIs(t, r, http.StatusForbidden)
	if eb := r.errBody(); eb["code"] != "policy_denied" || eb["rule_id"] != rule.ID {
		t.Fatalf("error = %v", eb)
	}
	r = e.postJob("idem-2", jobBody, "LS-Request-Properties", "mood=happy")
	statusIs(t, r, http.StatusForbidden)
	statusIs(t, e.postJob("idem-3", jobBody, "LS-Request-Properties", "((("), http.StatusBadRequest)
	if n := e.sqlInt(`SELECT count(*) FROM sch_job`); n != 0 {
		t.Fatalf("%d jobs were queued by refused requests", n)
	}
}

func TestOutsideTimeWindowQueuesTheJobAndAdmissionDecidesAgain(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			allowed := `["background","purpose"]`
			return grantsNewVersion{Models: models, AllowedProperties: &allowed}
		}
	})
	soon := time.Now().UTC().Add(2 * time.Hour)
	rule := e.addRule("night-only", false, 0,
		fmt.Sprintf(`{"format_version":1,"when":{"properties":{"purpose":"batch"}},"time_zone":"UTC","windows":[{"start":"%s","end":"%s"}]}`,
			soon.Format("15:04"), soon.Add(time.Hour).Format("15:04")))
	// A synchronous call is refused; a job is queued.
	statusIs(t, e.chat(okChat, "LS-Request-Properties", "purpose=batch"), http.StatusTooManyRequests)
	r := e.postJob("idem-1", jobBody, "LS-Request-Properties", "purpose=batch")
	statusIs(t, r, http.StatusAccepted)
	id := idOf(t, r)

	notBefore := e.sqlInt(`SELECT not_before FROM sch_job WHERE id = ?`, id)
	if d := time.UnixMilli(notBefore).Sub(time.Now()); d < time.Hour || d > 3*time.Hour {
		t.Fatalf("not_before is %v from now, want the opening of the window (about 2h)", d)
	}
	e.runJobs()
	time.Sleep(60 * time.Millisecond)
	if e.getJob(id).json()["status"] != "queued" || e.eng.Calls(fake.OpChat) != 0 {
		t.Fatal("the job ran before its window opened")
	}

	// The window opens early (the rule is disabled): admission evaluates policy again and runs it.
	e.execSQL(`UPDATE grt_policy_rule SET enabled = 0 WHERE id = ?`, rule.ID)
	e.execSQL(`UPDATE sch_job SET not_before = NULL WHERE id = ?`, id)
	e.waitJob(id, "succeeded")
}

func TestPolicyIsDecidedAgainAtAdmission(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			allowed := `["background","purpose"]`
			return grantsNewVersion{Models: models, AllowedProperties: &allowed}
		}
	})
	id := idOf(t, e.postJob("idem-1", jobBody, "LS-Request-Properties", "purpose=secret"))
	// The host adds a deny rule while the job waits.
	e.addRule("no-secret", false, 0, `{"format_version":1,"when":{"properties":{"purpose":"secret"}},"deny":true}`)
	e.runJobs()
	m := e.waitJob(id, "failed")
	if m["failure_reason"] != "policy_denied" {
		t.Fatalf("job = %v", m)
	}
	if e.eng.Calls(fake.OpChat) != 0 || e.sqlInt(`SELECT count(*) FROM use_record`) != 0 {
		t.Fatal("a denied job reached the engine or was metered")
	}
}

func TestQueueFullAtAdmissionLeavesTheJobQueued(t *testing.T) {
	e := newEnv(t)
	var mu sync.Mutex
	refused := 0
	e.timed.mu.Lock()
	e.timed.refuse = func(r scheduler.Request) error {
		mu.Lock()
		defer mu.Unlock()
		refused++
		// An interactive arrival took the job's place in a full queue (req_001 D-26).
		return &scheduler.Error{Code: scheduler.CodeQueueFull, RetryAfter: 90 * time.Second, Message: "the admission queue is full"}
	}
	e.timed.mu.Unlock()
	id := idOf(t, e.postJob("idem-1", jobBody))
	e.runJobs()
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return refused >= 1 }, "the first admission")
	waitFor(t, func() bool { return e.sqlInt(`SELECT coalesce(not_before, 0) FROM sch_job WHERE id = ?`, id) > 0 }, "the retry time")
	d := time.UnixMilli(e.sqlInt(`SELECT not_before FROM sch_job WHERE id = ?`, id)).Sub(time.Now())
	if d < 80*time.Second || d > 100*time.Second {
		t.Fatalf("retry in %v, want about the Retry-After of 90s", d)
	}
	if got := e.getJob(id).json(); got["status"] != "queued" || got["failure_reason"] != nil {
		t.Fatalf("job = %v", got)
	}
	mu.Lock()
	n := refused
	mu.Unlock()
	time.Sleep(60 * time.Millisecond)
	mu.Lock()
	if refused != n {
		t.Fatal("the worker retried before Retry-After passed")
	}
	mu.Unlock()
	// Room again: the retry time passes and the job runs.
	e.timed.mu.Lock()
	e.timed.refuse = nil
	e.timed.mu.Unlock()
	e.execSQL(`UPDATE sch_job SET not_before = NULL WHERE id = ?`, id)
	e.waitJob(id, "succeeded")
}

func TestRevokedKeyFailsAQueuedJobAtAdmission(t *testing.T) {
	e := newEnv(t)
	hdr, keyID := e.issue(auth.Narrowing{})
	r := e.doAuth(hdr, http.MethodPost, "/v1/jobs", jobBody, "Idempotency-Key", "idem-1")
	statusIs(t, r, http.StatusAccepted)
	id := idOf(t, r)
	err := e.bus.Write(context.Background(), e.db, func(tx *storage.Tx) error { return e.au.Revoke(tx, keyID, "") })
	if err != nil {
		t.Fatal(err)
	}
	e.runJobs()
	m := e.waitJob(id, "failed")
	if m["failure_reason"] != "invalid_api_key" || e.eng.Calls(fake.OpChat) != 0 {
		t.Fatalf("job = %v, engine calls %d", m, e.eng.Calls(fake.OpChat))
	}
}

// A grant version that expires while the job waits no longer admits it: the job fails with the
// code of a key that is not valid, and the engine is never called.
func TestExpiredGrantVersionFailsAQueuedJobAtAdmission(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			return grantsNewVersion{Models: models, ExpiresAt: &expires}
		}
	})
	id := idOf(t, e.postJob("idem-1", jobBody)) // accepted: the version is still valid
	// The job executor runs on a clock past the version's expiry.
	pol, err := policy.New(e.st, gateway.NewRuleSource(e.db, e.gr))
	if err != nil {
		t.Fatal(err)
	}
	late, err := gateway.New(gateway.Config{DB: e.db, Settings: e.st, Events: e.bus, Grants: e.gr, Auth: e.au, Limits: e.lim,
		Policy: pol, Scheduler: e.timed, Sharing: e.share, Engine: e.eng, Ledger: e.meter, Checkpoints: e.cps,
		Jobs: e.jobs, Telemetry: e.tel, Clock: func() time.Time { return expires.Add(time.Minute) }})
	if err != nil {
		t.Fatal(err)
	}
	e.set(jobs.KeyPollInterval, "5ms")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); e.jobs.Run(ctx, late.JobExecutor()) }()
	t.Cleanup(func() { cancel(); <-done })
	m := e.waitJob(id, "failed")
	if m["failure_reason"] != "invalid_api_key" || e.eng.Calls(fake.OpChat) != 0 {
		t.Fatalf("job = %v, engine calls %d", m, e.eng.Calls(fake.OpChat))
	}
}

func TestUsedUpQuotaStopsNewJobsButNotReadingThem(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			return grantsNewVersion{Models: models, QuotaTokensPerDay: i64(20)}
		}
	})
	id := idOf(t, e.postJob("idem-1", jobBody))
	statusIs(t, e.chat(okChat), http.StatusOK)
	statusIs(t, e.chat(okChat), http.StatusOK) // over the quota now
	r := e.postJob("idem-2", jobBody)
	statusIs(t, r, http.StatusTooManyRequests)
	if r.errBody()["code"] != "quota_exceeded" {
		t.Fatalf("error = %v", r.errBody())
	}
	statusIs(t, e.getJob(id), http.StatusOK)
	statusIs(t, e.cancelJob(id), http.StatusOK)
}

func TestJobRequestContentNeverReachesTheLogOrTheDatabaseInTheClear(t *testing.T) {
	e := newEnv(t)
	const secret = "zebra-moon-secret-phrase"
	id := idOf(t, e.postJob("idem-1", `{"model":"`+chatModel+`","messages":[{"role":"user","content":"`+secret+`"}]}`,
		"LS-Request-Properties", "background"))
	e.runJobs()
	e.waitJob(id, "succeeded")
	statusIs(t, e.getResult(id), http.StatusOK)
	if strings.Contains(e.tel.logText(), secret) || strings.Contains(e.tel.logText(), fake.DefaultReply) {
		t.Fatal("job content in the log")
	}
	for _, q := range []string{
		`SELECT coalesce(group_concat(id || consumer_id || idempotency_key || model || identity || status, '|'), '') FROM sch_job`,
		`SELECT coalesce(group_concat(payload, '|'), '') FROM ntf_event`,
		`SELECT coalesce(group_concat(model || operation, '|'), '') FROM use_record`,
	} {
		var s string
		if err := e.db.Reader().QueryRow(context.Background(), q).Scan(&s); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(s, secret) || strings.Contains(s, fake.DefaultReply) {
			t.Fatalf("content found by %q", q)
		}
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(3 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// serveTo sends a request from the key of the test consumer to w, which may fail its writes.
func (e *env) serveTo(w http.ResponseWriter, method, path string) {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "100.64.0.1:5555"
	req.Header.Set("Authorization", e.key)
	e.h.ServeHTTP(w, req)
}

// droppingWriter is a ResponseWriter whose connection drops after `after` bytes of the body
// (negative: never), or whose final flush fails.
type droppingWriter struct {
	*httptest.ResponseRecorder
	after    int
	flushErr error
	written  int
	dropped  bool
}

func (d *droppingWriter) Write(p []byte) (int, error) {
	if d.after >= 0 && d.written+len(p) > d.after {
		n := d.after - d.written
		_, _ = d.ResponseRecorder.Write(p[:n])
		d.written += n
		d.dropped = true
		return n, io.ErrClosedPipe
	}
	d.written += len(p)
	return d.ResponseRecorder.Write(p)
}

func (d *droppingWriter) FlushError() error {
	if d.flushErr != nil {
		d.dropped = true
		return d.flushErr
	}
	d.ResponseRecorder.Flush()
	return nil
}
