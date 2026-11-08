package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// This file is the storage side of the off-host snapshot (req_001 D-28, D-41, D-64, D-69): a
// consistent copy of the database, the foreign-key graph the minimize rule reads, and the
// delete-and-vacuum of a copy. The snapshot adapter (internal/offhost/snapshot) cannot import a
// database driver, so it reaches SQLite only through these.

var tableNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// VacuumInto writes a consistent, compacted copy of the database to path with VACUUM INTO
// (req_001 D-28). The file must not exist. The copy is a snapshot of one moment: it does not
// block the writer. The caller owns the file (VACUUM INTO creates it with the process's default
// mode, so put it in a user-only directory and tighten it).
func (db *DB) VacuumInto(ctx context.Context, path string) error {
	if path == "" {
		return errors.New("storage: VacuumInto needs a path")
	}
	// A separate connection: it never queues behind the writer. (A query_only or mode=ro
	// connection refuses VACUUM INTO, which writes the new file; the source is only read.)
	c, err := sql.Open("sqlite", "file:"+uriPathEscaper.Replace(db.cfg.Path)+"?_pragma=busy_timeout("+strconv.FormatInt(db.cfg.BusyTimeout.Milliseconds(), 10)+")")
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetMaxOpenConns(1)
	if _, err := c.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("storage: vacuum into: %w", err)
	}
	return nil
}

// ForeignKeyReferrers returns, for every table, the tables whose foreign keys point at it
// directly, each list sorted. A table that references itself is in its own list. It reads the
// live schema (pragma_foreign_key_list), so it follows migrations (req_001 D-69).
func (db *DB) ForeignKeyReferrers(ctx context.Context) (map[string][]string, error) {
	rows, err := db.read.QueryContext(ctx, `SELECT DISTINCT m.name, f."table"
		FROM sqlite_master m JOIN pragma_foreign_key_list(m.name) f
		WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var child, parent string
		if err := rows.Scan(&child, &parent); err != nil {
			return nil, err
		}
		out[parent] = append(out[parent], child)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out, nil
}

// MinimizeFile deletes every row of tables in the SQLite file at path and vacuums it, so no free
// page keeps a deleted row (req_001 D-64). It opens the file on its own connection, with
// foreign_keys=ON and the rollback journal (no -wal file is left behind), never through a
// storage.DB: the file is a copy, not the host's database. The deletes run in one transaction
// that checks foreign keys at commit, so the order of tables does not matter, and a violation
// (a kept table still referencing a minimized one, req_001 D-69) fails the whole minimize and
// leaves the rows. A name that is not a table of the file is an error.
func MinimizeFile(ctx context.Context, path string, tables []string) (err error) {
	if len(tables) == 0 {
		return nil
	}
	if fi, serr := os.Stat(path); serr != nil || fi.IsDir() {
		return fmt.Errorf("storage: minimize: %s is not a database file", path)
	}
	c, err := sql.Open("sqlite", "file:"+uriPathEscaper.Replace(path)+"?_pragma=foreign_keys(1)&_pragma=journal_mode(DELETE)&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, c.Close()) }()
	c.SetMaxOpenConns(1)
	conn, err := c.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var fk int
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		return errors.New("storage: minimize: cannot turn foreign keys on for the copy")
	}
	have := map[string]bool{}
	rows, err := conn.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		have[n] = true
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, t := range tables {
		if !tableNameRE.MatchString(t) || !have[t] {
			return fmt.Errorf("storage: minimize: %q is not a table of the copy", t)
		}
	}
	order, err := childrenFirst(ctx, conn, tables)
	if err != nil {
		return err
	}
	tables = order
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
		}
	}()
	if _, err := conn.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}
	for _, t := range tables {
		if _, err := conn.ExecContext(ctx, `DELETE FROM "`+strings.ReplaceAll(t, `"`, `""`)+`"`); err != nil {
			return fmt.Errorf("storage: minimize %s: %w", t, err)
		}
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("storage: minimize: commit: %w", err)
	}
	committed = true
	if _, err := conn.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("storage: minimize: vacuum: %w", err)
	}
	return nil
}

// childrenFirst orders tables so a table that references another of them comes before it: a
// RESTRICT foreign key is checked at once, deferred or not, so a parent's rows can only go after
// its children's. Tables in a reference cycle keep their given order (deferral covers them).
func childrenFirst(ctx context.Context, conn *sql.Conn, tables []string) ([]string, error) {
	in := map[string]bool{}
	for _, t := range tables {
		in[t] = true
	}
	referrers := map[string]map[string]bool{} // parent -> children within the set
	for _, t := range tables {
		rows, err := conn.QueryContext(ctx, `SELECT DISTINCT "table" FROM pragma_foreign_key_list(?)`, t)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var parent string
			if err := rows.Scan(&parent); err != nil {
				rows.Close()
				return nil, err
			}
			if in[parent] && parent != t {
				if referrers[parent] == nil {
					referrers[parent] = map[string]bool{}
				}
				referrers[parent][t] = true
			}
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, err
		}
	}
	var out []string
	done := map[string]bool{}
	for len(out) < len(tables) {
		progressed := false
		for _, t := range tables {
			if done[t] {
				continue
			}
			ready := true
			for child := range referrers[t] {
				if !done[child] {
					ready = false
					break
				}
			}
			if ready {
				done[t] = true
				out = append(out, t)
				progressed = true
			}
		}
		if !progressed { // a cycle: the rest in the given order
			for _, t := range tables {
				if !done[t] {
					done[t] = true
					out = append(out, t)
				}
			}
		}
	}
	return out, nil
}

// IntegrityCheck runs SQLite's integrity_check and foreign_key_check and returns an error
// naming the first problem, never a row's content (the foreign-key check names tables only).
func (db *DB) IntegrityCheck(ctx context.Context) error {
	var res string
	if err := db.read.QueryRowContext(ctx, `PRAGMA integrity_check(1)`).Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("storage: integrity check failed: %s", res)
	}
	rows, err := db.read.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var table string
		var rowid sql.NullInt64
		var parent string
		var fkid int
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return err
		}
		return fmt.Errorf("storage: foreign key check failed: a row of %s references a missing row of %s", table, parent)
	}
	return rows.Err()
}
