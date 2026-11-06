package power

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// StateSource says whether the host is sharing now. *host.SharingState is the real one.
type StateSource interface {
	Effective(ctx context.Context) (host.State, error)
}

// lockHeldMetric is 1 while the sleep lock is held and 0 while it is not.
const lockHeldMetric = "power_sleep_lock_held"

// Keeper holds the sleep lock exactly while the host's effective sharing state is Sharing
// (req_001 D-30, D-06, D-38): it releases it while the host is paused or in quiet hours. It
// reads the state every power.check_interval; it does not change the sharing state.
type Keeper struct {
	power Power
	src   StateSource
	st    *settings.Store
	log   *slog.Logger
	held  telemetry.Gauge

	mu   sync.Mutex
	lock Lock
	// lostWarned is set when a lock was found dead, and cleared once a lock has survived a check,
	// so a helper that dies at once is warned about once, not every interval.
	lostWarned bool
	// lastErr is the text of the last error logged for each kind of failure; a failure repeating
	// with the same text is not logged again until something else happens.
	lastErr map[string]string
}

// NewKeeper registers the power settings with st and returns a Keeper that takes locks from p.
func NewKeeper(p Power, src StateSource, st *settings.Store, tel telemetry.Telemetry) (*Keeper, error) {
	if err := st.Register(Defs()...); err != nil {
		return nil, err
	}
	k := &Keeper{power: p, src: src, st: st, log: tel.Logger(), held: tel.Gauge(lockHeldMetric), lastErr: map[string]string{}}
	k.held.Set(0)
	return k, nil
}

// Held reports whether the Keeper holds a live lock now.
func (k *Keeper) Held() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.lock != nil && k.lock.Alive()
}

// Run checks the sharing state every power.check_interval until ctx ends, then releases the
// lock. It is safe to run under a supervisor that restarts it: the lock is released whenever Run
// returns or panics.
func (k *Keeper) Run(ctx context.Context) error {
	defer k.release("worker stopped")
	for {
		wait := k.Check(ctx)
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-t.C:
		}
	}
}

// Check reads the setting and the sharing state once and takes or releases the lock to match. It
// returns how long to wait before the next check.
func (k *Keeper) Check(ctx context.Context) time.Duration {
	wait, err := k.st.Duration(ctx, KeyCheckInterval, "")
	if err != nil {
		k.logOnce(ctx, "interval", err, "power: read check interval; using the default")
		wait = 0
		if d, ok := k.st.Definition(KeyCheckInterval); ok {
			wait, _ = time.ParseDuration(d.Default)
		}
	} else if wait <= 0 {
		// The check is off: hold nothing, and look again at the default pace for a change.
		k.release("check interval is off")
		d, _ := time.ParseDuration(defaultCheckInterval)
		return d
	}
	k.apply(ctx)
	return wait
}

// apply takes or releases the lock for the effective sharing state.
func (k *Keeper) apply(ctx context.Context) {
	state, err := k.src.Effective(ctx)
	if err != nil {
		k.logOnce(ctx, "state", err, "power: read sharing state")
	} else {
		k.clearErr("state")
	}
	if state == "" {
		return // the state is unknown: leave the lock as it is
	}
	if state != host.Sharing {
		k.release("sharing is " + string(state))
		return
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	if k.lock != nil {
		if k.lock.Alive() {
			k.lostWarned = false
			return
		}
		if !k.lostWarned {
			k.lostWarned = true
			k.log.WarnContext(ctx, "the sleep lock was lost; taking it again")
		}
		_ = k.lock.Release()
		k.lock = nil
		k.held.Set(0)
	}
	l, err := k.power.Acquire(ctx)
	if err != nil {
		k.logOnceLocked(ctx, "acquire", err, "power: take the sleep lock")
		return
	}
	delete(k.lastErr, "acquire")
	k.lock = l
	k.held.Set(1)
	k.log.InfoContext(ctx, "sleep lock taken")
}

// release gives the lock back if one is held.
func (k *Keeper) release(why string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.lock == nil {
		return
	}
	if err := k.lock.Release(); err != nil {
		k.log.Warn("power: release the sleep lock", "error", err)
	}
	k.lock = nil
	k.lostWarned = false
	k.held.Set(0)
	k.log.Info("sleep lock released", "reason", why)
}

func (k *Keeper) logOnce(ctx context.Context, kind string, err error, msg string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.logOnceLocked(ctx, kind, err, msg)
}

func (k *Keeper) logOnceLocked(ctx context.Context, kind string, err error, msg string) {
	if k.lastErr[kind] == err.Error() {
		return
	}
	k.lastErr[kind] = err.Error()
	k.log.ErrorContext(ctx, msg, "error", err)
}

func (k *Keeper) clearErr(kind string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.lastErr, kind)
}
