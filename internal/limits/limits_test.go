package limits_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/usage"
)

func i64(v int64) *int64 { return &v }

type env struct {
	t    *testing.T
	db   *storage.DB
	bus  *events.Bus
	st   *settings.Store
	auth *auth.Service
	lim  *limits.Limiter
	ir   *identity.Repos
	cons identity.Consumer

	mu   sync.Mutex
	now  time.Time
	logs bytes.Buffer

	evMu sync.Mutex
	evs  []events.Event
	seq  atomic.Int64
}

func (e *env) clock() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.now
}

func (e *env) advance(d time.Duration) {
	e.mu.Lock()
	e.now = e.now.Add(d)
	e.mu.Unlock()
}

type lockedWriter struct{ e *env }

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.e.mu.Lock()
	defer w.e.mu.Unlock()
	return w.e.logs.Write(p)
}

func setup(t *testing.T, grantQuota *int64) *env {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"),
		settings.Module(), identity.Module(), grants.Module(), audit.Module(), events.Module(), usage.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{t: t, db: db, bus: events.New(), st: settings.New(db), ir: identity.NewRepos(nil),
		now: time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)}
	gr := grants.NewRepos(nil)
	e.auth, err = auth.New(auth.Config{DB: db, Identity: e.ir, Grants: gr, Events: e.bus, Settings: e.st, Clock: e.clock})
	if err != nil {
		t.Fatal(err)
	}
	e.lim, err = limits.New(limits.Config{DB: db, Identity: e.ir, Grants: gr, Events: e.bus, Settings: e.st, Clock: e.clock,
		Logger: slog.New(slog.NewTextHandler(&lockedWriter{e: e}, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.bus.Subscribe("test", func(_ context.Context, ev events.Event) error {
		e.evMu.Lock()
		e.evs = append(e.evs, ev)
		e.evMu.Unlock()
		return nil
	}, events.QuotaNearLimit); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx(), func(tx *storage.Tx) error {
		p, err := e.ir.Principals.Create(tx, "consumer", nil, nil)
		if err != nil {
			return err
		}
		if e.cons, err = e.ir.Consumers.Create(tx, p.ID, identity.ConsumerApplication, nil, nil); err != nil {
			return err
		}
		g, err := gr.Grants.Create(tx, e.cons.ID)
		if err != nil {
			return err
		}
		m, err := gr.Models.Create(tx, "m1")
		if err != nil {
			return err
		}
		_, err = gr.Versions.Create(tx, g.ID, grants.NewVersion{QuotaTokensPerDay: grantQuota,
			Models: []grants.VersionModel{{ModelID: m.ID}}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) set(key, value string) {
	e.t.Helper()
	if err := e.st.Set(ctx(), settings.Host, key, "", value); err != nil {
		e.t.Fatal(err)
	}
}

type key struct {
	id   *auth.Identity
	text string
	hdr  string
}

func (e *env) issue(nar auth.Narrowing) key {
	e.t.Helper()
	var is auth.Issued
	err := e.bus.Write(ctx(), e.db, func(tx *storage.Tx) (err error) {
		is, err = e.auth.Issue(tx, auth.IssueParams{ConsumerID: e.cons.ID, Narrowing: nar})
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	k := key{text: is.Key.Reveal(), hdr: "Bearer " + is.Key.Reveal()}
	k.id, err = e.auth.Authenticate(ctx(), k.hdr, "100.64.0.1:1")
	if err != nil {
		e.t.Fatal(err)
	}
	return k
}

// use inserts a usage record that finishes at the current clock time.
func (e *env) use(k key, tokens int64) {
	e.t.Helper()
	at := e.clock().UnixMilli()
	id := "u" + string(rune('a'+e.seq.Add(1)))
	err := e.db.Write(ctx(), func(tx *storage.Tx) error {
		_, err := tx.Exec(`INSERT INTO use_record (id, consumer_id, access_key_id, grant_version_id, operation, model, outcome,
prompt_tokens, completion_tokens, total_tokens, latency_ms, started_at, finished_at, receipt_kid, receipt_signature, created_at)
VALUES (?, ?, ?, ?, 'chat.completions', 'm1', 'completed', 0, ?, ?, 1, ?, ?, 'kid', x'00', ?)`,
			id, k.id.ConsumerID, k.id.KeyID, k.id.GrantVersionID, tokens, tokens, at, at, at)
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) events() []events.Event {
	e.evMu.Lock()
	defer e.evMu.Unlock()
	return append([]events.Event(nil), e.evs...)
}

func ctx() context.Context { return context.Background() }

func TestOverQuotaKeyGetsQuotaExceededWithRetryAfter(t *testing.T) {
	e := setup(t, i64(1000))
	k := e.issue(auth.Narrowing{QuotaTokensPerDay: i64(100)})
	e.use(k, 99)
	if err := e.lim.Check(ctx(), k.id); err != nil {
		t.Fatalf("under quota: %v", err)
	}
	e.use(k, 1)
	err := e.lim.Check(ctx(), k.id)
	var qe *limits.QuotaError
	if !errors.As(err, &qe) || !errors.Is(err, limits.ErrQuotaExceeded) {
		t.Fatalf("err = %v, want QuotaError", err)
	}
	if qe.Scope != limits.ScopeAccessKey || qe.Code() != "quota_exceeded" || qe.Limit != 100 || qe.Used != 100 {
		t.Fatalf("quota error = %+v", qe)
	}
	// 10:00 UTC with a 24h fixed window: it resets at midnight, 14h away.
	if qe.RetryAfter != 14*time.Hour {
		t.Fatalf("RetryAfter = %s, want 14h", qe.RetryAfter)
	}
	e.advance(14 * time.Hour) // a new window: earlier usage no longer counts
	if err := e.lim.Check(ctx(), k.id); err != nil {
		t.Fatalf("new window: %v", err)
	}
}

func TestGrantQuotaCountsAllKeysOfConsumer(t *testing.T) {
	e := setup(t, i64(100))
	k1, k2 := e.issue(auth.Narrowing{}), e.issue(auth.Narrowing{})
	e.use(k1, 60)
	e.use(k2, 40)
	var qe *limits.QuotaError
	if err := e.lim.Check(ctx(), k1.id); !errors.As(err, &qe) || qe.Scope != limits.ScopeGrant {
		t.Fatalf("err = %v, want grant QuotaError", err)
	}
}

func TestKeyNarrowingLimitsOnlyThatKey(t *testing.T) {
	e := setup(t, i64(1000))
	a, b := e.issue(auth.Narrowing{QuotaTokensPerDay: i64(10)}), e.issue(auth.Narrowing{})
	e.use(a, 10)
	if err := e.lim.Check(ctx(), b.id); err != nil {
		t.Fatalf("other key must be unaffected: %v", err)
	}
	if err := e.lim.Check(ctx(), a.id); !errors.Is(err, limits.ErrQuotaExceeded) {
		t.Fatalf("narrowed key: %v", err)
	}
}

func TestNoQuotaMeansNoQuotaError(t *testing.T) {
	e := setup(t, nil)
	k := e.issue(auth.Narrowing{})
	e.use(k, 1_000_000)
	if err := e.lim.Check(ctx(), k.id); err != nil {
		t.Fatal(err)
	}
}

func TestRateLimitNPlusOne(t *testing.T) {
	e := setup(t, nil)
	e.set(limits.KeyRateCapacity, "3")
	e.set(limits.KeyRateWindow, "3s") // one request per second
	k := e.issue(auth.Narrowing{})
	other := e.issue(auth.Narrowing{})
	for i := 0; i < 3; i++ {
		if err := e.lim.Check(ctx(), k.id); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
	err := e.lim.Check(ctx(), k.id)
	var re *limits.RateLimitError
	if !errors.As(err, &re) || !errors.Is(err, limits.ErrRateLimited) {
		t.Fatalf("request 4: %v", err)
	}
	if re.Code() != "rate_limit_exceeded" || re.LockedOut || re.RetryAfter <= 0 || re.RetryAfter > time.Second+time.Millisecond {
		t.Fatalf("rate error = %+v", re)
	}
	if err := e.lim.Check(ctx(), other.id); err != nil {
		t.Fatalf("other key shares no bucket: %v", err)
	}
	e.advance(re.RetryAfter)
	if err := e.lim.Check(ctx(), k.id); err != nil {
		t.Fatalf("after Retry-After: %v", err)
	}
	e.set(limits.KeyRateEnabled, "false")
	for i := 0; i < 10; i++ {
		if err := e.lim.Check(ctx(), k.id); err != nil {
			t.Fatalf("rate limit off: %v", err)
		}
	}
}

func TestNearLimitEmitsExactlyOncePerWindow(t *testing.T) {
	e := setup(t, nil)
	e.set(limits.KeyNearLimitPercent, "80")
	k := e.issue(auth.Narrowing{QuotaTokensPerDay: i64(100)})
	rec := func() {
		t.Helper()
		if err := e.lim.Record(ctx(), k.id); err != nil {
			t.Fatal(err)
		}
	}
	e.use(k, 79)
	rec()
	if n := len(e.events()); n != 0 {
		t.Fatalf("below threshold: %d events", n)
	}
	e.use(k, 1) // 80%
	rec()
	e.use(k, 15)
	rec()
	rec()
	e.use(k, 10) // over quota
	rec()
	evs := e.events()
	if len(evs) != 1 {
		t.Fatalf("events = %d, want 1", len(evs))
	}
	ev := evs[0]
	if ev.Type != events.QuotaNearLimit || ev.TargetPrincipalID != k.id.PrincipalID {
		t.Fatalf("event = %+v", ev)
	}
	var p map[string]any
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p["scope"] != "access_key" || p["key_id"] != k.id.KeyID || p["tokens"] != float64(100) || p["used_tokens"] != float64(80) || p["percent"] != float64(80) {
		t.Fatalf("payload = %s", ev.Payload)
	}
	e.advance(24 * time.Hour) // next window
	e.use(k, 90)
	rec()
	rec()
	if n := len(e.events()); n != 2 {
		t.Fatalf("after new window: %d events, want 2", n)
	}
	e.set(limits.KeyNearLimitPercent, "0")
	e.advance(24 * time.Hour)
	e.use(k, 100)
	rec()
	if n := len(e.events()); n != 2 {
		t.Fatalf("percent 0 must turn the event off, got %d", n)
	}
}

func TestNearLimitConcurrentRecordEmitsOnce(t *testing.T) {
	e := setup(t, nil)
	k := e.issue(auth.Narrowing{QuotaTokensPerDay: i64(100)})
	e.use(k, 90)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = e.lim.Record(ctx(), k.id) }()
	}
	wg.Wait()
	if n := len(e.events()); n != 1 {
		t.Fatalf("events = %d, want 1", n)
	}
}

func TestLockout(t *testing.T) {
	e := setup(t, nil)
	e.set(limits.KeyLockoutFailures, "3")
	e.set(limits.KeyLockoutWindow, "10m")
	e.set(limits.KeyLockoutDuration, "15m")
	good := e.issue(auth.Narrowing{})
	bad := "Bearer " + auth.FormatKey("abcdef123456", strings.Repeat("0", 64))
	addr := "203.0.113.9:5555"
	fail := func(hdr, from string) {
		t.Helper()
		_, err := e.auth.Authenticate(ctx(), hdr, from)
		if !errors.Is(err, auth.ErrInvalidKey) {
			t.Fatalf("auth err = %v", err)
		}
		e.lim.AuthResult(ctx(), from, hdr, err)
	}
	for i := 0; i < 2; i++ {
		if err := e.lim.CheckLockout(ctx(), addr, bad); err != nil {
			t.Fatalf("before limit: %v", err)
		}
		fail(bad, addr)
	}
	// Success clears the counters (address and prefix), so two more failures are fine.
	e.lim.AuthResult(ctx(), addr, good.hdr, nil)
	// (a different prefix, so only the address counter is in play)
	bad = "Bearer " + auth.FormatKey("abcdef999999", strings.Repeat("0", 64))
	fail(bad, addr)
	fail(bad, addr)
	if err := e.lim.CheckLockout(ctx(), addr, bad); err != nil {
		t.Fatalf("success must have cleared the counter: %v", err)
	}
	fail(bad, addr) // third in a row
	err := e.lim.CheckLockout(ctx(), addr, bad)
	var re *limits.RateLimitError
	if !errors.As(err, &re) || !re.LockedOut || re.Code() != "rate_limit_exceeded" || re.RetryAfter != 15*time.Minute {
		t.Fatalf("lockout err = %v (%+v)", err, re)
	}
	// A good key from the locked address is refused, and so is the locked prefix elsewhere.
	if err := e.lim.CheckLockout(ctx(), addr, good.hdr); !errors.Is(err, limits.ErrRateLimited) {
		t.Fatalf("good key from locked address: %v", err)
	}
	if err := e.lim.CheckLockout(ctx(), "198.51.100.1:1", bad); !errors.Is(err, limits.ErrRateLimited) {
		t.Fatalf("locked prefix from other address: %v", err)
	}
	if err := e.lim.CheckLockout(ctx(), "198.51.100.1:1", good.hdr); err != nil {
		t.Fatalf("unrelated caller: %v", err)
	}
	if err := e.lim.CheckLockout(ctx(), "203.0.113.9:1", ""); !errors.Is(err, limits.ErrRateLimited) {
		t.Fatalf("same host, other port: %v", err)
	}
	e.advance(14*time.Minute + 59*time.Second)
	if err := e.lim.CheckLockout(ctx(), addr, bad); err == nil {
		t.Fatal("lockout ended early")
	}
	e.advance(2 * time.Second)
	if err := e.lim.CheckLockout(ctx(), addr, bad); err != nil {
		t.Fatalf("lockout must expire: %v", err)
	}
	// Failures that are not guesses do not count.
	for i := 0; i < 10; i++ {
		e.lim.AuthResult(ctx(), "192.0.2.5:1", bad, auth.ErrNoGrant)
		e.lim.AuthResult(ctx(), "192.0.2.5:1", bad, errors.New("db down"))
	}
	if err := e.lim.CheckLockout(ctx(), "192.0.2.5:1", bad); err != nil {
		t.Fatalf("non-credential errors counted: %v", err)
	}
	// Failures older than the window are forgotten.
	other := "Bearer " + auth.FormatKey("fedcba654321", strings.Repeat("1", 64))
	fail(other, "192.0.2.6:1")
	fail(other, "192.0.2.6:1")
	e.advance(11 * time.Minute)
	fail(other, "192.0.2.6:1")
	if err := e.lim.CheckLockout(ctx(), "192.0.2.6:1", other); err != nil {
		t.Fatalf("stale failures counted: %v", err)
	}
}

func TestProtectionsAndMemoryBound(t *testing.T) {
	e := setup(t, nil)
	e.set(limits.KeyLockoutFailures, "1")
	e.set(limits.KeyTrackerMaxEntries, "5")
	bad := "Bearer " + auth.FormatKey("abcdef123456", strings.Repeat("0", 64))
	for i := 0; i < 50; i++ { // many addresses: the cap holds and nothing panics
		e.lim.AuthResult(ctx(), "10.0.0."+strings.Repeat("1", i%9+1)+":1", "", auth.ErrInvalidKey)
		e.advance(time.Second)
	}
	e.set(limits.KeyLockoutEnabled, "false")
	e.lim.AuthResult(ctx(), "9.9.9.9:1", bad, auth.ErrInvalidKey)
	if err := e.lim.CheckLockout(ctx(), "9.9.9.9:1", bad); err != nil {
		t.Fatalf("lockout off: %v", err)
	}
	p, err := e.lim.ProtectionsEnabled(ctx())
	if err != nil || p.All() || !p.RateLimit || p.Lockout || !p.BodyLimit {
		t.Fatalf("protections = %+v, %v", p, err)
	}
	e.set(limits.KeyLockoutEnabled, "true")
	if p, _ := e.lim.ProtectionsEnabled(ctx()); !p.All() {
		t.Fatalf("protections = %+v", p)
	}
}

func TestBodyLimit(t *testing.T) {
	e := setup(t, nil)
	e.set(limits.KeyMaxBodyBytes, "10")
	req := func(body string, declared bool) *http.Request {
		r := httptest.NewRequest("POST", "/v1/chat/completions", io.NopCloser(strings.NewReader(body)))
		if declared {
			r.ContentLength = int64(len(body))
		} else {
			r.ContentLength = -1
		}
		return r
	}
	r := req("0123456789", true)
	if err := e.lim.LimitBody(ctx(), r); err != nil {
		t.Fatal(err)
	}
	if b, err := io.ReadAll(r.Body); err != nil || len(b) != 10 {
		t.Fatalf("exactly at limit: %q %v", b, err)
	}
	var te *limits.TooLargeError
	if err := e.lim.LimitBody(ctx(), req("0123456789a", true)); !errors.As(err, &te) || !errors.Is(err, limits.ErrTooLarge) || te.Limit != 10 {
		t.Fatalf("declared oversize: %v", err)
	}
	r = req("0123456789abcdefghij", false) // unknown length: caught while reading
	if err := e.lim.LimitBody(ctx(), r); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r.Body)
	if !errors.Is(err, limits.ErrTooLarge) || len(b) != 10 {
		t.Fatalf("streamed oversize: %q %v", b, err)
	}
	e.set(limits.KeyBodyLimitEnabled, "false")
	if err := e.lim.LimitBody(ctx(), req("0123456789abcdefghij", true)); err != nil {
		t.Fatalf("limit off: %v", err)
	}
}

func TestInactiveSubjectsAreNotQuotaSubjects(t *testing.T) {
	e := setup(t, i64(100))
	revoked := e.issue(auth.Narrowing{})
	expiring := e.issue(auth.Narrowing{})
	// The identities were authenticated (and cached) before the change: Check must notice.
	if err := e.bus.Write(ctx(), e.db, func(tx *storage.Tx) error { return e.auth.Revoke(tx, revoked.id.KeyID, "") }); err != nil {
		t.Fatal(err)
	}
	if err := e.lim.Check(ctx(), revoked.id); !errors.Is(err, auth.ErrRevoked) {
		t.Fatalf("revoked key: %v", err)
	}
	exp := e.clock().Add(time.Hour)
	if err := e.db.Write(ctx(), func(tx *storage.Tx) error {
		return e.ir.AccessKeys.Update(tx, expiring.id.KeyID, nil, nil, nil, &exp)
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.lim.Check(ctx(), expiring.id); err != nil {
		t.Fatal(err)
	}
	e.advance(2 * time.Hour)
	if err := e.lim.Check(ctx(), expiring.id); !errors.Is(err, auth.ErrExpired) {
		t.Fatalf("expired key: %v", err)
	}
	live := e.issue(auth.Narrowing{})
	if err := e.db.Write(ctx(), func(tx *storage.Tx) error {
		_, err := tx.Exec(`UPDATE idn_principal SET status = 'suspended' WHERE id = ?`, live.id.PrincipalID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.lim.Check(ctx(), live.id); !errors.Is(err, auth.ErrConsumerNotActive) {
		t.Fatalf("suspended consumer: %v", err)
	}
}

func TestConcurrentUse(t *testing.T) {
	e := setup(t, i64(1_000_000))
	e.set(limits.KeyRateCapacity, "1000")
	k := e.issue(auth.Narrowing{QuotaTokensPerDay: i64(500_000)})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			addr := "10.1.0." + strings.Repeat("7", g+1) + ":1"
			for i := 0; i < 25; i++ {
				_ = e.lim.Check(ctx(), k.id)
				_ = e.lim.Record(ctx(), k.id)
				_ = e.lim.CheckLockout(ctx(), addr, k.hdr)
				e.lim.AuthResult(ctx(), addr, k.hdr, auth.ErrInvalidKey)
				e.lim.AuthResult(ctx(), addr, k.hdr, nil)
				_ = e.lim.LimitBody(ctx(), httptest.NewRequest("POST", "/", strings.NewReader("x")))
				e.advance(time.Millisecond)
			}
		}(g)
	}
	wg.Wait()
}

func TestSecretsNeverInErrorsOrLogs(t *testing.T) {
	e := setup(t, i64(10))
	e.set(limits.KeyLockoutFailures, "1")
	e.set(limits.KeyRateCapacity, "1")
	k := e.issue(auth.Narrowing{})
	parsed, err := auth.ParseKey(k.text)
	if err != nil {
		t.Fatal(err)
	}
	secret := parsed.Secret.Reveal()
	e.use(k, 10)
	errs := []error{e.lim.Check(ctx(), k.id), e.lim.Check(ctx(), k.id)}
	e.lim.AuthResult(ctx(), "203.0.113.1:1", k.hdr, auth.ErrInvalidKey)
	errs = append(errs, e.lim.CheckLockout(ctx(), "203.0.113.1:1", k.hdr))
	_ = e.lim.Record(ctx(), k.id)
	for _, err := range errs {
		if err == nil {
			t.Fatal("expected a limit error")
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaks the key: %v", err)
		}
	}
	e.mu.Lock()
	logs := e.logs.String()
	e.mu.Unlock()
	if strings.Contains(logs, secret) {
		t.Fatal("log leaks the secret")
	}
	for _, ev := range e.events() {
		if strings.Contains(string(ev.Payload), secret) {
			t.Fatal("event leaks the secret")
		}
	}
}

func TestDefsAllHostOwnedWithDefaults(t *testing.T) {
	for _, d := range limits.Defs() {
		if d.Owner != settings.Host || d.Default == "" || d.Description == "" || !strings.HasPrefix(d.Key, "limits.") {
			t.Errorf("def %+v", d)
		}
	}
}
