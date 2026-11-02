package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// recTelemetry records counters and logs.
type recTelemetry struct {
	telemetry.Telemetry
	log  *slog.Logger
	buf  *syncBuffer
	mu   sync.Mutex
	incs map[string]int
}

func newRecTelemetry() *recTelemetry {
	buf := &syncBuffer{}
	return &recTelemetry{Telemetry: telemetry.Nop(), buf: buf, incs: map[string]int{},
		log: slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))}
}

func (r *recTelemetry) Logger() *slog.Logger { return r.log }
func (r *recTelemetry) Counter(name string, _ ...string) telemetry.Counter {
	return recCounter{r, name}
}

func (r *recTelemetry) count(name, label string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.incs[name+"|"+label]
}

type recCounter struct {
	r    *recTelemetry
	name string
}

func (c recCounter) Inc(l ...string) { c.Add(1, l...) }
func (c recCounter) Add(d float64, l ...string) {
	c.r.mu.Lock()
	c.r.incs[c.name+"|"+strings.Join(l, ",")] += int(d)
	c.r.mu.Unlock()
}

// newTestSupervisor returns a supervisor whose sleep returns at once and records the backoff.
func newTestSupervisor(tel telemetry.Telemetry, backoff time.Duration) (*supervisor, *[]time.Duration) {
	var mu sync.Mutex
	var waits []time.Duration
	s := newSupervisor(tel, func() time.Duration { return backoff })
	s.sleep = func(ctx context.Context, d time.Duration) bool {
		mu.Lock()
		waits = append(waits, d)
		mu.Unlock()
		return ctx.Err() == nil
	}
	return s, &waits
}

// A worker that panics, errors or returns early is restarted after the backoff, named in the log,
// and counted; the panic is recovered with its stack.
func TestSupervisorRestartsWorker(t *testing.T) {
	cases := []struct {
		name   string
		fn     func(ctx context.Context) error
		reason string
	}{
		{"panic", func(context.Context) error { panic("boom") }, "panic"},
		{"error", func(context.Context) error { return errors.New("it broke") }, "it broke"},
		{"nil return", func(context.Context) error { return nil }, "returned"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tel := newRecTelemetry()
			sup, waits := newTestSupervisor(tel, 7*time.Second)
			ctx, cancel := context.WithCancel(context.Background())
			runs := make(chan struct{}, 10)
			sup.Go(ctx, "demo-worker", func(ctx context.Context) error {
				runs <- struct{}{}
				if len(runs) >= 3 {
					<-ctx.Done()
					return ctx.Err()
				}
				return tc.fn(ctx)
			})
			waitFor(t, "three runs", func() bool { return len(runs) >= 3 })
			cancel()
			sup.Wait()

			if got := tel.count(restartsMetric, "demo-worker"); got != 2 {
				t.Errorf("restarts counted = %d, want 2", got)
			}
			for _, w := range *waits {
				if w != 7*time.Second {
					t.Errorf("waited %v before a restart, want the injected backoff 7s", w)
				}
			}
			contains(t, "log", tel.buf.String(), "worker=demo-worker", tc.reason)
			if tc.name == "panic" {
				contains(t, "log", tel.buf.String(), "worker panicked", "boom", "goroutine")
			}
		})
	}
}

func TestSupervisorStopsWhenContextCancelled(t *testing.T) {
	tel := newRecTelemetry()
	sup, _ := newTestSupervisor(tel, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	sup.Go(ctx, "w", func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return nil // returns nil on shutdown: must not count as a restart
	})
	<-started
	cancel()
	sup.Wait()
	if got := tel.count(restartsMetric, "w"); got != 0 {
		t.Errorf("a worker that stopped for shutdown was counted as restarted %d times", got)
	}
}

func TestSupervisorBackoffWaitStopsOnCancel(t *testing.T) {
	sup := newSupervisor(newRecTelemetry(), func() time.Duration { return time.Hour })
	ctx, cancel := context.WithCancel(context.Background())
	failed := make(chan struct{})
	sup.Go(ctx, "w", func(context.Context) error {
		select {
		case <-failed:
		default:
			close(failed)
		}
		return errors.New("x")
	})
	<-failed
	cancel()
	done := make(chan struct{})
	go func() { sup.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return although the worker sat in an hour-long backoff")
	}
}

// A backoff of zero or less (an unreadable setting) falls back to the registered default.
func TestSupervisorBackoffFallsBackToDefault(t *testing.T) {
	sup, waits := newTestSupervisor(newRecTelemetry(), 0)
	ctx, cancel := context.WithCancel(context.Background())
	var n atomic.Int32
	sup.Go(ctx, "w", func(ctx context.Context) error {
		if n.Add(1) >= 2 {
			<-ctx.Done()
		}
		return errors.New("x")
	})
	waitFor(t, "second run", func() bool { return n.Load() >= 2 })
	cancel()
	sup.Wait()
	if len(*waits) == 0 || (*waits)[0] != defaultRestartBackoff {
		t.Errorf("waits = %v, want the default %v", *waits, defaultRestartBackoff)
	}
}
