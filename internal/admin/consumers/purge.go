package consumers

import (
	"context"
	"errors"
	"time"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// defaultPurgeInterval is the wait between purge runs when retention.interval cannot be read or is
// not positive, as settings.Store.Run does.
const defaultPurgeInterval = time.Hour

// PurgeDeleted purges every consumer that was deleted longer ago than consumers.deleted_retention
// and returns how many it purged (req_001 D-106, D-134, D-136). The purge removes the consumer's
// consent records with their evidence, credentials, sessions and access keys, and clears the
// reference to it on the invites it redeemed (redeemed_consumer_id); the consumer and principal rows
// stay as a tombstone for the audit trail and the grants. Each consumer is purged in its own unit of work with its audit row
// (consumer.purged, no actor, the counts only). The retention is read now; zero or less keeps
// everything. A consumer whose purge fails does not stop the run: the next one is purged and the
// errors come back joined. A consumer already purged is not
// selected again, so a second run purges nothing and audits nothing.
func (s *Service) PurgeDeleted(ctx context.Context) (int, error) {
	keep, err := s.cfg.Settings.Duration(ctx, KeyDeletedRetention, "")
	if err != nil {
		return 0, err
	}
	if keep <= 0 {
		return 0, nil
	}
	batch, err := s.cfg.Settings.Int(ctx, settings.KeyRetentionBatchRows, "")
	if err != nil {
		return 0, err
	}
	batch = max(batch, 1)
	cutoff := s.now().Add(-keep)
	purged := 0
	var errs []error
	after := ""
	for {
		page, err := s.cfg.Identity.Consumers.ListPurgeable(ctx, s.cfg.DB.Reader(), cutoff, after, int(batch))
		if err != nil {
			errs = append(errs, wrap("list deleted consumers to purge", err))
			break
		}
		for _, c := range page {
			after = c.ID
			if err := s.purgeOne(ctx, c); err != nil {
				errs = append(errs, wrap("purge consumer "+c.ID, err))
				continue
			}
			purged++
		}
		if int64(len(page)) < batch {
			break
		}
	}
	if purged > 0 {
		s.cfg.Auth.Invalidate() // the purged keys may still sit in the key cache
	}
	return purged, errors.Join(errs...)
}

// purgeOne purges one consumer and audits it in the same unit of work.
func (s *Service) purgeOne(ctx context.Context, c identity.Consumer) error {
	return s.write(ctx, func(tx *storage.Tx) error {
		n, err := s.cfg.Identity.Consumers.Purge(tx, c.ID)
		if err != nil {
			return err
		}
		// Ids and counts only: the audit trail is kept forever and holds nothing personal.
		return s.record(tx, audit.Event{Action: ActionConsumerPurged, TargetID: c.PrincipalID, Detail: map[string]any{
			"consumer_id": c.ID, "consents": n.Consents, "credentials": n.Credentials, "sessions": n.Sessions,
			"access_keys": n.AccessKeys, "invite_refs_cleared": n.InviteRefsCleared}})
	})
}

// RunPurge calls PurgeDeleted now and then every retention.interval, read before each wait (one
// hour when it cannot be read or is not positive), until ctx ends. A failure is logged with ids
// only and the loop goes on.
func (s *Service) RunPurge(ctx context.Context) {
	for {
		if _, err := s.PurgeDeleted(ctx); err != nil && ctx.Err() == nil {
			s.log.ErrorContext(ctx, "admin consumers: purge of deleted consumers failed", "err", err)
		}
		wait, err := s.cfg.Settings.Duration(ctx, settings.KeyRetentionInterval, "")
		if err != nil || wait <= 0 {
			wait = defaultPurgeInterval
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
