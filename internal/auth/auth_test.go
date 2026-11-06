package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func i64(v int64) *int64 { return &v }

type fakeWhoIs struct {
	node string
	err  error
}

func (f *fakeWhoIs) NodeOf(context.Context, string) (string, error) { return f.node, f.err }

type env struct {
	db    *storage.DB
	bus   *events.Bus
	st    *settings.Store
	svc   *auth.Service
	ir    *identity.Repos
	gr    *grants.Repos
	whois *fakeWhoIs
	now   time.Time
	cons  identity.Consumer
	grant grants.Grant
	model grants.Model
	// onNow, if set, runs once on the next clock call and is then cleared.
	onNow func()
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"),
		settings.Module(), identity.Module(), grants.Module(), audit.Module(), events.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{db: db, bus: events.New(), st: settings.New(db), ir: identity.NewRepos(nil), gr: grants.NewRepos(nil),
		whois: &fakeWhoIs{}, now: time.Now()}
	e.svc, err = auth.New(auth.Config{DB: db, Identity: e.ir, Grants: e.gr, Events: e.bus, Settings: e.st, WhoIs: e.whois,
		Clock: func() time.Time {
			if e.onNow != nil {
				f := e.onNow
				e.onNow = nil
				f()
			}
			return e.now
		}})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *storage.Tx) error {
		p, err := e.ir.Principals.Create(tx, "consumer", nil, nil)
		if err != nil {
			return err
		}
		if e.cons, err = e.ir.Consumers.Create(tx, p.ID, identity.ConsumerApplication, nil, nil); err != nil {
			return err
		}
		if e.grant, err = e.gr.Grants.Create(tx, e.cons.ID); err != nil {
			return err
		}
		if e.model, err = e.gr.Models.Create(tx, "m1"); err != nil {
			return err
		}
		_, err = e.gr.Versions.Create(tx, e.grant.ID, grants.NewVersion{Quotas: []grants.QuotaEntry{{Metric: grants.MetricTokens, Limit: 1000, Window: 24 * time.Hour}},
			Models: []grants.VersionModel{{ModelID: e.model.ID, ContextCap: i64(4096)}}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) issue(t *testing.T, p auth.IssueParams) auth.Issued {
	t.Helper()
	p.ConsumerID = e.cons.ID
	var is auth.Issued
	err := e.bus.Write(context.Background(), e.db, func(tx *storage.Tx) (err error) {
		is, err = e.svc.Issue(tx, p)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return is
}

func (e *env) revoke(t *testing.T, id string) {
	t.Helper()
	if err := e.bus.Write(context.Background(), e.db, func(tx *storage.Tx) error { return e.svc.Revoke(tx, id, "") }); err != nil {
		t.Fatal(err)
	}
}

func (e *env) check(key string) (*auth.Identity, error) {
	return e.svc.Authenticate(context.Background(), "Bearer "+key, "100.64.0.1:1")
}

func TestIssueAndAuthenticate(t *testing.T) {
	e := setup(t)
	is := e.issue(t, auth.IssueParams{})
	if !strings.HasPrefix(is.Key.Reveal(), "ls_"+is.AccessKey.Prefix+"_") {
		t.Fatalf("key %q does not start with its prefix", is.Key.Reveal())
	}
	id, err := e.check(is.Key.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	if id.ConsumerID != e.cons.ID || id.PrincipalID != e.cons.PrincipalID || id.GrantVersionNo != 1 || id.KeyID != is.AccessKey.ID {
		t.Fatalf("identity = %+v", id)
	}
	if len(id.Limits.Models) != 1 || len(id.Limits.Quotas) != 1 || id.Limits.Quotas[0].Limit != 1000 {
		t.Fatalf("limits = %+v", id.Limits)
	}
}

func TestSecretNeverStored(t *testing.T) {
	e := setup(t)
	is := e.issue(t, auth.IssueParams{ActorID: ""})
	e.revoke(t, is.AccessKey.ID)
	p, err := auth.ParseKey(is.Key.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`SELECT count(*) FROM idn_access_key WHERE instr(id||prefix||secret_hash||ifnull(label,'')||ifnull(narrowing_json,'')||ifnull(bound_node,''), ?) > 0`,
		`SELECT count(*) FROM aud_event WHERE instr(action||ifnull(detail,''), ?) > 0`,
		`SELECT count(*) FROM ntf_event WHERE instr(payload, ?) > 0`,
	} {
		var n int
		if err := e.db.Reader().QueryRow(context.Background(), q, p.Secret.Reveal()).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("secret found in storage by %q", q)
		}
	}
	var h string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT secret_hash FROM idn_access_key`).Scan(&h); err != nil {
		t.Fatal(err)
	}
	if h != auth.HashSecret(p.Secret.Reveal()) || h == p.Secret.Reveal() {
		t.Fatalf("stored hash = %q", h)
	}
	for _, s := range []string{is.String(), is.GoString()} {
		if strings.Contains(s, p.Secret.Reveal()) {
			t.Fatal("Issued prints the secret")
		}
	}
	if s := fmt.Sprintf("%+v", p); strings.Contains(s, p.Secret.Reveal()) {
		t.Fatal("Parsed prints the secret")
	}
	if b, err := json.Marshal(is); err != nil || strings.Contains(string(b), p.Secret.Reveal()) {
		t.Fatalf("Issued marshals the secret: %s %v", b, err)
	}
}

func TestRejects(t *testing.T) {
	e := setup(t)
	is := e.issue(t, auth.IssueParams{})
	p, _ := auth.ParseKey(is.Key.Reveal())
	wrong := auth.FormatKey(p.Prefix, strings.Repeat("0", 64))
	unknown := auth.FormatKey(strings.Repeat("0", 12), p.Secret.Reveal())
	for name, key := range map[string]string{"wrong secret": wrong, "unknown prefix": unknown, "malformed": "ls_x"} {
		if _, err := e.check(key); !errors.Is(err, auth.ErrInvalidKey) {
			t.Errorf("%s: err = %v, want ErrInvalidKey", name, err)
		}
	}
	if _, err := e.svc.Authenticate(context.Background(), is.Key.Reveal(), ""); !errors.Is(err, auth.ErrInvalidKey) {
		t.Errorf("no Bearer scheme: err = %v", err)
	}
}

func TestRevokedKeyRejectedAndCacheCleared(t *testing.T) {
	e := setup(t)
	is := e.issue(t, auth.IssueParams{})
	if _, err := e.check(is.Key.Reveal()); err != nil {
		t.Fatal(err)
	}
	// Revoking straight in the table, without an event, is invisible to the cache: this proves
	// the entry is served from memory.
	if err := e.db.Write(context.Background(), func(tx *storage.Tx) error { return e.ir.AccessKeys.Revoke(tx, is.AccessKey.ID) }); err != nil {
		t.Fatal(err)
	}
	if _, err := e.check(is.Key.Reveal()); err != nil {
		t.Fatalf("expected the cached entry to still answer, got %v", err)
	}
	// An access_key.revoked event clears it.
	if err := e.bus.Write(context.Background(), e.db, func(tx *storage.Tx) error {
		_, err := e.bus.Emit(tx, events.Event{Type: events.AccessKeyRevoked})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.check(is.Key.Reveal()); !errors.Is(err, auth.ErrRevoked) {
		t.Fatalf("err = %v, want ErrRevoked", err)
	}
}

func TestServiceRevokeAuditedAndRejected(t *testing.T) {
	e := setup(t)
	is := e.issue(t, auth.IssueParams{})
	if _, err := e.check(is.Key.Reveal()); err != nil {
		t.Fatal(err)
	}
	e.revoke(t, is.AccessKey.ID)
	if _, err := e.check(is.Key.Reveal()); !errors.Is(err, auth.ErrRevoked) {
		t.Fatalf("err = %v, want ErrRevoked", err)
	}
	var detail string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT detail FROM aud_event WHERE action = ?`, auth.ActionKeyRevoked).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(detail), &d); err != nil || d["consumer_id"] != e.cons.ID {
		t.Fatalf("revoke detail = %s, %v; want consumer_id %s", detail, err, e.cons.ID)
	}
	err := e.bus.Write(context.Background(), e.db, func(tx *storage.Tx) error { return e.svc.Revoke(tx, is.AccessKey.ID, "") })
	if !errors.Is(err, auth.ErrRevoked) {
		t.Fatalf("second revoke = %v, want ErrRevoked", err)
	}
	for action, want := range map[string]int{auth.ActionKeyIssued: 1, auth.ActionKeyRevoked: 1} {
		var n int
		if err := e.db.Reader().QueryRow(context.Background(), `SELECT count(*) FROM aud_event WHERE action = ?`, action).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Errorf("audit %s rows = %d, want %d", action, n, want)
		}
	}
}

func TestIssueRollsBackWithAudit(t *testing.T) {
	e := setup(t)
	boom := errors.New("boom")
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		if _, err := e.svc.Issue(tx, auth.IssueParams{ConsumerID: e.cons.ID}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	for _, q := range []string{`SELECT count(*) FROM idn_access_key`, `SELECT count(*) FROM aud_event`} {
		var n int
		if err := e.db.Reader().QueryRow(context.Background(), q).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s = %d, %v; want 0", q, n, err)
		}
	}
}

func TestExpiredKeyRejected(t *testing.T) {
	e := setup(t)
	exp := e.now.Add(time.Hour)
	is := e.issue(t, auth.IssueParams{ExpiresAt: &exp})
	if _, err := e.check(is.Key.Reveal()); err != nil {
		t.Fatal(err)
	}
	e.now = exp.Add(time.Second)
	if _, err := e.check(is.Key.Reveal()); !errors.Is(err, auth.ErrExpired) {
		t.Fatalf("err = %v, want ErrExpired", err)
	}
}

func TestGrantChangeEventClearsCache(t *testing.T) {
	e := setup(t)
	is := e.issue(t, auth.IssueParams{})
	if id, err := e.check(is.Key.Reveal()); err != nil || id.GrantVersionNo != 1 {
		t.Fatalf("%+v %v", id, err)
	}
	if err := e.bus.Write(context.Background(), e.db, func(tx *storage.Tx) error {
		if _, err := e.gr.Versions.Create(tx, e.grant.ID, grants.NewVersion{Quotas: []grants.QuotaEntry{{Metric: grants.MetricTokens, Limit: 5, Window: 24 * time.Hour}}}); err != nil {
			return err
		}
		_, err := e.bus.Emit(tx, events.Event{Type: events.GrantChanged})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	id, err := e.check(is.Key.Reveal())
	if err != nil || id.GrantVersionNo != 2 || id.Limits.Quotas[0].Limit != 5 {
		t.Fatalf("%+v %v", id, err)
	}
}

func TestCacheTTLSetting(t *testing.T) {
	e := setup(t)
	is := e.issue(t, auth.IssueParams{})
	if err := e.st.Set(context.Background(), settings.Host, auth.KeyCacheTTL, "", "0s"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.check(is.Key.Reveal()); err != nil {
		t.Fatal(err)
	}
	if err := e.db.Write(context.Background(), func(tx *storage.Tx) error { return e.ir.AccessKeys.Revoke(tx, is.AccessKey.ID) }); err != nil {
		t.Fatal(err)
	}
	if _, err := e.check(is.Key.Reveal()); !errors.Is(err, auth.ErrRevoked) {
		t.Fatalf("with the cache off a direct revoke must show at once, got %v", err)
	}
}

func TestNoGrantAndExpiredGrant(t *testing.T) {
	e := setup(t)
	is := e.issue(t, auth.IssueParams{})
	past := e.now.Add(time.Hour)
	if err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := e.gr.Versions.Create(tx, e.grant.ID, grants.NewVersion{ExpiresAt: &past})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e.now = past.Add(time.Minute)
	if _, err := e.check(is.Key.Reveal()); !errors.Is(err, auth.ErrGrantExpired) {
		t.Fatalf("err = %v, want ErrGrantExpired", err)
	}
}

func TestNarrowingOnlyNarrows(t *testing.T) {
	e := setup(t)
	// The key lists a model the grant does not have, a higher quota and a lower context cap.
	is := e.issue(t, auth.IssueParams{Narrowing: auth.Narrowing{ModelIDs: []string{e.model.ID, "other"},
		Quotas: []grants.QuotaEntry{{Metric: grants.MetricTokens, Limit: 5000, Window: 24 * time.Hour}}, ContextCap: i64(1024), MaxConcurrency: i64(2)}})
	id, err := e.check(is.Key.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	l := id.Limits
	if len(l.Quotas) != 1 || l.Quotas[0].Limit != 1000 {
		t.Errorf("quotas = %v, narrowing must not widen the grant's 1000", l.Quotas)
	}
	if len(l.Models) != 1 || l.Models[0].ModelID != e.model.ID || *l.Models[0].ContextCap != 1024 {
		t.Errorf("models = %+v", l.Models)
	}
	if l.MaxConcurrency == nil || *l.MaxConcurrency != 2 {
		t.Errorf("max concurrency = %v, want 2 (grant has none)", l.MaxConcurrency)
	}
	only := e.issue(t, auth.IssueParams{Narrowing: auth.Narrowing{ModelIDs: []string{"other"}}})
	if id, err = e.check(only.Key.Reveal()); err != nil || len(id.Limits.Models) != 0 {
		t.Errorf("a narrowing with no shared model must leave none: %+v %v", id, err)
	}
	if _, err := auth.ParseNarrowing(`{"quota_tokens_per_day":-1}`); !errors.Is(err, auth.ErrInvalidNarrowing) {
		t.Errorf("negative: %v", err)
	}
	if _, err := auth.ParseNarrowing(`{"bogus":1}`); !errors.Is(err, auth.ErrInvalidNarrowing) {
		t.Errorf("unknown field: %v", err)
	}
	for _, bad := range []string{`{"quota_tokens_per_day":1}}`, `{"quota_tokens_per_day":1}]`, `{"quota_tokens_per_day":1} {}`} {
		if _, err := auth.ParseNarrowing(bad); !errors.Is(err, auth.ErrInvalidNarrowing) {
			t.Errorf("%q: %v, want ErrInvalidNarrowing", bad, err)
		}
	}
	if _, err := auth.ParseNarrowing("{\"quota_tokens_per_day\":1}\n"); err != nil {
		t.Errorf("trailing newline: %v", err)
	}
}

func TestBadStoredNarrowingFailsClosed(t *testing.T) {
	e := setup(t)
	is := e.issue(t, auth.IssueParams{})
	if err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		bad := `{"quota_tokens_per_day":"x"}`
		return e.ir.AccessKeys.Update(tx, is.AccessKey.ID, nil, &bad, nil, nil)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.check(is.Key.Reveal()); err == nil {
		t.Fatal("a key whose narrowing cannot be read must not authenticate")
	}
}

func TestBoundNode(t *testing.T) {
	e := setup(t)
	is := e.issue(t, auth.IssueParams{BoundNode: "node-a"})
	e.whois.node = "node-a"
	if _, err := e.check(is.Key.Reveal()); err != nil {
		t.Fatal(err)
	}
	e.whois.node = "node-b"
	if _, err := e.check(is.Key.Reveal()); !errors.Is(err, auth.ErrNodeMismatch) {
		t.Fatalf("other node: %v", err)
	} else if errors.Is(err, auth.ErrNodeLookup) {
		t.Fatalf("other node is not a lookup failure: %v", err)
	}
	e.whois.node, e.whois.err = "node-a", errors.New("down")
	if _, err := e.check(is.Key.Reveal()); !errors.Is(err, auth.ErrNodeMismatch) {
		t.Fatalf("WhoIs failure must refuse: %v", err)
	} else if !errors.Is(err, auth.ErrNodeLookup) {
		t.Fatalf("WhoIs failure must satisfy ErrNodeLookup: %v", err)
	}
	empty := ""
	if err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		return e.ir.AccessKeys.Update(tx, is.AccessKey.ID, nil, nil, &empty, nil)
	}); err != nil {
		t.Fatal(err)
	}
	e.svc.Invalidate()
	e.whois.node, e.whois.err = "", nil
	if _, err := e.check(is.Key.Reveal()); !errors.Is(err, auth.ErrNodeMismatch) {
		t.Fatalf("empty binding must refuse: %v", err)
	}
}

func TestRevokeDuringLookupNotCached(t *testing.T) {
	e := setup(t)
	is := e.issue(t, auth.IssueParams{})
	// On a cache miss, the first clock call in Authenticate is loadedAt in lookup, right after
	// the DB read. The revoke and the cache clear land between the read and the store.
	e.onNow = func() {
		if err := e.db.Write(context.Background(), func(tx *storage.Tx) error { return e.ir.AccessKeys.Revoke(tx, is.AccessKey.ID) }); err != nil {
			t.Error(err)
		}
		e.svc.Invalidate()
	}
	_, _ = e.check(is.Key.Reveal()) // already in flight; the result does not matter
	if _, err := e.check(is.Key.Reveal()); !errors.Is(err, auth.ErrRevoked) {
		t.Fatalf("err = %v, want ErrRevoked", err)
	}
}

func TestSuspendedConsumerRejected(t *testing.T) {
	e := setup(t)
	is := e.issue(t, auth.IssueParams{})
	if _, err := e.check(is.Key.Reveal()); err != nil {
		t.Fatal(err)
	}
	set := func(status string) {
		t.Helper()
		if err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
			return e.ir.Principals.SetStatus(tx, e.cons.PrincipalID, status)
		}); err != nil {
			t.Fatal(err)
		}
		e.svc.Invalidate()
	}
	set(identity.StatusSuspended)
	if _, err := e.check(is.Key.Reveal()); !errors.Is(err, auth.ErrConsumerNotActive) {
		t.Fatalf("err = %v, want ErrConsumerNotActive", err)
	}
	set(identity.StatusActive)
	if _, err := e.check(is.Key.Reveal()); err != nil {
		t.Fatalf("after reactivation: %v", err)
	}
}

func TestIssueAuditHasNarrowing(t *testing.T) {
	e := setup(t)
	e.issue(t, auth.IssueParams{Narrowing: auth.Narrowing{QuotaTokensPerDay: i64(77)}})
	var detail string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT detail FROM aud_event WHERE action = ?`, auth.ActionKeyIssued).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	var d struct {
		Narrowing struct {
			Quota int64 `json:"quota_tokens_per_day"`
		} `json:"narrowing"`
	}
	if err := json.Unmarshal([]byte(detail), &d); err != nil || d.Narrowing.Quota != 77 {
		t.Fatalf("issue detail = %s, %v", detail, err)
	}
}

// TestQuotaEntriesMergePerMetricAndWindow covers the effective quota list (req_001 D-107): per
// (metric, window) the smaller limit of grant and key wins, and an entry only one side has is kept.
func TestQuotaEntriesMergePerMetricAndWindow(t *testing.T) {
	e := setup(t) // the grant has tokens 1000 per 24h
	k := e.issue(t, auth.IssueParams{Narrowing: auth.Narrowing{Quotas: []grants.QuotaEntry{
		{Metric: grants.MetricTokens, Limit: 300, Window: 24 * time.Hour}, // narrower than the grant's: wins
		{Metric: grants.MetricTokens, Limit: 50, Window: time.Hour},       // a window the grant has not: kept
		{Metric: grants.MetricRequests, Limit: 9, Window: 24 * time.Hour}, // another metric: kept
	}}})
	id, err := e.check(k.Key.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	want := map[grants.QuotaEntry]bool{
		{Metric: grants.MetricTokens, Limit: 300, Window: 24 * time.Hour}: true,
		{Metric: grants.MetricTokens, Limit: 50, Window: time.Hour}:       true,
		{Metric: grants.MetricRequests, Limit: 9, Window: 24 * time.Hour}: true,
	}
	if len(id.Limits.Quotas) != len(want) {
		t.Fatalf("quotas = %v, want %v", id.Limits.Quotas, want)
	}
	for _, q := range id.Limits.Quotas {
		if !want[q] {
			t.Errorf("unexpected effective entry %v", q)
		}
	}
	wide := e.issue(t, auth.IssueParams{Narrowing: auth.Narrowing{Quotas: []grants.QuotaEntry{{Metric: grants.MetricTokens, Limit: 5000, Window: 24 * time.Hour}}}})
	if id, err = e.check(wide.Key.Reveal()); err != nil || len(id.Limits.Quotas) != 1 || id.Limits.Quotas[0].Limit != 1000 {
		t.Errorf("a wider key entry must not widen the grant's: %+v %v", id.Limits.Quotas, err)
	}
}

func TestParseNarrowingQuotas(t *testing.T) {
	n, err := auth.ParseNarrowing(`{"quotas":[{"metric":"tokens","limit":5,"window_seconds":60},{"metric":"requests","limit":0,"window_seconds":3600}],"quota_tokens_per_day":9}`)
	if err != nil || len(n.Quotas) != 2 || n.Quotas[1].Window != time.Hour || n.Quotas[1].Limit != 0 || n.QuotaTokensPerDay == nil {
		t.Fatalf("ParseNarrowing = %+v, %v", n, err)
	}
	text, err := auth.Narrowing{Quotas: n.Quotas}.Text()
	if err != nil || text == nil || *text != `{"quotas":[{"metric":"tokens","limit":5,"window_seconds":60},{"metric":"requests","limit":0,"window_seconds":3600}]}` {
		t.Fatalf("Text = %v, %v", text, err)
	}
	for _, bad := range []string{
		`{"quotas":[{"metric":"bytes","limit":1,"window_seconds":60}]}`,
		`{"quotas":[{"metric":"tokens","limit":-1,"window_seconds":60}]}`,
		`{"quotas":[{"metric":"tokens","limit":1,"window_seconds":0}]}`,
		`{"quotas":[{"metric":"tokens","limit":1}]}`,
		`{"quotas":[{"metric":"tokens","limit":1,"window_seconds":60,"x":1}]}`,
		`{"quotas":[{"metric":"tokens","limit":1,"window_seconds":60},{"metric":"tokens","limit":2,"window_seconds":60}]}`,
	} {
		if _, err := auth.ParseNarrowing(bad); !errors.Is(err, auth.ErrInvalidNarrowing) {
			t.Errorf("ParseNarrowing(%s) = %v, want ErrInvalidNarrowing", bad, err)
		}
	}
}
