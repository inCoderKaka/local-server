package credential

import (
	"context"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
)

var t0 = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

func cfg() LockoutConfig {
	return LockoutConfig{Threshold: 3, Window: 10 * time.Minute, Duration: 15 * time.Minute, IdleTTL: time.Hour, MaxEntries: 100}
}

func TestLockoutTriggersAndExpires(t *testing.T) {
	l := NewLockout()
	keys := LoginKeys("203.0.113.9:5555", "Admin")
	for i := 0; i < 2; i++ {
		if l.Fail(keys, t0, cfg()) {
			t.Fatalf("failure %d triggered a lockout", i+1)
		}
		if w := l.Check(keys, t0); w != 0 {
			t.Fatalf("locked out after %d failures", i+1)
		}
	}
	if !l.Fail(keys, t0, cfg()) {
		t.Fatal("the third failure did not trigger a lockout")
	}
	if w := l.Check(keys, t0); w != 15*time.Minute {
		t.Fatalf("wait = %v, want 15m", w)
	}
	// Another name from the same address is locked by the address key; the same name from
	// another address is locked by the name key.
	if l.Check(LoginKeys("203.0.113.9:1", "other"), t0) == 0 {
		t.Error("the address is not locked out")
	}
	if l.Check(LoginKeys("198.51.100.1:1", "admin"), t0) == 0 {
		t.Error("the login name (case-insensitively) is not locked out")
	}
	if l.Check(LoginKeys("198.51.100.1:1", "other"), t0) != 0 {
		t.Error("an unrelated address and name is locked out")
	}
	if w := l.Check(keys, t0.Add(14*time.Minute)); w != time.Minute {
		t.Errorf("wait after 14m = %v, want 1m", w)
	}
	if w := l.Check(keys, t0.Add(15*time.Minute)); w != 0 {
		t.Errorf("still locked out after the duration: %v", w)
	}
	// A failure during the lockout does not extend it.
	l.Fail(keys, t0.Add(time.Minute), cfg())
	if w := l.Check(keys, t0.Add(15*time.Minute)); w != 0 {
		t.Errorf("a failure during the lockout extended it: %v", w)
	}
}

func TestLockoutCountsOnlyWithinTheWindow(t *testing.T) {
	l := NewLockout()
	keys := LoginKeys("203.0.113.9:1", "admin")
	l.Fail(keys, t0, cfg())
	l.Fail(keys, t0.Add(time.Minute), cfg())
	if l.Fail(keys, t0.Add(11*time.Minute), cfg()) {
		t.Fatal("failures outside the window were added up")
	}
}

// Succeed clears a counter but never lifts a lockout in force (req_001 D-99).
func TestLockoutSucceedDoesNotLiftALockout(t *testing.T) {
	l := NewLockout()
	keys := LoginKeys("203.0.113.9:1", "admin")
	l.Fail(keys, t0, cfg())
	l.Fail(keys, t0, cfg())
	l.Succeed(keys, t0)
	if l.Len() != 0 {
		t.Fatalf("Succeed left %d entries; want the counters cleared", l.Len())
	}
	for range 3 {
		l.Fail(keys, t0, cfg())
	}
	if l.Check(keys, t0) == 0 {
		t.Fatal("no lockout after three failures")
	}
	l.Succeed(keys, t0.Add(time.Second))
	if l.Check(keys, t0.Add(time.Second)) == 0 {
		t.Fatal("Succeed lifted an active lockout")
	}
}

func TestLockoutIsBounded(t *testing.T) {
	l := NewLockout()
	c := cfg()
	c.MaxEntries = 5
	for i := range 50 {
		l.Fail([]string{"a:" + time.Duration(i).String()}, t0.Add(time.Duration(i)*time.Second), c)
	}
	if l.Len() > 5 {
		t.Fatalf("tracker holds %d entries, cap 5", l.Len())
	}
	// Idle entries are swept.
	l.Fail([]string{"a:late"}, t0.Add(3*time.Hour), c)
	if l.Len() != 1 {
		t.Fatalf("after the idle TTL the tracker holds %d entries, want 1", l.Len())
	}
}

func TestLoginKeysBoundTheNameAndDropThePort(t *testing.T) {
	keys := LoginKeys("[::1]:8485", "  "+string(make([]byte, 5000))+"X")
	if keys[0] != "a:::1" {
		t.Errorf("address key = %q", keys[0])
	}
	if len(keys[1]) > maxLoginKeyLen+2 {
		t.Errorf("name key is %d bytes", len(keys[1]))
	}
	if got := LoginKeys("", " "); len(got) != 0 {
		t.Errorf("empty inputs gave keys %v", got)
	}
}

func TestLockoutSettings(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	if err := RegisterLockout(st); err != nil {
		t.Fatal(err)
	}
	c, err := LoadLockout(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if c.Threshold != 5 || c.Window != 10*time.Minute || c.Duration != 15*time.Minute || c.IdleTTL != time.Hour || c.MaxEntries != 10000 {
		t.Fatalf("defaults = %+v", c)
	}
	for _, k := range []string{KeyLockoutFailures, KeyLockoutWindow, KeyLockoutDuration, KeyTrackerIdleTTL, KeyTrackerMaxEntries} {
		v := "0"
		if k == KeyLockoutWindow || k == KeyLockoutDuration || k == KeyTrackerIdleTTL {
			v = "0s"
		}
		if err := st.Set(ctx, settings.Host, k, "", v); err == nil {
			t.Errorf("Set(%s, %s) was accepted", k, v)
		}
	}
}
