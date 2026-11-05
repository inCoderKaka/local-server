package credential

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// Keys of the login lockout settings (req_001 D-54). All are owned by the host and read at the
// moment of use.
const (
	KeyLockoutFailures   = "auth.login_lockout.failures"
	KeyLockoutWindow     = "auth.login_lockout.window"
	KeyLockoutDuration   = "auth.login_lockout.duration"
	KeyTrackerIdleTTL    = "auth.login_lockout.tracker_idle_ttl"
	KeyTrackerMaxEntries = "auth.login_lockout.tracker_max_entries"
)

// maxLoginKeyLen bounds the login name kept in a tracker key, so a huge name sent by a client
// cannot fill memory.
const maxLoginKeyLen = 128

// LockoutDefs are the lockout settings; RegisterLockout registers them. The window and duration
// match the access key lockout (limits.lockout_*); the failure count is lower, because a
// password is guessed at far fewer tries than a key.
func LockoutDefs() []settings.Def {
	h := settings.Host
	return []settings.Def{
		{Key: KeyLockoutFailures, Kind: settings.Int, Default: "5", Owner: h,
			Description: "Failed password logins from one address, or against one login name, within the lockout window that lock that address or name out."},
		{Key: KeyLockoutWindow, Kind: settings.Duration, Default: "10m", Owner: h,
			Description: "Time over which failed password logins are counted."},
		{Key: KeyLockoutDuration, Kind: settings.Duration, Default: "15m", Owner: h,
			Description: "How long an address or login name stays locked out of password login."},
		{Key: KeyTrackerIdleTTL, Kind: settings.Duration, Default: "1h", Owner: h,
			Description: "How long an idle address or login name is remembered by the failed-login counters before it is dropped from memory."},
		{Key: KeyTrackerMaxEntries, Kind: settings.Int, Default: "10000", Owner: h,
			Description: "Most addresses and login names the failed-login counters remember at once; the least recently seen is dropped to make room."},
	}
}

// RegisterLockout registers the lockout settings and refuses a value that would break them
// (a zero threshold, window or duration).
func RegisterLockout(st *settings.Store) error {
	if err := st.Register(LockoutDefs()...); err != nil {
		return err
	}
	for _, key := range []string{KeyLockoutFailures, KeyTrackerMaxEntries} {
		if err := st.SetValidator(key, func(_ context.Context, v string) error { return positiveInt(v) }); err != nil {
			return err
		}
	}
	for _, key := range []string{KeyLockoutWindow, KeyLockoutDuration, KeyTrackerIdleTTL} {
		if err := st.SetValidator(key, func(_ context.Context, v string) error { return positiveDuration(v) }); err != nil {
			return err
		}
	}
	return nil
}

// LockoutConfig is the lockout settings, read once per decision.
type LockoutConfig struct {
	Threshold  int64
	Window     time.Duration
	Duration   time.Duration
	IdleTTL    time.Duration
	MaxEntries int64
}

// LoadLockout reads the lockout settings from st. Values below 1 are raised to 1 (a millisecond
// for durations), the same guard the access key lockout has.
func LoadLockout(ctx context.Context, st *settings.Store) (LockoutConfig, error) {
	var c LockoutConfig
	var err error
	if c.Threshold, err = st.Int(ctx, KeyLockoutFailures, ""); err != nil {
		return c, err
	}
	if c.Window, err = st.Duration(ctx, KeyLockoutWindow, ""); err != nil {
		return c, err
	}
	if c.Duration, err = st.Duration(ctx, KeyLockoutDuration, ""); err != nil {
		return c, err
	}
	if c.IdleTTL, err = st.Duration(ctx, KeyTrackerIdleTTL, ""); err != nil {
		return c, err
	}
	if c.MaxEntries, err = st.Int(ctx, KeyTrackerMaxEntries, ""); err != nil {
		return c, err
	}
	c.Threshold = max(c.Threshold, 1)
	c.MaxEntries = max(c.MaxEntries, 1)
	c.Window = max(c.Window, time.Millisecond)
	c.Duration = max(c.Duration, time.Millisecond)
	c.IdleTTL = max(c.IdleTTL, time.Millisecond)
	return c, nil
}

// LoginKeys returns the tracker keys of one login attempt: the remote host ("a:"+host, the port
// dropped) and the lower-cased login name ("u:"+name, cut to a bounded length). An empty remote
// address or name has no key.
func LoginKeys(remoteAddr, login string) []string {
	var out []string
	if remoteAddr != "" {
		host, _, err := net.SplitHostPort(remoteAddr)
		if err != nil {
			host = remoteAddr
		}
		out = append(out, "a:"+host)
	}
	if name := strings.ToLower(strings.TrimSpace(login)); name != "" {
		if len(name) > maxLoginKeyLen {
			name = name[:maxLoginKeyLen]
		}
		out = append(out, "u:"+name)
	}
	return out
}

// Lockout counts failed password logins by key and locks a key out after too many. It lives in
// memory only: a restart forgets it, like the access key lockout. It is safe for concurrent use.
type Lockout struct {
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

// NewLockout returns an empty tracker.
func NewLockout() *Lockout { return &Lockout{m: map[string]*failures{}} }

// Check returns how long the longest active lockout among keys still has to run, or 0 when none
// is locked out.
func (t *Lockout) Check(keys []string, now time.Time) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	var wait time.Duration
	for _, k := range keys {
		if f, ok := t.m[k]; ok && f.lockedUntil.After(now) {
			wait = max(wait, f.lockedUntil.Sub(now))
		}
	}
	return wait
}

// Fail counts one failed login against every key. It reports whether this failure started a
// lockout of at least one key, so a caller can record the trigger (req_001 D-89, PLT-F-055). A
// key that is already locked out is not counted again.
func (t *Lockout) Fail(keys []string, now time.Time, c LockoutConfig) (triggered bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep(now, c)
	for _, k := range keys {
		f, ok := t.m[k]
		if !ok {
			if int64(len(t.m)) >= c.MaxEntries && !t.evictOldest(now) {
				continue // full of locked entries: this one goes untracked
			}
			f = &failures{windowStart: now}
			t.m[k] = f
		}
		f.lastSeen = now
		if f.lockedUntil.After(now) {
			continue
		}
		if now.Sub(f.windowStart) >= c.Window {
			f.count, f.windowStart = 0, now
		}
		f.count++
		if f.count >= c.Threshold {
			f.lockedUntil = now.Add(c.Duration)
			f.count = 0
			triggered = true
		}
	}
	return triggered
}

// Succeed resets the failed-login counters of keys. A key under an active lockout is kept, with
// its count reset, so a success that raced past Check does not lift the lockout (req_001 D-99).
// A caller passes only the keys a success proves belong to the legitimate user (the login name),
// never the address: otherwise a holder of one valid login could reset its address counter while
// guessing other names.
func (t *Lockout) Succeed(keys []string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, k := range keys {
		if f, ok := t.m[k]; ok && f.lockedUntil.After(now) {
			f.count, f.windowStart = 0, now
			continue
		}
		delete(t.m, k)
	}
}

// Len is the number of keys remembered; tests use it.
func (t *Lockout) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.m)
}

// sweep drops idle entries, at most once per idle TTL.
func (t *Lockout) sweep(now time.Time, c LockoutConfig) {
	if now.Sub(t.lastSweep) < c.IdleTTL {
		return
	}
	for k, f := range t.m {
		if now.Sub(f.lastSeen) >= c.IdleTTL && !f.lockedUntil.After(now) {
			delete(t.m, k)
		}
	}
	t.lastSweep = now
}

// evictOldest makes room by dropping the least recently seen entry that is not locked out.
func (t *Lockout) evictOldest(now time.Time) bool {
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
