package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var fileRe = regexp.MustCompile(`^(\d+)_([a-z0-9_]+)\.sql$`)

type migration struct {
	module   string
	version  int
	name     string
	checksum string
	sql      string
}

func loadMigrations(m Module) ([]migration, error) {
	if m.Name == "" || m.Migrations == nil {
		return nil, errors.New("storage: a Module needs a Name and Migrations")
	}
	entries, err := fs.ReadDir(m.Migrations, ".")
	if err != nil {
		return nil, fmt.Errorf("storage: module %s: %w", m.Name, err)
	}
	var out []migration
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		g := fileRe.FindStringSubmatch(e.Name())
		if g == nil {
			return nil, fmt.Errorf("storage: module %s: migration file %q must be named NNNN_name.sql", m.Name, e.Name())
		}
		v, _ := strconv.Atoi(g[1])
		if prev, dup := seen[v]; dup {
			return nil, fmt.Errorf("storage: module %s: migrations %q and %q share version %d", m.Name, prev, e.Name(), v)
		}
		seen[v] = e.Name()
		b, err := fs.ReadFile(m.Migrations, e.Name())
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		out = append(out, migration{m.Name, v, g[2], hex.EncodeToString(sum[:]), string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func tableExists(ctx context.Context, c *sql.Conn, name string) (bool, error) {
	var n int
	err := c.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
	return n > 0, err
}

// migrate applies every unapplied migration of every module, storage first. Each runs in
// its own BEGIN IMMEDIATE transaction together with its bookkeeping rows.
func migrate(ctx context.Context, c *sql.Conn, mods []Module) error {
	names := map[string]bool{}
	for _, m := range mods {
		if names[m.Name] {
			return fmt.Errorf("storage: module %q registered twice", m.Name)
		}
		names[m.Name] = true
	}
	for _, m := range mods {
		migs, err := loadMigrations(m)
		if err != nil {
			return err
		}
		applied := map[int]string{} // version -> checksum
		maxApplied := 0
		if ok, err := tableExists(ctx, c, "ops_schema_migrations"); err != nil {
			return err
		} else if ok {
			rows, err := c.QueryContext(ctx, `SELECT version, checksum FROM ops_schema_migrations WHERE module=?`, m.Name)
			if err != nil {
				return err
			}
			for rows.Next() {
				var v int
				var sum string
				if err := rows.Scan(&v, &sum); err != nil {
					rows.Close()
					return err
				}
				applied[v] = sum
				maxApplied = max(maxApplied, v)
			}
			if err := errors.Join(rows.Err(), rows.Close()); err != nil {
				return err
			}
		}
		have := map[int]bool{}
		for _, mg := range migs {
			have[mg.version] = true
			if sum, ok := applied[mg.version]; ok {
				if sum != mg.checksum {
					return fmt.Errorf("storage: module %s migration %04d_%s changed after it was applied (checksum mismatch); migrations are forward-only, add a new one", m.Name, mg.version, mg.name)
				}
				continue
			}
			if mg.version < maxApplied {
				return fmt.Errorf("storage: module %s migration %04d_%s is older than applied version %d; versions must increase", m.Name, mg.version, mg.name, maxApplied)
			}
			if err := apply(ctx, c, mg); err != nil {
				return err
			}
		}
		for v := range applied {
			if !have[v] {
				return fmt.Errorf("storage: module %s migration version %d is applied but its file is missing", m.Name, v)
			}
		}
	}
	return nil
}

func apply(ctx context.Context, c *sql.Conn, mg migration) (err error) {
	start := time.Now()
	if _, err := c.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer func() {
		if err == nil {
			return
		}
		c.ExecContext(ctx, "ROLLBACK")
		err = fmt.Errorf("storage: module %s migration %04d_%s: %w", mg.module, mg.version, mg.name, err)
		// Log the failure when the log table exists (it does from storage migration 1 on).
		if ok, qerr := tableExists(ctx, c, "ops_migration_run"); qerr == nil && ok {
			c.ExecContext(ctx, `INSERT INTO ops_migration_run (module, version, name, started_at, finished_at, outcome, error) VALUES (?, ?, ?, ?, ?, 'failed', ?)`,
				mg.module, mg.version, mg.name, start.UnixMilli(), time.Now().UnixMilli(), err.Error())
		}
	}()
	if _, err = c.ExecContext(ctx, mg.sql); err != nil {
		return err
	}
	end := time.Now()
	if _, err = c.ExecContext(ctx, `INSERT INTO ops_schema_migrations (module, version, name, checksum, applied_at, duration_ms) VALUES (?, ?, ?, ?, ?, ?)`,
		mg.module, mg.version, mg.name, mg.checksum, end.UnixMilli(), end.Sub(start).Milliseconds()); err != nil {
		return err
	}
	if _, err = c.ExecContext(ctx, `INSERT INTO ops_migration_run (module, version, name, started_at, finished_at, outcome) VALUES (?, ?, ?, ?, ?, 'applied')`,
		mg.module, mg.version, mg.name, start.UnixMilli(), end.UnixMilli()); err != nil {
		return err
	}
	_, err = c.ExecContext(ctx, "COMMIT")
	return err
}

// ---- data catalog ----

var classes = map[string]bool{"public": true, "internal": true, "personal": true, "sensitive": true, "content": true}

type catalogEntry struct {
	table, column, module, class, comment string
	billable                              bool
}

var directiveRe = regexp.MustCompile(`^--@(table|column)\s+(\S+)\s+(.*?)\s+\|\s+(.+)$`)

// parseDirectives reads the "--@table" and "--@column" lines of one migration file.
func parseDirectives(module, file, src string) ([]catalogEntry, error) {
	var out []catalogEntry
	for i, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "--@") {
			continue
		}
		g := directiveRe.FindStringSubmatch(line)
		if g == nil {
			return nil, fmt.Errorf("storage: %s %s:%d: malformed catalog line, want \"--@table|column name classification=X billable=Y | comment\"", module, file, i+1)
		}
		e := catalogEntry{module: module, comment: g[4]}
		if g[1] == "table" {
			e.table = g[2]
		} else {
			t, c, ok := strings.Cut(g[2], ".")
			if !ok || t == "" || c == "" {
				return nil, fmt.Errorf("storage: %s %s:%d: column name must be table.column", module, file, i+1)
			}
			e.table, e.column = t, c
		}
		for _, kv := range strings.Fields(g[3]) {
			k, v, _ := strings.Cut(kv, "=")
			switch k {
			case "classification":
				e.class = v
			case "billable":
				b, err := strconv.ParseBool(v)
				if err != nil {
					return nil, fmt.Errorf("storage: %s %s:%d: billable must be true or false", module, file, i+1)
				}
				e.billable = b
			default:
				return nil, fmt.Errorf("storage: %s %s:%d: unknown key %q", module, file, i+1, k)
			}
		}
		if !classes[e.class] {
			return nil, fmt.Errorf("storage: %s %s:%d: classification %q is not public, internal, personal, sensitive or content", module, file, i+1, e.class)
		}
		if !strings.Contains(g[3], "billable=") {
			return nil, fmt.Errorf("storage: %s %s:%d: billable is required", module, file, i+1)
		}
		out = append(out, e)
	}
	return out, nil
}

// syncCatalog rebuilds ops_data_catalog from the migrations and checks it covers every
// table and column in the database and respects each module's prefixes.
func syncCatalog(ctx context.Context, c *sql.Conn, mods []Module) error {
	entries := map[[2]string]catalogEntry{}
	prefixes := map[string][]string{}
	for _, m := range mods {
		prefixes[m.Name] = m.Prefixes
		migs, err := loadMigrations(m)
		if err != nil {
			return err
		}
		for _, mg := range migs { // version order: a later line overrides an earlier one
			es, err := parseDirectives(m.Name, fmt.Sprintf("%04d_%s.sql", mg.version, mg.name), mg.sql)
			if err != nil {
				return err
			}
			for _, e := range es {
				entries[[2]string{e.table, e.column}] = e
			}
		}
	}
	rows, err := c.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, n)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	var final []catalogEntry
	for _, t := range tables {
		te, ok := entries[[2]string{t, ""}]
		if !ok {
			return fmt.Errorf("storage: table %s has no --@table catalog line in any migration", t)
		}
		okPrefix := false
		for _, p := range prefixes[te.module] {
			okPrefix = okPrefix || strings.HasPrefix(t, p)
		}
		if !okPrefix {
			return fmt.Errorf("storage: table %s does not start with a prefix of module %s %v (D-44)", t, te.module, prefixes[te.module])
		}
		final = append(final, te)
		cols, err := columnNames(ctx, c, t)
		if err != nil {
			return err
		}
		for _, col := range cols {
			ce, ok := entries[[2]string{t, col}]
			if !ok {
				return fmt.Errorf("storage: column %s.%s has no --@column catalog line", t, col)
			}
			ce.module = te.module
			final = append(final, ce)
		}
	}
	if _, err := c.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	err = func() error {
		if _, err := c.ExecContext(ctx, "DELETE FROM ops_data_catalog"); err != nil {
			return err
		}
		for _, e := range final {
			b := 0
			if e.billable {
				b = 1
			}
			if _, err := c.ExecContext(ctx, `INSERT INTO ops_data_catalog (table_name, column_name, module, classification, billable, comment) VALUES (?, ?, ?, ?, ?, ?)`,
				e.table, e.column, e.module, e.class, b, e.comment); err != nil {
				return err
			}
		}
		return nil
	}()
	if err != nil {
		c.ExecContext(ctx, "ROLLBACK")
		return err
	}
	_, err = c.ExecContext(ctx, "COMMIT")
	return err
}

func columnNames(ctx context.Context, c *sql.Conn, table string) ([]string, error) {
	rows, err := c.QueryContext(ctx, `SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, n)
	}
	return out, errors.Join(rows.Err(), rows.Close())
}
