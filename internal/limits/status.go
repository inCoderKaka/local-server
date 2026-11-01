package limits

import (
	"context"
	"fmt"
	"time"

	"github.com/inCoderKaka/local-server/internal/auth"
)

// QuotaStatus is one scope's quotas and what is left of them in the current window. A nil
// Tokens or Requests means no quota of that unit applies in the scope.
type QuotaStatus struct {
	Scope    string // ScopeGrant or ScopeAccessKey
	Window   time.Duration
	ResetsAt time.Time

	Tokens, UsedTokens             *int64
	Requests, UsedRequests         *int64
	RemainingTokens, RemainingReqs *int64
}

// RateStatus is the per-key request allowance (the settings limits.rate_capacity and
// limits.rate_window).
type RateStatus struct {
	Capacity int64
	Window   time.Duration
}

// Status is what a caller may still use: the quotas with their use, the rate limit when one
// applies, and the earliest expiry of the key and the grant version.
type Status struct {
	Quotas    []QuotaStatus
	Rate      *RateStatus
	ExpiresAt *time.Time
}

// StatusOf reports id's limits and remaining quota without counting a request or consuming rate
// allowance (req_001 D-05, D-25, D-98). Quotas are the key's own narrowing and the grant
// version's figure, read separately, as Check does.
func (l *Limiter) StatusOf(ctx context.Context, id *auth.Identity) (Status, error) {
	var out Status
	rd := l.cfg.DB.Reader()
	key, err := l.cfg.Identity.AccessKeys.Get(ctx, rd, id.KeyID)
	if err != nil {
		return out, fmt.Errorf("limits: load key: %w", err)
	}
	v, err := l.cfg.Grants.Versions.Get(ctx, rd, id.GrantVersionID)
	if err != nil {
		return out, fmt.Errorf("limits: load grant version: %w", err)
	}
	out.ExpiresAt = earliest(key.ExpiresAt, v.ExpiresAt)
	qs, err := l.quotas(ctx, id, key, l.now())
	if err != nil {
		return out, err
	}
	for _, scope := range []string{ScopeGrant, ScopeAccessKey} {
		var s *QuotaStatus
		for _, q := range qs {
			if q.scope != scope {
				continue
			}
			if s == nil {
				s = &QuotaStatus{Scope: scope, Window: q.window, ResetsAt: q.end}
			}
			limit, used, left := q.limit, q.used, max(q.limit-q.used, 0)
			if q.unit == UnitTokens {
				s.Tokens, s.UsedTokens, s.RemainingTokens = &limit, &used, &left
			} else {
				s.Requests, s.UsedRequests, s.RemainingReqs = &limit, &used, &left
			}
		}
		if s != nil {
			out.Quotas = append(out.Quotas, *s)
		}
	}
	st := l.cfg.Settings
	on, err := st.Bool(ctx, KeyRateEnabled, "")
	if err != nil {
		return out, err
	}
	if on {
		capacity, err := st.Int(ctx, KeyRateCapacity, "")
		if err != nil {
			return out, err
		}
		window, err := st.Duration(ctx, KeyRateWindow, "")
		if err != nil {
			return out, err
		}
		out.Rate = &RateStatus{Capacity: max(capacity, 1), Window: max(window, time.Second)}
	}
	return out, nil
}

func earliest(a, b *time.Time) *time.Time {
	switch {
	case a == nil:
		return b
	case b == nil || a.Before(*b):
		return a
	default:
		return b
	}
}
