package requests

import (
	"context"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// ExpireDue moves every open request whose end time has passed (it was opened longer ago than
// requests.ttl) to expired, and returns how many it moved. A request that is not open is never
// touched, so approved, rejected and withdrawn requests keep their state. All of them, and their
// sync changes, are one unit of work. The consumer's text is never read or logged.
func (s *Service) ExpireDue(ctx context.Context) (int, error) {
	now := s.now()
	var n int
	err := s.write(ctx, func(tx *storage.Tx) error {
		n = 0
		rows, err := tx.Query(`UPDATE req_resource_request SET status = 'expired', decided_at = ?, row_version = row_version + 1, updated_at = ?
WHERE status = 'open' AND expires_at <= ? RETURNING id, consumer_id, created_at, row_version`, ms(now), ms(now), ms(now))
		if err != nil {
			return err
		}
		type expired struct {
			q Request
			v int64
		}
		due, err := collect(rows, func(r storage.Row) (expired, error) {
			var e expired
			var created int64
			err := r.Scan(&e.q.ID, &e.q.ConsumerID, &created, &e.v)
			e.q.CreatedAt, e.q.Status, e.q.DecidedAt = tm(created), StatusExpired, ptr(now.UTC())
			return e, err
		})
		if err != nil {
			return err
		}
		for _, e := range due {
			if err := captureTx(tx, e.q, e.v, "update"); err != nil {
				return err
			}
		}
		n = len(due)
		return nil
	})
	if err != nil {
		return 0, wrap("expire", err)
	}
	return n, nil
}

// RunSweeper calls ExpireDue every requests.sweep_interval until ctx is done. The interval is
// read before every wait, so a change applies to the next one. A failure is logged, with no
// request text, and the loop goes on.
func (s *Service) RunSweeper(ctx context.Context) {
	for {
		wait, err := s.cfg.Settings.Duration(ctx, KeySweepInterval, "")
		if err != nil || wait <= 0 {
			wait = defaultSweepInterval
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		n, err := s.ExpireDue(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			s.log.ErrorContext(ctx, "requests: expiring old requests failed", "err", err)
		case n > 0:
			s.log.InfoContext(ctx, "requests: open resource requests expired", "count", n)
		}
	}
}
