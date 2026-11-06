package power

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// fakeState is a StateSource whose answer a test sets.
type fakeState struct {
	mu    sync.Mutex
	state host.State
	err   error
}

func (f *fakeState) set(s host.State, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state, f.err = s, err
}

func (f *fakeState) Effective(context.Context) (host.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, f.err
}

// fakePower is the Power of the Keeper tests: it counts and records every lock it hands out.
type fakePower struct {
	mu       sync.Mutex
	acquired int
	locks    []*fakeLock
	err      error
	noop     bool // hand out the lock of a host that has nothing to hold
}

type fakeLock struct {
	released atomic.Int32
	dead     atomic.Bool
	err      error // why it died; set before dead
}

func (l *fakeLock) Release() error { l.released.Add(1); return nil }
func (l *fakeLock) Alive() bool    { return !l.dead.Load() && l.released.Load() == 0 }
func (l *fakeLock) Err() error {
	if l.dead.Load() {
		return l.err
	}
	return nil
}

func (p *fakePower) Acquire(context.Context) (Lock, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return nil, p.err
	}
	p.acquired++
	if p.noop {
		return noopLock{}, nil
	}
	l := &fakeLock{}
	p.locks = append(p.locks, l)
	return l, nil
}

func (p *fakePower) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.acquired
}

func (p *fakePower) lock(i int) *fakeLock {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.locks[i]
}

type keeperEnv struct {
	k     *Keeper
	p     *fakePower
	src   *fakeState
	st    *settings.Store
	log   *lockedBuf
	gauge *fakeGauge
}

func newKeeperEnv(t *testing.T) *keeperEnv {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"), settings.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	log, buf := testLogger()
	e := &keeperEnv{p: &fakePower{}, src: &fakeState{state: host.Sharing}, st: settings.New(db), log: buf, gauge: &fakeGauge{}}
	e.k, err = NewKeeper(e.p, e.src, e.st, logTel{Telemetry: telemetry.Nop(), log: log, gauge: e.gauge})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *keeperEnv) set(t *testing.T, v string) {
	t.Helper()
	if err := e.st.Set(context.Background(), settings.Host, KeyCheckInterval, "", v); err != nil {
		t.Fatal(err)
	}
}

func TestKeeperDefaultInterval(t *testing.T) {
	e := newKeeperEnv(t)
	if got := e.k.Check(context.Background()); got != 15*time.Second {
		t.Errorf("default check interval = %v, want 15s", got)
	}
	e.set(t, "2s")
	if got := e.k.Check(context.Background()); got != 2*time.Second {
		t.Errorf("check interval = %v, want 2s", got)
	}
}

// The lock is held in the sharing state, released while paused and when quiet hours start, and
// taken again afterwards; an unchanged state does not take it twice.
func TestKeeperFollowsSharingState(t *testing.T) {
	e := newKeeperEnv(t)
	ctx := context.Background()

	e.k.Check(ctx)
	e.k.Check(ctx)
	if !e.k.Held() || e.p.count() != 1 {
		t.Fatalf("sharing: held=%v acquired=%d; want held once", e.k.Held(), e.p.count())
	}

	e.src.set(host.Paused, nil)
	e.k.Check(ctx)
	if e.k.Held() || e.p.lock(0).released.Load() != 1 {
		t.Fatalf("paused: held=%v released=%d; want released", e.k.Held(), e.p.lock(0).released.Load())
	}

	e.src.set(host.Sharing, nil)
	e.k.Check(ctx)
	if !e.k.Held() || e.p.count() != 2 {
		t.Fatalf("resumed: held=%v acquired=%d; want held again", e.k.Held(), e.p.count())
	}

	e.src.set(host.Quiet, nil)
	e.k.Check(ctx)
	e.k.Check(ctx)
	if e.k.Held() || e.p.lock(1).released.Load() != 1 {
		t.Fatalf("quiet: held=%v released=%d; want released once", e.k.Held(), e.p.lock(1).released.Load())
	}
}

func TestKeeperTakesNothingWhilePausedFromTheStart(t *testing.T) {
	e := newKeeperEnv(t)
	e.src.set(host.Paused, nil)
	e.k.Check(context.Background())
	if e.k.Held() || e.p.count() != 0 {
		t.Errorf("held=%v acquired=%d; want nothing taken", e.k.Held(), e.p.count())
	}
}

// A lock whose helper died is dropped and taken again, with one warning however often it repeats.
func TestKeeperTakesALostLockAgain(t *testing.T) {
	e := newKeeperEnv(t)
	ctx := context.Background()
	e.k.Check(ctx)
	e.p.lock(0).dead.Store(true)
	e.k.Check(ctx)
	if e.p.count() != 2 || !e.k.Held() {
		t.Fatalf("acquired=%d held=%v; want a second lock held", e.p.count(), e.k.Held())
	}
	if e.p.lock(0).released.Load() != 1 {
		t.Error("the dead lock was not released")
	}
	e.p.lock(1).dead.Store(true)
	e.k.Check(ctx)
	if n := countOf(e.log.String(), "the sleep lock was lost"); n != 1 {
		t.Errorf("warned %d times, want 1:\n%s", n, e.log.String())
	}
}

// The warning for a lost lock carries the reason the helper ended, when the lock knows it.
func TestKeeperLostWarningSaysWhy(t *testing.T) {
	e := newKeeperEnv(t)
	ctx := context.Background()
	e.k.Check(ctx)
	e.p.lock(0).err = errors.New("caffeinate ended: exit status 1")
	e.p.lock(0).dead.Store(true)
	e.k.Check(ctx)
	if out := e.log.String(); !strings.Contains(out, "the sleep lock was lost") || !strings.Contains(out, "exit status 1") {
		t.Errorf("the lost warning lacks the reason:\n%s", out)
	}
}

// A host with no sleep lock keeps its no-op lock, so it is not retried, but reports nothing held:
// the gauge stays 0, Held is false and no "taken" or "released" line is logged.
func TestKeeperHoldsNothingOnAHostWithoutASleepLock(t *testing.T) {
	e := newKeeperEnv(t)
	ctx := context.Background()
	e.p.noop = true
	e.k.Check(ctx)
	e.k.Check(ctx)
	if e.p.count() != 1 {
		t.Errorf("acquired %d times, want 1 (not retried)", e.p.count())
	}
	if e.k.Held() {
		t.Error("Held is true for a lock that holds nothing")
	}
	if v := e.gauge.value(); v != 0 {
		t.Errorf("gauge = %v, want 0", v)
	}
	if strings.Contains(e.log.String(), "sleep lock taken") {
		t.Errorf("logged a lock taken that holds nothing:\n%s", e.log.String())
	}
	e.src.set(host.Paused, nil)
	e.k.Check(ctx)
	if strings.Contains(e.log.String(), "sleep lock released") {
		t.Errorf("logged a release of a lock that held nothing:\n%s", e.log.String())
	}
}

// A check interval that fails to read, reads fine, then fails the same way again is logged both
// times: a good read clears the repeat suppression.
func TestKeeperIntervalErrorLoggedAgainAfterAGoodRead(t *testing.T) {
	e := newKeeperEnv(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel() // a read with this context fails with the same text each time
	e.k.Check(cancelled)
	e.k.Check(cancelled)
	if n := countOf(e.log.String(), "read check interval"); n != 1 {
		t.Fatalf("logged the same error %d times in a row, want 1:\n%s", n, e.log.String())
	}
	e.k.Check(context.Background())
	e.k.Check(cancelled)
	if n := countOf(e.log.String(), "read check interval"); n != 2 {
		t.Errorf("logged %d times, want 2 (once before and once after the good read):\n%s", n, e.log.String())
	}
}

// A state that cannot be read leaves the lock as it is; an unusable schedule, which comes with
// the manual state, is still honoured.
func TestKeeperStateErrors(t *testing.T) {
	e := newKeeperEnv(t)
	ctx := context.Background()
	e.k.Check(ctx)

	e.src.set("", errors.New("database is locked"))
	e.k.Check(ctx)
	e.k.Check(ctx)
	if !e.k.Held() || e.p.count() != 1 {
		t.Fatalf("unreadable state: held=%v acquired=%d; want the lock kept", e.k.Held(), e.p.count())
	}
	if n := countOf(e.log.String(), "read sharing state"); n != 1 {
		t.Errorf("logged the same error %d times, want 1", n)
	}

	e.src.set(host.Paused, host.ErrInvalidSchedule)
	e.k.Check(ctx)
	if e.k.Held() {
		t.Error("a pause with an unusable schedule must release the lock")
	}
	e.src.set(host.Sharing, host.ErrInvalidSchedule)
	e.k.Check(ctx)
	if !e.k.Held() {
		t.Error("sharing with an unusable schedule must hold the lock")
	}
}

func TestKeeperAcquireFailureIsRetried(t *testing.T) {
	e := newKeeperEnv(t)
	ctx := context.Background()
	e.p.err = errors.New("boom")
	e.k.Check(ctx)
	e.k.Check(ctx)
	if e.k.Held() {
		t.Fatal("held after a failed acquire")
	}
	if n := countOf(e.log.String(), "take the sleep lock"); n != 1 {
		t.Errorf("logged %d times, want 1", n)
	}
	e.p.mu.Lock()
	e.p.err = nil
	e.p.mu.Unlock()
	e.k.Check(ctx)
	if !e.k.Held() {
		t.Error("not held after the failure cleared")
	}
}

// A check interval of zero turns the lock off; it is released and not retaken until it is on.
func TestKeeperIntervalOffReleases(t *testing.T) {
	e := newKeeperEnv(t)
	ctx := context.Background()
	e.k.Check(ctx)
	e.set(t, "0s")
	if got := e.k.Check(ctx); got != 15*time.Second {
		t.Errorf("wait while off = %v, want the 15s default", got)
	}
	if e.k.Held() {
		t.Error("still held with the check off")
	}
	e.set(t, "1s")
	e.k.Check(ctx)
	if !e.k.Held() {
		t.Error("not held after turning the check back on")
	}
}

// Run takes the lock, follows a change, and releases the lock when ctx ends.
func TestKeeperRun(t *testing.T) {
	e := newKeeperEnv(t)
	e.set(t, "5ms")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.k.Run(ctx) }()

	waitUntil(t, "lock held", e.k.Held)
	e.src.set(host.Paused, nil)
	waitUntil(t, "lock released on pause", func() bool { return !e.k.Held() })
	e.src.set(host.Sharing, nil)
	waitUntil(t, "lock held again", e.k.Held)

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if e.k.Held() {
		t.Error("lock still held after shutdown")
	}
	last := e.p.lock(e.p.count() - 1)
	if last.released.Load() != 1 {
		t.Errorf("last lock released %d times, want 1", last.released.Load())
	}
}

func waitUntil(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func countOf(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}

// logTel is a telemetry.Telemetry that logs to a chosen logger and records nothing else.
type logTel struct {
	telemetry.Telemetry
	log   *slog.Logger
	gauge telemetry.Gauge // returned for every gauge when set
}

func (l logTel) Logger() *slog.Logger { return l.log }

func (l logTel) Gauge(name string, labelNames ...string) telemetry.Gauge {
	if l.gauge != nil {
		return l.gauge
	}
	return l.Telemetry.Gauge(name, labelNames...)
}

// fakeGauge is a telemetry.Gauge that keeps the last value it was given.
type fakeGauge struct {
	mu  sync.Mutex
	val float64
}

func (g *fakeGauge) Set(v float64, _ ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.val = v
}

func (g *fakeGauge) Add(d float64, _ ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.val += d
}

func (g *fakeGauge) Delete(...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.val = 0
}

func (g *fakeGauge) value() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.val
}
