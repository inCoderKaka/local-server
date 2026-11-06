package grants_test

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// TestPolicyRuleVersions covers D-87: an edit of the document makes a new immutable version and
// moves the head, a switch makes none, and the versions are listed newest first.
func TestPolicyRuleVersions(t *testing.T) {
	db, ir, gr := open(t)
	ctx := context.Background()
	c := newConsumer(t, db, ir)
	actor := c.PrincipalID
	var rule grants.PolicyRule
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) {
		rule, err = gr.PolicyRules.CreateBy(tx, &actor, nil, "first", 3, 1, `{"a":1}`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if rule.VersionNo != 1 {
		t.Fatalf("version = %d, want 1", rule.VersionNo)
	}
	var no int64
	write := func(fn func(tx *storage.Tx) error) {
		t.Helper()
		if err := db.Write(ctx, fn); err != nil {
			t.Fatal(err)
		}
	}
	write(func(tx *storage.Tx) (err error) {
		no, err = gr.PolicyRules.UpdateBy(tx, &actor, rule.ID, "first", 3, 1, `{"a":2}`, true)
		return err
	})
	if no != 2 {
		t.Fatalf("edit gave version %d, want 2", no)
	}
	write(func(tx *storage.Tx) (err error) {
		no, err = gr.PolicyRules.UpdateBy(tx, nil, rule.ID, "first", 3, 1, `{"a":2}`, false) // switch only
		return err
	})
	if no != 2 {
		t.Fatalf("switch made version %d, want 2", no)
	}
	write(func(tx *storage.Tx) error { return gr.PolicyRules.SetEnabled(tx, rule.ID, true) })
	head, err := gr.PolicyRules.Get(ctx, db.Reader(), rule.ID)
	if err != nil || head.VersionNo != 2 || head.Body != `{"a":2}` || !head.Enabled {
		t.Fatalf("head = %+v, %v", head, err)
	}
	vs, err := gr.PolicyRules.Versions(ctx, db.Reader(), rule.ID)
	if err != nil || len(vs) != 2 || vs[0].VersionNo != 2 || vs[1].Body != `{"a":1}` || vs[0].CreatedBy == nil || *vs[0].CreatedBy != actor {
		t.Fatalf("versions = %+v, %v", vs, err)
	}
	// Versions are immutable.
	for _, q := range []string{`UPDATE grt_policy_rule_version SET name = 'x'`, `DELETE FROM grt_policy_rule_version`} {
		err := db.Write(ctx, func(tx *storage.Tx) error { _, err := tx.Exec(q); return err })
		if err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("%s: want immutable error, got %v", q, err)
		}
	}
}

// TestModelConfig covers the host's sharing columns of a model.
func TestModelConfig(t *testing.T) {
	db, _, gr := open(t)
	ctx := context.Background()
	var m grants.Model
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) {
		m, err = gr.Models.CreateWithAllowed(tx, "example-model", false)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	k := int64(300)
	if err := db.Write(ctx, func(tx *storage.Tx) error { return gr.Models.SetConfig(tx, m.ID, true, &k) }); err != nil {
		t.Fatal(err)
	}
	got, err := gr.Models.Get(ctx, db.Reader(), m.ID)
	if err != nil || !got.Allowed || got.KeepAliveSeconds == nil || *got.KeepAliveSeconds != 300 {
		t.Fatalf("model = %+v, %v", got, err)
	}
	neg := int64(-1)
	if err := db.Write(ctx, func(tx *storage.Tx) error { return gr.Models.SetConfig(tx, m.ID, true, &neg) }); err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("negative keep-alive: want CHECK error, got %v", err)
	}
}

// TestMigration3BackfillsPolicyRuleVersions applies migrations 1 and 2, stores a rule and a
// model, then opens the same database with all of them: the rule becomes version 1 of itself
// and the model is allowed.
func TestMigration3BackfillsPolicyRuleVersions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "t.db")
	full := grants.Module()
	old := fstest.MapFS{}
	for _, name := range []string{"0001_grants_tables.sql", "0002_version_quota.sql"} {
		b, err := fs.ReadFile(full.Migrations, name)
		if err != nil {
			t.Fatal(err)
		}
		old[name] = &fstest.MapFile{Data: b}
	}
	oldMod := full
	oldMod.Migrations = old

	db, err := storage.Open(ctx, storage.DefaultConfig(path, identity.Module(), oldMod))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *storage.Tx) error {
		if _, err := tx.Exec(`INSERT INTO grt_policy_rule (id, name, position, format_version, body, enabled, created_at, updated_at) VALUES ('r1', 'old', 4, 1, '{"x":1}', 1, 1000, 2000)`); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO grt_model (id, name, created_at) VALUES ('m1', 'example-model', 1000)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err = storage.Open(ctx, storage.DefaultConfig(path, identity.Module(), full))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	gr := grants.NewRepos(nil)
	r, err := gr.PolicyRules.Get(ctx, db.Reader(), "r1")
	if err != nil || r.VersionNo != 1 || r.DeletedAt != nil || r.VersionAt.UnixMilli() != 1000 {
		t.Fatalf("rule = %+v, %v", r, err)
	}
	vs, err := gr.PolicyRules.Versions(ctx, db.Reader(), "r1")
	if err != nil || len(vs) != 1 || vs[0].Name != "old" || vs[0].Position != 4 || vs[0].Body != `{"x":1}` || vs[0].CreatedBy != nil || vs[0].CreatedAt.UnixMilli() != 1000 {
		t.Fatalf("versions = %+v, %v", vs, err)
	}
	m, err := gr.Models.Get(ctx, db.Reader(), "m1")
	if err != nil || !m.Allowed || m.KeepAliveSeconds != nil {
		t.Fatalf("model = %+v, %v", m, err)
	}
}
