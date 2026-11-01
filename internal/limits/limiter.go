package limits

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// Config is what New needs.
type Config struct {
	DB       *storage.DB
	Identity *identity.Repos
	Grants   *grants.Repos
	Events   events.Notifier
	Settings *settings.Store
	// Clock is the time source; nil means time.Now.
	Clock func() time.Time
	// Logger receives problems (never secrets or content); nil means slog.Default().
	Logger *slog.Logger
}

// Limiter applies the limits of one host. It is safe for concurrent use.
type Limiter struct {
	cfg Config
	now func() time.Time
	log *slog.Logger

	rate   *buckets
	fails  *tracker
	admits *admitted

	mu    sync.Mutex
	noted map[noteKey]int64 // quota.near_limit already raised: window start (ms) per subject
}

type noteKey struct{ scope, subject string }

// New builds the limiter and registers its settings.
func New(cfg Config) (*Limiter, error) {
	if cfg.DB == nil || cfg.Identity == nil || cfg.Grants == nil || cfg.Events == nil || cfg.Settings == nil {
		return nil, errors.New("limits: DB, Identity, Grants, Events and Settings are required")
	}
	l := &Limiter{cfg: cfg, now: cfg.Clock, log: cfg.Logger, rate: newBuckets(), fails: newTracker(), admits: newAdmitted(), noted: map[noteKey]int64{}}
	if l.now == nil {
		l.now = time.Now
	}
	if l.log == nil {
		l.log = slog.Default()
	}
	if err := cfg.Settings.Register(Defs()...); err != nil {
		return nil, err
	}
	if err := setValidators(cfg.Settings); err != nil {
		return nil, err
	}
	return l, nil
}

// Protections says which of the protections public mode requires (req_001 D-24) that this
// module owns are on: rate limit, lockout and body limit. Time limits are not here; public mode
// (PLT-F-039) must check them separately.
type Protections struct {
	RateLimit bool
	Lockout   bool
	BodyLimit bool
}

// All reports whether all three are on.
func (p Protections) All() bool { return p.RateLimit && p.Lockout && p.BodyLimit }

// ProtectionsEnabled reads the three enable settings. Turning one on or off is a settings
// change (limits.rate_enabled, limits.lockout_enabled, limits.body_limit_enabled).
func (l *Limiter) ProtectionsEnabled(ctx context.Context) (Protections, error) {
	var p Protections
	var err error
	if p.RateLimit, err = l.cfg.Settings.Bool(ctx, KeyRateEnabled, ""); err != nil {
		return Protections{}, err
	}
	if p.Lockout, err = l.cfg.Settings.Bool(ctx, KeyLockoutEnabled, ""); err != nil {
		return Protections{}, err
	}
	if p.BodyLimit, err = l.cfg.Settings.Bool(ctx, KeyBodyLimitEnabled, ""); err != nil {
		return Protections{}, err
	}
	return p, nil
}

// Check is F1 step 3 for an authenticated identity, before the request is admitted. It
// returns, in this order of checking:
//
//   - *RateLimitError when the key's request allowance is empty (one request is used per call);
//   - auth.ErrRevoked, auth.ErrExpired or auth.ErrConsumerNotActive when the key or its
//     consumer stopped being usable since auth cached the identity: such a subject never gets
//     quota treatment;
//   - *QuotaError when the key's or the grant's token quota, or request quota
//     (quota_requests_per_day), for the current window is used up (req_001 D-98).
//
// Any other error is a failure to read state; the caller answers 5xx, not a limit.
func (l *Limiter) Check(ctx context.Context, id *auth.Identity) error {
	now := l.now()
	if err := l.checkRate(ctx, id.KeyID, now); err != nil {
		return err
	}
	rd := l.cfg.DB.Reader()
	key, err := l.cfg.Identity.AccessKeys.Get(ctx, rd, id.KeyID)
	if err != nil {
		return fmt.Errorf("limits: load key: %w", err)
	}
	switch {
	case key.RevokedAt != nil:
		return auth.ErrRevoked
	case key.ExpiresAt != nil && !now.Before(*key.ExpiresAt):
		return auth.ErrExpired
	}
	pr, err := l.cfg.Identity.Principals.Get(ctx, rd, id.PrincipalID)
	if err != nil {
		return fmt.Errorf("limits: load principal: %w", err)
	}
	if pr.Status != identity.StatusActive {
		return auth.ErrConsumerNotActive
	}
	qs, err := l.quotas(ctx, id, key, now)
	if err != nil {
		return err
	}
	for _, q := range qs {
		if q.used >= q.limit {
			return &QuotaError{Scope: q.scope, Unit: q.unit, Limit: q.limit, Used: q.used, Window: q.window, ResetsAt: q.end,
				RetryAfter: retryAfterAtLeast(q.end.Sub(now), time.Millisecond)}
		}
	}
	return nil
}

func (l *Limiter) checkRate(ctx context.Context, keyID string, now time.Time) error {
	st := l.cfg.Settings
	on, err := st.Bool(ctx, KeyRateEnabled, "")
	if err != nil || !on {
		return err
	}
	capacity, err := st.Int(ctx, KeyRateCapacity, "")
	if err != nil {
		return err
	}
	window, err := st.Duration(ctx, KeyRateWindow, "")
	if err != nil {
		return err
	}
	ok, wait := l.rate.take(keyID, max(capacity, 1), max(window, time.Millisecond), now)
	if !ok {
		return &RateLimitError{RetryAfter: wait}
	}
	return nil
}

// quota is one quota and its use in the current window.
type quota struct {
	scope, subject string
	unit           string // UnitTokens or UnitRequests
	limit, used    int64
	window         time.Duration
	start, end     time.Time
}

// quotas returns the quotas that apply to id, each with its use in the current window. The
// key's own narrowing and the grant version's figure are read separately, because
// auth.Identity.Limits holds only the smaller of the two. Request quotas count admitted calls
// in the same fixed window as token quotas (req_001 D-98, D-100).
func (l *Limiter) quotas(ctx context.Context, id *auth.Identity, key identity.AccessKey, now time.Time) ([]quota, error) {
	var keyLimit, keyReqLimit *int64
	if key.NarrowingJSON != nil {
		n, err := auth.ParseNarrowing(*key.NarrowingJSON)
		if err != nil {
			return nil, fmt.Errorf("limits: key %s: %w", key.ID, err) // fail closed, as auth does
		}
		keyLimit, keyReqLimit = n.QuotaTokensPerDay, n.QuotaRequestsPerDay
	}
	v, err := l.cfg.Grants.Versions.Get(ctx, l.cfg.DB.Reader(), id.GrantVersionID)
	if err != nil {
		return nil, fmt.Errorf("limits: load grant version: %w", err)
	}
	if keyLimit == nil && v.QuotaTokensPerDay == nil && keyReqLimit == nil && v.QuotaRequestsPerDay == nil {
		return nil, nil
	}
	window, start, end, err := l.quotaWindow(ctx, now)
	if err != nil {
		return nil, err
	}
	var out []quota
	add := func(scope, subject, unit string, by usage.By, value string, limit *int64) error {
		if limit == nil {
			return nil
		}
		used, err := l.used(ctx, unit, by, value, start, end)
		if err != nil {
			return err
		}
		out = append(out, quota{scope: scope, subject: subject, unit: unit, limit: *limit, used: used, window: window, start: start, end: end})
		return nil
	}
	// A consumer has one grant, so the grant's use is the consumer's use across all its keys.
	for _, a := range []struct {
		scope, subject, unit string
		by                   usage.By
		value                string
		limit                *int64
	}{
		{ScopeGrant, id.GrantID, UnitTokens, usage.ByConsumer, id.ConsumerID, v.QuotaTokensPerDay},
		{ScopeAccessKey, id.KeyID, UnitTokens, usage.ByAccessKey, id.KeyID, keyLimit},
		{ScopeGrant, id.GrantID, UnitRequests, usage.ByConsumer, id.ConsumerID, v.QuotaRequestsPerDay},
		{ScopeAccessKey, id.KeyID, UnitRequests, usage.ByAccessKey, id.KeyID, keyReqLimit},
	} {
		if err := add(a.scope, a.subject, a.unit, a.by, a.value, a.limit); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// quotaWindow returns the quota window length and the fixed window that contains now. The
// length is the setting limits.quota_window, at least one second.
func (l *Limiter) quotaWindow(ctx context.Context, now time.Time) (window time.Duration, start, end time.Time, err error) {
	window, err = l.cfg.Settings.Duration(ctx, KeyQuotaWindow, "")
	if err != nil {
		return 0, time.Time{}, time.Time{}, err
	}
	window = max(window, time.Second)
	start = now.UTC().Truncate(window)
	return window, start, start.Add(window), nil
}

// used returns the tokens (UnitTokens) or the number of calls (UnitRequests) that finished in
// [from, to), read through the usage module. Calls count every call the gateway admitted
// (req_001 D-100), metered or not, and at least the metered records.
func (l *Limiter) used(ctx context.Context, unit string, by usage.By, value string, from, to time.Time) (int64, error) {
	if unit == UnitRequests {
		return l.requestsUsed(ctx, by, value, from, to)
	}
	n, err := usage.Tokens(ctx, l.cfg.DB.Reader(), by, value, from, to)
	if err != nil {
		return 0, fmt.Errorf("limits: read usage: %w", err)
	}
	return n, nil
}
