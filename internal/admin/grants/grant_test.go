package grants_test

import (
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	admingrants "github.com/inCoderKaka/local-server/internal/admin/grants"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// A grant edit makes a new version, makes it current, keeps the old one, and clears the key cache.
// The audit row and the event are written with the version (req_001 D-25).
func TestGrantEditCreatesNewVersionAndClearsKeyCache(t *testing.T) {
	e := newEnv(t)
	e.openModels()
	c := e.newConsumer("App")
	v1, err := e.svc.SetGrant(ctx, e.admin, c.ID, limits("m1"))
	if err != nil || v1.Version != 1 {
		t.Fatalf("first grant = %+v, %v", v1, err)
	}
	secret := e.issueKey(c.ID)
	id, err := e.au.Authenticate(ctx, "Bearer "+secret, "100.64.0.1:1")
	if err != nil || id.GrantVersionNo != 1 {
		t.Fatalf("authenticate = %+v, %v", id, err)
	}
	if _, err := e.au.Authenticate(ctx, "Bearer "+secret, "100.64.0.1:1"); err != nil { // warm
		t.Fatal(err)
	}
	clears := e.au.count()
	events := len(e.emitted)

	e.tick()
	v2, err := e.svc.SetGrant(ctx, e.admin, c.ID, limits("m1")) // identical terms: still a new version
	if err != nil || v2.Version != 2 || v2.ID == v1.ID || v2.GrantID != v1.GrantID {
		t.Fatalf("second grant = %+v, %v", v2, err)
	}
	if e.au.count() != clears+1 {
		t.Fatalf("Auth.Invalidate called %d times, want %d", e.au.count()-clears, 1)
	}
	if id, err = e.au.Authenticate(ctx, "Bearer "+secret, "100.64.0.1:1"); err != nil || id.GrantVersionNo != 2 {
		t.Fatalf("after the edit the key still sees version %d (%v)", id.GrantVersionNo, err)
	}
	cur, err := e.svc.GetGrant(ctx, c.ID)
	if err != nil || cur.Version != 2 || cur.ID != v2.ID {
		t.Fatalf("current = %+v, %v", cur, err)
	}
	if n := e.count(`SELECT count(*) FROM grt_version WHERE grant_id = ?`, v1.GrantID); n != 2 {
		t.Fatalf("%d versions stored, want 2", n)
	}
	if err := e.db.Write(ctx, func(tx *storage.Tx) error { _, err := tx.Exec(`UPDATE grt_version SET reason = 'x'`); return err }); err == nil {
		t.Fatal("a stored version could be edited")
	}

	// One grant.changed event and one audit row per version, naming the admin and the consumer's
	// principal, with ids and the version number only.
	if len(e.emitted)-events != 1 || string(e.emitted[len(e.emitted)-1].Type) != "grant.changed" {
		t.Fatalf("events = %+v", e.emitted[events:])
	}
	ev := e.emitted[len(e.emitted)-1]
	if ev.ActorPrincipalID != e.admin || ev.TargetPrincipalID != c.PrincipalID || !strings.Contains(string(ev.Payload), `"version":2`) {
		t.Fatalf("event = %+v payload %s", ev, ev.Payload)
	}
	if n := e.count(`SELECT count(*) FROM ntf_event WHERE type = 'grant.changed'`); n != 2 {
		t.Fatalf("%d grant.changed events stored, want 2", n)
	}
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = 'grant.changed' AND actor_id = ? AND target_id = ?`, e.admin, c.PrincipalID); n != 2 {
		t.Fatalf("%d grant.changed audit rows, want 2", n)
	}
	if txt := e.auditText(); strings.Contains(txt, "m1") {
		t.Fatalf("the audit detail names a model: %s", txt)
	}
}

// A refused grant writes nothing and clears nothing.
func TestRefusedGrantWritesNothing(t *testing.T) {
	e := newEnv(t)
	c := e.newConsumer("App")
	clears := e.au.count()
	_, err := e.svc.SetGrant(ctx, e.admin, c.ID, limits("no-such-model"))
	wantKind(t, err, admingrants.KindInvalid, "limits.models.0.model")
	if e.au.count() != clears || e.count(`SELECT count(*) FROM grt_grant`) != 0 || e.count(`SELECT count(*) FROM aud_event WHERE action = 'grant.changed'`) != 0 || len(e.emitted) != 0 {
		t.Fatal("a refused grant left a trace")
	}
}

func TestGrantNeedsALiveConsumer(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.SetGrant(ctx, e.admin, "0192e5a0-0001-7000-8000-000000000001", limits())
	wantKind(t, err, admingrants.KindNotFound, "")
	_, err = e.svc.GetGrant(ctx, "0192e5a0-0001-7000-8000-000000000001")
	wantKind(t, err, admingrants.KindNotFound, "")

	c := e.newConsumer("App")
	if _, err := e.svc.GetGrant(ctx, c.ID); err == nil {
		t.Fatal("a consumer without a grant has one")
	} else {
		wantKind(t, err, admingrants.KindNotFound, "")
	}
	page, err := e.svc.ListGrantVersions(ctx, c.ID, admingrants.ListQuery{})
	if err != nil || len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatalf("versions of a consumer without a grant = %+v, %v", page, err)
	}
	if err := e.cons.DeleteConsumer(ctx, e.admin, c.ID); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.SetGrant(ctx, e.admin, c.ID, limits())
	wantKind(t, err, admingrants.KindConflict, "")
}

// Windowed quota entries are stored and read back; the deprecated alias shows the entries whose
// window is the host's quota window (req_001 D-107).
func TestGrantWindowedQuotasRoundTrip(t *testing.T) {
	e := newEnv(t)
	c := e.newConsumer("App")
	in := limits()
	in.Quotas = []grants.QuotaEntry{
		{Metric: grants.MetricRequests, Limit: 50, Window: time.Hour},
		{Metric: grants.MetricTokens, Limit: 100000, Window: 24 * time.Hour},
		{Metric: grants.MetricTokens, Limit: 9000, Window: time.Hour},
	}
	v, err := e.svc.SetGrant(ctx, e.admin, c.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.GetGrant(ctx, c.ID)
	if err != nil || got.ID != v.ID {
		t.Fatalf("get = %+v, %v", got, err)
	}
	want := []grants.QuotaEntry{
		{Metric: grants.MetricTokens, Limit: 9000, Window: time.Hour},
		{Metric: grants.MetricTokens, Limit: 100000, Window: 24 * time.Hour},
		{Metric: grants.MetricRequests, Limit: 50, Window: time.Hour},
	}
	if len(got.Limits.Quotas) != 3 {
		t.Fatalf("quotas = %+v", got.Limits.Quotas)
	}
	for i, q := range want {
		if got.Limits.Quotas[i] != q {
			t.Fatalf("quota %d = %+v, want %+v", i, got.Limits.Quotas[i], q)
		}
	}
	// Only the token entry with the 24h default window is in the alias.
	if a := got.Limits.Quota; a == nil || a.Tokens == nil || *a.Tokens != 100000 || a.Requests != nil {
		t.Fatalf("alias = %+v", a)
	}
	// The legacy columns stay empty: the entries are in grt_version_quota.
	if n := e.count(`SELECT count(*) FROM grt_version WHERE quota_tokens_per_day IS NOT NULL OR quota_requests_per_day IS NOT NULL`); n != 0 {
		t.Fatal("a new version wrote the deprecated columns")
	}
}

func TestGrantQuotaWindowOutsideTheHostBoundsIsRefused(t *testing.T) {
	e := newEnv(t)
	c := e.newConsumer("App")
	for _, w := range []time.Duration{time.Second, 10000 * time.Hour} { // too short, too long
		in := limits()
		in.Quotas = []grants.QuotaEntry{{Metric: grants.MetricTokens, Limit: 1, Window: time.Hour}, {Metric: grants.MetricRequests, Limit: 1, Window: w}}
		_, err := e.svc.SetGrant(ctx, e.admin, c.ID, in)
		wantKind(t, err, admingrants.KindInvalid, "limits.quotas.1.window_seconds")
	}
	// A change of the bound applies to the next grant (req_001 D-38).
	e.set("limits.quota_window_min", "1s")
	in := limits()
	in.Quotas = []grants.QuotaEntry{{Metric: grants.MetricRequests, Limit: 1, Window: time.Second}}
	if _, err := e.svc.SetGrant(ctx, e.admin, c.ID, in); err != nil {
		t.Fatalf("after lowering the bound: %v", err)
	}
	// Storage-level problems name the entry and field.
	in.Quotas = []grants.QuotaEntry{{Metric: grants.MetricRequests, Limit: -1, Window: time.Hour}}
	_, err := e.svc.SetGrant(ctx, e.admin, c.ID, in)
	wantKind(t, err, admingrants.KindInvalid, "limits.quotas.0.limit")
	in.Quotas = []grants.QuotaEntry{{Metric: "bytes", Limit: 1, Window: time.Hour}}
	_, err = e.svc.SetGrant(ctx, e.admin, c.ID, in)
	wantKind(t, err, admingrants.KindInvalid, "limits.quotas.0.metric")
}

// The deprecated quota expands to entries with the host's quota window; when quotas is given too,
// quotas wins and the answer warns.
func TestGrantDeprecatedQuota(t *testing.T) {
	e := newEnv(t)
	c := e.newConsumer("App")
	in := limits()
	in.Quota = &consumers.Quota{Tokens: i64(1000), Requests: i64(10)}
	v, err := e.svc.SetGrant(ctx, e.admin, c.ID, in)
	if err != nil || len(v.Warnings) != 0 {
		t.Fatalf("grant = %+v, %v", v, err)
	}
	if len(v.Limits.Quotas) != 2 || v.Limits.Quotas[0].Window != 24*time.Hour || v.Limits.Quotas[0].Limit != 1000 {
		t.Fatalf("quotas = %+v", v.Limits.Quotas)
	}
	if q := v.Limits.Quota; q == nil || *q.Tokens != 1000 || *q.Requests != 10 {
		t.Fatalf("alias = %+v", q)
	}

	e.set("limits.quota_window", "12h") // the window is read when the grant is written
	in.Quotas = []grants.QuotaEntry{{Metric: grants.MetricTokens, Limit: 7, Window: time.Hour}}
	v, err = e.svc.SetGrant(ctx, e.admin, c.ID, in)
	if err != nil || len(v.Warnings) != 1 || v.Warnings[0] != consumers.QuotaWarning {
		t.Fatalf("grant = %+v, %v", v, err)
	}
	if len(v.Limits.Quotas) != 1 || v.Limits.Quotas[0].Limit != 7 || v.Limits.Quota != nil {
		t.Fatalf("quotas win: %+v alias %+v", v.Limits.Quotas, v.Limits.Quota)
	}
	in.Quotas, in.Quota = nil, &consumers.Quota{Tokens: i64(-1)}
	_, err = e.svc.SetGrant(ctx, e.admin, c.ID, in)
	wantKind(t, err, admingrants.KindInvalid, "limits.quota.tokens")
}

// A model must be known to the host, not retired and opened for sharing. A model the host has not
// stored yet is fetched from the engine once (req_001 D-25).
func TestGrantModelsAreChecked(t *testing.T) {
	e := newEnv(t)
	c := e.newConsumer("App")

	// m1 is in the engine; it is stored on first sight but not opened.
	_, err := e.svc.SetGrant(ctx, e.admin, c.ID, limits("m1"))
	wantKind(t, err, admingrants.KindInvalid, "limits.models.0.model")
	m, err := e.gr.Models.GetByName(ctx, e.db.Reader(), "m1")
	if err != nil || m.Allowed {
		t.Fatalf("m1 = %+v, %v", m, err)
	}
	// Unknown to the engine as well.
	_, err = e.svc.SetGrant(ctx, e.admin, c.ID, limits("m1", "ghost"))
	wantKind(t, err, admingrants.KindInvalid, "limits.models.0.model") // m1 is first and still closed

	allow := true
	if _, err := e.svc.UpdateModel(ctx, e.admin, m.ID, admingrants.ModelChange{Allowed: &allow}); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.SetGrant(ctx, e.admin, c.ID, limits("m1", "ghost"))
	wantKind(t, err, admingrants.KindInvalid, "limits.models.1.model")
	v, err := e.svc.SetGrant(ctx, e.admin, c.ID, limits("m1"))
	if err != nil || len(v.Limits.Models) != 1 || v.Limits.Models[0] != (admingrants.ModelLimit{Model: "m1", ContextLimit: 4096, MaxOutputTokens: 1024}) {
		t.Fatalf("grant = %+v, %v", v, err)
	}

	// Closing the model does not edit the version that holds it; only a new grant is refused.
	deny := false
	if _, err := e.svc.UpdateModel(ctx, e.admin, m.ID, admingrants.ModelChange{Allowed: &deny}); err != nil {
		t.Fatal(err)
	}
	cur, err := e.svc.GetGrant(ctx, c.ID)
	if err != nil || cur.ID != v.ID || len(cur.Limits.Models) != 1 {
		t.Fatalf("closing a model changed the grant: %+v, %v", cur, err)
	}
	_, err = e.svc.SetGrant(ctx, e.admin, c.ID, limits("m1"))
	wantKind(t, err, admingrants.KindInvalid, "limits.models.0.model")

	// A retired model is refused as well.
	if err := e.db.Write(ctx, func(tx *storage.Tx) error { return e.gr.Models.Retire(tx, m.ID) }); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.SetGrant(ctx, e.admin, c.ID, limits("m1"))
	wantKind(t, err, admingrants.KindInvalid, "limits.models.0.model")
	// The old version still names it.
	if cur, err = e.svc.GetGrant(ctx, c.ID); err != nil || cur.Limits.Models[0].Model != "m1" {
		t.Fatalf("grant = %+v, %v", cur, err)
	}
}

func TestGrantLimitsAreChecked(t *testing.T) {
	e := newEnv(t)
	e.openModels()
	c := e.newConsumer("App")
	cases := []struct {
		name  string
		edit  func(l *admingrants.Limits)
		param string
	}{
		{"concurrency", func(l *admingrants.Limits) { l.Concurrency = 0 }, "limits.concurrency"},
		{"overflow", func(l *admingrants.Limits) { l.OnContextOverflow = "shrug" }, "limits.on_context_overflow"},
		{"context limit", func(l *admingrants.Limits) { l.Models[0].ContextLimit = 0 }, "limits.models.0.context_limit"},
		{"max output", func(l *admingrants.Limits) { l.Models[0].MaxOutputTokens = 0 }, "limits.models.0.max_output_tokens"},
		{"duplicate model", func(l *admingrants.Limits) { l.Models = append(l.Models, l.Models[0]) }, "limits.models.1.model"},
		{"blank model", func(l *admingrants.Limits) { l.Models[0].Model = " " }, "limits.models.0.model"},
		{"past expiry", func(l *admingrants.Limits) { t := e.now.Add(-time.Minute); l.ExpiresAt = &t }, "limits.expires_at"},
		{"expiry now", func(l *admingrants.Limits) { t := e.now; l.ExpiresAt = &t }, "limits.expires_at"},
		{"blank property", func(l *admingrants.Limits) { l.AllowedProperties = []string{""} }, "limits.allowed_request_properties"},
	}
	for _, tc := range cases {
		l := limits("m1")
		tc.edit(&l)
		_, err := e.svc.SetGrant(ctx, e.admin, c.ID, l)
		if err == nil {
			t.Fatalf("%s: accepted", tc.name)
		}
		wantKind(t, err, admingrants.KindInvalid, tc.param)
	}
	l := limits("m1")
	exp := e.now.Add(time.Hour)
	l.OnContextOverflow, l.ExpiresAt = "truncate", &exp
	v, err := e.svc.SetGrant(ctx, e.admin, c.ID, l)
	if err != nil || v.Limits.OnContextOverflow != "truncate" || v.Limits.ExpiresAt == nil || !v.Limits.ExpiresAt.Equal(exp) {
		t.Fatalf("grant = %+v, %v", v, err)
	}
}

// Null allowed properties allow any request property; an empty list allows none.
func TestGrantAllowedPropertiesNullIsAnyEmptyIsNone(t *testing.T) {
	e := newEnv(t)
	c := e.newConsumer("App")
	for name, props := range map[string][]string{"any": nil, "none": {}, "some": {"background", "tag"}} {
		l := limits()
		l.AllowedProperties = props
		v, err := e.svc.SetGrant(ctx, e.admin, c.ID, l)
		if err != nil {
			t.Fatal(err)
		}
		got, err := e.svc.GetGrant(ctx, c.ID)
		if err != nil || got.ID != v.ID {
			t.Fatal(err)
		}
		if (props == nil) != (got.Limits.AllowedProperties == nil) || len(got.Limits.AllowedProperties) != len(props) {
			t.Fatalf("%s: stored %#v, read %#v", name, props, got.Limits.AllowedProperties)
		}
		var stored *string
		if err := e.db.Reader().QueryRow(ctx, `SELECT allowed_properties FROM grt_version WHERE id = ?`, v.ID).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		switch name {
		case "any":
			if stored != nil {
				t.Fatalf("any is stored as %q, want NULL", *stored)
			}
		case "none":
			if stored == nil || *stored != "[]" {
				t.Fatalf("none is stored as %v, want []", stored)
			}
		}
	}
}

func TestGrantVersionsListNewestFirstInPages(t *testing.T) {
	e := newEnv(t)
	c := e.newConsumer("App")
	for i := 0; i < 5; i++ {
		e.tick()
		if _, err := e.svc.SetGrant(ctx, e.admin, c.ID, limits()); err != nil {
			t.Fatal(err)
		}
	}
	two := 2
	var seen []int64
	cursor := ""
	for pages := 0; pages < 5; pages++ {
		p, err := e.svc.ListGrantVersions(ctx, c.ID, admingrants.ListQuery{Cursor: cursor, Limit: &two})
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range p.Items {
			seen = append(seen, v.Version)
			if v.ConsumerID != c.ID || v.ResourceRequestID != nil {
				t.Fatalf("version = %+v", v)
			}
		}
		if cursor = p.NextCursor; cursor == "" {
			break
		}
	}
	if len(seen) != 5 || seen[0] != 5 || seen[4] != 1 {
		t.Fatalf("versions seen = %v", seen)
	}
	zero := 0
	_, err := e.svc.ListGrantVersions(ctx, c.ID, admingrants.ListQuery{Limit: &zero})
	wantKind(t, err, admingrants.KindInvalid, "limit")
	for _, bad := range []string{"x", "v0", "v-1", "v01", "v", "5"} {
		_, err = e.svc.ListGrantVersions(ctx, c.ID, admingrants.ListQuery{Cursor: bad})
		wantKind(t, err, admingrants.KindInvalid, "cursor")
	}
	// The page size is a host setting, and caps the limit asked for.
	e.set("admin.list_page_size", "1")
	p, err := e.svc.ListGrantVersions(ctx, c.ID, admingrants.ListQuery{Limit: &two})
	if err != nil || len(p.Items) != 1 || p.NextCursor == "" {
		t.Fatalf("page = %+v, %v", p, err)
	}
}

func (e *env) issueKey(consumerID string) string {
	e.t.Helper()
	k, err := e.cons.IssueAccessKey(ctx, e.admin, consumerID, consumers.NewKey{})
	if err != nil {
		e.t.Fatal(err)
	}
	return k.Secret.Reveal()
}
