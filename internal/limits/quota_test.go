package limits_test

import (
	"errors"
	"log/slog"
	"sync"
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

func entry(metric string, limit int64, window time.Duration) grants.QuotaEntry {
	return grants.QuotaEntry{Metric: metric, Limit: limit, Window: window}
}

func narrowing(es ...grants.QuotaEntry) auth.Narrowing { return auth.Narrowing{Quotas: es} }

func quotaErr(t *testing.T, err error) *limits.QuotaError {
	t.Helper()
	var qe *limits.QuotaError
	if !errors.As(err, &qe) || !errors.Is(err, limits.ErrQuotaExceeded) {
		t.Fatalf("err = %v, want *QuotaError", err)
	}
	return qe
}

// A windowed entry refuses with quota_exceeded and a Retry-After to the end of its own window.
func TestWindowedEntryRefusesWithRetryAfter(t *testing.T) {
	e := setup(t, nil)
	e.now = time.Date(2026, 11, 6, 10, 20, 0, 0, time.UTC)
	k := e.issue(narrowing(entry(grants.MetricTokens, 100, time.Hour)))
	e.use(k, 100)
	qe := quotaErr(t, e.lim.Check(ctx(), k.id))
	if qe.Scope != limits.ScopeAccessKey || qe.Unit != limits.UnitTokens || qe.Window != time.Hour || qe.Limit != 100 || qe.Used != 100 ||
		qe.Code() != "quota_exceeded" {
		t.Fatalf("quota error = %+v", qe)
	}
	if want := time.Date(2026, 11, 6, 11, 0, 0, 0, time.UTC); !qe.ResetsAt.Equal(want) || qe.RetryAfter != 40*time.Minute {
		t.Fatalf("ResetsAt = %s, RetryAfter = %s; want 11:00 and 40m", qe.ResetsAt, qe.RetryAfter)
	}
	e.advance(40 * time.Minute) // the next hour
	if err := e.lim.Check(ctx(), k.id); err != nil {
		t.Fatalf("new window: %v", err)
	}
}

// A request entry with a window of its own refuses and resets with that window.
func TestWindowedRequestEntry(t *testing.T) {
	e := setup(t, nil)
	e.now = time.Date(2026, 11, 6, 10, 0, 30, 0, time.UTC)
	k := e.issue(narrowing(entry(grants.MetricRequests, 2, time.Minute)))
	for i := 0; i < 2; i++ {
		if err := e.admit(k); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
	qe := quotaErr(t, e.lim.Check(ctx(), k.id))
	if qe.Unit != limits.UnitRequests || qe.Window != time.Minute || qe.RetryAfter != 30*time.Second {
		t.Fatalf("quota error = %+v", qe)
	}
	e.advance(30 * time.Second)
	if err := e.admit(k); err != nil {
		t.Fatalf("next minute: %v", err)
	}
}

func TestLimitZeroRefusesEveryCall(t *testing.T) {
	e := setup(t, nil)
	tok := e.issue(narrowing(entry(grants.MetricTokens, 0, time.Hour)))
	req := e.issue(narrowing(entry(grants.MetricRequests, 0, time.Hour)))
	if qe := quotaErr(t, e.lim.Check(ctx(), tok.id)); qe.Unit != limits.UnitTokens || qe.Used != 0 {
		t.Errorf("tokens: %+v", qe)
	}
	if qe := quotaErr(t, e.lim.Check(ctx(), req.id)); qe.Unit != limits.UnitRequests {
		t.Errorf("requests: %+v", qe)
	}
}

// Two entries with different windows on one subject are both enforced, independently.
func TestTwoWindowsOnOneSubjectAreBothEnforced(t *testing.T) {
	e := setup(t, nil)
	e.now = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	k := e.issue(narrowing(entry(grants.MetricTokens, 100, time.Hour), entry(grants.MetricTokens, 150, 24*time.Hour)))

	e.use(k, 100) // the hour entry is used up, the day entry is not
	qe := quotaErr(t, e.lim.Check(ctx(), k.id))
	if qe.Window != time.Hour || qe.Limit != 100 {
		t.Fatalf("first refusal = %+v, want the hourly entry", qe)
	}
	e.advance(time.Hour) // a fresh hour: only the day entry has 100 of 150 used
	if err := e.lim.Check(ctx(), k.id); err != nil {
		t.Fatalf("new hour: %v", err)
	}
	e.use(k, 50) // day total 150
	qe = quotaErr(t, e.lim.Check(ctx(), k.id))
	if qe.Window != 24*time.Hour || qe.Limit != 150 || qe.Used != 150 {
		t.Fatalf("second refusal = %+v, want the daily entry", qe)
	}
	st, err := e.lim.StatusOf(ctx(), k.id)
	if err != nil || len(st.Quotas) != 2 {
		t.Fatalf("status = %+v, %v; want one QuotaStatus per window", st, err)
	}
	if h, d := st.Quotas[0], st.Quotas[1]; h.Window != time.Hour || *h.UsedTokens != 50 || d.Window != 24*time.Hour || *d.UsedTokens != 150 ||
		h.Scope != limits.ScopeAccessKey || h.Requests != nil {
		t.Fatalf("statuses = %+v %+v", h, d)
	}
}

// StatusOf groups the token and request entries of one scope and window into one QuotaStatus.
func TestStatusGroupsEntriesPerScopeAndWindow(t *testing.T) {
	e := setupVersion(t, grants.NewVersion{Quotas: []grants.QuotaEntry{
		entry(grants.MetricTokens, 1000, 24*time.Hour), entry(grants.MetricRequests, 50, 24*time.Hour), entry(grants.MetricRequests, 5, time.Minute)}})
	k := e.issue(narrowing(entry(grants.MetricTokens, 100, 24*time.Hour)))
	e.use(k, 10)
	if err := e.admit(k); err != nil {
		t.Fatal(err)
	}
	st, err := e.lim.StatusOf(ctx(), k.id)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Quotas) != 3 {
		t.Fatalf("quotas = %+v, want grant 1m, grant 24h, key 24h", st.Quotas)
	}
	g1, g24, ky := st.Quotas[0], st.Quotas[1], st.Quotas[2]
	if g1.Scope != limits.ScopeGrant || g1.Window != time.Minute || g1.Tokens != nil || *g1.Requests != 5 || *g1.UsedRequests != 1 {
		t.Errorf("grant 1m = %+v", g1)
	}
	if g24.Scope != limits.ScopeGrant || g24.Window != 24*time.Hour || *g24.Tokens != 1000 || *g24.UsedTokens != 10 || *g24.Requests != 50 || *g24.RemainingReqs != 49 {
		t.Errorf("grant 24h = %+v", g24)
	}
	if ky.Scope != limits.ScopeAccessKey || *ky.Tokens != 100 || *ky.RemainingTokens != 90 || ky.Requests != nil {
		t.Errorf("key 24h = %+v", ky)
	}
}

// A grant entry counts the consumer's calls across its keys; a key entry counts that key's only.
func TestGrantAndKeyScopesOfEntries(t *testing.T) {
	e := setupVersion(t, grants.NewVersion{Quotas: []grants.QuotaEntry{entry(grants.MetricRequests, 3, time.Hour)}})
	a := e.issue(narrowing(entry(grants.MetricRequests, 2, time.Hour)))
	b := e.issue(auth.Narrowing{})
	for _, k := range []key{a, b} {
		if err := e.admit(k); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.admit(a); err != nil { // a: 2 of its own 2, grant: 3 of 3
		t.Fatal(err)
	}
	// a hits both: its own entry (2) is checked after the grant's (3), the grant is used up first.
	qe := quotaErr(t, e.lim.Check(ctx(), a.id))
	if qe.Scope != limits.ScopeGrant || qe.Used != 3 {
		t.Fatalf("a: %+v, want the grant entry", qe)
	}
	if qe := quotaErr(t, e.lim.Check(ctx(), b.id)); qe.Scope != limits.ScopeGrant {
		t.Fatalf("b shares the grant: %+v", qe)
	}
}

func TestKeyEntryCountsOnlyThatKey(t *testing.T) {
	e := setup(t, nil)
	a := e.issue(narrowing(entry(grants.MetricRequests, 1, time.Hour)))
	b := e.issue(narrowing(entry(grants.MetricRequests, 1, time.Hour)))
	if err := e.admit(a); err != nil {
		t.Fatal(err)
	}
	if qe := quotaErr(t, e.lim.Check(ctx(), a.id)); qe.Scope != limits.ScopeAccessKey {
		t.Fatalf("a: %+v", qe)
	}
	if err := e.lim.Check(ctx(), b.id); err != nil {
		t.Fatalf("b must be unaffected by a's calls: %v", err)
	}
}

// The request counter is in the database: it survives a restart.
func TestCountersSurviveRestart(t *testing.T) {
	e := setupVersion(t, grants.NewVersion{Quotas: []grants.QuotaEntry{entry(grants.MetricRequests, 3, time.Hour)}})
	k := e.issue(narrowing(entry(grants.MetricRequests, 2, 24*time.Hour)))
	for i := 0; i < 2; i++ {
		if err := e.admit(k); err != nil {
			t.Fatal(err)
		}
	}
	// Close everything and open the same file again, with a new settings store and a new limiter.
	if err := e.db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := storage.Open(ctx(), storage.DefaultConfig(e.path, settings.Module(), identity.Module(), grants.Module(), audit.Module(), events.Module(), usage.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db2.Close() })
	lim2, err := limits.New(limits.Config{DB: db2, Identity: e.ir, Grants: e.gr, Events: e.bus, Settings: settings.New(db2), Clock: e.clock, Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	qe := quotaErr(t, lim2.Check(ctx(), k.id))
	if qe.Unit != limits.UnitRequests || qe.Used != 2 || qe.Limit != 2 {
		t.Fatalf("after restart: %+v, want the 2 counted calls to still count", qe)
	}
	// The key's entry is full: counting a third call is refused and raises neither entry.
	quotaErr(t, lim2.CountAdmitted(ctx(), k.id))
	st, err := lim2.StatusOf(ctx(), k.id)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range st.Quotas {
		if q.Window == time.Hour && *q.UsedRequests != 2 {
			t.Errorf("grant counter after restart = %d, want 2", *q.UsedRequests)
		}
	}
}

// A call admitted at the end of window W and finished in W+1 is charged once, to W (req_001 D-107).
func TestCallSpanningAWindowBoundaryIsChargedToItsAdmissionWindow(t *testing.T) {
	e := setup(t, nil)
	k := e.issue(narrowing(entry(grants.MetricTokens, 100, time.Hour), entry(grants.MetricRequests, 1, time.Hour)))
	boundary := time.Date(2026, 11, 6, 11, 0, 0, 0, time.UTC)
	e.now = boundary.Add(-2 * time.Second)
	if err := e.admit(k); err != nil { // admitted in the 10:00 window
		t.Fatal(err)
	}
	e.advance(5 * time.Second) // the call finishes at 11:00:03 and is metered then
	e.useAt(k, 100, boundary.Add(-2*time.Second), e.clock())

	// The next window sees neither the tokens nor the request.
	if err := e.lim.Check(ctx(), k.id); err != nil {
		t.Fatalf("the call was charged to the window it finished in: %v", err)
	}
	st, err := e.lim.StatusOf(ctx(), k.id)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Quotas) != 1 || *st.Quotas[0].UsedTokens != 0 || *st.Quotas[0].UsedRequests != 0 {
		t.Fatalf("window after the boundary: %+v, want 0 tokens and 0 requests", st.Quotas)
	}
	// The window it was admitted in holds it once.
	e.advance(-5 * time.Second)
	qe := quotaErr(t, e.lim.Check(ctx(), k.id))
	if qe.Unit != limits.UnitTokens || qe.Used != 100 {
		t.Fatalf("admission window: %+v, want the 100 tokens in the window it was admitted in", qe)
	}
	st, err = e.lim.StatusOf(ctx(), k.id)
	if err != nil || *st.Quotas[0].UsedTokens != 100 || *st.Quotas[0].UsedRequests != 1 {
		t.Fatalf("admission window status = %+v, %v; want 100 tokens, 1 request", st.Quotas, err)
	}
}

// An instant exactly at the end of a window belongs to the next window.
func TestWindowEndIsExclusive(t *testing.T) {
	e := setup(t, nil)
	k := e.issue(narrowing(entry(grants.MetricRequests, 1, time.Hour)))
	e.now = time.Date(2026, 11, 6, 10, 59, 59, 999_000_000, time.UTC)
	if err := e.admit(k); err != nil {
		t.Fatal(err)
	}
	e.advance(time.Millisecond) // 11:00:00.000
	if err := e.admit(k); err != nil {
		t.Fatalf("11:00:00 is the next window: %v", err)
	}
}

// The deprecated alias columns and fields are still enforced, with the window limits.quota_window.
func TestLegacyAliasIsEnforcedWithQuotaWindow(t *testing.T) {
	e := setupLegacy(t, i64(100), i64(2))
	e.set(limits.KeyQuotaWindow, "1h")
	e.now = time.Date(2026, 11, 6, 10, 15, 0, 0, time.UTC)
	k := e.issue(auth.Narrowing{QuotaTokensPerDay: i64(40)}) // a key stored before the list existed
	e.use(k, 40)
	qe := quotaErr(t, e.lim.Check(ctx(), k.id))
	if qe.Scope != limits.ScopeAccessKey || qe.Window != time.Hour || qe.Limit != 40 || qe.RetryAfter != 45*time.Minute {
		t.Fatalf("legacy key figure: %+v", qe)
	}
	e.advance(time.Hour)
	if err := e.admit(k); err != nil {
		t.Fatal(err)
	}
	if err := e.admit(k); err != nil {
		t.Fatal(err)
	}
	qe = quotaErr(t, e.lim.Check(ctx(), k.id))
	if qe.Scope != limits.ScopeGrant || qe.Unit != limits.UnitRequests || qe.Window != time.Hour || qe.Limit != 2 {
		t.Fatalf("legacy grant column: %+v", qe)
	}
	// The setting is read at check time: with a 2h window the same figure counts over 2h.
	e.set(limits.KeyQuotaWindow, "2h") // now 11:15: the 2h window 10:00-12:00 holds the 40 tokens of 10:15
	if qe := quotaErr(t, e.lim.Check(ctx(), k.id)); qe.Scope != limits.ScopeAccessKey || qe.Unit != limits.UnitTokens || qe.Window != 2*time.Hour {
		t.Fatalf("after the setting changed: %+v, want the key's token figure over 2h", qe)
	}
}

// When a subject has a list entry of a metric, the deprecated figure of that metric is ignored.
func TestListEntryWinsOverLegacyFigureOfSameMetric(t *testing.T) {
	e := setup(t, nil)
	k := e.issue(auth.Narrowing{QuotaTokensPerDay: i64(10), Quotas: []grants.QuotaEntry{entry(grants.MetricTokens, 1000, time.Hour)}})
	e.use(k, 500)
	if err := e.lim.Check(ctx(), k.id); err != nil {
		t.Fatalf("the legacy figure 10 must be ignored, the list's 1000 applies: %v", err)
	}
	// A figure of a metric the list does not cover still applies.
	r := e.issue(auth.Narrowing{QuotaRequestsPerDay: i64(1), Quotas: []grants.QuotaEntry{entry(grants.MetricTokens, 1000, time.Hour)}})
	if err := e.admit(r); err != nil {
		t.Fatal(err)
	}
	if qe := quotaErr(t, e.lim.Check(ctx(), r.id)); qe.Unit != limits.UnitRequests || qe.Limit != 1 {
		t.Fatalf("legacy request figure: %+v", qe)
	}
}

// CountAdmitted counts every admitted call: with no usage record, and with a request entry on
// the grant for the consumer and on the key for the key.
func TestCountAdmittedCountsEveryAdmittedCall(t *testing.T) {
	e := setupVersion(t, grants.NewVersion{Quotas: []grants.QuotaEntry{entry(grants.MetricRequests, 100, time.Hour)}})
	k := e.issue(narrowing(entry(grants.MetricRequests, 100, 24*time.Hour)))
	for i := 0; i < 7; i++ {
		if err := e.lim.CountAdmitted(ctx(), k.id); err != nil {
			t.Fatal(err)
		}
	}
	rd := e.db.Reader()
	start := e.clock().Truncate(time.Hour)
	if n, err := usage.RequestCount(ctx(), rd, usage.ByConsumer, k.id.ConsumerID, time.Hour, start); err != nil || n != 7 {
		t.Errorf("consumer counter = %d, %v; want 7", n, err)
	}
	if n, err := usage.RequestCount(ctx(), rd, usage.ByAccessKey, k.id.KeyID, 24*time.Hour, e.clock().Truncate(24*time.Hour)); err != nil || n != 7 {
		t.Errorf("key counter = %d, %v; want 7", n, err)
	}
	// No counter for a window no entry asks for.
	if n, _ := usage.RequestCount(ctx(), rd, usage.ByAccessKey, k.id.KeyID, time.Hour, start); n != 0 {
		t.Errorf("key hour counter = %d, want 0", n)
	}
}

// CountAdmitted re-checks the request entries and the check and the count are one unit of work:
// a full window refuses with a *QuotaError and no entry of either scope is raised.
func TestCountAdmittedRefusesAFullWindowAndCountsNothing(t *testing.T) {
	e := setupVersion(t, grants.NewVersion{Quotas: []grants.QuotaEntry{entry(grants.MetricRequests, 100, time.Hour)}})
	k := e.issue(narrowing(entry(grants.MetricRequests, 2, time.Hour)))
	for i := 0; i < 2; i++ {
		if err := e.lim.CountAdmitted(ctx(), k.id); err != nil {
			t.Fatal(err)
		}
	}
	qe := quotaErr(t, e.lim.CountAdmitted(ctx(), k.id))
	if qe.Unit != limits.UnitRequests || qe.Limit != 2 || qe.Used != 2 || qe.Scope != limits.ScopeAccessKey || qe.RetryAfter <= 0 {
		t.Fatalf("QuotaError = %+v, want the key's request entry, 2 of 2", qe)
	}
	rd := e.db.Reader()
	start := e.clock().Truncate(time.Hour)
	if n, err := usage.RequestCount(ctx(), rd, usage.ByConsumer, k.id.ConsumerID, time.Hour, start); err != nil || n != 2 {
		t.Errorf("consumer counter = %d, %v; want 2 (the refused call counted nothing)", n, err)
	}
	if n, err := usage.RequestCount(ctx(), rd, usage.ByAccessKey, k.id.KeyID, time.Hour, start); err != nil || n != 2 {
		t.Errorf("key counter = %d, %v; want 2", n, err)
	}
}

func TestCountAdmittedWithoutRequestEntriesWritesNothing(t *testing.T) {
	e := setup(t, i64(100))
	k := e.issue(keyDay(grants.MetricTokens, 10))
	if err := e.lim.CountAdmitted(ctx(), k.id); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := e.db.Reader().QueryRow(ctx(), `SELECT count(*) FROM use_request_counter`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("counter rows = %d, %v; want 0", n, err)
	}
}

func TestCountAdmittedConcurrentIsExact(t *testing.T) {
	e := setup(t, nil)
	k := e.issue(narrowing(entry(grants.MetricRequests, 1_000_000, time.Hour)))
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				if err := e.lim.CountAdmitted(ctx(), k.id); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	st, err := e.lim.StatusOf(ctx(), k.id)
	if err != nil || *st.Quotas[0].UsedRequests != 200 {
		t.Fatalf("status = %+v, %v; want 200 requests", st.Quotas, err)
	}
}

// Counters of windows that ended more than limits.counter_retention ago are pruned from
// CountAdmitted; a running window and a recent one are kept.
func TestCountersArePrunedByRetention(t *testing.T) {
	e := setup(t, nil)
	e.set(limits.KeyCounterRetention, "4h")
	e.set(limits.KeyCounterPruneInterval, "1h")
	k := e.issue(narrowing(entry(grants.MetricRequests, 1_000_000, time.Hour)))
	counters := func() int {
		var n int
		if err := e.db.Reader().QueryRow(ctx(), `SELECT count(*) FROM use_request_counter WHERE subject_kind = 'access_key'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	e.now = time.Date(2026, 11, 6, 10, 30, 0, 0, time.UTC)
	for h := 0; h < 6; h++ { // windows 10:00, 11:00, ... 15:00, one call each; each admission may prune
		if err := e.lim.CountAdmitted(ctx(), k.id); err != nil {
			t.Fatal(err)
		}
		e.advance(time.Hour)
	}
	// Now 16:30. The prune runs at most every limits.counter_prune_interval = 1h: the window 10:00-11:00 ended 5h30m
	// ago and is gone once a prune has run at or after 15:00; windows ending within 4h are kept.
	if err := e.lim.CountAdmitted(ctx(), k.id); err != nil {
		t.Fatal(err)
	}
	// Kept: windows ending after 12:30, i.e. 12:00-13:00 .. 16:00-17:00 (5 windows).
	if got := counters(); got != 5 {
		t.Fatalf("counters after prune = %d, want 5", got)
	}
	e.advance(50 * time.Minute) // 17:20: less than an hour after the last prune at 16:30
	before := counters()
	if err := e.lim.CountAdmitted(ctx(), k.id); err != nil {
		t.Fatal(err)
	}
	if got := counters(); got != before+1 {
		t.Fatalf("counters = %d, want %d: a prune ran again within the prune interval", got, before+1)
	}
}

func TestRetentionIsNeverShorterThanTheRunningWindow(t *testing.T) {
	e := setup(t, nil)
	e.set(limits.KeyCounterRetention, "1s")
	k := e.issue(narrowing(entry(grants.MetricRequests, 5, 24*time.Hour)))
	for i := 0; i < 3; i++ {
		if err := e.lim.CountAdmitted(ctx(), k.id); err != nil {
			t.Fatal(err)
		}
		e.advance(time.Minute)
	}
	st, err := e.lim.StatusOf(ctx(), k.id)
	if err != nil || *st.Quotas[0].UsedRequests != 3 {
		t.Fatalf("status = %+v, %v; a running window's counter was pruned", st.Quotas, err)
	}
}

func TestCheckWindowAndLegacyWindow(t *testing.T) {
	e := setup(t, nil)
	for _, tc := range []struct {
		name string
		w    time.Duration
		ok   bool
	}{
		{"default minimum", time.Minute, true},
		{"default maximum", 720 * time.Hour, true},
		{"below minimum", 59 * time.Second, false},
		{"above maximum", 720*time.Hour + time.Second, false},
		{"fractional second", 90*time.Second + time.Millisecond, false},
		{"zero", 0, false},
		{"negative", -time.Hour, false},
	} {
		err := limits.CheckWindow(ctx(), e.st, tc.w)
		var we *limits.WindowError
		switch {
		case tc.ok && err != nil:
			t.Errorf("%s: %v", tc.name, err)
		case !tc.ok && !errors.As(err, &we):
			t.Errorf("%s: err = %v, want *WindowError", tc.name, err)
		}
	}
	e.set(limits.KeyQuotaWindowMin, "10s")
	e.set(limits.KeyQuotaWindowMax, "2h")
	if err := limits.CheckWindow(ctx(), e.st, 10*time.Second); err != nil {
		t.Errorf("new minimum: %v", err)
	}
	if err := limits.CheckWindow(ctx(), e.st, 2*time.Hour+time.Second); err == nil {
		t.Error("new maximum not applied")
	}
	// A maximum below the minimum allows no window.
	e.set(limits.KeyQuotaWindowMax, "5s")
	if err := limits.CheckWindow(ctx(), e.st, 7*time.Second); err == nil {
		t.Error("a window passed with max < min")
	}

	if w, err := limits.LegacyWindow(ctx(), e.st); err != nil || w != 24*time.Hour {
		t.Errorf("LegacyWindow = %s, %v; want 24h", w, err)
	}
	e.set(limits.KeyQuotaWindow, "90s")
	if w, _ := limits.LegacyWindow(ctx(), e.st); w != 90*time.Second {
		t.Errorf("LegacyWindow = %s, want 90s", w)
	}
	if err := e.st.Set(ctx(), settings.Host, limits.KeyQuotaWindow, "", "500ms"); !errors.Is(err, settings.ErrRejected) {
		t.Errorf("quota_window 500ms: %v, want ErrRejected", err)
	}
}

// A maximum below the minimum allows no window, and the refusal says so.
func TestCheckWindowSaysWhenTheBoundsAllowNoWindow(t *testing.T) {
	e := setup(t, nil)
	e.set(limits.KeyQuotaWindowMax, "10m")
	e.set(limits.KeyQuotaWindowMin, "1h")
	err := limits.CheckWindow(ctx(), e.st, 30*time.Minute)
	var we *limits.WindowError
	if !errors.As(err, &we) {
		t.Fatalf("err = %v, want *WindowError", err)
	}
	want := "This host allows no quota window: its shortest window (1h0m0s) is longer than its longest (10m0s)."
	if we.Reason != want {
		t.Fatalf("reason = %q, want %q", we.Reason, want)
	}
}

func TestNearLimitIsPerWindow(t *testing.T) {
	e := setup(t, nil)
	e.set(limits.KeyNearLimitPercent, "50")
	k := e.issue(narrowing(entry(grants.MetricTokens, 100, time.Hour), entry(grants.MetricTokens, 1000, 24*time.Hour)))
	e.now = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	e.use(k, 60) // 60% of the hourly entry, 6% of the daily one
	for i := 0; i < 2; i++ {
		if err := e.lim.Record(ctx(), k.id); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(e.events()); n != 1 {
		t.Fatalf("events = %d, want 1 (hourly entry only)", n)
	}
	e.use(k, 500) // now 56% of the daily entry too
	if err := e.lim.Record(ctx(), k.id); err != nil {
		t.Fatal(err)
	}
	if n := len(e.events()); n != 2 {
		t.Fatalf("events = %d, want 2: the daily window has its own mark", n)
	}
}
