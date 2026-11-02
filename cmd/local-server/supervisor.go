package main

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// supervisor runs the background workers (req_001 D-38, D-39 F9 step 5). A worker that panics,
// returns an error, or returns nil before shutdown is started again after a backoff, and each
// restart is logged with the worker's name and counted in supervisor_worker_restarts_total.
// Nothing restarts once the context is cancelled.
type supervisor struct {
	log      *slog.Logger
	restarts telemetry.Counter
	// backoff returns the wait before a restart. It is read at each restart, so a settings
	// change applies to the next one. A result of zero or less means fallbackBackoff.
	backoff func() time.Duration
	// sleep waits d or until ctx ends; it returns false if ctx ended first. Tests replace it.
	sleep func(ctx context.Context, d time.Duration) bool

	wg sync.WaitGroup
}

// restartsMetric counts worker restarts by worker name (module_thing_total, label worker).
const restartsMetric = "supervisor_worker_restarts_total"

// newSupervisor builds a supervisor logging and counting through tel.
func newSupervisor(tel telemetry.Telemetry, backoff func() time.Duration) *supervisor {
	return &supervisor{
		log:      tel.Logger(),
		restarts: tel.Counter(restartsMetric, "worker"),
		backoff:  backoff,
		sleep:    realSleep,
	}
}

func realSleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Go starts the worker name in its own goroutine and restarts it as described on supervisor.
// fn must return when ctx is cancelled.
func (s *supervisor) Go(ctx context.Context, name string, fn func(ctx context.Context) error) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			reason := s.runOnce(ctx, name, fn)
			if ctx.Err() != nil {
				return
			}
			s.restarts.Inc(name)
			wait := s.backoff()
			if wait <= 0 {
				wait = defaultRestartBackoff
			}
			s.log.ErrorContext(ctx, "worker stopped; restarting", "worker", name, "reason", reason, "restart_in", wait.String())
			if !s.sleep(ctx, wait) {
				return
			}
		}
	}()
}

// Wait blocks until every worker has returned after ctx was cancelled.
func (s *supervisor) Wait() { s.wg.Wait() }

// runOnce runs fn once and says why it ended. A panic is recovered and logged with its stack.
func (s *supervisor) runOnce(ctx context.Context, name string, fn func(context.Context) error) (reason string) {
	defer func() {
		if r := recover(); r != nil {
			reason = "panic"
			s.log.ErrorContext(ctx, "worker panicked", "worker", name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	if err := fn(ctx); err != nil {
		return "error: " + err.Error()
	}
	return "returned"
}
