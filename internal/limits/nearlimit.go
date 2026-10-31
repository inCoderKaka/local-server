package limits

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// Record is called after a call's usage record has been committed. It raises quota.near_limit
// when a quota's use has reached limits.near_limit_percent of the limit, once per quota subject
// (the key, or the grant) per window. A window is the quota window of Check. The mark is kept in
// memory, so a restart can raise one more event for a window that already raised one. A failure
// to emit is logged and the event is tried again by the next Record; the returned error is only
// for failing to read state, and the caller logs it: the call itself has already succeeded.
func (l *Limiter) Record(ctx context.Context, id *auth.Identity) error {
	pct, err := l.cfg.Settings.Int(ctx, KeyNearLimitPercent, "")
	if err != nil || pct <= 0 {
		return err
	}
	now := l.now()
	key, err := l.cfg.Identity.AccessKeys.Get(ctx, l.cfg.DB.Reader(), id.KeyID)
	if err != nil {
		return fmt.Errorf("limits: load key: %w", err)
	}
	qs, err := l.quotas(ctx, id, key, now)
	if err != nil {
		return err
	}
	for _, q := range qs {
		if q.limit <= 0 || float64(q.used)*100 < float64(q.limit)*float64(pct) {
			continue
		}
		nk := noteKey{q.scope, q.subject}
		start := q.start.UnixMilli()
		l.mu.Lock()
		done := l.noted[nk] == start
		if !done {
			l.noted[nk] = start
		}
		l.mu.Unlock()
		if done {
			continue
		}
		if err := l.emitNearLimit(ctx, id, q, pct); err != nil {
			l.mu.Lock()
			delete(l.noted, nk) // not raised: let the next Record try again
			l.mu.Unlock()
			l.log.Error("limits: quota.near_limit not emitted", "scope", q.scope, "subject", q.subject, "err", err)
		}
	}
	return nil
}

// nearLimitPayload mirrors the QuotaStatus schema (api/openapi.yaml) plus the ids of who it is
// about. Numbers and ids only (D-31).
type nearLimitPayload struct {
	Scope           string `json:"scope"`
	WindowSeconds   int64  `json:"window_seconds"`
	Tokens          int64  `json:"tokens"`
	UsedTokens      int64  `json:"used_tokens"`
	RemainingTokens int64  `json:"remaining_tokens"`
	ResetsAt        string `json:"resets_at"`
	Percent         int64  `json:"percent"`
	KeyID           string `json:"key_id,omitempty"`
	GrantID         string `json:"grant_id"`
	ConsumerID      string `json:"consumer_id"`
}

func (l *Limiter) emitNearLimit(ctx context.Context, id *auth.Identity, q quota, pct int64) error {
	p := nearLimitPayload{Scope: q.scope, WindowSeconds: int64(q.window / time.Second), Tokens: q.limit, UsedTokens: q.used,
		RemainingTokens: max(q.limit-q.used, 0), ResetsAt: q.end.UTC().Format(time.RFC3339), Percent: pct,
		GrantID: id.GrantID, ConsumerID: id.ConsumerID}
	if q.scope == ScopeAccessKey {
		p.KeyID = id.KeyID
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return l.cfg.Events.Write(ctx, l.cfg.DB, func(tx *storage.Tx) error {
		_, err := l.cfg.Events.Emit(tx, events.Event{Type: events.QuotaNearLimit, TargetPrincipalID: id.PrincipalID, Payload: b})
		return err
	})
}
