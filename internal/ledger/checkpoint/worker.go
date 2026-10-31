package checkpoint

import (
	"context"
	"time"
)

// Worker signs checkpoints when they are due and rotates host keys on schedule.
type Worker struct {
	svc   *Service
	clock Clock
	// OnError receives an error of a cycle; the worker carries on. Nil logs it.
	OnError func(error)

	lastCheckpoint time.Time
	haveLast       bool
	nextKeyCheck   time.Time
	started        bool
}

// NewWorker returns the checkpoint worker of svc, on the service's clock.
func NewWorker(svc *Service) *Worker { return &Worker{svc: svc, clock: svc.clock} }

// Run runs cycles until ctx ends, one every ledger.checkpoint_poll_interval, and returns
// ctx.Err(). A cycle error goes to OnError; it never stops the worker.
func (w *Worker) Run(ctx context.Context) error {
	for {
		if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			if w.OnError != nil {
				w.OnError(err)
			} else {
				logger().Warn("ledger checkpoint worker", "error", err)
			}
		}
		poll, err := w.svc.settings.Duration(ctx, KeyPollInterval, "")
		if err != nil || poll <= 0 {
			poll = 5 * time.Second // setting unreadable: stay alive on the registered default
		}
		t := w.clock.NewTimer(poll)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C():
		}
	}
}

// RunOnce is one cycle: sign a checkpoint if one is due, then rotate keys if the check
// interval has passed. Both parts run even if the first fails; the errors are joined.
func (w *Worker) RunOnce(ctx context.Context) error {
	cpErr := w.checkpointIfDue(ctx)
	keyErr := w.rotateIfDue(ctx)
	if cpErr != nil {
		return cpErr
	}
	return keyErr
}

func (w *Worker) checkpointIfDue(ctx context.Context) error {
	s := w.svc
	if !w.started {
		w.started = true
		if cp, err := s.Latest(ctx); err == nil {
			w.lastCheckpoint, w.haveLast = cp.CreatedAt, true
		}
	}
	leaves, err := s.LeafCount(ctx)
	if err != nil {
		return err
	}
	var covered int64
	if cp, err := s.Latest(ctx); err == nil {
		covered = cp.TreeSize
	}
	fresh := leaves - covered
	if fresh <= 0 {
		return nil
	}
	n, err := s.settings.Int(ctx, KeyMinLeaves, "")
	if err != nil {
		return err
	}
	every, err := s.settings.Duration(ctx, KeyMaxInterval, "")
	if err != nil {
		return err
	}
	now := w.clock.Now()
	due := (n > 0 && fresh >= n) || !w.haveLast || now.Sub(w.lastCheckpoint) >= every
	if !due {
		return nil
	}
	_, made, err := s.Build(ctx)
	if err != nil {
		return err
	}
	if made {
		w.lastCheckpoint, w.haveLast = now, true
	}
	return nil
}

func (w *Worker) rotateIfDue(ctx context.Context) error {
	now := w.clock.Now()
	if w.nextKeyCheck.After(now) {
		return nil
	}
	every, err := w.svc.settings.Duration(ctx, KeyKeyCheckEvery, "")
	if err != nil {
		return err
	}
	if _, err := w.svc.keys.EnsureCurrent(ctx); err != nil {
		return err
	}
	w.nextKeyCheck = now.Add(every)
	return nil
}
