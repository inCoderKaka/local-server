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

	rate  *buckets
	fails *tracker

	mu         sync.Mutex
	noted      map[noteKey]int64 // quota.near_limit already raised: window start (ms) per subject and window length
	lastPruned time.Time         // when the request counters were last pruned by CountAdmitted
}

type noteKey struct {
	scope, subject string
	window         time.Duration
}

// New builds the limiter and registers its settings.
func New(cfg Config) (*Limiter, error) {
	if cfg.DB == nil || cfg.Identity == nil || cfg.Grants == nil || cfg.Events == nil || cfg.Settings == nil {
		return nil, errors.New("limits: DB, Identity, Grants, Events and Settings are required")
	}
	l := &Limiter{cfg: cfg, now: cfg.Clock, log: cfg.Logger, rate: newBuckets(), fails: newTracker(), noted: map[noteKey]int64{}}
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
//   - *QuotaError when any quota entry of the key or of the grant, token or request, is used up
//     in the window that contains now (req_001 D-107).
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

// quota is one quota entry that applies to a call and its use in the current window.
type quota struct {
	scope, subject string
	unit           string // UnitTokens or UnitRequests
	limit, used    int64
	window         time.Duration
	start, end     time.Time
}

// entry is a quota entry with the scope it applies in and whose use it counts.
type entry struct {
	scope, subject string // ScopeGrant with the grant id, or ScopeAccessKey with the key id
	by             usage.By
	byID           string // the consumer id (grant scope) or the access key id (key scope)
	grants.QuotaEntry
}

// entries returns the quota entries that apply to id: the grant version's, whose use is the
// consumer's across all its keys (a consumer has one grant), and the key narrowing's, whose use is
// that key's. Order: the grant's entries, then the key's. A deprecated single figure (the
// quota_tokens_per_day and quota_requests_per_day aliases) becomes one entry with the window
// limits.quota_window when its scope has no entry of that metric; the list wins (req_001 D-107).
func (l *Limiter) entries(ctx context.Context, id *auth.Identity, key identity.AccessKey) ([]entry, error) {
	var nar auth.Narrowing
	if key.NarrowingJSON != nil {
		var err error
		if nar, err = auth.ParseNarrowing(*key.NarrowingJSON); err != nil {
			return nil, fmt.Errorf("limits: key %s: %w", key.ID, err) // fail closed, as auth does
		}
	}
	v, err := l.cfg.Grants.Versions.Get(ctx, l.cfg.DB.Reader(), id.GrantVersionID)
	if err != nil {
		return nil, fmt.Errorf("limits: load grant version: %w", err)
	}
	legacy, err := LegacyWindow(ctx, l.cfg.Settings)
	if err != nil {
		return nil, err
	}
	var out []entry
	for _, s := range []struct {
		scope, subject string
		by             usage.By
		byID           string
		list           []grants.QuotaEntry
	}{
		{ScopeGrant, id.GrantID, usage.ByConsumer, id.ConsumerID, v.AllQuotas(legacy)},
		{ScopeAccessKey, id.KeyID, usage.ByAccessKey, id.KeyID, nar.AllQuotas(legacy)},
	} {
		for _, q := range s.list {
			out = append(out, entry{scope: s.scope, subject: s.subject, by: s.by, byID: s.byID, QuotaEntry: q})
		}
	}
	return out, nil
}

// windowOf returns the fixed window of length w that contains now. Windows are aligned to whole
// multiples of w counted from 0001-01-01 00:00 UTC, which is what time.Time.Truncate does. A
// window is [start, end): an instant exactly at end belongs to the next window.
func windowOf(now time.Time, w time.Duration) (start, end time.Time) {
	start = now.UTC().Truncate(w)
	return start, start.Add(w)
}

// quotas returns the quota entries that apply to id, each with its use in the window that
// contains now (req_001 D-107). Token use is the sum of use_record rows admitted in the window
// (started_at); request use is the persisted request counter of the window, raised by
// CountAdmitted when a call is admitted. A call is therefore charged to the window it was
// admitted in, even if it finishes in a later one.
func (l *Limiter) quotas(ctx context.Context, id *auth.Identity, key identity.AccessKey, now time.Time) ([]quota, error) {
	es, err := l.entries(ctx, id, key)
	if err != nil {
		return nil, err
	}
	out := make([]quota, 0, len(es))
	for _, e := range es {
		start, end := windowOf(now, e.Window)
		used, err := l.used(ctx, e, start, end)
		if err != nil {
			return nil, err
		}
		out = append(out, quota{scope: e.scope, subject: e.subject, unit: e.Metric, limit: e.Limit, used: used, window: e.Window, start: start, end: end})
	}
	return out, nil
}

// used returns the tokens (UnitTokens) admitted in [start, end) or the calls the request counter
// holds for that window (UnitRequests).
func (l *Limiter) used(ctx context.Context, e entry, start, end time.Time) (int64, error) {
	var n int64
	var err error
	if e.Metric == UnitRequests {
		n, err = usage.RequestCount(ctx, l.cfg.DB.Reader(), e.by, e.byID, e.Window, start)
	} else {
		n, err = usage.TokensStartedIn(ctx, l.cfg.DB.Reader(), e.by, e.byID, start, end)
	}
	if err != nil {
		return 0, fmt.Errorf("limits: read usage: %w", err)
	}
	return n, nil
}
