package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
)

// ErrReadOnly is returned by Write on a database opened with OpenReadOnly.
var ErrReadOnly = errors.New("storage: database is open read-only")

// ErrSchemaMismatch matches (errors.Is) the *SchemaMismatchError of OpenReadOnly.
var ErrSchemaMismatch = errors.New("storage: database schema differs from the modules of this build")

// SchemaDiff is one module whose recorded migrations differ from the embedded ones.
type SchemaDiff struct {
	Module string
	// Recorded and Embedded are the highest migration version the database records and this
	// build embeds for the module; 0 means none.
	Recorded, Embedded int
	// Detail says what else differs when the highest versions are equal (a changed checksum, a
	// missing version).
	Detail string
}

// SchemaMismatchError says a database was not migrated by the modules passed to OpenReadOnly. A
// read-only open never migrates, so it refuses instead of reading a schema it does not know.
type SchemaMismatchError struct {
	Path  string
	Diffs []SchemaDiff
}

func (e *SchemaMismatchError) Error() string {
	parts := make([]string, len(e.Diffs))
	for i, d := range e.Diffs {
		parts[i] = fmt.Sprintf("module %s: database v%d, this build v%d", d.Module, d.Recorded, d.Embedded)
		if d.Detail != "" {
			parts[i] += " (" + d.Detail + ")"
		}
	}
	return fmt.Sprintf("storage: %s: schema differs from this build: %s; start local-server to migrate it, or use the build that created it", e.Path, strings.Join(parts, "; "))
}

// Is makes errors.Is(err, ErrSchemaMismatch) true.
func (e *SchemaMismatchError) Is(target error) bool { return target == ErrSchemaMismatch }

// OpenReadOnly opens an existing database file for reading only: no migration, no catalog
// sync, no writer goroutine, and the file is never created (a missing file is an error that
// matches fs.ErrNotExist). Reader works as on a database from Open; Write returns ErrReadOnly.
// It is for tools that must not change the database, such as doctor, which can run while the
// server is stopped or running.
//
// modules are the same modules Open would apply, without the storage module (always included).
// The recorded migrations must equal the embedded ones, version and checksum, in both
// directions; otherwise OpenReadOnly returns a *SchemaMismatchError naming both versions per
// module, because a build must not read tables it does not know. Call Close when done.
func OpenReadOnly(ctx context.Context, path string, modules ...Module) (*DB, error) {
	if path == "" || path == ":memory:" {
		return nil, errors.New("storage: OpenReadOnly needs a database file path")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("storage: open %s read-only: %w", path, err)
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("storage: open %s read-only: is a directory", path)
	}
	mods, err := withOwn(modules)
	if err != nil {
		return nil, err
	}
	busy := DefaultBusyTimeout
	q := url.Values{}
	q.Add("mode", "ro")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout("+strconv.FormatInt(busy.Milliseconds(), 10)+")")
	q.Add("_pragma", "query_only(1)")
	r, err := sql.Open("sqlite", "file:"+uriPathEscaper.Replace(path)+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	r.SetMaxOpenConns(DefaultReadPoolSize)
	fail := func(err error) (*DB, error) {
		r.Close()
		return nil, err
	}
	if err := checkSchema(ctx, r, path, mods); err != nil {
		return fail(err)
	}
	return &DB{cfg: Config{Path: path, BusyTimeout: busy}, read: r, mods: mods, readOnly: true,
		quit: make(chan struct{}), exited: make(chan struct{})}, nil
}

// checkSchema compares ops_schema_migrations with the embedded migrations of mods.
func checkSchema(ctx context.Context, r *sql.DB, path string, mods []Module) error {
	type rec struct {
		sums     map[int]string
		maxVer   int
		hasTable bool
	}
	recorded := map[string]*rec{}
	var hasTable int
	if err := r.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='ops_schema_migrations'`).Scan(&hasTable); err != nil {
		return fmt.Errorf("storage: read %s: %w", path, err)
	}
	if hasTable > 0 {
		rows, err := r.QueryContext(ctx, `SELECT module, version, checksum FROM ops_schema_migrations`)
		if err != nil {
			return fmt.Errorf("storage: read %s: %w", path, err)
		}
		for rows.Next() {
			var m, sum string
			var v int
			if err := rows.Scan(&m, &v, &sum); err != nil {
				rows.Close()
				return err
			}
			if recorded[m] == nil {
				recorded[m] = &rec{sums: map[int]string{}}
			}
			recorded[m].sums[v] = sum
			recorded[m].maxVer = max(recorded[m].maxVer, v)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
	}
	var diffs []SchemaDiff
	known := map[string]bool{}
	for _, m := range mods {
		known[m.Name] = true
		migs, err := loadMigrations(m)
		if err != nil {
			return err
		}
		d := SchemaDiff{Module: m.Name}
		have := map[int]string{}
		for _, mg := range migs {
			d.Embedded = max(d.Embedded, mg.version)
			have[mg.version] = mg.checksum
		}
		var detail []string
		if rc := recorded[m.Name]; rc != nil {
			d.Recorded = rc.maxVer
			for v, sum := range have {
				if got, ok := rc.sums[v]; !ok && v < rc.maxVer {
					detail = append(detail, fmt.Sprintf("version %d is not recorded", v))
				} else if ok && got != sum {
					detail = append(detail, fmt.Sprintf("version %d changed after it was applied", v))
				}
			}
			for v := range rc.sums {
				if _, ok := have[v]; !ok && v <= d.Embedded {
					detail = append(detail, fmt.Sprintf("version %d is recorded but not in this build", v))
				}
			}
		}
		sort.Strings(detail)
		d.Detail = strings.Join(detail, ", ")
		if d.Recorded != d.Embedded || d.Detail != "" {
			diffs = append(diffs, d)
		}
	}
	var extra []string
	for m := range recorded {
		if !known[m] {
			extra = append(extra, m)
		}
	}
	sort.Strings(extra)
	for _, m := range extra {
		diffs = append(diffs, SchemaDiff{Module: m, Recorded: recorded[m].maxVer, Detail: "module is not in this build"})
	}
	if len(diffs) > 0 {
		return &SchemaMismatchError{Path: path, Diffs: diffs}
	}
	return nil
}
