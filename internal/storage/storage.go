package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"strconv"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver; imported only here (D-28, archcheck)
)

// Defaults for the Config fields left at zero. They are starting values only: the runtime
// settings module (PLT-F-004) is not built yet, so the caller owns them (D-38, D-54).
const (
	DefaultBusyTimeout  = 5 * time.Second
	DefaultBatchMax     = 64
	DefaultBatchWindow  = 2 * time.Millisecond
	DefaultReadPoolSize = 4
)

//go:embed migrations/*.sql
var ownMigrations embed.FS

// ErrClosed is returned by Write after Close.
var ErrClosed = errors.New("storage: closed")

// Module registers one module's embedded migrations.
type Module struct {
	// Name is the module name from tools/archcheck, for example "ledger".
	Name string
	// Prefixes are the D-44 table prefixes the module owns, for example "ldg_". Every
	// table the module's migrations create must start with one of them.
	Prefixes []string
	// Migrations holds NNNN_name.sql files at its root. Pass fs.Sub(embedded, "migrations").
	Migrations fs.FS
}

// Config configures Open.
type Config struct {
	// Path is the database file. Required; ":memory:" is not supported (the pool needs a file).
	Path string
	// Modules are the other modules' migrations. The storage module's own are always applied first.
	Modules []Module
	// BusyTimeout is how long a connection waits for a lock. Zero means DefaultBusyTimeout.
	BusyTimeout time.Duration
	// BatchMax is the most units of work committed in one transaction. Zero means DefaultBatchMax.
	BatchMax int
	// BatchWindow is how long the writer waits for more units to join a batch after the
	// first arrives. Zero means do not wait (take only what is already queued). DefaultConfig sets DefaultBatchWindow.
	BatchWindow time.Duration
	// ReadPoolSize is the number of read connections. Zero means DefaultReadPoolSize.
	ReadPoolSize int
}

// DefaultConfig returns a Config for path with every default filled in.
func DefaultConfig(path string, modules ...Module) Config {
	return Config{Path: path, Modules: modules, BusyTimeout: DefaultBusyTimeout, BatchMax: DefaultBatchMax,
		BatchWindow: DefaultBatchWindow, ReadPoolSize: DefaultReadPoolSize}
}

// DB is the open database: one writer, a read pool.
type DB struct {
	cfg    Config
	write  *sql.DB
	conn   *sql.Conn // the only write connection, owned by the writer goroutine
	read   *sql.DB
	mods   []Module
	jobs   chan *job
	quit   chan struct{}
	exited chan struct{}
	once   sync.Once
}

func dsn(path string, busy time.Duration, readOnly bool) string {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout("+strconv.FormatInt(busy.Milliseconds(), 10)+")")
	if readOnly {
		q.Add("_pragma", "query_only(1)")
	} else {
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "synchronous(NORMAL)")
	}
	return "file:" + path + "?" + q.Encode()
}

// Open opens the database, applies the migrations, rebuilds the data catalog and starts
// the writer goroutine. Call Close when done.
func Open(ctx context.Context, cfg Config) (*DB, error) {
	if cfg.Path == "" || cfg.Path == ":memory:" {
		return nil, errors.New("storage: Config.Path must be a file path")
	}
	if cfg.BusyTimeout < 0 || cfg.BatchMax < 0 || cfg.BatchWindow < 0 || cfg.ReadPoolSize < 0 {
		return nil, errors.New("storage: Config values must not be negative")
	}
	if cfg.BusyTimeout == 0 {
		cfg.BusyTimeout = DefaultBusyTimeout
	}
	if cfg.BatchMax == 0 {
		cfg.BatchMax = DefaultBatchMax
	}
	if cfg.ReadPoolSize == 0 {
		cfg.ReadPoolSize = DefaultReadPoolSize
	}
	sub, err := fs.Sub(ownMigrations, "migrations")
	if err != nil {
		return nil, err
	}
	mods := append([]Module{{Name: "storage", Prefixes: []string{"ops_", "syn_"}, Migrations: sub}}, cfg.Modules...)

	w, err := sql.Open("sqlite", dsn(cfg.Path, cfg.BusyTimeout, false))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	conn, err := w.Conn(ctx)
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("storage: open %s: %w", cfg.Path, err)
	}
	fail := func(err error) (*DB, error) {
		conn.Close()
		w.Close()
		return nil, err
	}
	if err := migrate(ctx, conn, mods); err != nil {
		return fail(err)
	}
	if err := syncCatalog(ctx, conn, mods); err != nil {
		return fail(err)
	}
	r, err := sql.Open("sqlite", dsn(cfg.Path, cfg.BusyTimeout, true))
	if err != nil {
		return fail(err)
	}
	r.SetMaxOpenConns(cfg.ReadPoolSize)
	db := &DB{cfg: cfg, write: w, conn: conn, read: r, mods: mods,
		jobs: make(chan *job, cfg.BatchMax), quit: make(chan struct{}), exited: make(chan struct{})}
	go db.writerLoop()
	return db, nil
}

// Close stops the writer after it finishes the batch in progress, fails queued units with
// ErrClosed and closes every connection. It is safe to call more than once.
func (db *DB) Close() error {
	var err error
	db.once.Do(func() {
		close(db.quit)
		<-db.exited
		err = errors.Join(db.conn.Close(), db.write.Close(), db.read.Close())
	})
	return err
}
