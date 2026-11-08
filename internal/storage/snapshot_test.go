package storage

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// snapSQL has a RESTRICT foreign key (checked at once even when deferral is on), so the delete
// order matters: the child first.
const snapSQL = demoSQL + `
CREATE TABLE tst_leaf (id INTEGER PRIMARY KEY, part_id INTEGER NOT NULL REFERENCES tst_part(id) ON DELETE RESTRICT) STRICT;
--@table tst_leaf classification=internal billable=false | A leaf of a part.
--@column tst_leaf.id classification=internal billable=false | Id.
--@column tst_leaf.part_id classification=internal billable=false | Part.
`

func seedSnap(t *testing.T, db *DB) {
	t.Helper()
	err := db.Write(context.Background(), func(tx *Tx) error {
		for _, q := range []string{
			`INSERT INTO tst_item VALUES (1, 'secret-name-bytes')`,
			`INSERT INTO tst_part VALUES (1, 1)`,
			`INSERT INTO tst_leaf VALUES (1, 1)`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestVacuumIntoAndMinimizeFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := open(t, filepath.Join(dir, "a.db"), demo(map[string]string{"0001_init.sql": snapSQL}))
	seedSnap(t, db)

	copyPath := filepath.Join(dir, "copy.db")
	if err := db.VacuumInto(ctx, copyPath); err != nil {
		t.Fatal(err)
	}
	if err := db.VacuumInto(ctx, copyPath); err == nil {
		t.Fatal("VacuumInto over an existing file succeeded")
	}
	// The whole chain, given parent-first: the RESTRICT key only allows child-first.
	if err := MinimizeFile(ctx, copyPath, []string{"tst_item", "tst_part", "tst_leaf"}); err != nil {
		t.Fatalf("minimize chain: %v", err)
	}
	if b, _ := os.ReadFile(copyPath); bytes.Contains(b, []byte("secret-name-bytes")) {
		t.Fatal("a deleted row survives in a free page of the copy")
	}
	c, err := sql.Open("sqlite", "file:"+copyPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var n int
	if err := c.QueryRow(`SELECT (SELECT count(*) FROM tst_item) + (SELECT count(*) FROM tst_part) + (SELECT count(*) FROM tst_leaf)`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows left %d, err %v", n, err)
	}
	// The live database is untouched.
	if got := count(t, db, `SELECT count(*) FROM tst_item`); got != 1 {
		t.Fatalf("live rows = %d", got)
	}
	for _, leftover := range []string{copyPath + "-wal", copyPath + "-shm", copyPath + "-journal"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Fatalf("%s left behind", leftover)
		}
	}
}

func TestMinimizeFileRefusesABrokenKeepAndLeavesRows(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db := open(t, filepath.Join(dir, "a.db"), demo(map[string]string{"0001_init.sql": snapSQL}))
	seedSnap(t, db)
	copyPath := filepath.Join(dir, "copy.db")
	if err := db.VacuumInto(ctx, copyPath); err != nil {
		t.Fatal(err)
	}
	// tst_item without its referrers: the kept tst_part would dangle.
	if err := MinimizeFile(ctx, copyPath, []string{"tst_item"}); err == nil {
		t.Fatal("minimize of a referenced table with kept referrers succeeded")
	}
	c, err := sql.Open("sqlite", "file:"+copyPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var n int
	if err := c.QueryRow(`SELECT count(*) FROM tst_item`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("item rows = %d, err %v (the failed minimize must leave rows)", n, err)
	}
	if err := MinimizeFile(ctx, copyPath, []string{"no_such_table"}); err == nil || !strings.Contains(err.Error(), "not a table") {
		t.Fatalf("unknown table: %v", err)
	}
	if err := MinimizeFile(ctx, copyPath, []string{`x"; DROP TABLE tst_item; --`}); err == nil {
		t.Fatal("a table name with SQL in it was accepted")
	}
}

func TestForeignKeyReferrersAndIntegrity(t *testing.T) {
	ctx := context.Background()
	db := open(t, filepath.Join(t.TempDir(), "a.db"), demo(map[string]string{"0001_init.sql": snapSQL}))
	refs, err := db.ForeignKeyReferrers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(refs["tst_item"], ","); got != "tst_part" {
		t.Fatalf("referrers of tst_item = %q", got)
	}
	if got := strings.Join(refs["tst_part"], ","); got != "tst_leaf" {
		t.Fatalf("referrers of tst_part = %q", got)
	}
	if err := db.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
}
