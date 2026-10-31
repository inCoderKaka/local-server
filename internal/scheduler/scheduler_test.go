package scheduler_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/engine/fake"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

const gib = int64(1) << 30

// fakeClock is a manual clock: timers fire only when Advance passes them.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	clk     *fakeClock
	at      time.Time
	c       chan time.Time
	stopped bool
}

func (t *fakeTimer) C() <-chan time.Time { return t.c }
func (t *fakeTimer) Stop() bool {
	t.clk.mu.Lock()
	defer t.clk.mu.Unlock()
	t.stopped = true
	return true
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) NewTimer(d time.Duration) scheduler.Timer {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTimer{clk: f, at: f.now.Add(d), c: make(chan time.Time, 1)}
	f.timers = append(f.timers, t)
	return t
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	for _, t := range f.timers {
		if !t.stopped && !t.at.After(f.now) {
			t.stopped = true
			t.c <- f.now
		}
	}
}

type fakeSharing struct {
	mu sync.Mutex
	s  host.State
}

func (f *fakeSharing) Effective(context.Context) (host.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.s, nil
}

func (f *fakeSharing) set(s host.State) { f.mu.Lock(); f.s = s; f.mu.Unlock() }

type env struct {
	t   *testing.T
	ctx context.Context
	clk *fakeClock
	eng *fake.Engine
	st  *settings.Store
	sh  *fakeSharing
	s   *scheduler.Scheduler
}

func newEnv(t *testing.T, models ...fake.ModelSpec) *env {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"), settings.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	clk := &fakeClock{now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}
	if len(models) == 0 {
		models = []fake.ModelSpec{{Name: "m1", ContextLength: 8192, MemoryBytes: gib}, {Name: "m2", ContextLength: 8192, MemoryBytes: gib}}
	}
	e := &env{t: t, ctx: ctx, clk: clk, st: settings.New(db), sh: &fakeSharing{s: host.Sharing},
		eng: fake.New(fake.WithModels(models...), fake.WithClock(clk.Now))}
	e.s, err = scheduler.New(e.eng, e.st, e.sh, scheduler.Options{Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	// Roomy defaults; each test narrows what it tests.
	e.set(scheduler.KeyTotalConcurrency, "10")
	e.set(scheduler.KeyOllamaNumParallel, "10")
	e.set(scheduler.KeyGrantConcurrency, "10")
	e.set(scheduler.KeyQueueWait, "1h")
	return e
}

func (e *env) set(key, val string) {
	e.t.Helper()
	if err := e.st.Set(e.ctx, settings.Host, key, "", val); err != nil {
		e.t.Fatal(err)
	}
}

type result struct {
	tk  *scheduler.Ticket
	err error
}

// admitAsync starts Admit in a goroutine.
func (e *env) admitAsync(req scheduler.Request) <-chan result {
	ch := make(chan result, 1)
	go func() {
		tk, err := e.s.Admit(e.ctx, req)
		ch <- result{tk, err}
	}()
	return ch
}

func (e *env) admit(req scheduler.Request) *scheduler.Ticket {
	e.t.Helper()
	tk, err := e.s.Admit(e.ctx, req)
	if err != nil {
		e.t.Fatalf("Admit: %v", err)
	}
	return tk
}

func (e *env) waitQueued(n int) {
	e.t.Helper()
	for i := 0; i < 2000; i++ {
		if e.s.Stats().Queued == n {
			// Let the waiter reach its select.
			time.Sleep(2 * time.Millisecond)
			return
		}
		time.Sleep(time.Millisecond)
	}
	e.t.Fatalf("queue length = %d, want %d", e.s.Stats().Queued, n)
}

func (e *env) got(ch <-chan result) result {
	e.t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		e.t.Fatal("Admit did not return")
		return result{}
	}
}

func (e *env) notYet(ch <-chan result) {
	e.t.Helper()
	select {
	case r := <-ch:
		e.t.Fatalf("Admit returned early: %+v", r)
	case <-time.After(20 * time.Millisecond):
	}
}

func req(grant string, class scheduler.Class, model string) scheduler.Request {
	return scheduler.Request{GrantID: grant, Class: class, Model: model, EstimatedCtxTokens: 100}
}

func TestBackgroundNeverStarvesUnderInteractiveLoad(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyTotalConcurrency, "1")
	e.set(scheduler.KeyOllamaNumParallel, "1")
	e.set(scheduler.KeyBackgroundAging, "30s")

	held := e.admit(req("a", scheduler.ClassInteractive, "m1"))
	bg := e.admitAsync(req("job", scheduler.ClassBackground, "m1"))
	e.waitQueued(1)

	// Steady interactive load: a new interactive request every 10s, one slot.
	for round := 1; round <= 10; round++ {
		e.clk.Advance(10 * time.Second)
		next := e.admitAsync(req("a", scheduler.ClassInteractive, "m1"))
		e.waitQueued(2)
		held.Release()
		select {
		case r := <-bg:
			if r.err != nil {
				t.Fatal(r.err)
			}
			if round < 3 {
				t.Fatalf("background admitted at round %d, before it aged", round)
			}
			r.tk.Release()
			if r := e.got(next); r.err != nil { // the interactive one follows
				t.Fatal(r.err)
			}
			return
		case r := <-next:
			if r.err != nil {
				t.Fatal(r.err)
			}
			held = r.tk
		case <-time.After(5 * time.Second):
			t.Fatal("nobody admitted")
		}
	}
	t.Fatal("background starved for 100s of steady interactive load")
}

func TestInteractiveGoesAheadOfFreshBackground(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyTotalConcurrency, "1")
	e.set(scheduler.KeyOllamaNumParallel, "1")
	held := e.admit(req("a", scheduler.ClassInteractive, "m1"))
	bg := e.admitAsync(req("job", scheduler.ClassBackground, "m1"))
	e.waitQueued(1)
	e.clk.Advance(time.Second)
	in := e.admitAsync(req("b", scheduler.ClassInteractive, "m1"))
	e.waitQueued(2)
	held.Release()
	r := e.got(in)
	if r.err != nil {
		t.Fatal(r.err)
	}
	e.notYet(bg)
	r.tk.Release()
	if r := e.got(bg); r.err != nil {
		t.Fatal(r.err)
	}
}

func TestQueueFullReturnsRetryAfter(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyTotalConcurrency, "1")
	e.set(scheduler.KeyOllamaNumParallel, "1")
	e.set(scheduler.KeyQueueLength, "1")
	e.set(scheduler.KeyRetryAfter, "7s")
	held := e.admit(req("a", scheduler.ClassInteractive, "m1"))
	queued := e.admitAsync(req("a", scheduler.ClassInteractive, "m1"))
	e.waitQueued(1)

	_, err := e.s.Admit(e.ctx, req("a", scheduler.ClassInteractive, "m1"))
	var se *scheduler.Error
	if !errors.Is(err, scheduler.ErrQueueFull) || !errors.As(err, &se) || se.RetryAfter != 7*time.Second {
		t.Fatalf("err = %v, want queue_full with Retry-After 7s", err)
	}
	if got := e.s.Stats().Queued; got != 1 {
		t.Fatalf("queued = %d after the refusal, want 1", got)
	}
	held.Release()
	if r := e.got(queued); r.err != nil {
		t.Fatal(r.err)
	}
}

func TestQueueWaitLimit(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyTotalConcurrency, "1")
	e.set(scheduler.KeyOllamaNumParallel, "1")
	e.set(scheduler.KeyQueueWait, "10s")
	held := e.admit(req("a", scheduler.ClassInteractive, "m1"))
	w := e.admitAsync(req("a", scheduler.ClassInteractive, "m1"))
	e.waitQueued(1)
	e.clk.Advance(10 * time.Second)
	r := e.got(w)
	if !errors.Is(r.err, scheduler.ErrQueueFull) {
		t.Fatalf("err = %v, want queue_full after the wait limit", r.err)
	}
	if e.s.Stats().Queued != 0 {
		t.Fatal("expired request still queued")
	}
	held.Release()
}

func TestPerGrantConcurrency(t *testing.T) {
	e := newEnv(t)
	a1 := e.admit(scheduler.Request{GrantID: "a", GrantConcurrency: 1, Class: scheduler.ClassInteractive, Model: "m1"})
	a2 := e.admitAsync(scheduler.Request{GrantID: "a", GrantConcurrency: 1, Class: scheduler.ClassInteractive, Model: "m1"})
	e.waitQueued(1)
	// Another grant is not held up behind it.
	b := e.admit(scheduler.Request{GrantID: "b", GrantConcurrency: 1, Class: scheduler.ClassInteractive, Model: "m1"})
	e.notYet(a2)
	a1.Release()
	r := e.got(a2)
	if r.err != nil {
		t.Fatal(r.err)
	}
	r.tk.Release()
	b.Release()

	// A request that sets no limit gets the setting.
	e.set(scheduler.KeyGrantConcurrency, "2")
	x1 := e.admit(req("c", scheduler.ClassInteractive, "m1"))
	x2 := e.admit(req("c", scheduler.ClassInteractive, "m1"))
	x3 := e.admitAsync(req("c", scheduler.ClassInteractive, "m1"))
	e.waitQueued(1)
	e.notYet(x3)
	x1.Release()
	r = e.got(x3)
	r.tk.Release()
	x2.Release()
}

func TestTotalConcurrencyCappedByOllamaNumParallel(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyTotalConcurrency, "10")
	e.set(scheduler.KeyOllamaNumParallel, "2")
	t1 := e.admit(req("a", scheduler.ClassInteractive, "m1"))
	t2 := e.admit(req("b", scheduler.ClassInteractive, "m1"))
	w := e.admitAsync(req("c", scheduler.ClassInteractive, "m1"))
	e.waitQueued(1)
	e.notYet(w)
	if got := e.s.Stats().Running; got != 2 {
		t.Fatalf("running = %d, want 2", got)
	}
	t1.Release()
	r := e.got(w)
	r.tk.Release()
	t2.Release()

	// The smaller of the two settings wins the other way too.
	e.set(scheduler.KeyOllamaNumParallel, "10")
	e.set(scheduler.KeyTotalConcurrency, "1")
	u1 := e.admit(req("a", scheduler.ClassInteractive, "m1"))
	w = e.admitAsync(req("b", scheduler.ClassInteractive, "m1"))
	e.waitQueued(1)
	u1.Release()
	e.got(w).tk.Release()
}

func TestMemoryAdmission(t *testing.T) {
	e := newEnv(t,
		fake.ModelSpec{Name: "small", ContextLength: 8192, MemoryBytes: 4 * gib},
		fake.ModelSpec{Name: "other", ContextLength: 8192, MemoryBytes: 4 * gib},
		fake.ModelSpec{Name: "huge", ContextLength: 8192, MemoryBytes: 8 * gib})
	e.set(scheduler.KeyMemoryBudgetBytes, "6442450944") // 6 GiB
	e.set(scheduler.KeyContextBytesPerToken, "1048576") // 1 MiB per token

	// Can never fit: refused at once, not queued.
	_, err := e.s.Admit(e.ctx, req("a", scheduler.ClassInteractive, "huge"))
	if !errors.Is(err, scheduler.ErrOverBudget) {
		t.Fatalf("huge: err = %v, want over_budget", err)
	}
	// Model alone fits but model plus context does not.
	_, err = e.s.Admit(e.ctx, scheduler.Request{GrantID: "a", Class: scheduler.ClassInteractive, Model: "small", EstimatedCtxTokens: 3000})
	if !errors.Is(err, scheduler.ErrOverBudget) {
		t.Fatalf("small with 3000 tokens: err = %v, want over_budget", err)
	}

	small := e.admit(req("a", scheduler.ClassInteractive, "small"))
	if l, _ := e.eng.Loaded(e.ctx); len(l) != 1 || l[0].Name != "small" {
		t.Fatalf("loaded = %+v, want small loaded by Admit", l)
	}
	// "other" would push the loaded total to 8 GiB and "small" is running: queue.
	o := e.admitAsync(req("b", scheduler.ClassInteractive, "other"))
	e.waitQueued(1)
	e.notYet(o)
	// small finishes: it is idle, so it is unloaded to make room and "other" runs.
	small.Release()
	r := e.got(o)
	if r.err != nil {
		t.Fatal(r.err)
	}
	l, _ := e.eng.Loaded(e.ctx)
	if len(l) != 1 || l[0].Name != "other" {
		t.Fatalf("loaded = %+v, want only other (small reclaimed)", l)
	}
	r.tk.Release()
}

func TestMemoryAdmissionCountsRunningContexts(t *testing.T) {
	e := newEnv(t, fake.ModelSpec{Name: "m", ContextLength: 8192, MemoryBytes: gib})
	e.set(scheduler.KeyMemoryBudgetBytes, "2684354560") // 2.5 GiB: the model and one 1 GiB context fit, a second context does not
	e.set(scheduler.KeyContextBytesPerToken, "1048576")
	r := func() scheduler.Request {
		return scheduler.Request{GrantID: "a", Class: scheduler.ClassInteractive, Model: "m", EstimatedCtxTokens: 1024}
	}
	t1 := e.admit(r())
	t2 := e.admitAsync(r())
	e.waitQueued(1)
	e.notYet(t2)
	t1.Release()
	e.got(t2).tk.Release()
}

func TestPausedAndQuiet(t *testing.T) {
	for _, st := range []host.State{host.Paused, host.Quiet} {
		e := newEnv(t)
		e.sh.set(st)
		e.set(scheduler.KeyRetryAfter, "9s")
		_, err := e.s.Admit(e.ctx, req("a", scheduler.ClassInteractive, "m1"))
		var se *scheduler.Error
		if !errors.Is(err, scheduler.ErrHostPaused) || !errors.As(err, &se) || se.RetryAfter != 0 {
			t.Fatalf("%s: err = %v, want host_paused with no Retry-After", st, err)
		}

		// Background waits, and runs once the host resumes.
		bg := e.admitAsync(req("job", scheduler.ClassBackground, "m1"))
		e.waitQueued(1)
		e.notYet(bg)
		e.sh.set(host.Sharing)
		e.clk.Advance(2 * time.Second) // the recheck interval passes
		r := e.got(bg)
		if r.err != nil {
			t.Fatal(r.err)
		}
		r.tk.Release()
	}
}

func TestQueuedInteractiveFailsWhenHostPauses(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyTotalConcurrency, "1")
	e.set(scheduler.KeyOllamaNumParallel, "1")
	held := e.admit(req("a", scheduler.ClassInteractive, "m1"))
	w := e.admitAsync(req("b", scheduler.ClassInteractive, "m1"))
	e.waitQueued(1)
	e.sh.set(host.Paused)
	e.clk.Advance(2 * time.Second)
	if r := e.got(w); !errors.Is(r.err, scheduler.ErrHostPaused) {
		t.Fatalf("err = %v, want host_paused", r.err)
	}
	// The running request is not touched.
	if e.s.Stats().Running != 1 {
		t.Fatal("running request disturbed by the pause")
	}
	held.Release()
}

func TestShrinkingLimitsNeverInterruptsRunning(t *testing.T) {
	e := newEnv(t)
	t1 := e.admit(req("a", scheduler.ClassInteractive, "m1"))
	t2 := e.admit(req("b", scheduler.ClassInteractive, "m1"))
	e.set(scheduler.KeyTotalConcurrency, "1")
	e.set(scheduler.KeyMemoryBudgetBytes, "1") // far below what is loaded
	w := e.admitAsync(req("c", scheduler.ClassInteractive, "m2"))
	// m2 can never fit a 1 byte budget.
	if r := e.got(w); !errors.Is(r.err, scheduler.ErrOverBudget) {
		t.Fatalf("err = %v, want over_budget", r.err)
	}
	e.set(scheduler.KeyMemoryBudgetBytes, "0")
	w = e.admitAsync(req("c", scheduler.ClassInteractive, "m1"))
	e.waitQueued(1)
	if got := e.s.Stats().Running; got != 2 {
		t.Fatalf("running = %d, want both kept running", got)
	}
	l, _ := e.eng.Loaded(e.ctx)
	if len(l) != 1 || l[0].Name != "m1" {
		t.Fatalf("loaded = %+v; the model of running requests must stay", l)
	}
	t1.Release()
	e.notYet(w) // one still running and the limit is now 1
	t2.Release()
	e.got(w).tk.Release()
}

func TestContextCancelLeavesQueue(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyTotalConcurrency, "1")
	e.set(scheduler.KeyOllamaNumParallel, "1")
	held := e.admit(req("a", scheduler.ClassInteractive, "m1"))
	ctx, cancel := context.WithCancel(e.ctx)
	ch := make(chan error, 1)
	go func() { _, err := e.s.Admit(ctx, req("b", scheduler.ClassInteractive, "m1")); ch <- err }()
	e.waitQueued(1)
	cancel()
	if err := <-ch; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if e.s.Stats().Queued != 0 {
		t.Fatal("cancelled request still queued")
	}
	held.Release()
	if e.s.Stats().Running != 0 {
		t.Fatal("slot leaked")
	}
}

func TestLoadFailureFreesSlot(t *testing.T) {
	e := newEnv(t)
	e.eng.InjectError(fake.OpLoad, engine.Errorf(engine.CodeEngineUnavailable, nil, "down"), 1)
	_, err := e.s.Admit(e.ctx, req("a", scheduler.ClassInteractive, "m1"))
	if !errors.Is(err, engine.ErrEngineUnavailable) {
		t.Fatalf("err = %v, want engine_unavailable", err)
	}
	if e.s.Stats().Running != 0 {
		t.Fatal("slot leaked after load failure")
	}
}

func TestTicketReleaseIsIdempotent(t *testing.T) {
	e := newEnv(t)
	tk := e.admit(req("a", scheduler.ClassInteractive, "m1"))
	tk.Release()
	tk.Release()
	if s := e.s.Stats(); s.Running != 0 {
		t.Fatalf("running = %d", s.Running)
	}
}

func TestAdmitValidates(t *testing.T) {
	e := newEnv(t)
	if _, err := e.s.Admit(e.ctx, scheduler.Request{Class: scheduler.ClassInteractive}); err == nil {
		t.Fatal("missing model accepted")
	}
	if _, err := e.s.Admit(e.ctx, scheduler.Request{Model: "m1"}); err == nil {
		t.Fatal("missing class accepted")
	}
}

// cancelOnLoaded cancels a context the first time the scheduler reads the loaded models,
// which is inside the dispatch that grants the request: the client "disconnects" as the
// grant happens.
type cancelOnLoaded struct {
	*fake.Engine
	once   sync.Once
	cancel context.CancelFunc
}

func (c *cancelOnLoaded) Loaded(ctx context.Context) ([]engine.LoadedModel, error) {
	c.once.Do(c.cancel)
	return c.Engine.Loaded(ctx)
}

func TestCancelAtGrantDoesNotLeakLoading(t *testing.T) {
	e := newEnv(t)
	for round := 0; round < 32; round++ {
		ctx, cancel := context.WithCancel(e.ctx)
		w := &cancelOnLoaded{Engine: e.eng, cancel: cancel}
		s, err := scheduler.New(w, e.st, e.sh, scheduler.Options{Clock: e.clk})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Admit(ctx, req("a", scheduler.ClassInteractive, "m1")); err == nil {
			t.Fatalf("round %d: Admit with a cancelled context returned a ticket", round)
		}
		cancel()
		tk, err := s.Admit(context.Background(), req("a", scheduler.ClassInteractive, "m1"))
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if !loadedNames(t, e)["m1"] {
			t.Fatalf("round %d: m1 not loaded after a later Admit (loading marker leaked)", round)
		}
		tk.Release()
		if err := e.eng.Unload(e.ctx, "m1"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAgedBackgroundNotStarvedByMemory(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyMemoryBudgetBytes, "1610612736") // 1.5 GiB: one model at a time
	e.set(scheduler.KeyBackgroundAging, "30s")

	held := e.admit(req("a", scheduler.ClassInteractive, "m1"))
	bg := e.admitAsync(req("job", scheduler.ClassBackground, "m2"))
	e.waitQueued(1)

	for round := 1; round <= 9; round++ {
		e.clk.Advance(10 * time.Second)
		next := e.admitAsync(req("a", scheduler.ClassInteractive, "m1"))
		settled := false
		for i := 0; i < 2000 && !settled; i++ {
			st := e.s.Stats()
			settled = st.Running == 2 || st.Queued == 2
			if !settled {
				time.Sleep(time.Millisecond)
			}
		}
		if !settled {
			t.Fatalf("round %d: the interactive request is neither running nor queued", round)
		}
		held.Release()
		select {
		case r := <-bg:
			if r.err != nil {
				t.Fatal(r.err)
			}
			if round < 3 {
				t.Fatalf("background admitted at round %d, before it aged", round)
			}
			e.notYet(next) // m2 is running and m1 does not fit beside it
			r.tk.Release()
			r2 := e.got(next)
			if r2.err != nil {
				t.Fatal(r2.err)
			}
			r2.tk.Release()
			return
		case r := <-next:
			if r.err != nil {
				t.Fatal(r.err)
			}
			held = r.tk
		case <-time.After(5 * time.Second):
			t.Fatal("nobody admitted")
		}
	}
	t.Fatal("aged background request starved by memory for 90s of overlapping interactive load")
}

func TestOverBudgetBesidePreloadedModels(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyMemoryBudgetBytes, "1610612736") // 1.5 GiB
	e.set(scheduler.KeyPreloadModels, "m1")
	if err := e.s.Apply(e.ctx); err != nil {
		t.Fatal(err)
	}
	for _, class := range []scheduler.Class{scheduler.ClassInteractive, scheduler.ClassBackground} {
		_, err := e.s.Admit(e.ctx, req("a", class, "m2"))
		if !errors.Is(err, scheduler.ErrOverBudget) {
			t.Fatalf("%s m2: err = %v, want over_budget beside the preloaded m1", class, err)
		}
		if q := e.s.Stats().Queued; q != 0 {
			t.Fatalf("%s m2: queued = %d, want 0", class, q)
		}
	}
	// The preloaded model itself is not counted twice.
	e.admit(req("a", scheduler.ClassInteractive, "m1")).Release()
}

func TestSettingsValidators(t *testing.T) {
	e := newEnv(t)
	for key, val := range map[string]string{
		scheduler.KeyRetryAfter:       "-1s",
		scheduler.KeyRecheckInterval:  "0s",
		scheduler.KeyTotalConcurrency: "0",
	} {
		err := e.st.Set(e.ctx, settings.Host, key, "", val)
		if !errors.Is(err, settings.ErrRejected) {
			t.Fatalf("Set %s %q: err = %v, want ErrRejected", key, val, err)
		}
	}
	if err := e.st.Set(e.ctx, settings.Host, scheduler.KeyQueueLength, "", "0"); err != nil {
		t.Fatalf("queue_length 0: %v", err)
	}
}
