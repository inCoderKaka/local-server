package scheduler

import (
	"context"
	"errors"
	"fmt"
)

// Apply reconciles the loaded models with the preload setting: it loads each preloaded
// model that is not loaded yet and keeps it until unloaded, and hands models that left
// the setting back to the normal keep-alive policy. To make room under the memory budget
// it unloads idle models that are not preloaded (reclaim). Call it at start and when the
// setting changes (Run does both). It tries every model and returns the joined errors. A
// preload that would not fit the memory budget is skipped with ErrOverBudget (req_001
// D-22, D-26). It does not hold the dispatch lock while a model loads, so a slow load
// never blocks Admit or Release.
func (s *Scheduler) Apply(ctx context.Context) error {
	toLoad, errs, err := s.planPreload(ctx)
	if err != nil {
		return err
	}
	for _, m := range toLoad {
		// A negative keep-alive keeps the model until it is unloaded.
		err := s.eng.Load(ctx, m, -1)
		s.mu.Lock()
		delete(s.loading, m)
		if err != nil {
			delete(s.pinned, m)
		}
		s.mu.Unlock()
		if err != nil {
			errs = append(errs, fmt.Errorf("preload %s: %w", m, err))
		}
	}
	return errors.Join(errs...)
}

// planPreload is the first phase of Apply, under the dispatch lock: it unpins models
// that left the setting, makes room for each preload, and marks it pinned and loading so
// concurrent admissions and sweeps leave it alone. It returns the models to load, the
// errors of the ones it skipped, and an error if it could not plan at all.
func (s *Scheduler) planPreload(ctx context.Context) (toLoad []string, errs []error, _ error) {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, nil, err
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

	lm, err := s.eng.Loaded(ctx)
	if err != nil {
		return nil, nil, err
	}
	snap := &snapshot{loaded: map[string]int64{}, sizes: map[string]int64{}, noReclaim: map[string]bool{}}
	for _, m := range lm {
		snap.loaded[m.Name] = m.SizeBytes
		snap.sizes[m.Name] = m.SizeBytes
	}
preloads:
	for _, m := range cfg.preload {
		_, isLoaded := snap.loaded[m]
		s.mu.Lock()
		_, busy := s.loading[m]
		skip := busy || (s.pinned[m] && isLoaded)
		s.mu.Unlock()
		if skip {
			continue
		}
		if !isLoaded && cfg.budget > 0 {
			size, err := s.modelSize(ctx, snap.loaded, m)
			if err != nil {
				errs = append(errs, fmt.Errorf("preload %s: %w", m, err))
				continue
			}
			snap.sizes[m] = size
			s.mu.Lock()
			unloads, ok := s.fitsLocked(cfg, snap, &waiter{req: Request{Model: m}})
			s.mu.Unlock()
			if !ok {
				errs = append(errs, fmt.Errorf("preload %s: %w", m,
					&Error{Code: CodeOverBudget, Message: "loaded models, running contexts and this model exceed the memory budget"}))
				continue
			}
			for _, u := range unloads {
				if err := s.eng.Unload(ctx, u); err != nil {
					errs = append(errs, fmt.Errorf("preload %s: unload %s: %w", m, u, err))
					s.unloadFailures.Inc("reclaim")
					snap.noReclaim[u] = true
					continue preloads
				}
				s.unloads.Inc("reclaim")
				delete(snap.loaded, u)
				s.mu.Lock()
				delete(s.lastUsed, u)
				s.mu.Unlock()
			}
		}
		s.mu.Lock()
		s.loading[m] = snap.sizes[m]
		s.pinned[m] = true
		s.mu.Unlock()
		snap.loaded[m] = snap.sizes[m]
		toLoad = append(toLoad, m)
	}
	return toLoad, errs, nil
}

// Sweep unloads every loaded model that is idle (no running request, not being loaded,
// not preloaded) and has been unused for the keep-alive setting (idle unload). A
// negative keep-alive never unloads. A model first seen loaded is timed from now.
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
			s.unloadFailures.Inc("idle")
			continue
		}
		s.unloads.Inc("idle")
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
		interval := defaultMaintenanceInterval
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
