package storage

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

var update = flag.Bool("update", false, "regenerate docs/schema-reference.md")

const demoSQL = `CREATE TABLE tst_item (id INTEGER PRIMARY KEY, name TEXT NOT NULL) STRICT;
--@table tst_item classification=personal billable=true | A test item.
--@column tst_item.id classification=internal billable=false | Id.
--@column tst_item.name classification=personal billable=true | Name.
CREATE TABLE tst_part (id INTEGER PRIMARY KEY, item_id INTEGER NOT NULL REFERENCES tst_item(id)) STRICT;
--@table tst_part classification=internal billable=false | A part of an item.
--@column tst_part.id classification=internal billable=false | Id.
--@column tst_part.item_id classification=internal billable=false | Item.
`

func demo(files map[string]string) Module {
	m := fstest.MapFS{}
	for n, s := range files {
		m[n] = &fstest.MapFile{Data: []byte(s)}
	}
	return Module{Name: "demo", Prefixes: []string{"tst_"}, Migrations: m}
}

func open(t *testing.T, path string, mods ...Module) *DB {
	t.Helper()
	db, err := Open(context.Background(), DefaultConfig(path, mods...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func count(t *testing.T, db *DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Reader().QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMigrationChecksums(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.db")
	db := open(t, path, demo(map[string]string{"0001_init.sql": demoSQL}))
	if n := count(t, db, `SELECT count(*) FROM ops_schema_migrations`); n != 4 {
		t.Fatalf("applied rows = %d, want 4 (storage x3, demo x1)", n)
	}
	var sum string
	if err := db.Reader().QueryRow(context.Background(), `SELECT checksum FROM ops_schema_migrations WHERE module='demo'`).Scan(&sum); err != nil || len(sum) != 64 {
		t.Fatalf("checksum %q err %v", sum, err)
	}
	db.Close()

	// Reopen unchanged: nothing is re-applied.
	db = open(t, path, demo(map[string]string{"0001_init.sql": demoSQL}))
	if n := count(t, db, `SELECT count(*) FROM ops_migration_run`); n != 4 {
		t.Fatalf("run rows = %d, want 4", n)
	}
	// A new migration is applied.
	db.Close()
	db = open(t, path, demo(map[string]string{"0001_init.sql": demoSQL,
		"0002_more.sql": "CREATE TABLE tst_more (id INTEGER PRIMARY KEY) STRICT;\n--@table tst_more classification=public billable=false | More.\n--@column tst_more.id classification=public billable=false | Id.\n"}))
	if n := count(t, db, `SELECT count(*) FROM ops_data_catalog WHERE table_name='tst_more'`); n != 2 {
		t.Fatalf("catalog rows for tst_more = %d, want 2", n)
	}
	db.Close()

	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"changed file": {map[string]string{"0001_init.sql": demoSQL + "-- edited\n", "0002_more.sql": "SELECT 1;"}, "checksum mismatch"},
		"missing file": {map[string]string{"0001_init.sql": demoSQL}, "file is missing"},
		"bad name":     {map[string]string{"init.sql": demoSQL}, "NNNN_name.sql"},
		"duplicate":    {map[string]string{"0001_a.sql": "", "0001_b.sql": ""}, "share version"},
	}
	for name, c := range cases {
		_, err := Open(context.Background(), DefaultConfig(path, demo(c.files)))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want containing %q", name, err, c.want)
		}
	}
}

func TestOutOfOrderMigrationRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "o.db")
	open(t, path, demo(map[string]string{"0002_init.sql": demoSQL})).Close()
	_, err := Open(context.Background(), DefaultConfig(path, demo(map[string]string{"0001_old.sql": "SELECT 1;", "0002_init.sql": demoSQL})))
	if err == nil || !strings.Contains(err.Error(), "older than applied") {
		t.Fatalf("err = %v", err)
	}
}

func TestFailedMigrationRollsBackAndIsLogged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.db")
	_, err := Open(context.Background(), DefaultConfig(path, demo(map[string]string{"0001_bad.sql": "CREATE TABLE tst_x (id INTEGER) STRICT; SELECT * FROM nope;"})))
	if err == nil {
		t.Fatal("want error")
	}
	db := open(t, path)
	if n := count(t, db, `SELECT count(*) FROM sqlite_master WHERE name='tst_x'`); n != 0 {
		t.Fatal("failed migration left its table behind")
	}
	if n := count(t, db, `SELECT count(*) FROM ops_migration_run WHERE outcome='failed' AND module='demo'`); n != 1 {
		t.Fatalf("failed run rows = %d, want 1", n)
	}
}

func TestCatalogRowPerTableAndColumn(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "c.db"), demo(map[string]string{"0001_init.sql": demoSQL}))
	ctx := context.Background()
	rows, err := db.Reader().Query(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()
	if len(tables) < 6 {
		t.Fatalf("tables = %v", tables)
	}
	for _, tb := range tables {
		if count(t, db, `SELECT count(*) FROM ops_data_catalog WHERE table_name=? AND column_name=''`, tb) != 1 {
			t.Errorf("no catalog table row for %s", tb)
		}
		if count(t, db, `SELECT count(*) FROM ops_data_catalog WHERE table_name=? AND column_name<>''`, tb) !=
			count(t, db, `SELECT count(*) FROM pragma_table_info(?)`, tb) {
			t.Errorf("catalog column rows differ from columns for %s", tb)
		}
	}
	if count(t, db, `SELECT count(*) FROM ops_data_catalog WHERE table_name='tst_item' AND column_name='name' AND classification='personal' AND billable=1`) != 1 {
		t.Error("tst_item.name metadata wrong")
	}
}

func TestCatalogRules(t *testing.T) {
	cases := map[string]struct{ sql, want string }{
		"missing table line":  {"CREATE TABLE tst_a (id INTEGER) STRICT;", "no --@table"},
		"missing column line": {"CREATE TABLE tst_a (id INTEGER, b TEXT) STRICT;\n--@table tst_a classification=public billable=false | A.\n--@column tst_a.id classification=public billable=false | I.\n", "tst_a.b has no"},
		"wrong prefix":        {"CREATE TABLE zzz_a (id INTEGER) STRICT;\n--@table zzz_a classification=public billable=false | A.\n--@column zzz_a.id classification=public billable=false | I.\n", "does not start with a prefix"},
		"bad class":           {"--@table tst_a classification=secret billable=false | A.", "classification"},
		"no billable":         {"--@table tst_a classification=public | A.", "billable is required"},
	}
	for name, c := range cases {
		_, err := Open(context.Background(), DefaultConfig(filepath.Join(t.TempDir(), "x.db"), demo(map[string]string{"0001_x.sql": c.sql})))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want containing %q", name, err, c.want)
		}
	}
}

func TestWritesSerialised(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "w.db"), demo(map[string]string{"0001_init.sql": demoSQL}))
	ctx := context.Background()
	var inFlight, maxInFlight atomic.Int32
	var wg sync.WaitGroup
	const n = 100
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := db.Write(ctx, func(tx *Tx) error {
				cur := inFlight.Add(1)
				defer inFlight.Add(-1)
				for {
					m := maxInFlight.Load()
					if cur <= m || maxInFlight.CompareAndSwap(m, cur) {
						break
					}
				}
				var c int
				if err := tx.QueryRow(`SELECT count(*) FROM tst_item`).Scan(&c); err != nil {
					return err
				}
				time.Sleep(50 * time.Microsecond)
				_, err := tx.Exec(`INSERT INTO tst_item (id, name) VALUES (?, 'x')`, c+1) // read-modify-write: a clash would fail
				return err
			})
			if err != nil {
				t.Errorf("write %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if maxInFlight.Load() != 1 {
		t.Fatalf("max concurrent units = %d, want 1", maxInFlight.Load())
	}
	if got := count(t, db, `SELECT count(*) FROM tst_item`); got != n {
		t.Fatalf("rows = %d, want %d", got, n)
	}
}

func TestJoinedRowsRollBackWithCaller(t *testing.T) {
	// A wide window puts the failing and the succeeding unit in the same batch.
	cfg := DefaultConfig(filepath.Join(t.TempDir(), "r.db"), demo(map[string]string{"0001_init.sql": demoSQL}))
	cfg.BatchWindow = 100 * time.Millisecond
	db, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	boom := errors.New("boom")
	var wg sync.WaitGroup
	var failErr, panicErr, okErr error
	wg.Add(3)
	go func() {
		defer wg.Done()
		failErr = db.Write(ctx, func(tx *Tx) error {
			tx.Exec(`INSERT INTO tst_item (id, name) VALUES (1, 'a')`)
			tx.Exec(`INSERT INTO tst_part (id, item_id) VALUES (1, 1)`)
			if err := tx.Capture(Change{Dataset: "d", Table: "tst_item", PK: "1", Version: 1, Op: "insert"}); err != nil {
				return err
			}
			return boom
		})
	}()
	go func() {
		defer wg.Done()
		panicErr = db.Write(ctx, func(tx *Tx) error {
			tx.Exec(`INSERT INTO tst_item (id, name) VALUES (3, 'c')`)
			panic("oops")
		})
	}()
	go func() {
		defer wg.Done()
		okErr = db.Write(ctx, func(tx *Tx) error {
			if _, err := tx.Exec(`INSERT INTO tst_item (id, name) VALUES (2, 'b')`); err != nil {
				return err
			}
			return tx.Capture(Change{Dataset: "d", Table: "tst_item", PK: "2", Version: 1, Op: "insert", Payload: []byte(`{}`)})
		})
	}()
	wg.Wait()
	if !errors.Is(failErr, boom) || okErr != nil || panicErr == nil {
		t.Fatalf("errs: fail=%v panic=%v ok=%v", failErr, panicErr, okErr)
	}
	if n := count(t, db, `SELECT count(*) FROM tst_item`); n != 1 {
		t.Fatalf("items = %d, want 1 (only the successful unit)", n)
	}
	if n := count(t, db, `SELECT count(*) FROM tst_part`); n != 0 {
		t.Fatal("part row of the failed unit survived")
	}
	if n := count(t, db, `SELECT count(*) FROM syn_change`); n != 1 {
		t.Fatalf("syn_change rows = %d, want 1", n)
	}
	if n := count(t, db, `SELECT count(*) FROM syn_change WHERE pk='2'`); n != 1 {
		t.Fatal("wrong syn_change row survived")
	}
}

func TestPragmasAndReadPool(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "p.db"), demo(map[string]string{"0001_init.sql": demoSQL}))
	ctx := context.Background()
	if n := count(t, db, `PRAGMA foreign_keys`); n != 1 {
		t.Fatal("foreign_keys off on reader")
	}
	var mode string
	if err := db.Reader().QueryRow(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode %q %v", mode, err)
	}
	if n := count(t, db, `PRAGMA busy_timeout`); n != int(DefaultBusyTimeout.Milliseconds()) {
		t.Fatalf("busy_timeout = %d", n)
	}
	err := db.Write(ctx, func(tx *Tx) error { // foreign keys enforced on the writer
		_, err := tx.Exec(`INSERT INTO tst_part (id, item_id) VALUES (1, 99)`)
		return err
	})
	if err == nil {
		t.Fatal("foreign key not enforced")
	}
	rows, err := db.Reader().Query(ctx, `INSERT INTO tst_item (id, name) VALUES (1, 'x') RETURNING id`)
	if err == nil {
		err = rows.Err()
		rows.Close()
	}
	if err == nil {
		t.Fatal("read pool accepted a write")
	}
}

func TestClose(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "k.db"))
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(context.Background(), func(*Tx) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestTxNotUsableAfterUnit(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "e.db"))
	var kept *Tx
	db.Write(context.Background(), func(tx *Tx) error { kept = tx; return nil })
	if _, err := kept.Exec(`SELECT 1`); err == nil {
		t.Fatal("Tx usable after its unit of work")
	}
	if err := kept.QueryRow(`SELECT 1`).Scan(new(int)); err == nil {
		t.Fatal("Tx.QueryRow usable after its unit of work")
	}
	if _, err := kept.Query(`SELECT 1`); err == nil {
		t.Fatal("Tx.Query usable after its unit of work")
	}
}

// TestBatchAbortedWhenTransactionLost: a unit that ends the batch transaction must not let
// later units commit on their own; every unit's result must match what is on disk.
func TestBatchAbortedWhenTransactionLost(t *testing.T) {
	cfg := DefaultConfig(filepath.Join(t.TempDir(), "b.db"), demo(map[string]string{"0001_init.sql": demoSQL}))
	cfg.BatchWindow = 300 * time.Millisecond
	db, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	insert := func(id int) func(*Tx) error {
		return func(tx *Tx) error {
			_, err := tx.Exec(`INSERT INTO tst_item (id, name) VALUES (?, 'x')`, id)
			return err
		}
	}
	units := map[string]func(*Tx) error{
		"a": insert(1),
		"b": func(tx *Tx) error { _, err := tx.Exec("ROLLBACK"); return err },
		"c": insert(3),
	}
	ids := map[string]int{"a": 1, "c": 3}
	errs := map[string]error{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, name := range []string{"a", "b", "c"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := db.Write(ctx, units[name])
			mu.Lock()
			errs[name] = err
			mu.Unlock()
		}()
		time.Sleep(20 * time.Millisecond)
	}
	wg.Wait()
	if errs["b"] == nil {
		t.Fatal("unit that ended the transaction returned nil")
	}
	for name, id := range ids {
		exists := count(t, db, `SELECT count(*) FROM tst_item WHERE id=?`, id) == 1
		if (errs[name] == nil) != exists {
			t.Errorf("unit %s: err = %v but row exists = %v", name, errs[name], exists)
		}
	}
}

func TestNewDatabaseIsUserOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not POSIX on windows")
	}
	path := filepath.Join(t.TempDir(), "m.db")
	db := open(t, path)
	if err := db.Write(context.Background(), func(tx *Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{path, path + "-wal"} {
		st, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", f, st.Mode().Perm())
		}
	}
}

func TestPathWithURICharacters(t *testing.T) {
	name := "a%b#c"
	if runtime.GOOS != "windows" {
		name += "?d"
	}
	dir := filepath.Join(t.TempDir(), name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "s.db")
	db := open(t, path)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database not at the requested path: %v", err)
	}
	var fk int
	err := db.Write(context.Background(), func(tx *Tx) error {
		return tx.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	})
	if err != nil || fk != 1 {
		t.Fatalf("writer foreign_keys = %d, %v", fk, err)
	}
	if n := count(t, db, `PRAGMA query_only`); n != 1 {
		t.Fatalf("reader query_only = %d", n)
	}
}

// TestApplySkipsMigrationAppliedMeanwhile: apply re-checks under the lock, so a migration
// another process applied after migrate read the table is not run again.
func TestApplySkipsMigrationAppliedMeanwhile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.db")
	db := open(t, path)
	ctx := context.Background()
	other, err := sql.Open("sqlite", dsn(path, time.Second, false, SyncFull))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	conn, err := other.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	sub, err := fs.Sub(ownMigrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	migs, err := loadMigrations(Module{Name: "storage", Prefixes: []string{"ops_", "syn_"}, Migrations: sub})
	if err != nil {
		t.Fatal(err)
	}
	before := count(t, db, `SELECT count(*) FROM ops_migration_run`)
	if err := apply(ctx, conn, migs[0]); err != nil {
		t.Fatal(err)
	}
	if after := count(t, db, `SELECT count(*) FROM ops_migration_run`); after != before {
		t.Fatalf("ops_migration_run rows %d -> %d", before, after)
	}
	if n := count(t, db, `SELECT count(*) FROM ops_migration_run WHERE outcome='failed'`); n != 0 {
		t.Fatalf("%d failed rows", n)
	}
}

// TestSchemaDoc regenerates the schema reference and compares it with the committed file.
// Run `go generate ./internal/storage` (or go test -run TestSchemaDoc -update) to rewrite it.
// It covers the storage module's own tables; modules add theirs when they register in cmd/.
func TestSchemaDoc(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "d.db"))
	got, err := db.SchemaDoc(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join("..", "..", "docs", "schema-reference.md")
	if *update {
		if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%v (run go generate ./internal/storage)", err)
	}
	if string(want) != got {
		t.Fatal("docs/schema-reference.md is out of date; run go generate ./internal/storage")
	}
}

func pragmaSync(t *testing.T, db *DB) int {
	t.Helper()
	// The write connection is the one the DSN's synchronous pragma applies to.
	var n int
	err := db.Write(context.Background(), func(tx *Tx) error { return tx.QueryRow(`PRAGMA synchronous`).Scan(&n) })
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// PLT-B-010 (req_001 D-76): FULL by default, NORMAL when configured, other values refused.
func TestSynchronousDefaultsToFull(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "a.db"))
	if n := pragmaSync(t, db); n != 2 {
		t.Fatalf("PRAGMA synchronous = %d; want 2 (FULL)", n)
	}
}

func TestSynchronousNormalHonoured(t *testing.T) {
	cfg := DefaultConfig(filepath.Join(t.TempDir(), "a.db"))
	cfg.Synchronous = "normal"
	db, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if n := pragmaSync(t, db); n != 1 {
		t.Fatalf("PRAGMA synchronous = %d; want 1 (NORMAL)", n)
	}
}

func TestSynchronousInvalidRefused(t *testing.T) {
	cfg := DefaultConfig(filepath.Join(t.TempDir(), "a.db"))
	cfg.Synchronous = "OFF"
	if db, err := Open(context.Background(), cfg); err == nil {
		db.Close()
		t.Fatal("Open accepted synchronous=OFF")
	}
}
