package limits_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// keyDay is a key narrowing with one entry over 24 hours.
func keyDay(metric string, limit int64) auth.Narrowing {
	return auth.Narrowing{Quotas: []grants.QuotaEntry{day(metric, limit)}}
}

type env struct {
	t    *testing.T
	path string
	gr   *grants.Repos
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

// day is a quota entry over the default window of 24 hours.
func day(metric string, limit int64) grants.QuotaEntry {
	return grants.QuotaEntry{Metric: metric, Limit: limit, Window: 24 * time.Hour}
}

// dayQuotas turns the pair of optional day limits into the grant's entries.
func dayQuotas(tokens, requests *int64) []grants.QuotaEntry {
	var out []grants.QuotaEntry
	if tokens != nil {
		out = append(out, day(grants.MetricTokens, *tokens))
	}
	if requests != nil {
		out = append(out, day(grants.MetricRequests, *requests))
	}
	return out
}

// setup makes a consumer whose grant has a token quota per day (none when nil).
func setup(t *testing.T, grantQuota *int64) *env { return setupGrant(t, grantQuota, nil) }

func setupGrant(t *testing.T, grantQuota, grantRequests *int64) *env {
	t.Helper()
	return setupVersion(t, grants.NewVersion{Quotas: dayQuotas(grantQuota, grantRequests)})
}

// setupLegacy makes a consumer whose only grant version is one made before the quota entries: the
// deprecated alias columns hold the figures and the version has no entry.
func setupLegacy(t *testing.T, tokens, requests *int64) *env {
	t.Helper()
	return setupWith(t, func(tx *storage.Tx, gr *grants.Repos, grantID, modelID string) error {
		_, err := tx.Exec(`INSERT INTO grt_version (id, grant_id, version_no, quota_tokens_per_day, quota_requests_per_day, created_at) VALUES ('legacy-version', ?, 1, ?, ?, 1)`,
			grantID, tokens, requests)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO grt_version_model (version_id, model_id) VALUES ('legacy-version', ?)`, modelID); err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE grt_grant SET current_version_id = 'legacy-version' WHERE id = ?`, grantID)
		return err
	})
}

func setupVersion(t *testing.T, v grants.NewVersion) *env {
	t.Helper()
	return setupWith(t, func(tx *storage.Tx, gr *grants.Repos, grantID, modelID string) error {
		v.Models = []grants.VersionModel{{ModelID: modelID}}
		_, err := gr.Versions.Create(tx, grantID, v)
		return err
	})
}

func setupWith(t *testing.T, makeVersion func(tx *storage.Tx, gr *grants.Repos, grantID, modelID string) error) *env {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := storage.Open(context.Background(), storage.DefaultConfig(path,
		settings.Module(), identity.Module(), grants.Module(), audit.Module(), events.Module(), usage.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{t: t, path: path, gr: grants.NewRepos(nil), db: db, bus: events.New(), st: settings.New(db), ir: identity.NewRepos(nil),
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
		return makeVersion(tx, gr, g.ID, m.ID)
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

// useAt inserts a usage record admitted (started) at one time and finished at another.
func (e *env) useAt(k key, tokens int64, started, finished time.Time) {
	e.t.Helper()
	id := "u" + string(rune('a'+e.seq.Add(1)))
	err := e.db.Write(ctx(), func(tx *storage.Tx) error {
		_, err := tx.Exec(`INSERT INTO use_record (id, consumer_id, access_key_id, grant_version_id, operation, model, outcome,
prompt_tokens, completion_tokens, total_tokens, latency_ms, started_at, finished_at, receipt_kid, receipt_signature, created_at)
VALUES (?, ?, ?, ?, 'chat.completions', 'm1', 'completed', 0, ?, ?, 1, ?, ?, 'kid', x'00', ?)`,
			id, k.id.ConsumerID, k.id.KeyID, k.id.GrantVersionID, tokens, tokens, started.UnixMilli(), finished.UnixMilli(), finished.UnixMilli())
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

// admit is what the gateway does for one call that is let in: Check, then CountAdmitted.
func (e *env) admit(k key) error {
	e.t.Helper()
	if err := e.lim.Check(ctx(), k.id); err != nil {
		return err
	}
	if err := e.lim.CountAdmitted(ctx(), k.id); err != nil {
		e.t.Fatal(err)
	}
	return nil
}

func (e *env) events() []events.Event {
	e.evMu.Lock()
	defer e.evMu.Unlock()
	return append([]events.Event(nil), e.evs...)
}

func ctx() context.Context { return context.Background() }

func TestOverQuotaKeyGetsQuotaExceededWithRetryAfter(t *testing.T) {
	e := setup(t, i64(1000))
	k := e.issue(keyDay(grants.MetricTokens, 100))
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
	a, b := e.issue(keyDay(grants.MetricTokens, 10)), e.issue(auth.Narrowing{})
	e.use(a, 10)
	if err := e.lim.Check(ctx(), b.id); err != nil {
		t.Fatalf("other key must be unaffected: %v", err)
	}
	if err := e.lim.Check(ctx(), a.id); !errors.Is(err, limits.ErrQuotaExceeded) {
		t.Fatalf("narrowed key: %v", err)
	}
}

// A request entry counts the calls admitted in its window (req_001 D-107).
func TestRequestQuotaNPlusOne(t *testing.T) {
	e := setup(t, nil)
	k := e.issue(keyDay(grants.MetricRequests, 3))
	other := e.issue(keyDay(grants.MetricRequests, 3))
	for i := 1; i <= 3; i++ {
		if err := e.admit(k); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	err := e.lim.Check(ctx(), k.id)
	var qe *limits.QuotaError
	if !errors.As(err, &qe) || !errors.Is(err, limits.ErrQuotaExceeded) {
		t.Fatalf("request 4: %v", err)
	}
	if qe.Scope != limits.ScopeAccessKey || qe.Unit != limits.UnitRequests || qe.Code() != "quota_exceeded" ||
		qe.Limit != 3 || qe.Used != 3 || qe.RetryAfter != 14*time.Hour {
		t.Fatalf("quota error = %+v", qe)
	}
	if err := e.lim.Check(ctx(), other.id); err != nil {
		t.Fatalf("other key must be unaffected: %v", err)
	}
	e.advance(14 * time.Hour)
	if err := e.lim.Check(ctx(), k.id); err != nil {
		t.Fatalf("new window: %v", err)
	}
}

func TestGrantRequestQuotaCountsAllKeysOfConsumer(t *testing.T) {
	e := setupGrant(t, nil, i64(2))
	a, b := e.issue(auth.Narrowing{}), e.issue(auth.Narrowing{})
	if err := e.admit(a); err != nil {
		t.Fatalf("request 1: %v", err)
	}
	if err := e.admit(b); err != nil {
		t.Fatalf("request 2: %v", err)
	}
	var qe *limits.QuotaError
	if err := e.lim.Check(ctx(), a.id); !errors.As(err, &qe) || qe.Scope != limits.ScopeGrant || qe.Unit != limits.UnitRequests {
		t.Fatalf("request 3: %v", err)
	}
}

// req_001 D-107, held from D-100: the request quota counts every admitted call, metered or not.
func TestRequestQuotaCountsUnmeteredAdmittedCalls(t *testing.T) {
	e := setup(t, nil)
	k := e.issue(keyDay(grants.MetricRequests, 3))
	other := e.issue(auth.Narrowing{})
	for i := 1; i <= 3; i++ {
		if err := e.lim.Check(ctx(), k.id); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if err := e.lim.CountAdmitted(ctx(), k.id); err != nil { // admitted, never metered: no use_record
			t.Fatal(err)
		}
	}
	var qe *limits.QuotaError
	err := e.lim.Check(ctx(), k.id)
	if !errors.As(err, &qe) || qe.Unit != limits.UnitRequests || qe.Used != 3 || qe.Limit != 3 {
		t.Fatalf("request 4: %v, want the request quota used up by unmetered calls", err)
	}
	// A key with its own request quota is unaffected by another key's key-scoped count (the
	// grant has no request quota here).
	if err := e.lim.Check(ctx(), other.id); err != nil {
		t.Fatalf("other key: %v", err)
	}
	e.advance(14 * time.Hour)
	if err := e.lim.Check(ctx(), k.id); err != nil {
		t.Fatalf("new window: %v", err)
	}
}

// Metered records are not a source of the request count: only CountAdmitted raises it, so a record
// written for a call that was admitted is not counted a second time.
func TestRequestCountIsTheCounterNotTheRecords(t *testing.T) {
	e := setup(t, nil)
	k := e.issue(keyDay(grants.MetricRequests, 2))
	e.use(k, 1)
	e.use(k, 1)
	e.use(k, 1) // three records, no admission counted
	if err := e.lim.Check(ctx(), k.id); err != nil {
		t.Fatalf("records alone must not use the request quota: %v", err)
	}
	if err := e.admit(k); err != nil {
		t.Fatal(err)
	}
	e.use(k, 1) // the admitted call is then metered: still one call
	st, err := e.lim.StatusOf(ctx(), k.id)
	if err != nil || len(st.Quotas) != 1 || *st.Quotas[0].UsedRequests != 1 {
		t.Fatalf("status = %+v, %v; want 1 request used", st, err)
	}
}

func TestGrantRequestQuotaCountsAdmittedCallsOfAllKeys(t *testing.T) {
	e := setupGrant(t, nil, i64(2))
	a, b := e.issue(auth.Narrowing{}), e.issue(auth.Narrowing{})
	for _, k := range []key{a, b} {
		if err := e.lim.CountAdmitted(ctx(), k.id); err != nil {
			t.Fatal(err)
		}
	}
	var qe *limits.QuotaError
	if err := e.lim.Check(ctx(), a.id); !errors.As(err, &qe) || qe.Scope != limits.ScopeGrant || qe.Unit != limits.UnitRequests {
		t.Fatalf("request 3: %v", err)
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
	k := e.issue(keyDay(grants.MetricTokens, 100))
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
	if p["scope"] != "access_key" || p["key_id"] != k.id.KeyID || p["tokens"] != float64(100) || p["used_tokens"] != float64(80) || p["threshold_percent"] != float64(80) {
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
	k := e.issue(keyDay(grants.MetricTokens, 100))
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
	// A success does not clear the address counter (D-99): the third failure from addr locks it,
	// though a valid key succeeded in between.
	e.lim.AuthResult(ctx(), addr, good.hdr, nil)
	bad = "Bearer " + auth.FormatKey("abcdef999999", strings.Repeat("0", 64))
	if err := e.lim.CheckLockout(ctx(), addr, bad); err != nil {
		t.Fatalf("two failures are under the threshold: %v", err)
	}
	fail(bad, addr) // third failure from the address
	err := e.lim.CheckLockout(ctx(), addr, bad)
	var re *limits.RateLimitError
	if !errors.As(err, &re) || !re.LockedOut || re.Code() != "rate_limit_exceeded" || re.RetryAfter != 15*time.Minute {
		t.Fatalf("lockout err = %v (%+v)", err, re)
	}
	// A good key from the locked address is refused, and so is the locked prefix elsewhere.
	if err := e.lim.CheckLockout(ctx(), addr, good.hdr); !errors.Is(err, limits.ErrRateLimited) {
		t.Fatalf("good key from locked address: %v", err)
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

// A successful authentication clears only its own key prefix's failed-guess counter, never the
// source address's (req_001 D-99).
func TestSuccessClearsOnlyKeyPrefixCounter(t *testing.T) {
	e := setup(t, nil)
	e.set(limits.KeyLockoutFailures, "3")
	good := e.issue(auth.Narrowing{})
	p, err := auth.ParseHeader(good.hdr)
	if err != nil {
		t.Fatal(err)
	}
	guess := func(prefix string) string {
		return "Bearer " + auth.FormatKey(prefix, strings.Repeat("0", 64))
	}
	fail := func(hdr, from string) {
		t.Helper()
		e.lim.AuthResult(ctx(), from, hdr, auth.ErrInvalidKey)
	}

	// Prefix counter: two wrong guesses at good's prefix from address A, then the holder
	// authenticates from B. One more wrong guess from C must not lock the prefix.
	fail(guess(p.Prefix), "203.0.113.1:1")
	fail(guess(p.Prefix), "203.0.113.1:1")
	e.lim.AuthResult(ctx(), "203.0.113.2:1", good.hdr, nil)
	fail(guess(p.Prefix), "203.0.113.3:1")
	if err := e.lim.CheckLockout(ctx(), "203.0.113.4:1", guess(p.Prefix)); err != nil {
		t.Fatalf("success must clear the key prefix counter: %v", err)
	}
	fail(guess(p.Prefix), "203.0.113.3:1")
	fail(guess(p.Prefix), "203.0.113.3:1") // 3 since the success
	if err := e.lim.CheckLockout(ctx(), "203.0.113.4:1", guess(p.Prefix)); !errors.Is(err, limits.ErrRateLimited) {
		t.Fatalf("prefix must lock after 3 failures: %v", err)
	}
	e.lim.AuthResult(ctx(), "203.0.113.2:1", good.hdr, nil)
	if err := e.lim.CheckLockout(ctx(), "203.0.113.4:1", guess(p.Prefix)); !errors.Is(err, limits.ErrRateLimited) {
		t.Fatalf("success must not lift an active lockout: %v", err)
	}

	// Address counter: two failures at other prefixes from address D, then a success from D.
	// The next failure from D is the third and locks it.
	fail(guess("aaaaaaaaaaaa"), "198.51.100.7:1")
	fail(guess("bbbbbbbbbbbb"), "198.51.100.7:1")
	e.lim.AuthResult(ctx(), "198.51.100.7:1", good.hdr, nil)
	fail(guess("cccccccccccc"), "198.51.100.7:1")
	if err := e.lim.CheckLockout(ctx(), "198.51.100.7:1", ""); !errors.Is(err, limits.ErrRateLimited) {
		t.Fatalf("success must not clear the address counter: %v", err)
	}
}

func TestNodeLookupFailureIsNotAFailedGuess(t *testing.T) {
	e := setup(t, nil)
	e.set(limits.KeyLockoutFailures, "3")
	lookup := fmt.Errorf("%w: %w", auth.ErrNodeMismatch, auth.ErrNodeLookup)
	for i := 0; i < 5; i++ {
		e.lim.AuthResult(ctx(), "198.51.100.9:1", "", lookup)
	}
	if err := e.lim.CheckLockout(ctx(), "198.51.100.9:1", ""); err != nil {
		t.Fatalf("a failed node lookup must not count: %v", err)
	}
	for i := 0; i < 3; i++ {
		e.lim.AuthResult(ctx(), "198.51.100.10:1", "", auth.ErrNodeMismatch)
	}
	if err := e.lim.CheckLockout(ctx(), "198.51.100.10:1", ""); !errors.Is(err, limits.ErrRateLimited) {
		t.Fatalf("a plain node mismatch must lock after 3: %v", err)
	}
}

func TestTrackerEvictsLeastRecentlySeen(t *testing.T) {
	e := setup(t, nil)
	e.set(limits.KeyLockoutFailures, "2")
	e.set(limits.KeyTrackerMaxEntries, "5")
	e.lim.AuthResult(ctx(), "10.0.0.1:1", "", auth.ErrInvalidKey)
	e.advance(time.Second)
	for i := 2; i <= 6; i++ {
		e.lim.AuthResult(ctx(), fmt.Sprintf("10.0.0.%d:1", i), "", auth.ErrInvalidKey)
		e.advance(time.Second)
	}
	e.lim.AuthResult(ctx(), "10.0.0.1:1", "", auth.ErrInvalidKey)
	if err := e.lim.CheckLockout(ctx(), "10.0.0.1:1", ""); err != nil {
		t.Fatalf("first failure was evicted, so one failure must not lock: %v", err)
	}

	e.set(limits.KeyTrackerMaxEntries, "100")
	e.lim.AuthResult(ctx(), "10.0.1.1:1", "", auth.ErrInvalidKey)
	e.lim.AuthResult(ctx(), "10.0.1.1:1", "", auth.ErrInvalidKey)
	if err := e.lim.CheckLockout(ctx(), "10.0.1.1:1", ""); !errors.Is(err, limits.ErrRateLimited) {
		t.Fatalf("control: two failures must lock: %v", err)
	}
}

func TestProtectionsAndMemoryBound(t *testing.T) {
	e := setup(t, nil)
	e.set(limits.KeyLockoutFailures, "1")
	e.set(limits.KeyTrackerMaxEntries, "5")
	bad := "Bearer " + auth.FormatKey("abcdef123456", strings.Repeat("0", 64))
	for i := 0; i < 50; i++ { // many addresses with a tiny cap: nothing panics (eviction is asserted in TestTrackerEvictsLeastRecentlySeen)
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
	k := e.issue(keyDay(grants.MetricTokens, 500_000))
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

// The token quota query must use the started_at indexes of usage migration 4, not scan the table.
func TestQuotaQueriesUseIndexes(t *testing.T) {
	e := setup(t, nil)
	for _, c := range []struct{ col, index string }{
		{"access_key_id", "use_record_key_started"},
		{"consumer_id", "use_record_consumer_started"},
	} {
		rows, err := e.db.Reader().Query(ctx(), `EXPLAIN QUERY PLAN SELECT COALESCE(SUM(total_tokens), 0) FROM use_record WHERE `+
			c.col+` = ? AND started_at >= ? AND started_at < ?`, "x", 0, 1)
		if err != nil {
			t.Fatal(err)
		}
		var plan strings.Builder
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan.WriteString(detail + "\n")
		}
		rows.Close()
		if !strings.Contains(plan.String(), "USING INDEX "+c.index) && !strings.Contains(plan.String(), "USING COVERING INDEX "+c.index) {
			t.Fatalf("%s query plan does not use %s:\n%s", c.col, c.index, plan.String())
		}
	}
}

func TestSettingsRejectUnusableValues(t *testing.T) {
	e := setup(t, nil)
	for _, c := range []struct{ key, val string }{
		{limits.KeyLockoutFailures, "0"},
		{limits.KeyRateWindow, "0s"},
		{limits.KeyMaxBodyBytes, "-1"},
		{limits.KeyCounterRetention, "0s"},
		{limits.KeyCounterPruneInterval, "0s"},
		{limits.KeyQuotaWindowMin, "500ms"},
		{limits.KeyQuotaWindowMax, "0s"},
	} {
		if err := e.st.Set(ctx(), settings.Host, c.key, "", c.val); !errors.Is(err, settings.ErrRejected) {
			t.Fatalf("Set(%s, %s) = %v, want ErrRejected", c.key, c.val, err)
		}
	}
}
