package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Apply reconciles the loaded models with the preload setting: it loads each preloaded
// model that is not loaded yet and keeps it until unloaded, and returns models that
// left the setting to the normal keep-alive policy. Call it at start and when the
// setting changes (Run does both). It tries every model and returns the joined
// errors. A preload that would not fit the memory budget is skipped with ErrOverBudget
// (req_001 D-22, D-26).
func (s *Scheduler) Apply(ctx context.Context) error {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, m := range cfg.preload {
		want[m] = true
	}
	now := s.clock.Now()
	s.mu.Lock()
	for m := range s.pinned {
		if !want[m] {
			delete(s.pinned, m)
			s.lastUsed[m] = now // idle time counts from the unpin
		}
	}
	s.mu.Unlock()

	loaded, err := s.eng.Loaded(ctx)
	if err != nil {
		return err
	}
	var used int64
	in := map[string]bool{}
	for _, m := range loaded {
		used += m.SizeBytes
		in[m.Name] = true
	}
	var errs []error
	for _, m := range cfg.preload {
		s.mu.Lock()
		already := s.pinned[m] && in[m]
		s.mu.Unlock()
		if already {
			continue
		}
		if !in[m] && cfg.budget > 0 {
			size, err := s.modelSize(ctx, m)
			if err != nil {
				errs = append(errs, fmt.Errorf("preload %s: %w", m, err))
				continue
			}
			if used+size > cfg.budget {
				errs = append(errs, fmt.Errorf("preload %s: %w", m,
					&Error{Code: CodeOverBudget, Message: "loaded models plus this model exceed the share budget"}))
				continue
			}
			used += size
		}
		// A negative keep-alive keeps the model until it is unloaded.
		if err := s.eng.Load(ctx, m, -1); err != nil {
			errs = append(errs, fmt.Errorf("preload %s: %w", m, err))
			continue
		}
		s.mu.Lock()
		s.pinned[m] = true
		s.mu.Unlock()
	}
	return errors.Join(errs...)
}

// Sweep unloads every loaded model that is idle (no running request, not being loaded,
// not preloaded) and has been unused for the keep-alive setting (reclaim of idle
// memory). A negative keep-alive never unloads. A model first seen loaded is timed from
// now.
func (s *Scheduler) Sweep(ctx context.Context) error {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	loaded, err := s.eng.Loaded(ctx)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	in := map[string]bool{}
	var errs []error
	for _, m := range loaded {
		in[m.Name] = true
		s.mu.Lock()
		_, busy := s.loading[m.Name]
		busy = busy || s.byModel[m.Name] > 0 || s.pinned[m.Name]
		last, seen := s.lastUsed[m.Name]
		if !seen {
			s.lastUsed[m.Name] = now
		}
		s.mu.Unlock()
		if busy || !seen || cfg.keepAlive < 0 || now.Sub(last) < cfg.keepAlive {
			continue
		}
		if err := s.eng.Unload(ctx, m.Name); err != nil {
			errs = append(errs, fmt.Errorf("unload %s: %w", m.Name, err))
			continue
		}
		s.mu.Lock()
		delete(s.lastUsed, m.Name)
		s.mu.Unlock()
	}
	s.mu.Lock()
	for n := range s.lastUsed { // forget models the engine unloaded itself
		if !in[n] && s.byModel[n] == 0 {
			delete(s.lastUsed, n)
		}
	}
	s.mu.Unlock()
	return errors.Join(errs...)
}

// Run is the lifecycle worker: it applies the preload setting at start and then, every
// maintenance interval (a setting read each round), applies changes, unloads idle
// models and re-checks the queue. It returns when ctx is done.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		interval := time.Second
		if err := s.Apply(ctx); err != nil {
			s.log.Warn("scheduler: preload", "err", err)
		}
		if err := s.Sweep(ctx); err != nil {
			s.log.Warn("scheduler: sweep", "err", err)
		}
		if err := s.dispatch(ctx); err != nil {
			s.log.Warn("scheduler: dispatch", "err", err)
		}
		if cfg, err := s.loadConfig(ctx); err == nil {
			interval = cfg.maintenance
		}
		t := s.clock.NewTimer(interval)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C():
		}
	}
}
