package grants_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func i64(v int64) *int64 { return &v }

// open registers identity before grants: grants' tables reference idn_* and storage applies
// modules in the order given.
func open(t *testing.T) (*storage.DB, *identity.Repos, *grants.Repos) {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"), identity.Module(), grants.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, identity.NewRepos(nil), grants.NewRepos(nil)
}

func newConsumer(t *testing.T, db *storage.DB, ir *identity.Repos) identity.Consumer {
	t.Helper()
	var c identity.Consumer
	if err := db.Write(context.Background(), func(tx *storage.Tx) error {
		p, err := ir.Principals.Create(tx, "consumer", nil, nil)
		if err != nil {
			return err
		}
		c, err = ir.Consumers.Create(tx, p.ID, identity.ConsumerApplication, nil, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewVersionLeavesOldUnchanged(t *testing.T) {
	db, ir, gr := open(t)
	ctx := context.Background()
	c := newConsumer(t, db, ir)

	var g grants.Grant
	var m grants.Model
	var v1, v2 grants.Version
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) {
		if g, err = gr.Grants.Create(tx, c.ID); err != nil {
			return err
		}
		if m, err = gr.Models.Create(tx, "llama3"); err != nil {
			return err
		}
		v1, err = gr.Versions.Create(tx, g.ID, grants.NewVersion{
			Quotas: []grants.QuotaEntry{{Metric: grants.MetricTokens, Limit: 1000, Window: 24 * time.Hour}}, Models: []grants.VersionModel{{ModelID: m.ID, ContextCap: i64(4096), MaxOutputTokens: i64(512)}},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if v1.OnContextOverflow != grants.OverflowRefuse || v1.VersionNo != 1 {
		t.Fatalf("v1 = %+v, want refuse by default and number 1", v1)
	}
	before, err := gr.Versions.Get(ctx, db.Reader(), v1.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Write(ctx, func(tx *storage.Tx) (err error) {
		v2, err = gr.Versions.Create(tx, g.ID, grants.NewVersion{
			Quotas: []grants.QuotaEntry{{Metric: grants.MetricTokens, Limit: 5000, Window: 24 * time.Hour}}, OnContextOverflow: grants.OverflowTruncate,
			Models: []grants.VersionModel{{ModelID: m.ID, ContextCap: i64(8192)}},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if v2.VersionNo != 2 || v2.OnContextOverflow != grants.OverflowTruncate {
		t.Fatalf("v2 = %+v", v2)
	}

	after, err := gr.Versions.Get(ctx, db.Reader(), v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.VersionNo != before.VersionNo || len(after.Quotas) != 1 || after.Quotas[0].Limit != 1000 || after.OnContextOverflow != grants.OverflowRefuse ||
		len(after.Models) != 1 || *after.Models[0].ContextCap != 4096 || *after.Models[0].MaxOutputTokens != 512 || !after.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("v1 changed: before %+v after %+v", before, after)
	}
	cur, err := gr.Versions.Current(ctx, db.Reader(), g.ID)
	if err != nil || cur.ID != v2.ID {
		t.Fatalf("current = %+v, %v; want v2", cur, err)
	}
	all, _ := gr.Versions.ListByGrant(ctx, db.Reader(), g.ID)
	if len(all) != 2 {
		t.Fatalf("versions = %d", len(all))
	}

	// The database itself refuses to change or delete a version.
	for _, q := range []string{
		`UPDATE grt_version SET max_concurrency = 9 WHERE id = ?`,
		`DELETE FROM grt_version WHERE id = ?`,
		`UPDATE grt_version_model SET context_cap = 1 WHERE version_id = ?`,
		`DELETE FROM grt_version_model WHERE version_id = ?`,
		`UPDATE grt_version_quota SET quota_limit = 1 WHERE version_id = ?`,
		`DELETE FROM grt_version_quota WHERE version_id = ?`,
	} {
		err := db.Write(ctx, func(tx *storage.Tx) error { _, err := tx.Exec(q, v1.ID); return err })
		if err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("%s: want immutable error, got %v", q, err)
		}
	}
}

func TestGrantRestrictAndRules(t *testing.T) {
	db, ir, gr := open(t)
	ctx := context.Background()
	c := newConsumer(t, db, ir)

	var g grants.Grant
	var rule grants.PolicyRule
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) {
		if g, err = gr.Grants.Create(tx, c.ID); err != nil {
			return err
		}
		if _, err = gr.Versions.Create(tx, g.ID, grants.NewVersion{}); err != nil {
			return err
		}
		if _, err = gr.PolicyRules.Create(tx, nil, "global", 0, 1, `{}`); err != nil {
			return err
		}
		rule, err = gr.PolicyRules.Create(tx, &g.ID, "mine", 1, 1, `{}`)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// A consumer with a grant cannot be hard-deleted, and a grant with versions cannot be.
	for _, q := range []string{`DELETE FROM idn_consumer WHERE id = ?`, `DELETE FROM grt_grant WHERE id = ?`} {
		id := c.ID
		if strings.Contains(q, "grt_grant") {
			id = g.ID
		}
		err := db.Write(ctx, func(tx *storage.Tx) error { _, err := tx.Exec(q, id); return err })
		if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
			t.Fatalf("%s: want FOREIGN KEY error, got %v", q, err)
		}
	}
	// A second grant for one consumer is refused.
	err := db.Write(ctx, func(tx *storage.Tx) error { _, err := gr.Grants.Create(tx, c.ID); return err })
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("second grant: want UNIQUE error, got %v", err)
	}
	// An invalid overflow value is refused.
	err = db.Write(ctx, func(tx *storage.Tx) error {
		_, err := gr.Versions.Create(tx, g.ID, grants.NewVersion{OnContextOverflow: "shrug"})
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("overflow shrug: want CHECK error, got %v", err)
	}

	rules, _ := gr.PolicyRules.List(ctx, db.Reader(), &g.ID)
	global, _ := gr.PolicyRules.List(ctx, db.Reader(), nil)
	if len(rules) != 2 || len(global) != 1 {
		t.Fatalf("rules = %d, global = %d", len(rules), len(global))
	}
	if err := db.Write(ctx, func(tx *storage.Tx) error {
		if err := gr.PolicyRules.Update(tx, rule.ID, "mine", 2, 1, `{"x":1}`, false); err != nil {
			return err
		}
		return gr.PolicyRules.Delete(tx, rule.ID)
	}); err != nil {
		t.Fatal(err)
	}
}

// TestVersionQuotas covers the windowed quota entries of a version (req_001 D-107): they are stored
// and read back in order, the legacy columns are written empty and read as the legacy alias, and
// storage refuses a bad entry.
func TestVersionQuotas(t *testing.T) {
	db, ir, gr := open(t)
	ctx := context.Background()
	c := newConsumer(t, db, ir)
	in := []grants.QuotaEntry{
		{Metric: grants.MetricRequests, Limit: 5, Window: time.Hour},
		{Metric: grants.MetricTokens, Limit: 900, Window: 24 * time.Hour},
		{Metric: grants.MetricTokens, Limit: 100, Window: time.Minute},
	}
	var g grants.Grant
	var v grants.Version
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) {
		if g, err = gr.Grants.Create(tx, c.ID); err != nil {
			return err
		}
		v, err = gr.Versions.Create(tx, g.ID, grants.NewVersion{Quotas: in})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	want := []grants.QuotaEntry{in[2], in[1], in[0]} // tokens first, shortest window first
	for name, got := range map[string]grants.Version{"created": v, "get": must(gr.Versions.Get(ctx, db.Reader(), v.ID)), "current": must(gr.Versions.Current(ctx, db.Reader(), g.ID))} {
		if len(got.Quotas) != 3 || got.Quotas[0] != want[0] || got.Quotas[1] != want[1] || got.Quotas[2] != want[2] {
			t.Errorf("%s quotas = %v, want %v", name, got.Quotas, want)
		}
		if got.LegacyQuotaTokensPerDay != nil || got.LegacyQuotaRequestsPerDay != nil {
			t.Errorf("%s: Create wrote a legacy column: %+v", name, got)
		}
	}
	if all, err := gr.Versions.ListByGrant(ctx, db.Reader(), g.ID); err != nil || len(all) != 1 || len(all[0].Quotas) != 3 {
		t.Errorf("ListByGrant = %+v, %v", all, err)
	}

	// A legacy row (made by an older build) is read back as the alias fields.
	var legacyID string
	if err := db.Write(ctx, func(tx *storage.Tx) error {
		legacyID = "00000000-0000-7000-8000-000000000001"
		_, err := tx.Exec(`INSERT INTO grt_version (id, grant_id, version_no, quota_tokens_per_day, quota_requests_per_day, created_at) VALUES (?, ?, 2, 777, 9, 1)`, legacyID, g.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if lv := must(gr.Versions.Get(ctx, db.Reader(), legacyID)); lv.LegacyQuotaTokensPerDay == nil || *lv.LegacyQuotaTokensPerDay != 777 ||
		*lv.LegacyQuotaRequestsPerDay != 9 || len(lv.Quotas) != 0 {
		t.Errorf("legacy version = %+v", lv)
	}

	// Storage refuses an entry it cannot hold.
	for name, q := range map[string]grants.QuotaEntry{
		"bad metric":               {Metric: "bytes", Limit: 1, Window: time.Hour},
		"negative limit":           {Metric: grants.MetricTokens, Limit: -1, Window: time.Hour},
		"window under 1s":          {Metric: grants.MetricTokens, Limit: 1, Window: 500 * time.Millisecond},
		"window not whole seconds": {Metric: grants.MetricTokens, Limit: 1, Window: 1500 * time.Millisecond},
		"duplicate":                in[0],
	} {
		list := []grants.QuotaEntry{q}
		if name == "duplicate" {
			list = []grants.QuotaEntry{in[0], in[0]}
		}
		err := db.Write(ctx, func(tx *storage.Tx) error {
			_, err := gr.Versions.Create(tx, g.ID, grants.NewVersion{Quotas: list})
			return err
		})
		if err == nil {
			t.Errorf("%s: storage accepted it", name)
		}
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestValidateQuotas(t *testing.T) {
	tok := func(limit int64, w time.Duration) grants.QuotaEntry {
		return grants.QuotaEntry{Metric: grants.MetricTokens, Limit: limit, Window: w}
	}
	cases := []struct {
		name  string
		in    []grants.QuotaEntry
		index int // -1: valid
		field string
	}{
		{"empty", nil, -1, ""},
		{"valid", []grants.QuotaEntry{tok(0, time.Second), {Metric: grants.MetricRequests, Limit: 5, Window: time.Hour}, tok(7, time.Hour)}, -1, ""},
		{"bad metric", []grants.QuotaEntry{tok(1, time.Hour), {Metric: "x", Limit: 1, Window: time.Hour}}, 1, "metric"},
		{"negative limit", []grants.QuotaEntry{tok(-1, time.Hour)}, 0, "limit"},
		{"zero window", []grants.QuotaEntry{tok(1, 0)}, 0, "window_seconds"},
		{"fractional window", []grants.QuotaEntry{tok(1, 1500*time.Millisecond)}, 0, "window_seconds"},
		{"duplicate", []grants.QuotaEntry{tok(1, time.Hour), tok(2, time.Minute), tok(3, time.Hour)}, 2, "window_seconds"},
	}
	for _, tc := range cases {
		p := grants.ValidateQuotas(tc.in)
		switch {
		case tc.index < 0 && p != nil:
			t.Errorf("%s: unexpected problem %v", tc.name, p)
		case tc.index >= 0 && (p == nil || p.Index != tc.index || p.Field != tc.field):
			t.Errorf("%s: problem = %v, want index %d field %s", tc.name, p, tc.index, tc.field)
		}
	}
}

func TestQuotaEntryJSON(t *testing.T) {
	b, err := json.Marshal([]grants.QuotaEntry{{Metric: grants.MetricRequests, Limit: 3, Window: 90 * time.Second}})
	if err != nil || string(b) != `[{"metric":"requests","limit":3,"window_seconds":90}]` {
		t.Fatalf("marshal = %s, %v", b, err)
	}
	var back []grants.QuotaEntry
	if err := json.Unmarshal(b, &back); err != nil || len(back) != 1 || back[0].Window != 90*time.Second || back[0].Limit != 3 {
		t.Fatalf("unmarshal = %+v, %v", back, err)
	}
	if err := json.Unmarshal([]byte(`[{"metric":"tokens","limit":1,"window_seconds":60,"extra":1}]`), &back); err == nil {
		t.Error("unknown field accepted")
	}
}

func TestWithLegacy(t *testing.T) {
	list := []grants.QuotaEntry{{Metric: grants.MetricTokens, Limit: 1, Window: time.Minute}}
	got := grants.WithLegacy(list, i64(50), i64(7), time.Hour)
	// The list wins for tokens; requests comes from the figure.
	if len(got) != 2 || got[0] != list[0] || got[1] != (grants.QuotaEntry{Metric: grants.MetricRequests, Limit: 7, Window: time.Hour}) {
		t.Fatalf("WithLegacy = %v", got)
	}
	if len(list) != 1 {
		t.Fatal("the input list was changed")
	}
}
