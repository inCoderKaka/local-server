package limits

import (
	"context"
	"fmt"
	"time"

	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// CountAdmitted records that the gateway admitted one call of id, metered or not. Call it once
// per call, when the scheduler has admitted it and before the call runs. For every request
// quota entry that applies to the call (the grant version's, counted for the consumer, and the
// key's, counted for the key) it raises the persisted request counter of the window that
// contains the moment of admission, in one unit of work. Check reads those counters, so a count
// survives a restart and a call is charged to the window it was admitted in (req_001 D-107).
// A call with no request entry writes nothing. Counters of windows that ended more than
// limits.counter_retention ago are pruned from here, at most once per a quarter of that
// retention per process; a failed prune is logged and does not fail the call. If the entries
// cannot be read or the counters cannot be written, the call is not counted and the caller logs
// the error.
func (l *Limiter) CountAdmitted(ctx context.Context, id *auth.Identity) error {
	now := l.now()
	key, err := l.cfg.Identity.AccessKeys.Get(ctx, l.cfg.DB.Reader(), id.KeyID)
	if err != nil {
		return fmt.Errorf("limits: load key: %w", err)
	}
	es, err := l.entries(ctx, id, key)
	if err != nil {
		return err
	}
	var adds []entry
	for _, e := range es {
		if e.Metric == UnitRequests {
			adds = append(adds, e)
		}
	}
	if len(adds) > 0 {
		err := l.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
			for _, e := range adds {
				start, _ := windowOf(now, e.Window)
				if err := usage.AddRequest(tx, e.by, e.byID, e.Window, start); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("limits: count request: %w", err)
		}
	}
	l.pruneCounters(ctx, now)
	return nil
}

// pruneCounters deletes the request counters of windows that ended more than
// limits.counter_retention before now, when a quarter of that retention has passed since this
// process last tried. The interval comes from the setting, so there is no second figure.
func (l *Limiter) pruneCounters(ctx context.Context, now time.Time) {
	retention, err := l.cfg.Settings.Duration(ctx, KeyCounterRetention, "")
	if err != nil || retention <= 0 {
		if err != nil {
			l.log.Error("limits: read counter retention", "err", err)
		}
		return
	}
	l.mu.Lock()
	due := l.lastPruned.IsZero() || now.Sub(l.lastPruned) >= retention/4
	if due {
		l.lastPruned = now
	}
	l.mu.Unlock()
	if !due {
		return
	}
	err = l.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
		_, err := usage.PruneRequestCounters(tx, now.Add(-retention))
		return err
	})
	if err != nil {
		l.log.Error("limits: prune request counters", "err", err)
	}
}
