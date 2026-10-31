package limits

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/auth"
)

// tracker counts failed key checks by remote host ("a:"+host) and by key prefix ("k:"+prefix).
// A prefix is public (it is shown next to a key); the secret is never seen here.
type tracker struct {
	mu        sync.Mutex
	m         map[string]*failures
	lastSweep time.Time
}

type failures struct {
	count       int64
	windowStart time.Time
	lockedUntil time.Time
	lastSeen    time.Time
}

func newTracker() *tracker { return &tracker{m: map[string]*failures{}} }

type lockoutCfg struct {
	threshold  int64
	window     time.Duration
	duration   time.Duration
	idleTTL    time.Duration
	maxEntries int64
}

// subjects returns the tracker keys for a request. A header that does not parse has no prefix.
func subjects(remoteAddr, header string) []string {
	var out []string
	if remoteAddr != "" {
		host, _, err := net.SplitHostPort(remoteAddr)
		if err != nil {
			host = remoteAddr
		}
		out = append(out, "a:"+host)
	}
	if p, err := auth.ParseHeader(header); err == nil {
		out = append(out, "k:"+p.Prefix)
	}
	return out
}

func (t *tracker) locked(keys []string, now time.Time) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	var wait time.Duration
	for _, k := range keys {
		if f, ok := t.m[k]; ok && f.lockedUntil.After(now) {
			if d := f.lockedUntil.Sub(now); d > wait {
				wait = d
			}
		}
	}
	return wait
}

func (t *tracker) fail(keys []string, now time.Time, c lockoutCfg) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep(now, c, false)
	for _, k := range keys {
		f, ok := t.m[k]
		if !ok {
			if int64(len(t.m)) >= c.maxEntries && !t.evictOldest(now) {
				continue // full of locked entries: this one goes untracked
			}
			f = &failures{windowStart: now}
			t.m[k] = f
		}
		f.lastSeen = now
		if f.lockedUntil.After(now) {
			continue
		}
		if now.Sub(f.windowStart) >= c.window {
			f.count, f.windowStart = 0, now
		}
		f.count++
		if f.count >= c.threshold {
			f.lockedUntil = now.Add(c.duration)
			f.count = 0
		}
	}
}

func (t *tracker) clear(keys []string) {
	t.mu.Lock()
	for _, k := range keys {
		delete(t.m, k)
	}
	t.mu.Unlock()
}

// sweep drops idle entries, at most once per idle TTL unless forced.
func (t *tracker) sweep(now time.Time, c lockoutCfg, force bool) {
	if !force && now.Sub(t.lastSweep) < c.idleTTL {
		return
	}
	for k, f := range t.m {
		if now.Sub(f.lastSeen) >= c.idleTTL && !f.lockedUntil.After(now) {
			delete(t.m, k)
		}
	}
	t.lastSweep = now
}

// evictOldest makes room by dropping the least recently seen entry that is not locked out.
func (t *tracker) evictOldest(now time.Time) bool {
	var oldest string
	var at time.Time
	for k, f := range t.m {
		if f.lockedUntil.After(now) {
			continue
		}
		if oldest == "" || f.lastSeen.Before(at) {
			oldest, at = k, f.lastSeen
		}
	}
	if oldest == "" {
		return false
	}
	delete(t.m, oldest)
	return true
}

func (l *Limiter) lockoutCfg(ctx context.Context) (on bool, c lockoutCfg, err error) {
	st := l.cfg.Settings
	if on, err = st.Bool(ctx, KeyLockoutEnabled, ""); err != nil || !on {
		return false, c, err
	}
	if c.threshold, err = st.Int(ctx, KeyLockoutFailures, ""); err != nil {
		return false, c, err
	}
	if c.window, err = st.Duration(ctx, KeyLockoutWindow, ""); err != nil {
		return false, c, err
	}
	if c.duration, err = st.Duration(ctx, KeyLockoutDuration, ""); err != nil {
		return false, c, err
	}
	if c.idleTTL, err = st.Duration(ctx, KeyTrackerIdleTTL, ""); err != nil {
		return false, c, err
	}
	if c.maxEntries, err = st.Int(ctx, KeyTrackerMaxEntries, ""); err != nil {
		return false, c, err
	}
	c.threshold = max(c.threshold, 1)
	c.maxEntries = max(c.maxEntries, 1)
	c.window = max(c.window, time.Millisecond)
	c.duration = max(c.duration, time.Millisecond)
	c.idleTTL = max(c.idleTTL, time.Millisecond)
	return true, c, nil
}

// CheckLockout refuses a caller that is locked out. Call it before auth.Service.Authenticate
// with the Authorization header value and the connection's remote address. The error is a
// *RateLimitError with LockedOut set. A locked-out address is refused even with a good key.
func (l *Limiter) CheckLockout(ctx context.Context, remoteAddr, header string) error {
	on, _, err := l.lockoutCfg(ctx)
	if err != nil || !on {
		return err
	}
	if wait := l.fails.locked(subjects(remoteAddr, header), l.now()); wait > 0 {
		return &RateLimitError{LockedOut: true, RetryAfter: retryAfterAtLeast(wait, time.Millisecond)}
	}
	return nil
}

// AuthResult feeds an authentication outcome into the lockout counters: pass the error from
// auth.Service.Authenticate, nil on success. A bad, unknown, revoked or expired key, or one
// used from the wrong node, counts as a failure against the address and the key prefix; success
// clears both counters. Any other error (a database failure, a missing grant) is not a failed
// guess and changes nothing.
func (l *Limiter) AuthResult(ctx context.Context, remoteAddr, header string, authErr error) {
	on, c, err := l.lockoutCfg(ctx)
	if err != nil {
		l.log.Warn("limits: lockout settings unreadable, failure not counted", "err", err)
		return
	}
	if !on {
		return
	}
	keys := subjects(remoteAddr, header)
	switch {
	case authErr == nil:
		l.fails.clear(keys)
	case isCredentialFailure(authErr):
		l.fails.fail(keys, l.now(), c)
	}
}

func isCredentialFailure(err error) bool {
	for _, target := range []error{auth.ErrInvalidKey, auth.ErrRevoked, auth.ErrExpired, auth.ErrNodeMismatch} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
