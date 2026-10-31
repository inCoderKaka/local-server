package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Result is the outcome of an Exec.
type Result interface {
	LastInsertId() (int64, error)
	RowsAffected() (int64, error)
}

// Row is a single-row query result.
type Row interface{ Scan(dest ...any) error }

// Rows is a multi-row query result. Close it before the unit of work returns.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

// Change is one captured change (a syn_change row). Payload must not contain prompts or
// responses (D-31, D-40).
type Change struct {
	Dataset string
	Table   string
	PK      string
	Version int64
	Op      string // "insert", "update" or "delete"
	Payload []byte
}

// Tx is a unit of work's view of the write transaction. It is valid only inside the
// function passed to DB.Write, which runs on the writer goroutine.
type Tx struct {
	ctx   context.Context
	conn  *sql.Conn
	valid bool
}

// Exec runs a write statement inside the unit of work.
func (t *Tx) Exec(query string, args ...any) (Result, error) {
	if !t.valid {
		return nil, fmt.Errorf("storage: Tx used outside its unit of work")
	}
	return t.conn.ExecContext(t.ctx, query, args...)
}

// Query runs a read inside the unit of work (it sees the unit's own earlier writes).
func (t *Tx) Query(query string, args ...any) (Rows, error) {
	if !t.valid {
		return nil, fmt.Errorf("storage: Tx used outside its unit of work")
	}
	r, err := t.conn.QueryContext(t.ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// QueryRow runs a single-row read inside the unit of work.
func (t *Tx) QueryRow(query string, args ...any) Row {
	return t.conn.QueryRowContext(t.ctx, query, args...)
}

// Capture records a change in syn_change, in the same transaction as the change itself.
func (t *Tx) Capture(c Change) error {
	_, err := t.Exec(`INSERT INTO syn_change (dataset, table_name, pk, version, op, payload, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.Dataset, c.Table, c.PK, c.Version, c.Op, c.Payload, time.Now().UnixMilli())
	return err
}

type job struct {
	ctx  context.Context
	fn   func(*Tx) error
	done chan error
	err  error
}

// Write runs fn as one unit of work on the writer goroutine and returns after the batch
// it joined has committed. If fn returns an error or panics, everything it wrote through
// the Tx is rolled back and Write returns that error. See the package comment for rules.
func (db *DB) Write(ctx context.Context, fn func(*Tx) error) error {
	j := &job{ctx: ctx, fn: fn, done: make(chan error, 1)}
	select {
	case <-db.quit:
		return ErrClosed
	default:
	}
	select {
	case <-db.quit:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	case db.jobs <- j:
	}
	// The writer answers every queued unit, but one queued while Close was racing it may
	// be left behind after the writer exits.
	select {
	case err := <-j.done:
		return err
	case <-db.exited:
		select {
		case err := <-j.done:
			return err
		default:
			return ErrClosed
		}
	}
}

func (db *DB) writerLoop() {
	defer close(db.exited)
	for {
		select {
		case <-db.quit:
			db.drain()
			return
		case j := <-db.jobs:
			db.runBatch(db.collect(j))
		}
	}
}

func (db *DB) drain() {
	for {
		select {
		case j := <-db.jobs:
			j.done <- ErrClosed
		default:
			return
		}
	}
}

// collect gathers up to BatchMax units: those already queued, then whatever arrives
// within BatchWindow of the first.
func (db *DB) collect(first *job) []*job {
	batch := []*job{first}
	var timer <-chan time.Time
	if db.cfg.BatchWindow > 0 {
		t := time.NewTimer(db.cfg.BatchWindow)
		defer t.Stop()
		timer = t.C
	}
	for len(batch) < db.cfg.BatchMax {
		if timer == nil {
			select {
			case j := <-db.jobs:
				batch = append(batch, j)
			default:
				return batch
			}
			continue
		}
		select {
		case j := <-db.jobs:
			batch = append(batch, j)
		case <-timer:
			return batch
		case <-db.quit:
			return batch
		}
	}
	return batch
}

func (db *DB) runBatch(batch []*job) {
	ctx := context.Background()
	finish := func(common error) {
		for _, j := range batch {
			if j.err == nil {
				j.err = common
			}
			j.done <- j.err
		}
	}
	if _, err := db.conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		finish(fmt.Errorf("storage: begin: %w", err))
		return
	}
	ok := make([]*job, 0, len(batch))
	for _, j := range batch {
		if err := j.ctx.Err(); err != nil {
			j.err = err
			continue
		}
		j.err = db.runUnit(ctx, j)
		if j.err == nil {
			ok = append(ok, j)
		}
	}
	if _, err := db.conn.ExecContext(ctx, "COMMIT"); err != nil {
		db.conn.ExecContext(ctx, "ROLLBACK")
		cerr := fmt.Errorf("storage: commit: %w", err)
		for _, j := range ok {
			j.err = cerr
		}
	}
	finish(nil)
}

// runUnit runs one unit inside a savepoint so that its failure rolls back only itself.
func (db *DB) runUnit(ctx context.Context, j *job) (err error) {
	if _, err := db.conn.ExecContext(ctx, "SAVEPOINT unit"); err != nil {
		return fmt.Errorf("storage: savepoint: %w", err)
	}
	tx := &Tx{ctx: j.ctx, conn: db.conn, valid: true}
	func() {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("storage: unit of work panicked: %v", p)
			}
		}()
		err = j.fn(tx)
	}()
	tx.valid = false
	if err != nil {
		db.conn.ExecContext(ctx, "ROLLBACK TO unit")
	}
	if _, rerr := db.conn.ExecContext(ctx, "RELEASE unit"); rerr != nil && err == nil {
		err = fmt.Errorf("storage: release: %w", rerr)
	}
	return err
}

// Reader returns the read pool. Its connections are query_only; reads see data committed
// before the call started.
func (db *DB) Reader() *Reader { return &Reader{db: db} }

// Reader queries the read pool.
type Reader struct{ db *DB }

// Query runs a read. Close the Rows.
func (r *Reader) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	rows, err := r.db.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// QueryRow runs a single-row read.
func (r *Reader) QueryRow(ctx context.Context, query string, args ...any) Row {
	return r.db.read.QueryRowContext(ctx, query, args...)
}
