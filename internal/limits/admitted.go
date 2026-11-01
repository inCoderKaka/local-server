package limits

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// admitted counts the calls the gateway admitted, per subject and quota window, so the request
// quota counts every admitted call and not only the metered ones (req_001 D-100). It holds
// numbers keyed by id and window start: no content. It lives in memory; after a restart a
// counter is seeded from the use_record rows of the window, the floor the metered calls give.
// A counter is seeded again after a failed seed read.
type admitted struct {
	mu       sync.Mutex
	counts   map[admitKey]int64
	unseeded map[admitKey]bool // counters whose seed read failed; the next admission reads again
	swept    int64             // the newest window start (ms) the counters were last swept for
}

type admitKey struct {
	by    usage.By
	id    string
	start int64 // window start, Unix milliseconds
}

func newAdmitted() *admitted {
	return &admitted{counts: map[admitKey]int64{}, unseeded: map[admitKey]bool{}}
}

// peek returns the counter of k, or 0 when the process has admitted no call for it yet.
func (a *admitted) peek(k admitKey) int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.counts[k]
}

// needsSeed reports whether k has no counter yet or its counter was started without a seed.
func (a *admitted) needsSeed(k admitKey) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.counts[k]
	return !ok || a.unseeded[k]
}

// add raises the counter of k by one. A counter that does not exist yet starts at seed, the
// number of metered records already in the window; seeded is false when that read failed, and
// then the counter is seeded on a later add. Counters of earlier windows are dropped the first
// time a newer window is seen, so the map holds the current window only.
func (a *admitted) add(k admitKey, seed int64, seeded bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if k.start > a.swept {
		for old := range a.counts {
			if old.start < k.start {
				delete(a.counts, old)
				delete(a.unseeded, old)
			}
		}
		a.swept = k.start
	}
	n, ok := a.counts[k]
	wasUnseeded := a.unseeded[k]
	if !ok || (wasUnseeded && seeded) {
		n = max(n, seed)
	}
	a.counts[k] = n + 1
	if !ok || wasUnseeded {
		if seeded {
			delete(a.unseeded, k)
		} else {
			a.unseeded[k] = true
		}
	}
}

// CountAdmitted records that the gateway admitted one call of id, metered or not. Call it once
// per call, when the scheduler has admitted it and before the call runs. It raises the request
// counters of the key and of the consumer for the current quota window; Check reads them in
// place of counting use_record rows (the larger of the two wins, so a restart never lowers a
// count below the metered calls on record). If the quota window setting cannot be read, the
// call is not counted. If the window's metered records cannot be read, the call is counted on
// its own and the next admission in the window reads them again. The caller logs the error.
func (l *Limiter) CountAdmitted(ctx context.Context, id *auth.Identity) error {
	_, start, end, err := l.quotaWindow(ctx, l.now())
	if err != nil {
		return err
	}
	var firstErr error
	for _, s := range []struct {
		by usage.By
		id string
	}{{usage.ByConsumer, id.ConsumerID}, {usage.ByAccessKey, id.KeyID}} {
		k := admitKey{by: s.by, id: s.id, start: start.UnixMilli()}
		seed, seeded := int64(0), true
		if l.admits.needsSeed(k) {
			n, serr := usage.Count(ctx, l.cfg.DB.Reader(), s.by, s.id, start, end)
			seed, seeded = n, serr == nil
			if serr != nil && firstErr == nil {
				firstErr = fmt.Errorf("limits: seed request counter: %w", serr)
			}
		}
		l.admits.add(k, seed, seeded)
	}
	return firstErr
}

// requestsUsed returns the calls of a subject in [from, to): the larger of the admitted count
// kept in memory and the metered records on disk.
func (l *Limiter) requestsUsed(ctx context.Context, by usage.By, id string, from, to time.Time) (int64, error) {
	n, err := usage.Count(ctx, l.cfg.DB.Reader(), by, id, from, to)
	if err != nil {
		return 0, fmt.Errorf("limits: read usage: %w", err)
	}
	return max(n, l.admits.peek(admitKey{by: by, id: id, start: from.UnixMilli()})), nil
}
