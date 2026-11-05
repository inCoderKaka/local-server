package jobs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// secretPrompt must never appear in the database outside the sealed blobs, in an event, a log or
// an error text.
const (
	secretPrompt = "the-prompt-nobody-may-see"
	secretAnswer = "the-answer-nobody-may-see"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type tenv struct {
	t    *testing.T
	db   *storage.DB
	st   *settings.Store
	bus  *events.Bus
	keys *hostkeys.Keys
	svc  *Service
	clk  *clock

	mu   sync.Mutex
	evs  []events.Event
	logs bytes.Buffer
}

func newEnv(t *testing.T) *tenv {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"),
		settings.Module(), host.Module(), events.Module(), scheduler.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &tenv{t: t, db: db, st: settings.New(db), bus: events.New(), clk: &clock{t: time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)}}
	master := make([]byte, hostkeys.KeySize)
	if _, err := rand.Read(master); err != nil {
		t.Fatal(err)
	}
	if e.keys, _, err = hostkeys.Open(ctx, db, e.st, master); err != nil {
		t.Fatal(err)
	}
	if err := e.bus.Subscribe("test", func(_ context.Context, ev events.Event) error {
		e.mu.Lock()
		e.evs = append(e.evs, ev)
		e.mu.Unlock()
		return nil
	}, events.JobDone); err != nil {
		t.Fatal(err)
	}
	e.svc, err = New(Config{DB: db, Settings: e.st, Events: e.bus, Keys: e.keys, Clock: e.clk.Now, Telemetry: newLogTelemetry(&e.logs, &e.mu)})
	if err != nil {
		t.Fatal(err)
	}
	e.set(KeyPollInterval, "5ms")
	return e
}

func (e *tenv) set(key, value string) {
	e.t.Helper()
	if err := e.st.Set(context.Background(), settings.Host, key, "", value); err != nil {
		e.t.Fatal(err)
	}
}

func (e *tenv) setConsumer(consumer, key, value string) {
	e.t.Helper()
	if err := e.st.Set(context.Background(), settings.Consumer, key, consumer, value); err != nil {
		e.t.Fatal(err)
	}
}

func (e *tenv) newJob(consumer, accessKey, idem string) NewJob {
	return NewJob{ConsumerID: consumer, PrincipalID: "principal-" + consumer, AccessKeyID: accessKey, GrantVersionID: "gv-1",
		IdempotencyKey: idem, Model: "m", Identity: []byte(`{"KeyID":"` + accessKey + `"}`), Payload: []byte(`{"prompt":"` + secretPrompt + `"}`)}
}

func (e *tenv) create(consumer, accessKey, idem string) (Job, bool) {
	e.t.Helper()
	j, created, err := e.svc.Create(context.Background(), e.newJob(consumer, accessKey, idem))
	if err != nil {
		e.t.Fatal(err)
	}
	return j, created
}

func (e *tenv) get(consumer, id string) Job {
	e.t.Helper()
	j, err := e.svc.Get(context.Background(), consumer, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return j
}

func (e *tenv) exec(query string, args ...any) {
	e.t.Helper()
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error { _, err := tx.Exec(query, args...); return err })
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *tenv) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.Reader().QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *tenv) payloadRows(id string) int {
	return e.count(`SELECT count(*) FROM sch_job_payload WHERE job_id = ?`, id)
}

func (e *tenv) resultRows(id string) int {
	return e.count(`SELECT count(*) FROM sch_job_result WHERE job_id = ?`, id)
}

// run starts the worker with exec and stops it when the test ends.
func (e *tenv) run(exec Executor) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); e.svc.Run(ctx, exec) }()
	var once sync.Once
	stop = func() { once.Do(func() { cancel(); <-done }) }
	e.t.Cleanup(stop)
	return stop
}

func (e *tenv) waitStatus(consumer, id string, want Status) Job {
	e.t.Helper()
	var j Job
	eventually(e.t, func() bool { j = e.get(consumer, id); return j.Status == want }, "job %s to be %s", id, want)
	return j
}

func eventually(t *testing.T, cond func() bool, what string, args ...any) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for "+what, args...)
}

// fakeExec is an Executor whose steps a test scripts.
type fakeExec struct {
	mu      sync.Mutex
	admit   func(ctx context.Context, j Job, payload []byte) (Slot, error)
	admits  []string
	payload []byte
}

func (f *fakeExec) Admit(ctx context.Context, j Job, payload []byte) (Slot, error) {
	f.mu.Lock()
	f.admits = append(f.admits, j.ID)
	f.payload = payload
	fn := f.admit
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, j, payload)
	}
	return &fakeSlot{run: okRun}, nil
}

func (f *fakeExec) admitted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.admits...)
}

type fakeSlot struct {
	run      func(ctx context.Context) (Output, error)
	mu       sync.Mutex
	released int
}

func (s *fakeSlot) Run(ctx context.Context) (Output, error) { return s.run(ctx) }
func (s *fakeSlot) Release()                                { s.mu.Lock(); s.released++; s.mu.Unlock() }

func okRun(context.Context) (Output, error) {
	return Output{Body: []byte(`{"answer":"` + secretAnswer + `"}`), Receipt: "receipt-jws"}, nil
}

func slotOf(run func(ctx context.Context) (Output, error)) func(context.Context, Job, []byte) (Slot, error) {
	return func(context.Context, Job, []byte) (Slot, error) { return &fakeSlot{run: run}, nil }
}

// ---- idempotency ----

func TestDuplicateKeyReturnsTheSameJob(t *testing.T) {
	e := newEnv(t)
	first, created := e.create("c1", "k1", "idem-1")
	if !created || first.Status != Queued {
		t.Fatalf("first = %+v created %v", first, created)
	}
	again, created := e.create("c1", "k1", "idem-1")
	if created || again.ID != first.ID {
		t.Fatalf("duplicate: id %s created %v, want %s false", again.ID, created, first.ID)
	}
	if n := e.count(`SELECT count(*) FROM sch_job`); n != 1 {
		t.Fatalf("%d job rows, want 1", n)
	}
	// The key is per access key: another key of the same consumer, or another consumer, is a new job.
	if other, created := e.create("c1", "k2", "idem-1"); !created || other.ID == first.ID {
		t.Fatalf("another access key: id %s created %v", other.ID, created)
	}
	if other, created := e.create("c2", "k3", "idem-1"); !created || other.ID == first.ID {
		t.Fatalf("another consumer: id %s created %v", other.ID, created)
	}
	if j, created := e.create("c1", "k1", "idem-2"); !created || j.ID == first.ID {
		t.Fatalf("another key: id %s created %v", j.ID, created)
	}
}

func TestDuplicateKeyRaceCreatesOneJob(t *testing.T) {
	e := newEnv(t)
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	createdN := make(chan bool, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, created, err := e.svc.Create(context.Background(), e.newJob("c1", "k1", "same"))
			if err != nil {
				t.Error(err)
				return
			}
			ids <- j.ID
			createdN <- created
		}()
	}
	wg.Wait()
	close(ids)
	close(createdN)
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	won := 0
	for c := range createdN {
		if c {
			won++
		}
	}
	if len(seen) != 1 || won != 1 {
		t.Fatalf("%d distinct ids, %d creators; want 1 and 1", len(seen), won)
	}
}

func TestDuplicateAfterTerminalStatesStillReturnsTheExistingJob(t *testing.T) {
	e := newEnv(t)
	for i, status := range []Status{Succeeded, Failed, Cancelled, Expired} {
		idem := "idem-" + string(status)
		j, _ := e.create("c1", "k1", idem)
		reason := any(nil)
		if status == Failed {
			reason = "engine_unavailable"
		}
		e.exec(`UPDATE sch_job SET status = ?, failure_reason = ?, finished_at = ? WHERE id = ?`, string(status), reason, ms(e.clk.Now()), j.ID)
		again, created := e.create("c1", "k1", idem)
		if created || again.ID != j.ID || again.Status != status {
			t.Fatalf("%d: %s duplicate = %+v created %v", i, status, again, created)
		}
	}
}

func TestHostRestartFailsRunningJobAndTheKeyStartsANewAttempt(t *testing.T) {
	e := newEnv(t)
	running, _ := e.create("c1", "k1", "idem-r")
	queued, _ := e.create("c1", "k1", "idem-q")
	if ok, err := e.svc.markRunning(context.Background(), running.ID); err != nil || !ok {
		t.Fatalf("markRunning = %v, %v", ok, err)
	}
	n, err := e.svc.FailInterrupted(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("FailInterrupted = %d, %v; want 1", n, err)
	}
	got := e.get("c1", running.ID)
	if got.Status != Failed || got.FailureReason != ReasonHostRestart || got.FinishedAt.IsZero() {
		t.Fatalf("running job after restart = %+v", got)
	}
	if e.payloadRows(running.ID) != 0 {
		t.Fatal("payload of the interrupted job was kept")
	}
	if e.get("c1", queued.ID).Status != Queued || e.payloadRows(queued.ID) != 1 {
		t.Fatal("a queued job must survive a restart untouched")
	}

	next, created := e.create("c1", "k1", "idem-r")
	if !created || next.ID == running.ID || next.Status != Queued {
		t.Fatalf("resubmit = %+v created %v, want a new queued job", next, created)
	}
	if e.get("c1", running.ID).Status != Failed {
		t.Fatal("the old attempt must stay as history")
	}
	// The new attempt now holds the key.
	again, created := e.create("c1", "k1", "idem-r")
	if created || again.ID != next.ID {
		t.Fatalf("second resubmit = %+v created %v, want the new attempt", again, created)
	}
	// A restart does not touch jobs that are already terminal, and does nothing twice.
	if n, err := e.svc.FailInterrupted(context.Background()); err != nil || n != 0 {
		t.Fatalf("second FailInterrupted = %d, %v", n, err)
	}
}

func TestPayloadMinimizedFailureAcceptsTheKeyAgain(t *testing.T) {
	e := newEnv(t)
	old, _ := e.create("c1", "k1", "idem-m")
	// What a restore does (req_001 D-68): the job lost its payload and is failed(payload_minimized).
	e.exec(`UPDATE sch_job SET status = 'failed', failure_reason = ?, finished_at = ? WHERE id = ?`, ReasonPayloadMinimized, ms(e.clk.Now()), old.ID)
	e.exec(`DELETE FROM sch_job_payload WHERE job_id = ?`, old.ID)

	next, created := e.create("c1", "k1", "idem-m")
	if !created || next.ID == old.ID {
		t.Fatalf("resubmit = %+v created %v, want a new attempt", next, created)
	}
	if got := e.get("c1", old.ID); got.Status != Failed || got.FailureReason != ReasonPayloadMinimized {
		t.Fatalf("old attempt = %+v", got)
	}
}

func TestOtherFailureReasonsKeepTheKey(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-f")
	e.exec(`UPDATE sch_job SET status = 'failed', failure_reason = 'engine_unavailable' WHERE id = ?`, j.ID)
	again, created := e.create("c1", "k1", "idem-f")
	if created || again.ID != j.ID {
		t.Fatalf("a job that failed for another reason holds its key: %+v created %v", again, created)
	}
}

func TestQueuedJobWithoutPayloadFailsAsPayloadMinimized(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	e.exec(`DELETE FROM sch_job_payload WHERE job_id = ?`, j.ID)
	exec := &fakeExec{}
	e.run(exec)
	got := e.waitStatus("c1", j.ID, Failed)
	if got.FailureReason != ReasonPayloadMinimized || len(exec.admitted()) != 0 {
		t.Fatalf("job = %+v, admitted %v", got, exec.admitted())
	}
}

// ---- visibility ----

func TestAJobOfAnotherConsumerLooksNotFound(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	ctx := context.Background()
	if _, err := e.svc.Get(ctx, "c2", j.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get by another consumer: %v", err)
	}
	if _, err := e.svc.Cancel(ctx, "c2", j.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Cancel by another consumer: %v", err)
	}
	if _, err := e.svc.Result(ctx, "c2", j.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Result by another consumer: %v", err)
	}
	if _, err := e.svc.Get(ctx, "c1", uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get of an unknown id: %v", err)
	}
	if e.get("c1", j.ID).Status != Queued {
		t.Fatal("another consumer's cancel changed the job")
	}
}

// ---- the worker ----

func TestWorkerRunsAJobToSucceeded(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	exec := &fakeExec{}
	e.run(exec)
	got := e.waitStatus("c1", j.ID, Succeeded)
	if got.StartedAt.IsZero() || got.FinishedAt.IsZero() || !got.ResultAvailable() {
		t.Fatalf("job = %+v", got)
	}
	if want := e.clk.Now().Add(24 * time.Hour); !got.ResultExpiresAt.Equal(want) {
		t.Fatalf("result expires %v, want %v (the default 24h TTL)", got.ResultExpiresAt, want)
	}
	if !strings.Contains(string(exec.payload), secretPrompt) {
		t.Fatalf("the executor did not get the opened payload: %q", exec.payload)
	}
	res, err := e.svc.Result(context.Background(), "c1", j.ID)
	if err != nil || res.Receipt != "receipt-jws" || !strings.Contains(string(res.Body), secretAnswer) {
		t.Fatalf("Result = %+v, %v", res, err)
	}
	if e.payloadRows(j.ID) != 0 {
		t.Fatal("payload kept after success")
	}
	eventually(t, func() bool { e.mu.Lock(); defer e.mu.Unlock(); return len(e.evs) == 1 }, "the job.done event")
	e.mu.Lock()
	ev := e.evs[0]
	e.mu.Unlock()
	if ev.Type != events.JobDone || ev.TargetPrincipalID != "principal-c1" {
		t.Fatalf("event = %+v", ev)
	}
	var p map[string]any
	if err := json.Unmarshal(ev.Payload, &p); err != nil || p["job_id"] != j.ID || p["status"] != "succeeded" || p["consumer_id"] != "c1" {
		t.Fatalf("payload = %s, %v", ev.Payload, err)
	}
	if strings.Contains(string(ev.Payload), secretPrompt) || strings.Contains(string(ev.Payload), secretAnswer) {
		t.Fatalf("job.done payload carries content: %s", ev.Payload)
	}
}

func TestWorkerStartsOldestFirstAndHonoursMaxActive(t *testing.T) {
	e := newEnv(t)
	e.set(KeyMaxActive, "1")
	var ids []string
	for _, k := range []string{"a", "b", "c"} {
		j, _ := e.create("c1", "k1", k)
		ids = append(ids, j.ID)
		e.clk.Advance(time.Second)
	}
	var mu sync.Mutex
	active, peak := 0, 0
	exec := &fakeExec{admit: slotOf(func(context.Context) (Output, error) {
		mu.Lock()
		active++
		peak = max(peak, active)
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		return okRun(nil)
	})}
	e.run(exec)
	for _, id := range ids {
		e.waitStatus("c1", id, Succeeded)
	}
	got := exec.admitted()
	if strings.Join(got, ",") != strings.Join(ids, ",") {
		t.Fatalf("admitted %v, want oldest first %v", got, ids)
	}
	mu.Lock()
	defer mu.Unlock()
	if peak != 1 {
		t.Fatalf("peak concurrency %d with jobs.max_active=1", peak)
	}
}

func TestPayloadIsDeletedAtEveryTerminalState(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	// succeeded and failed through the worker
	okJob, _ := e.create("c1", "k1", "ok")
	failJob, _ := e.create("c1", "k1", "fail")
	exec := &fakeExec{admit: func(_ context.Context, j Job, _ []byte) (Slot, error) {
		if j.ID == failJob.ID {
			return &fakeSlot{run: func(context.Context) (Output, error) { return Output{}, &FailError{Reason: "engine_unavailable"} }}, nil
		}
		return &fakeSlot{run: okRun}, nil
	}}
	stop := e.run(exec)
	e.waitStatus("c1", okJob.ID, Succeeded)
	if f := e.waitStatus("c1", failJob.ID, Failed); f.FailureReason != "engine_unavailable" {
		t.Fatalf("failed job = %+v", f)
	}
	stop()
	// cancelled, expired and host_restart through the service
	cancelJob, _ := e.create("c1", "k1", "cancel")
	if _, err := e.svc.Cancel(ctx, "c1", cancelJob.ID); err != nil {
		t.Fatal(err)
	}
	expireJob, _ := e.create("c1", "k1", "expire")
	e.clk.Advance(25 * time.Hour)
	if err := e.svc.sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if e.get("c1", expireJob.ID).Status != Expired {
		t.Fatal("job past its deadline did not expire")
	}
	e.clk.Advance(-25 * time.Hour)
	restartJob, _ := e.create("c1", "k1", "restart")
	if ok, err := e.svc.markRunning(ctx, restartJob.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := e.svc.FailInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]string{"succeeded": okJob.ID, "failed": failJob.ID, "cancelled": cancelJob.ID, "expired": expireJob.ID, "host_restart": restartJob.ID} {
		if n := e.payloadRows(id); n != 0 {
			t.Errorf("%s: payload row kept", name)
		}
	}
	// A payload left behind a terminal job by anything else is swept.
	stray, _ := e.create("c1", "k1", "stray")
	e.exec(`UPDATE sch_job SET status = 'failed', failure_reason = 'payload_minimized' WHERE id = ?`, stray.ID)
	if e.payloadRows(stray.ID) != 1 {
		t.Fatal("test setup: the stray payload must exist")
	}
	if err := e.svc.sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if e.payloadRows(stray.ID) != 0 {
		t.Error("the sweep left a payload behind a terminal job")
	}
}

func TestCancelQueuedJob(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	got, err := e.svc.Cancel(context.Background(), "c1", j.ID)
	if err != nil || got.Status != Cancelled || got.FinishedAt.IsZero() {
		t.Fatalf("Cancel = %+v, %v", got, err)
	}
	if e.get("c1", j.ID).Status != Cancelled || e.payloadRows(j.ID) != 0 {
		t.Fatal("job not cancelled, or payload kept")
	}
	// Terminal states are never reopened: a second cancel returns the job unchanged.
	again, err := e.svc.Cancel(context.Background(), "c1", j.ID)
	if !errors.Is(err, ErrFinished) || again.Status != Cancelled {
		t.Fatalf("second Cancel = %+v, %v", again, err)
	}
	// The worker never starts a cancelled job.
	exec := &fakeExec{}
	e.run(exec)
	time.Sleep(50 * time.Millisecond)
	if len(exec.admitted()) != 0 {
		t.Fatalf("a cancelled job was admitted: %v", exec.admitted())
	}
}

func TestCancelRunningJobCancelsTheEngineCall(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	started := make(chan struct{})
	var sawCancel sync.WaitGroup
	sawCancel.Add(1)
	slot := &fakeSlot{run: func(ctx context.Context) (Output, error) {
		close(started)
		<-ctx.Done() // the engine call, blocked until the job is cancelled
		sawCancel.Done()
		return Output{}, ctx.Err()
	}}
	e.run(&fakeExec{admit: func(context.Context, Job, []byte) (Slot, error) { return slot, nil }})
	<-started
	if got := e.get("c1", j.ID); got.Status != Running {
		t.Fatalf("job = %+v, want running", got)
	}
	got, err := e.svc.Cancel(context.Background(), "c1", j.ID)
	if err != nil || got.Status != Cancelled {
		t.Fatalf("Cancel = %+v, %v", got, err)
	}
	sawCancel.Wait()
	eventually(t, func() bool { slot.mu.Lock(); defer slot.mu.Unlock(); return slot.released >= 1 }, "the slot to be released")
	time.Sleep(30 * time.Millisecond) // the worker must not move it again
	final := e.get("c1", j.ID)
	if final.Status != Cancelled || final.FailureReason != "" || e.resultRows(j.ID) != 0 || e.payloadRows(j.ID) != 0 {
		t.Fatalf("job after cancel = %+v result rows %d", final, e.resultRows(j.ID))
	}
}

func TestCancelWhileWaitingForASlotStopsTheAdmission(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	waiting := make(chan struct{})
	gone := make(chan error, 1)
	e.run(&fakeExec{admit: func(ctx context.Context, _ Job, _ []byte) (Slot, error) {
		close(waiting)
		<-ctx.Done() // Scheduler.Admit blocked in the queue
		gone <- context.Cause(ctx)
		return nil, ctx.Err()
	}})
	<-waiting
	if _, err := e.svc.Cancel(context.Background(), "c1", j.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case cause := <-gone:
		if !errors.Is(cause, errCancelled) {
			t.Fatalf("cause = %v", cause)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the admission was not cancelled")
	}
	if e.get("c1", j.ID).Status != Cancelled {
		t.Fatal("job not cancelled")
	}
}

func TestEngineFailureFailsTheJob(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	e.run(&fakeExec{admit: slotOf(func(context.Context) (Output, error) { return Output{}, &FailError{Reason: "engine_timeout"} })})
	got := e.waitStatus("c1", j.ID, Failed)
	if got.FailureReason != "engine_timeout" || got.ResultAvailable() || e.resultRows(j.ID) != 0 {
		t.Fatalf("job = %+v", got)
	}
}

func TestStopLeavesARunningJobRunningAndTheNextStartFailsIt(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	started := make(chan struct{})
	stop := e.run(&fakeExec{admit: slotOf(func(ctx context.Context) (Output, error) {
		close(started)
		<-ctx.Done()
		return Output{}, ctx.Err()
	})})
	<-started
	stop()
	if got := e.get("c1", j.ID); got.Status != Running {
		t.Fatalf("after a stop the job is %s, want running", got.Status)
	}
	n, err := e.svc.FailInterrupted(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("FailInterrupted = %d, %v", n, err)
	}
	if got := e.get("c1", j.ID); got.Status != Failed || got.FailureReason != ReasonHostRestart {
		t.Fatalf("job = %+v", got)
	}
}

func TestAPanickingExecutorFailsOnlyThatJob(t *testing.T) {
	e := newEnv(t)
	bad, _ := e.create("c1", "k1", "bad")
	e.clk.Advance(time.Second)
	good, _ := e.create("c1", "k1", "good")
	slot := &fakeSlot{run: func(context.Context) (Output, error) { panic("boom " + secretPrompt) }}
	e.run(&fakeExec{admit: func(_ context.Context, j Job, _ []byte) (Slot, error) {
		if j.ID == bad.ID {
			return slot, nil
		}
		return &fakeSlot{run: okRun}, nil
	}})
	got := e.waitStatus("c1", bad.ID, Failed)
	if got.FailureReason != ReasonInternal {
		t.Fatalf("job = %+v", got)
	}
	e.waitStatus("c1", good.ID, Succeeded)
	slot.mu.Lock()
	released := slot.released
	slot.mu.Unlock()
	if released == 0 {
		t.Fatal("the slot of the panicking job was not released")
	}
	e.mu.Lock()
	logs := e.logs.String()
	e.mu.Unlock()
	if strings.Contains(logs, secretPrompt) {
		t.Fatal("the panic value reached the log")
	}
}

// ---- deadline and TTL ----

func TestQueuedJobPastItsDeadlineExpires(t *testing.T) {
	e := newEnv(t)
	e.setConsumer("c1", KeyDeadline, "1h")
	j, _ := e.create("c1", "k1", "idem-1")
	if want := e.clk.Now().Add(time.Hour); !j.DeadlineAt.Equal(want) {
		t.Fatalf("deadline %v, want %v", j.DeadlineAt, want)
	}
	// The default applies to a consumer that set nothing.
	d, _ := e.create("c2", "k2", "idem-1")
	if want := e.clk.Now().Add(24 * time.Hour); !d.DeadlineAt.Equal(want) {
		t.Fatalf("default deadline %v, want %v", d.DeadlineAt, want)
	}
	// 0s means no deadline.
	e.setConsumer("c3", KeyDeadline, "0s")
	if n, _ := e.create("c3", "k3", "idem-1"); !n.DeadlineAt.IsZero() {
		t.Fatalf("deadline %v with 0s", n.DeadlineAt)
	}

	e.clk.Advance(time.Hour - time.Second)
	if err := e.svc.sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.get("c1", j.ID).Status != Queued {
		t.Fatal("expired before the deadline")
	}
	e.clk.Advance(time.Second)
	if err := e.svc.sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := e.get("c1", j.ID)
	if got.Status != Expired || got.FinishedAt.IsZero() || e.payloadRows(j.ID) != 0 {
		t.Fatalf("job = %+v", got)
	}
	if e.get("c2", d.ID).Status != Queued {
		t.Fatal("another consumer's job expired with the wrong deadline")
	}
}

func TestJobExpiresWhileWaitingForASlot(t *testing.T) {
	e := newEnv(t)
	e.setConsumer("c1", KeyDeadline, "1h")
	j, _ := e.create("c1", "k1", "idem-1")
	waiting := make(chan struct{})
	gone := make(chan error, 1)
	e.run(&fakeExec{admit: func(ctx context.Context, _ Job, _ []byte) (Slot, error) {
		close(waiting)
		<-ctx.Done()
		gone <- context.Cause(ctx)
		return nil, ctx.Err()
	}})
	<-waiting
	e.clk.Advance(2 * time.Hour)
	select {
	case cause := <-gone:
		if !errors.Is(cause, errExpired) {
			t.Fatalf("cause = %v", cause)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the admission of an expired job was not cancelled")
	}
	e.waitStatus("c1", j.ID, Expired)
}

func TestARunningJobDoesNotExpire(t *testing.T) {
	e := newEnv(t)
	e.setConsumer("c1", KeyDeadline, "1h")
	j, _ := e.create("c1", "k1", "idem-1")
	release := make(chan struct{})
	e.run(&fakeExec{admit: slotOf(func(context.Context) (Output, error) { <-release; return okRun(nil) })})
	e.waitStatus("c1", j.ID, Running)
	e.clk.Advance(2 * time.Hour)
	time.Sleep(30 * time.Millisecond)
	if got := e.get("c1", j.ID); got.Status != Running {
		t.Fatalf("running job became %s past its deadline", got.Status)
	}
	close(release)
	e.waitStatus("c1", j.ID, Succeeded)
}

func TestResultPastItsTTLIsGoneAndSwept(t *testing.T) {
	e := newEnv(t)
	e.setConsumer("c1", KeyResultTTL, "10m")
	j, _ := e.create("c1", "k1", "idem-1")
	e.run(&fakeExec{})
	got := e.waitStatus("c1", j.ID, Succeeded)
	if want := e.clk.Now().Add(10 * time.Minute); !got.ResultExpiresAt.Equal(want) {
		t.Fatalf("result expires %v, want %v", got.ResultExpiresAt, want)
	}
	ctx := context.Background()
	e.clk.Advance(10*time.Minute - time.Second)
	if _, err := e.svc.Result(ctx, "c1", j.ID); err != nil {
		t.Fatalf("Result before the TTL: %v", err)
	}
	e.clk.Advance(time.Second)
	if _, err := e.svc.Result(ctx, "c1", j.ID); !errors.Is(err, ErrResultGone) {
		t.Fatalf("Result at the TTL: %v, want ErrResultGone", err)
	}
	if e.get("c1", j.ID).ResultAvailable() {
		t.Fatal("job reports a result past its TTL")
	}
	eventually(t, func() bool { return e.resultRows(j.ID) == 0 }, "the sweep to delete the expired result")
	if _, err := e.svc.Result(ctx, "c1", j.ID); !errors.Is(err, ErrResultGone) {
		t.Fatalf("Result after the sweep: %v", err)
	}
}

func TestResultOfAJobThatHasNotSucceeded(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	if _, err := e.svc.Result(context.Background(), "c1", j.ID); !errors.Is(err, ErrNotSucceeded) {
		t.Fatalf("Result of a queued job: %v", err)
	}
}

// ---- admission outcomes ----

func TestOutsideTimeWindowKeepsTheJobQueued(t *testing.T) {
	e := newEnv(t)
	opens := e.clk.Now().Add(time.Hour)
	j, _ := e.create("c1", "k1", "idem-1")
	calls := 0
	exec := &fakeExec{admit: func(_ context.Context, _ Job, _ []byte) (Slot, error) {
		calls++
		if e.clk.Now().Before(opens) {
			return nil, &DeferError{After: opens.Sub(e.clk.Now()), Code: "outside_time_window"}
		}
		return &fakeSlot{run: okRun}, nil
	}}
	e.run(exec)
	eventually(t, func() bool { return e.get("c1", j.ID).NotBefore.Equal(opens) }, "the job to be deferred until the window opens")
	got := e.get("c1", j.ID)
	if got.Status != Queued || got.FailureReason != "" || e.payloadRows(j.ID) != 1 {
		t.Fatalf("job = %+v", got)
	}
	time.Sleep(50 * time.Millisecond)
	if calls != 1 {
		t.Fatalf("Admit called %d times before the window opened, want 1", calls)
	}
	e.clk.Advance(time.Hour)
	e.waitStatus("c1", j.ID, Succeeded)
}

func TestNotBeforeAtCreateKeepsTheJobFromTheScheduler(t *testing.T) {
	e := newEnv(t)
	opens := e.clk.Now().Add(time.Hour)
	n := e.newJob("c1", "k1", "idem-1")
	n.NotBefore = opens
	j, _, err := e.svc.Create(context.Background(), n)
	if err != nil || !j.NotBefore.Equal(opens) {
		t.Fatalf("Create = %+v, %v", j, err)
	}
	exec := &fakeExec{}
	e.run(exec)
	time.Sleep(50 * time.Millisecond)
	if len(exec.admitted()) != 0 {
		t.Fatalf("admitted before not_before: %v", exec.admitted())
	}
	e.clk.Advance(time.Hour)
	e.waitStatus("c1", j.ID, Succeeded)
}

func TestQueueFullLeavesTheJobQueuedAndRetriesAfterRetryAfter(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	var mu sync.Mutex
	calls := 0
	exec := &fakeExec{admit: func(context.Context, Job, []byte) (Slot, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			// An interactive arrival took the job's place in the full queue (req_001 D-26).
			return nil, &DeferError{After: 5 * time.Second, Code: "queue_full"}
		}
		return &fakeSlot{run: okRun}, nil
	}}
	e.run(exec)
	retryAt := e.clk.Now().Add(5 * time.Second)
	eventually(t, func() bool { return e.get("c1", j.ID).NotBefore.Equal(retryAt) }, "the retry time to be stored")
	if got := e.get("c1", j.ID); got.Status != Queued || got.FailureReason != "" {
		t.Fatalf("job = %+v, want queued", got)
	}
	e.clk.Advance(4 * time.Second)
	time.Sleep(40 * time.Millisecond)
	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 1 {
		t.Fatalf("retried after %d calls before Retry-After passed", n)
	}
	e.clk.Advance(time.Second)
	e.waitStatus("c1", j.ID, Succeeded)
}

func TestUnknownAdmitErrorRetriesAfterTheRetryInterval(t *testing.T) {
	e := newEnv(t)
	e.set(KeyRetryInterval, "7s")
	j, _ := e.create("c1", "k1", "idem-1")
	e.run(&fakeExec{admit: func(context.Context, Job, []byte) (Slot, error) { return nil, errors.New("boom") }})
	retryAt := e.clk.Now().Add(7 * time.Second)
	eventually(t, func() bool { return e.get("c1", j.ID).NotBefore.Equal(retryAt) }, "jobs.retry_interval to apply")
	if e.get("c1", j.ID).Status != Queued {
		t.Fatal("job must stay queued")
	}
}

func TestFailErrorAtAdmissionFailsTheJob(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	e.run(&fakeExec{admit: func(context.Context, Job, []byte) (Slot, error) { return nil, &FailError{Reason: "policy_denied"} }})
	got := e.waitStatus("c1", j.ID, Failed)
	if got.FailureReason != "policy_denied" || got.StartedAt != (time.Time{}) {
		t.Fatalf("job = %+v", got)
	}
}

func TestSlotIsReleasedWhenTheJobWasCancelledBeforeItStarted(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	slot := &fakeSlot{run: okRun}
	admitted := make(chan struct{})
	proceed := make(chan struct{})
	e.run(&fakeExec{admit: func(context.Context, Job, []byte) (Slot, error) {
		close(admitted)
		<-proceed
		return slot, nil // granted at the moment the consumer cancels
	}})
	<-admitted
	if _, err := e.svc.Cancel(context.Background(), "c1", j.ID); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	eventually(t, func() bool { slot.mu.Lock(); defer slot.mu.Unlock(); return slot.released == 1 }, "the unused slot to be released")
	if e.get("c1", j.ID).Status != Cancelled {
		t.Fatal("job must stay cancelled")
	}
}

// A job whose retry time could not be stored is on hold in memory: the scheduler is not offered it
// before the hold ends, and it does not crowd newer jobs out of the page.
func TestAJobOnHoldIsNotOfferedUntilTheHoldEnds(t *testing.T) {
	e := newEnv(t)
	e.set(KeyMaxActive, "1")
	held, _ := e.create("c1", "k1", "held")
	e.clk.Advance(time.Second)
	newer, _ := e.create("c1", "k1", "newer")
	e.svc.mu.Lock()
	e.svc.hold[held.ID] = e.clk.Now().Add(time.Hour)
	e.svc.mu.Unlock()

	exec := &fakeExec{}
	ctx := context.Background()
	if err := e.svc.dispatch(ctx, exec); err != nil {
		t.Fatal(err)
	}
	e.svc.wg.Wait()
	if got := exec.admitted(); len(got) != 1 || got[0] != newer.ID {
		t.Fatalf("admitted %v, want only the newer job %s", got, newer.ID)
	}
	if e.get("c1", held.ID).Status != Queued {
		t.Fatal("the held job did not stay queued")
	}

	e.clk.Advance(time.Hour)
	if err := e.svc.dispatch(ctx, exec); err != nil {
		t.Fatal(err)
	}
	e.svc.wg.Wait()
	if got := exec.admitted(); len(got) != 2 || got[1] != held.ID {
		t.Fatalf("admitted %v, want the held job %s offered after the hold", got, held.ID)
	}
	e.svc.mu.Lock()
	n := len(e.svc.hold)
	e.svc.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d hold entries left after the hold ended", n)
	}
}

func TestDefaultDurationIsTheRegisteredDefault(t *testing.T) {
	for key, want := range map[string]time.Duration{KeyPollInterval: 2 * time.Second, KeyRetryInterval: 30 * time.Second} {
		if got := defaultDuration(key); got <= 0 || got != want {
			t.Errorf("defaultDuration(%s) = %v, want %v", key, got, want)
		}
	}
}

// ---- encryption at rest ----

func TestContentIsEncryptedAtRestAndBoundToTheConsumer(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	var sealed []byte
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT sealed FROM sch_job_payload WHERE job_id = ?`, j.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte(secretPrompt)) {
		t.Fatal("the stored payload holds the prompt in the clear")
	}
	key := e.keys.DataKey()
	plain, err := hostkeys.Unseal(key, sealed, aad(tablePayload, j.ID, "c1"))
	if err != nil || !bytes.Contains(plain, []byte(secretPrompt)) {
		t.Fatalf("Unseal with the right AAD: %q, %v", plain, err)
	}
	for name, a := range map[string][]byte{
		"another consumer": aad(tablePayload, j.ID, "c2"),
		"another job":      aad(tablePayload, uuid.NewString(), "c1"),
		"another table":    aad(tableResult, j.ID, "c1"),
	} {
		if _, err := hostkeys.Unseal(key, sealed, a); !errors.Is(err, hostkeys.ErrOpen) {
			t.Errorf("Unseal with %s's AAD: %v, want ErrOpen", name, err)
		}
	}

	e.run(&fakeExec{})
	e.waitStatus("c1", j.ID, Succeeded)
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT sealed FROM sch_job_result WHERE job_id = ?`, j.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte(secretAnswer)) {
		t.Fatal("the stored result holds the answer in the clear")
	}
	if _, err := hostkeys.Unseal(key, sealed, aad(tableResult, j.ID, "c2")); !errors.Is(err, hostkeys.ErrOpen) {
		t.Errorf("result opened with another consumer's AAD: %v", err)
	}
}

func TestNoContentOutsideTheSealedBlobs(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	e.run(&fakeExec{})
	e.waitStatus("c1", j.ID, Succeeded)
	// Every non-sealed column of every table of the module, and the events and the log.
	for _, q := range []string{
		`SELECT group_concat(id || consumer_id || principal_id || access_key_id || idempotency_key || model || identity || status, '|') FROM sch_job`,
		`SELECT group_concat(receipt, '|') FROM sch_job_result`,
		`SELECT group_concat(type || payload, '|') FROM ntf_event`,
	} {
		var s string
		if err := e.db.Reader().QueryRow(context.Background(), q).Scan(&s); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(s, secretPrompt) || strings.Contains(s, secretAnswer) {
			t.Fatalf("content found by %q", q)
		}
	}
	e.mu.Lock()
	logs := e.logs.String()
	e.mu.Unlock()
	if strings.Contains(logs, secretPrompt) || strings.Contains(logs, secretAnswer) {
		t.Fatal("content in the log")
	}
}

// ---- the result is deleted only after the body was written (req_001 D-85) ----

func TestResultIsKeptUntilTheBodyWasWrittenWhole(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	e.run(&fakeExec{})
	e.waitStatus("c1", j.ID, Succeeded)
	ctx := context.Background()

	// A fetch begins and the connection drops mid-body: the write fails, so nothing is deleted.
	serve := func(w http.ResponseWriter) {
		res, err := e.svc.Result(ctx, "c1", j.ID)
		if err != nil {
			t.Fatal(err)
		}
		w.Header().Set("LS-Receipt", res.Receipt)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(res.Body)
	}
	rec := httptest.NewRecorder()
	dropped := NewDelivery(&breakingWriter{ResponseRecorder: rec, after: 5})
	serve(dropped)
	if dropped.Complete() {
		t.Fatal("a response cut mid-body was reported complete")
	}
	if e.resultRows(j.ID) != 1 {
		t.Fatal("the result was deleted although the body did not reach the consumer")
	}
	if !e.get("c1", j.ID).ResultAvailable() {
		t.Fatal("job no longer reports a result after a dropped fetch")
	}

	// A flush that fails after complete writes is also not a delivery.
	flushFails := NewDelivery(&breakingWriter{ResponseRecorder: httptest.NewRecorder(), after: -1, flushErr: io.ErrClosedPipe})
	serve(flushFails)
	if flushFails.Complete() || e.resultRows(j.ID) != 1 {
		t.Fatal("a failed flush must not delete the result")
	}

	// The next fetch delivers it whole; only then is it deleted.
	ok := NewDelivery(httptest.NewRecorder())
	serve(ok)
	if !ok.Complete() {
		t.Fatal("a fully written response was not reported complete")
	}
	if e.resultRows(j.ID) != 1 {
		t.Fatal("the result was deleted before the caller deleted it")
	}
	if err := e.svc.DeleteResult(ctx, "c1", j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Result(ctx, "c1", j.ID); !errors.Is(err, ErrResultGone) {
		t.Fatalf("Result after a delivered fetch: %v, want ErrResultGone", err)
	}
	if got := e.get("c1", j.ID); got.Status != Succeeded || got.ResultAvailable() {
		t.Fatalf("job after delivery = %+v", got)
	}
}

func TestDeleteResultIsScopedToTheConsumer(t *testing.T) {
	e := newEnv(t)
	j, _ := e.create("c1", "k1", "idem-1")
	e.run(&fakeExec{})
	e.waitStatus("c1", j.ID, Succeeded)
	if err := e.svc.DeleteResult(context.Background(), "c2", j.ID); err != nil {
		t.Fatal(err)
	}
	if e.resultRows(j.ID) != 1 {
		t.Fatal("another consumer deleted the result")
	}
}

func TestDeliveryJudgesStatusAndShortWrites(t *testing.T) {
	d := NewDelivery(httptest.NewRecorder())
	d.WriteHeader(http.StatusInternalServerError)
	_, _ = d.Write([]byte("x"))
	if d.Complete() {
		t.Error("a 500 is not a delivery")
	}
	d = NewDelivery(&breakingWriter{ResponseRecorder: httptest.NewRecorder(), after: 2, short: true})
	_, _ = d.Write([]byte("abcdef"))
	if d.Complete() {
		t.Error("a short write is not a delivery")
	}
	if NewDelivery(httptest.NewRecorder()).Complete() {
		t.Error("nothing written is not a delivery")
	}
}

// breakingWriter is a ResponseWriter whose connection drops: Write fails once after bytes have
// been accepted (a short write when short is set), and Flush fails with flushErr.
type breakingWriter struct {
	*httptest.ResponseRecorder
	after    int // bytes accepted before the failure; negative never fails
	short    bool
	flushErr error
	written  int
}

func (b *breakingWriter) Write(p []byte) (int, error) {
	if b.after >= 0 && b.written+len(p) > b.after {
		n := b.after - b.written
		_, _ = b.ResponseRecorder.Write(p[:n])
		b.written += n
		if b.short {
			return n, nil
		}
		return n, io.ErrClosedPipe
	}
	b.written += len(p)
	return b.ResponseRecorder.Write(p)
}

func (b *breakingWriter) FlushError() error {
	if b.flushErr != nil {
		return b.flushErr
	}
	b.ResponseRecorder.Flush()
	return nil
}

// ---- settings ----

func TestSettingsAreRegisteredWithOwnersAndDefaults(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		key    string
		owner  settings.Owner
		def    string
		scoped bool
	}{
		{KeyDeadline, settings.Consumer, "24h", true},
		{KeyResultTTL, settings.Consumer, "24h", true},
		{KeyPollInterval, settings.Host, "2s", false},
		{KeyRetryInterval, settings.Host, "30s", false},
		{KeyMaxActive, settings.Host, "4", false},
	} {
		d, ok := e.st.Definition(c.key)
		if !ok || d.Owner != c.owner || d.Default != c.def || d.Scoped != c.scoped {
			t.Errorf("%s = %+v, %v; want owner %s default %s scoped %v", c.key, d, ok, c.owner, c.def, c.scoped)
		}
	}
	ctx := context.Background()
	// A consumer owns its two settings and the host owns the rest.
	if err := e.st.Set(ctx, settings.Consumer, KeyPollInterval, "", "1s"); !errors.Is(err, settings.ErrNotOwner) {
		t.Errorf("a consumer set a host setting: %v", err)
	}
	if err := e.st.Set(ctx, settings.Host, KeyResultTTL, "c1", "1h"); !errors.Is(err, settings.ErrNotOwner) {
		t.Errorf("the host set a consumer setting: %v", err)
	}
	for key, bad := range map[string]string{KeyResultTTL: "0s", KeyDeadline: "-1s", KeyMaxActive: "0", KeyPollInterval: "0s"} {
		owner, scope := settings.Host, ""
		if key == KeyResultTTL || key == KeyDeadline {
			owner, scope = settings.Consumer, "c1"
		}
		if err := e.st.Set(ctx, owner, key, scope, bad); !errors.Is(err, settings.ErrRejected) {
			t.Errorf("%s=%s accepted: %v", key, bad, err)
		}
	}
}

// ---- construction ----

func TestNewNeedsItsDependencies(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New accepted an empty Config")
	}
}

func TestCreateRejectsAnIncompleteJob(t *testing.T) {
	e := newEnv(t)
	n := e.newJob("c1", "k1", "")
	if _, _, err := e.svc.Create(context.Background(), n); err == nil {
		t.Fatal("Create accepted a job with no Idempotency-Key")
	}
}

// logTelemetry captures the log, so a test can check that no content reaches it.
type logTelemetry struct {
	telemetry.Telemetry
	buf *bytes.Buffer
	mu  *sync.Mutex
}

type lockedWriter struct {
	buf *bytes.Buffer
	mu  *sync.Mutex
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func newLogTelemetry(buf *bytes.Buffer, mu *sync.Mutex) *logTelemetry {
	return &logTelemetry{Telemetry: telemetry.Nop(), buf: buf, mu: mu}
}

func (l *logTelemetry) Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(lockedWriter{l.buf, l.mu}, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
