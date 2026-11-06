package power

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProc is a Process that records Stop. It never starts anything.
type fakeProc struct {
	done  chan struct{}
	stops atomic.Int32
	once  sync.Once
}

func newFakeProc() *fakeProc { return &fakeProc{done: make(chan struct{})} }

func (p *fakeProc) Done() <-chan struct{} { return p.done }
func (p *fakeProc) Stop() error {
	p.stops.Add(1)
	p.once.Do(func() { close(p.done) })
	return nil
}

// die ends the process as if it had crashed.
func (p *fakeProc) die() { p.once.Do(func() { close(p.done) }) }

// fakeRunner is a Runner that records what it was asked to start and starts nothing.
type fakeRunner struct {
	mu    sync.Mutex
	calls [][]string
	procs []*fakeProc
	err   error
}

func (r *fakeRunner) Start(name string, args ...string) (Process, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string{name}, args...))
	if r.err != nil {
		return nil, r.err
	}
	p := newFakeProc()
	r.procs = append(r.procs, p)
	return p, nil
}

func (r *fakeRunner) started() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *fakeRunner) proc(i int) *fakeProc {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.procs[i]
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func testLogger() (*slog.Logger, *lockedBuf) {
	buf := &lockedBuf{}
	return slog.New(slog.NewTextHandler(buf, nil)), buf
}

func TestCommands(t *testing.T) {
	d := darwinCommand(42)
	if d.name != "caffeinate" || strings.Join(d.args, " ") != "-i -w 42" {
		t.Errorf("darwin = %v", d)
	}
	l := linuxCommand(42)
	got := l.name + " " + strings.Join(l.args, " ")
	for _, want := range []string{"systemd-inhibit", "--what=sleep:idle", "--mode=block", "tail --pid=42 -f /dev/null"} {
		if !strings.Contains(got, want) {
			t.Errorf("linux command %q lacks %q", got, want)
		}
	}
}

func TestProcessPowerAcquireAndIdempotentRelease(t *testing.T) {
	r := &fakeRunner{}
	p := newProcessPower(r, darwinCommand, 7, nil)
	l, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !l.Alive() || r.started() != 1 {
		t.Fatalf("alive=%v started=%d", l.Alive(), r.started())
	}
	if got := strings.Join(r.calls[0], " "); got != "caffeinate -i -w 7" {
		t.Errorf("started %q", got)
	}
	for i := 0; i < 3; i++ {
		if err := l.Release(); err != nil {
			t.Fatal(err)
		}
	}
	if n := r.proc(0).stops.Load(); n != 1 {
		t.Errorf("process stopped %d times, want 1", n)
	}
	if l.Alive() {
		t.Error("released lock is still alive")
	}
}

func TestProcessLockNotAliveWhenHelperDies(t *testing.T) {
	r := &fakeRunner{}
	l, err := newProcessPower(r, darwinCommand, 7, nil).Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r.proc(0).die()
	if l.Alive() {
		t.Error("lock alive after its helper died")
	}
}

func TestMissingToolIsLoggedOnceAndNotAnError(t *testing.T) {
	log, buf := testLogger()
	r := &fakeRunner{}
	r.err = errors.Join(ErrToolMissing, errors.New("caffeinate"))
	p := newProcessPower(r, darwinCommand, 7, log)
	for i := 0; i < 3; i++ {
		l, err := p.Acquire(context.Background())
		if err != nil || l == nil {
			t.Fatalf("Acquire = %v, %v; want a lock and no error", l, err)
		}
		if !l.Alive() {
			t.Error("a no-op lock must report alive so it is not retried")
		}
		if err := l.Release(); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(buf.String(), "sleep lock unavailable"); n != 1 {
		t.Errorf("logged %d times, want 1:\n%s", n, buf.String())
	}
}

func TestOtherStartErrorIsAnError(t *testing.T) {
	r := &fakeRunner{err: errors.New("permission denied")}
	if l, err := newProcessPower(r, linuxCommand, 7, nil).Acquire(context.Background()); err == nil || l != nil {
		t.Errorf("Acquire = %v, %v; want an error", l, err)
	}
}

func TestThreadPower(t *testing.T) {
	var mu sync.Mutex
	var calls []uintptr
	p := newThreadPower(func(f uintptr) error {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, f)
		return nil
	})
	l, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !l.Alive() {
		t.Error("not alive after Acquire")
	}
	for i := 0; i < 3; i++ {
		if err := l.Release(); err != nil {
			t.Fatal(err)
		}
	}
	if l.Alive() {
		t.Error("alive after Release")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 || calls[0] != esContinuous|esSystemRequired || calls[1] != esContinuous {
		t.Errorf("SetThreadExecutionState calls = %#x; want [ES_CONTINUOUS|ES_SYSTEM_REQUIRED, ES_CONTINUOUS]", calls)
	}
}

func TestThreadPowerSetFailure(t *testing.T) {
	p := newThreadPower(func(uintptr) error { return errors.New("denied") })
	if l, err := p.Acquire(context.Background()); err == nil || l != nil {
		t.Errorf("Acquire = %v, %v; want an error", l, err)
	}
}

// TestExecRunner runs this test binary as the helper program (never caffeinate or
// systemd-inhibit): Start returns at once, Stop kills it and waits, and a second Stop is fine.
func TestExecRunner(t *testing.T) {
	if os.Getenv("POWER_TEST_HELPER") == "1" {
		time.Sleep(time.Minute)
		return
	}
	t.Setenv("POWER_TEST_HELPER", "1")
	proc, err := ExecRunner{}.Start(os.Args[0], "-test.run=TestExecRunner")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-proc.Done():
		t.Fatal("helper exited by itself")
	case <-time.After(50 * time.Millisecond):
	}
	if err := proc.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-proc.Done():
	default:
		t.Error("Done not closed after Stop")
	}
	if err := proc.Stop(); err != nil {
		t.Errorf("second Stop = %v", err)
	}
}

func TestExecRunnerMissingTool(t *testing.T) {
	_, err := ExecRunner{}.Start("local-server-no-such-tool-xyz")
	if !errors.Is(err, ErrToolMissing) {
		t.Errorf("Start = %v; want ErrToolMissing", err)
	}
}
