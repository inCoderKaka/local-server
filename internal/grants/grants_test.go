package grants_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

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
			QuotaTokensPerDay: i64(1000), Models: []grants.VersionModel{{ModelID: m.ID, ContextCap: i64(4096), MaxOutputTokens: i64(512)}},
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
			QuotaTokensPerDay: i64(5000), OnContextOverflow: grants.OverflowTruncate,
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
	if after.VersionNo != before.VersionNo || *after.QuotaTokensPerDay != 1000 || after.OnContextOverflow != grants.OverflowRefuse ||
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
