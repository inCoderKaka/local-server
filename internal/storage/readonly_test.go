package storage

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenReadOnlyReadsExistingDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "a.db")
	w := open(t, path, demo(map[string]string{"0001_a.sql": demoSQL}))
	if err := w.Write(ctx, func(tx *Tx) error {
		_, err := tx.Exec(`INSERT INTO tst_item (id, name) VALUES (7, 'x')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	w.Close() // as after a stopped server

	ro, err := OpenReadOnly(ctx, path, demo(map[string]string{"0001_a.sql": demoSQL}))
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if n := count(t, ro, `SELECT count(*) FROM tst_item WHERE id = 7`); n != 1 {
		t.Errorf("read %d rows, want 1", n)
	}
	// The server running at the same time is fine too.
	w2 := open(t, path, demo(map[string]string{"0001_a.sql": demoSQL}))
	if err := w2.Write(ctx, func(tx *Tx) error {
		_, err := tx.Exec(`INSERT INTO tst_item (id, name) VALUES (8, 'y')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, ro, `SELECT count(*) FROM tst_item`); n != 2 {
		t.Errorf("read %d rows after a concurrent write, want 2", n)
	}
}

func TestOpenReadOnlyCannotWrite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "a.db")
	m := demo(map[string]string{"0001_a.sql": demoSQL})
	open(t, path, m).Close()
	ro, err := OpenReadOnly(ctx, path, m)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if err := ro.Write(ctx, func(*Tx) error { t.Error("unit of work ran"); return nil }); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Write = %v; want ErrReadOnly", err)
	}
	// Even a statement sent straight down the read pool cannot change the file.
	rows, err := ro.Reader().Query(ctx, `INSERT INTO tst_item (id, name) VALUES (1, 'x') RETURNING id`)
	if err == nil {
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
	}
	if err == nil {
		t.Error("an INSERT through the read pool succeeded")
	}
	if n := count(t, ro, `SELECT count(*) FROM tst_item`); n != 0 {
		t.Errorf("table has %d rows, want 0", n)
	}
}

func TestOpenReadOnlyDoesNotCreateMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.db")
	_, err := OpenReadOnly(context.Background(), path, demo(map[string]string{"0001_a.sql": demoSQL}))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v; want fs.ErrNotExist", err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("OpenReadOnly created files: %v", ents)
	}
}

func TestOpenReadOnlyRefusesDifferentSchemaVersion(t *testing.T) {
	ctx := context.Background()
	two := map[string]string{"0001_a.sql": demoSQL, "0002_b.sql": "ALTER TABLE tst_item ADD COLUMN extra TEXT;\n--@column tst_item.extra classification=internal billable=false | Extra.\n"}
	one := map[string]string{"0001_a.sql": demoSQL}
	cases := []struct {
		name      string
		created   []Module
		build     []Module
		wantParts []string
	}{
		{"database older than build", []Module{demo(one)}, []Module{demo(two)}, []string{"module demo: database v1, this build v2"}},
		{"database newer than build", []Module{demo(two)}, []Module{demo(one)}, []string{"module demo: database v2, this build v1"}},
		{"build has a module the database lacks", nil, []Module{demo(one)}, []string{"module demo: database v0, this build v1"}},
		{"database has a module the build lacks", []Module{demo(one)}, nil, []string{"module demo: database v1, this build v0", "not in this build"}},
		{"changed file", []Module{demo(one)}, []Module{demo(map[string]string{"0001_a.sql": demoSQL + "-- edited\n"})}, []string{"version 1 changed after it was applied"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "a.db")
			open(t, path, tc.created...).Close()
			_, err := OpenReadOnly(ctx, path, tc.build...)
			if !errors.Is(err, ErrSchemaMismatch) {
				t.Fatalf("err = %v; want ErrSchemaMismatch", err)
			}
			var sm *SchemaMismatchError
			if !errors.As(err, &sm) || len(sm.Diffs) == 0 {
				t.Fatalf("err = %v; want a *SchemaMismatchError with diffs", err)
			}
			for _, want := range tc.wantParts {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks %q", err, want)
				}
			}
		})
	}
}

func TestOpenReadOnlyRefusesAFileThatIsNotOurs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.db")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := OpenReadOnly(context.Background(), path)
	if !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("err = %v; want ErrSchemaMismatch for an empty database", err)
	}
}
