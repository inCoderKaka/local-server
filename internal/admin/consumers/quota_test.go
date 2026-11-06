package consumers_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func entry(metric string, limit int64, window time.Duration) grants.QuotaEntry {
	return grants.QuotaEntry{Metric: metric, Limit: limit, Window: window}
}

// quotaEnv is a consumer with a grant of tokens 1000 per day, tokens 100 per hour and requests 50
// per day.
func quotaEnv(t *testing.T) (*env, identity.ConsumerAccount) {
	t.Helper()
	e := newEnv(t)
	c := e.newConsumer("App")
	e.grant(c.ID, grants.NewVersion{Quotas: []grants.QuotaEntry{
		entry(grants.MetricTokens, 1000, 24*time.Hour), entry(grants.MetricTokens, 100, time.Hour), entry(grants.MetricRequests, 50, 24*time.Hour)}}, "m1")
	return e, c
}

func (e *env) narrowingJSON(keyID string) string {
	e.t.Helper()
	var s *string
	if err := e.db.Reader().QueryRow(ctx, `SELECT narrowing_json FROM idn_access_key WHERE id = ?`, keyID).Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	if s == nil {
		return ""
	}
	return *s
}

func TestQuotaEntriesAreStoredAndShown(t *testing.T) {
	e, c := quotaEnv(t)
	in := consumers.Narrowing{Quotas: []grants.QuotaEntry{
		entry(grants.MetricRequests, 5, 24*time.Hour), entry(grants.MetricTokens, 40, 2*time.Hour), entry(grants.MetricTokens, 500, 24*time.Hour)}}
	is := e.issue(c.ID, consumers.NewKey{Narrowing: &in})
	// Stored as the list, tokens first then by window; no deprecated field is written.
	want := `{"quotas":[{"metric":"tokens","limit":40,"window_seconds":7200},{"metric":"tokens","limit":500,"window_seconds":86400},{"metric":"requests","limit":5,"window_seconds":86400}]}`
	if got := e.narrowingJSON(is.Key.ID); got != want {
		t.Fatalf("stored narrowing = %s\nwant %s", got, want)
	}
	n := is.Key.Narrowing
	if n == nil || len(n.Quotas) != 3 || n.Quotas[0].Limit != 40 {
		t.Fatalf("issued narrowing = %+v", n)
	}
	// The deprecated alias shows the entries whose window is limits.quota_window (24h now).
	if n.Quota == nil || *n.Quota.Tokens != 500 || *n.Quota.Requests != 5 {
		t.Fatalf("alias = %+v", n.Quota)
	}
	if len(is.Key.Warnings) != 0 {
		t.Errorf("warnings = %v", is.Key.Warnings)
	}
	l, err := e.svc.ListAccessKeys(ctx, c.ID)
	if err != nil || len(l[0].Narrowing.Quotas) != 3 || l[0].Narrowing.Quota == nil {
		t.Fatalf("listed = %+v, %v", l, err)
	}
	// Enforced: the limiter-facing effective list has them merged per (metric, window).
	id, err := e.au.Authenticate(ctx, "Bearer "+is.Secret.Reveal(), "100.64.0.1:1")
	if err != nil || len(id.Limits.Quotas) != 4 {
		t.Fatalf("effective quotas = %+v, %v; want grant's 3 merged with key's 3 over 4 windows/metrics", id.Limits.Quotas, err)
	}
	// If the host changes limits.quota_window the alias follows it, the entries do not move.
	e.set(limits.KeyQuotaWindow, "2h")
	l, _ = e.svc.ListAccessKeys(ctx, c.ID)
	if a := l[0].Narrowing.Quota; a == nil || *a.Tokens != 40 || a.Requests != nil {
		t.Fatalf("alias after the setting changed = %+v", a)
	}
}

func TestQuotaEntryValidation(t *testing.T) {
	e, c := quotaEnv(t)
	tok := func(limit int64, w time.Duration) grants.QuotaEntry { return entry(grants.MetricTokens, limit, w) }
	bad := map[string]struct {
		qs    []grants.QuotaEntry
		param string
	}{
		"bad metric":        {[]grants.QuotaEntry{entry("bytes", 1, time.Hour)}, "narrowing.quotas.0.metric"},
		"negative limit":    {[]grants.QuotaEntry{tok(1, time.Hour), tok(-1, 2*time.Hour)}, "narrowing.quotas.1.limit"},
		"zero window":       {[]grants.QuotaEntry{tok(1, 0)}, "narrowing.quotas.0.window_seconds"},
		"fractional window": {[]grants.QuotaEntry{tok(1, 90*time.Second+time.Millisecond)}, "narrowing.quotas.0.window_seconds"},
		"duplicate":         {[]grants.QuotaEntry{tok(1, time.Hour), tok(2, time.Hour)}, "narrowing.quotas.1.window_seconds"},
		"below minimum":     {[]grants.QuotaEntry{tok(1, 30*time.Second)}, "narrowing.quotas.0.window_seconds"},
		"above maximum":     {[]grants.QuotaEntry{tok(1, 721*time.Hour)}, "narrowing.quotas.0.window_seconds"},
	}
	for name, b := range bad {
		_, err := e.svc.IssueAccessKey(ctx, e.admin, c.ID, consumers.NewKey{Narrowing: &consumers.Narrowing{Quotas: b.qs}})
		if err == nil {
			t.Errorf("%s accepted", name)
			continue
		}
		wantKind(t, err, consumers.KindInvalid, b.param)
	}
	if n := e.count(`SELECT count(*) FROM idn_access_key`); n != 0 {
		t.Fatalf("%d keys after refused issues", n)
	}
	// The bounds are host settings, read now.
	e.set(limits.KeyQuotaWindowMin, "10s")
	e.set(limits.KeyQuotaWindowMax, "1000h")
	e.issue(c.ID, consumers.NewKey{Narrowing: &consumers.Narrowing{Quotas: []grants.QuotaEntry{tok(1, 30*time.Second), tok(1, 721*time.Hour)}}})
	// A window message names the bound.
	e.set(limits.KeyQuotaWindowMin, "1m")
	_, err := e.svc.IssueAccessKey(ctx, e.admin, c.ID, consumers.NewKey{Narrowing: &consumers.Narrowing{Quotas: []grants.QuotaEntry{tok(1, 30*time.Second)}}})
	var ce *consumers.Error
	if !errors.As(err, &ce) || !strings.Contains(ce.Message, "1m0s") {
		t.Errorf("message = %v, want the bound in it", err)
	}
}

// A key entry is wider than the grant only against the grant's entry of the same metric and
// window; different windows are not comparable.
func TestKeyEntryWiderThanGrantByMetricAndWindow(t *testing.T) {
	e, c := quotaEnv(t) // grant: tokens 1000/24h, tokens 100/1h, requests 50/24h
	issue := func(qs ...grants.QuotaEntry) error {
		_, err := e.svc.IssueAccessKey(ctx, e.admin, c.ID, consumers.NewKey{Narrowing: &consumers.Narrowing{Quotas: qs}})
		return err
	}
	for name, tc := range map[string]struct {
		qs    []grants.QuotaEntry
		param string // "" means accepted
	}{
		"equal":                       {[]grants.QuotaEntry{entry(grants.MetricTokens, 1000, 24*time.Hour)}, ""},
		"wider, same window":          {[]grants.QuotaEntry{entry(grants.MetricTokens, 101, time.Hour)}, "narrowing.quotas.0.limit"},
		"second entry wider":          {[]grants.QuotaEntry{entry(grants.MetricTokens, 1, 24*time.Hour), entry(grants.MetricRequests, 51, 24*time.Hour)}, "narrowing.quotas.1.limit"},
		"larger number, other window": {[]grants.QuotaEntry{entry(grants.MetricTokens, 5000, 2*time.Hour)}, ""},
		"requests, other window":      {[]grants.QuotaEntry{entry(grants.MetricRequests, 99999, time.Hour)}, ""},
	} {
		err := issue(tc.qs...)
		if tc.param == "" && err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if tc.param != "" {
			if err == nil {
				t.Errorf("%s accepted", name)
			} else {
				wantKind(t, err, consumers.KindInvalid, tc.param)
			}
		}
	}
}

func TestDeprecatedQuotaIsExpandedWithTheHostWindow(t *testing.T) {
	e, c := quotaEnv(t)
	q := consumers.Narrowing{Quota: &consumers.Quota{Tokens: i64(300), Requests: i64(7)}}
	is := e.issue(c.ID, consumers.NewKey{Narrowing: &q})
	want := `{"quotas":[{"metric":"tokens","limit":300,"window_seconds":86400},{"metric":"requests","limit":7,"window_seconds":86400}]}`
	if got := e.narrowingJSON(is.Key.ID); got != want {
		t.Fatalf("stored = %s, want %s", got, want)
	}
	if n := is.Key.Narrowing; len(n.Quotas) != 2 || *n.Quota.Tokens != 300 || len(is.Key.Warnings) != 0 {
		t.Fatalf("issued = %+v warnings %v", n, is.Key.Warnings)
	}
	// With another host window the figure gets that window when it is written.
	e.set(limits.KeyQuotaWindow, "1h")
	is2 := e.issue(c.ID, consumers.NewKey{Narrowing: &consumers.Narrowing{Quota: &consumers.Quota{Tokens: i64(50)}}})
	if got := e.narrowingJSON(is2.Key.ID); got != `{"quotas":[{"metric":"tokens","limit":50,"window_seconds":3600}]}` {
		t.Fatalf("stored = %s", got)
	}
	// The deprecated figure is checked against the grant's entry of the same window like any entry.
	if _, err := e.svc.IssueAccessKey(ctx, e.admin, c.ID, consumers.NewKey{Narrowing: &consumers.Narrowing{Quota: &consumers.Quota{Tokens: i64(101)}}}); err == nil {
		t.Error("101 tokens per hour accepted against the grant's 100 per hour")
	} else {
		wantKind(t, err, consumers.KindInvalid, "narrowing.quota.tokens")
	}
	// A host window outside the min and max is the host's own and is not refused for the alias.
	e.set(limits.KeyQuotaWindow, "5s")
	is3 := e.issue(c.ID, consumers.NewKey{Narrowing: &consumers.Narrowing{Quota: &consumers.Quota{Requests: i64(1)}}})
	if got := e.narrowingJSON(is3.Key.ID); got != `{"quotas":[{"metric":"requests","limit":1,"window_seconds":5}]}` {
		t.Fatalf("stored = %s", got)
	}
}

func TestQuotasWinOverTheDeprecatedQuota(t *testing.T) {
	e, c := quotaEnv(t)
	in := consumers.Narrowing{Quotas: []grants.QuotaEntry{entry(grants.MetricTokens, 10, time.Hour)}, Quota: &consumers.Quota{Tokens: i64(999999)}}
	is := e.issue(c.ID, consumers.NewKey{Narrowing: &in})
	if got := e.narrowingJSON(is.Key.ID); got != `{"quotas":[{"metric":"tokens","limit":10,"window_seconds":3600}]}` {
		t.Fatalf("stored = %s", got)
	}
	if len(is.Key.Warnings) != 1 || !strings.Contains(is.Key.Warnings[0], "quota is deprecated") {
		t.Fatalf("warnings = %v", is.Key.Warnings)
	}
	// The ignored alias is not validated against the grant (it is not used).
	u, err := e.svc.UpdateAccessKey(ctx, e.admin, is.Key.ID, consumers.KeyChange{Narrowing: consumers.Value(in)})
	if err != nil || len(u.Warnings) != 1 {
		t.Fatalf("update warnings = %v, %v", u.Warnings, err)
	}
	u, err = e.svc.UpdateAccessKey(ctx, e.admin, is.Key.ID, consumers.KeyChange{Narrowing: consumers.Value(consumers.Narrowing{Quotas: in.Quotas})})
	if err != nil || len(u.Warnings) != 0 {
		t.Fatalf("update without the alias: warnings %v, %v", u.Warnings, err)
	}
}

// A key stored before the list existed is shown with its deprecated figures as entries.
func TestStoredLegacyNarrowingIsShownAsEntries(t *testing.T) {
	e, c := quotaEnv(t)
	is := e.issue(c.ID, consumers.NewKey{})
	if err := e.db.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.Exec(`UPDATE idn_access_key SET narrowing_json = ? WHERE id = ?`, `{"quota_tokens_per_day":42,"quota_requests_per_day":3}`, is.Key.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	l, err := e.svc.ListAccessKeys(ctx, c.ID)
	if err != nil || l[0].Narrowing == nil {
		t.Fatalf("listed = %+v, %v", l, err)
	}
	n := l[0].Narrowing
	if len(n.Quotas) != 2 || n.Quotas[0] != entry(grants.MetricTokens, 42, 24*time.Hour) || n.Quotas[1] != entry(grants.MetricRequests, 3, 24*time.Hour) ||
		*n.Quota.Tokens != 42 || *n.Quota.Requests != 3 {
		t.Fatalf("narrowing = %+v", n)
	}
	// Re-saving it replaces the narrowing whole and writes the list form.
	u, err := e.svc.UpdateAccessKey(ctx, e.admin, is.Key.ID, consumers.KeyChange{Narrowing: consumers.Value(*n)})
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Quotas []json.RawMessage `json:"quotas"`
		Tokens *int64            `json:"quota_tokens_per_day"`
	}
	if err := json.Unmarshal([]byte(e.narrowingJSON(u.ID)), &stored); err != nil || len(stored.Quotas) != 2 || stored.Tokens != nil {
		t.Fatalf("stored after update = %s, %v", e.narrowingJSON(u.ID), err)
	}
}

// Who may set windows: a consumer that is not chargeable cannot do it on its own keys.
func TestOnlyAChargeableConsumerSetsWindowsOnItsOwnKeys(t *testing.T) {
	e, c := quotaEnv(t)
	own := c.PrincipalID
	withQuota := &consumers.Narrowing{Quotas: []grants.QuotaEntry{entry(grants.MetricTokens, 10, time.Hour)}}
	asAlias := &consumers.Narrowing{Quota: &consumers.Quota{Tokens: i64(10)}}
	noQuota := &consumers.Narrowing{Concurrency: i64(1)}

	for name, n := range map[string]*consumers.Narrowing{"entries": withQuota, "deprecated quota": asAlias} {
		_, err := e.svc.IssueAccessKey(ctx, own, c.ID, consumers.NewKey{Narrowing: n})
		wantKind(t, err, consumers.KindForbidden, "narrowing.quotas")
		if err == nil {
			t.Errorf("%s: a non-chargeable consumer set a quota on its own key", name)
		}
	}
	if n := e.count(`SELECT count(*) FROM idn_access_key`); n != 0 {
		t.Fatalf("%d keys after refused issues", n)
	}
	// It may issue a key with no quota entry, and the host may set windows for it.
	own1, err := e.svc.IssueAccessKey(ctx, own, c.ID, consumers.NewKey{Narrowing: noQuota})
	if err != nil {
		t.Fatalf("self-service key without quota: %v", err)
	}
	if _, err := e.svc.IssueAccessKey(ctx, e.admin, c.ID, consumers.NewKey{Narrowing: withQuota}); err != nil {
		t.Fatalf("host sets windows: %v", err)
	}
	// Update: the same rule.
	_, err = e.svc.UpdateAccessKey(ctx, own, own1.Key.ID, consumers.KeyChange{Narrowing: consumers.Value(*withQuota)})
	wantKind(t, err, consumers.KindForbidden, "narrowing.quotas")
	if _, err := e.svc.UpdateAccessKey(ctx, own, own1.Key.ID, consumers.KeyChange{Narrowing: consumers.Value(*noQuota)}); err != nil {
		t.Fatalf("self-service update without quota: %v", err)
	}
	if _, err := e.svc.UpdateAccessKey(ctx, e.admin, own1.Key.ID, consumers.KeyChange{Narrowing: consumers.Value(*withQuota)}); err != nil {
		t.Fatalf("host update: %v", err)
	}

	// Once the consumer is chargeable (prepaid or postpaid) it may.
	for _, mode := range []string{identity.BillingPrepaid, identity.BillingPostpaid} {
		if _, err := e.svc.UpdateConsumer(ctx, e.admin, c.ID, consumers.ConsumerChange{BillingMode: consumers.Value(mode)}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.IssueAccessKey(ctx, own, c.ID, consumers.NewKey{Narrowing: withQuota}); err != nil {
			t.Fatalf("%s consumer on its own key: %v", mode, err)
		}
	}
	// ... and back to none: refused again.
	if _, err := e.svc.UpdateConsumer(ctx, e.admin, c.ID, consumers.ConsumerChange{BillingMode: consumers.Value(identity.BillingNone)}); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.IssueAccessKey(ctx, own, c.ID, consumers.NewKey{Narrowing: withQuota})
	wantKind(t, err, consumers.KindForbidden, "narrowing.quotas")
}

func TestMaySetQuotaWindows(t *testing.T) {
	none := identity.Consumer{PrincipalID: "p", BillingMode: identity.BillingNone}
	paid := identity.Consumer{PrincipalID: "p", BillingMode: identity.BillingPostpaid}
	for _, tc := range []struct {
		name  string
		c     identity.Consumer
		actor string
		want  bool
	}{
		{"own principal, none", none, "p", false},
		{"own principal, chargeable", paid, "p", true},
		{"host actor, none", none, "host", true},
		{"host actor, chargeable", paid, "host", true},
		{"no actor recorded", none, "", true},
	} {
		if got := consumers.MaySetQuotaWindows(tc.c, tc.actor); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBillingModeOnConsumers(t *testing.T) {
	e := newEnv(t)
	plain := e.newConsumer("Default")
	if plain.BillingMode != identity.BillingNone {
		t.Fatalf("default billing mode = %q", plain.BillingMode)
	}
	paid, err := e.svc.CreateConsumer(ctx, e.admin, consumers.NewConsumer{Kind: identity.ConsumerApplication, DisplayName: "Paying", BillingMode: identity.BillingPrepaid})
	if err != nil || paid.BillingMode != identity.BillingPrepaid {
		t.Fatalf("created = %+v, %v", paid, err)
	}
	if _, err := e.svc.CreateConsumer(ctx, e.admin, consumers.NewConsumer{Kind: identity.ConsumerApplication, DisplayName: "X", BillingMode: "free"}); err == nil {
		t.Error("an unknown billing mode was accepted on create")
	} else {
		wantKind(t, err, consumers.KindInvalid, "billing_mode")
	}

	e.tick()
	u, err := e.svc.UpdateConsumer(ctx, e.admin, plain.ID, consumers.ConsumerChange{BillingMode: consumers.Value(identity.BillingPostpaid)})
	if err != nil || u.BillingMode != identity.BillingPostpaid || u.RowVersion != plain.RowVersion+1 || u.DisplayName == nil || *u.DisplayName != "Default" {
		t.Fatalf("updated = %+v, %v", u, err)
	}
	g, err := e.svc.GetConsumer(ctx, plain.ID)
	if err != nil || g.BillingMode != identity.BillingPostpaid {
		t.Fatalf("got = %+v, %v", g, err)
	}
	// The same value again writes nothing; name and mode together are one audit row.
	before := e.count(`SELECT count(*) FROM aud_event WHERE action = 'consumer.updated'`)
	if _, err := e.svc.UpdateConsumer(ctx, e.admin, plain.ID, consumers.ConsumerChange{BillingMode: consumers.Value(identity.BillingPostpaid)}); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = 'consumer.updated'`); n != before {
		t.Errorf("an unchanged billing mode wrote an audit row")
	}
	u, err = e.svc.UpdateConsumer(ctx, e.admin, plain.ID, consumers.ConsumerChange{DisplayName: consumers.Value("Renamed"), BillingMode: consumers.Value(identity.BillingNone)})
	if err != nil || *u.DisplayName != "Renamed" || u.BillingMode != identity.BillingNone {
		t.Fatalf("name and mode = %+v, %v", u, err)
	}
	if !strings.Contains(e.auditText(), `"billing_mode"`) {
		t.Errorf("the audit trail does not name the billing_mode field:\n%s", e.auditText())
	}
	for name, ch := range map[string]consumers.Field[string]{"unknown": consumers.Value("free"), "null": consumers.Null[string]()} {
		_, err := e.svc.UpdateConsumer(ctx, e.admin, plain.ID, consumers.ConsumerChange{BillingMode: ch})
		if err == nil {
			t.Errorf("%s billing mode accepted on update", name)
			continue
		}
		wantKind(t, err, consumers.KindInvalid, "billing_mode")
	}
}
