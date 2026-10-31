package settings

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

var identRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Rule says how long the rows of one table are kept (a retention rule, D-40).
type Rule struct {
	Table string
	// TimeColumn holds the row's time as Unix milliseconds UTC. Rows with a NULL time are kept.
	TimeColumn string
	// Setting is the key of a registered, unscoped Duration setting: rows older than it
	// are deleted. Zero or less keeps rows forever.
	Setting string
	// Where is an optional extra SQL condition written by the registering module (never
	// built from user input), for example "status IN ('succeeded','failed')". Only rows that
	// match it are pruned. Args are its placeholders' values.
	Where string
	Args  []any
}

// Pruned reports the rows one rule deleted in a run.
type Pruned struct {
	Table string
	Rows  int64
}

// RegisterRetention adds retention rules for the worker.
func (s *Store) RegisterRetention(rules ...Rule) error {
	for _, r := range rules {
		if !identRE.MatchString(r.Table) || !identRE.MatchString(r.TimeColumn) {
			return fmt.Errorf("%w: retention rule %q.%q: table and column must be plain identifiers", ErrDefinition, r.Table, r.TimeColumn)
		}
		d, ok := s.Definition(r.Setting)
		switch {
		case !ok:
			return fmt.Errorf("%w: retention of %s: %s", ErrUnknownKey, r.Table, r.Setting)
		case d.Kind != Duration || d.Scoped:
			return fmt.Errorf("%w: retention setting %s must be an unscoped duration", ErrDefinition, r.Setting)
		}
	}
	s.mu.Lock()
	s.rules = append(s.rules, rules...)
	s.mu.Unlock()
	return nil
}

// PruneOnce deletes the expired rows of every registered rule, in batches of the
// retention.batch_rows setting, and reports what it deleted. It reads each retention
// setting now. It continues past a failing rule and returns the errors joined.
func (s *Store) PruneOnce(ctx context.Context) ([]Pruned, error) {
	batch, err := s.Int(ctx, KeyRetentionBatchRows, "")
	if err != nil {
		return nil, err
	}
	if batch < 1 {
		batch = 1
	}
	s.mu.RLock()
	rules := append([]Rule(nil), s.rules...)
	s.mu.RUnlock()
	var out []Pruned
	var errs []error
	for _, r := range rules {
		n, err := s.prune(ctx, r, batch)
		if err != nil {
			errs = append(errs, fmt.Errorf("settings: prune %s: %w", r.Table, err))
			continue
		}
		out = append(out, Pruned{Table: r.Table, Rows: n})
	}
	return out, errors.Join(errs...)
}

func (s *Store) prune(ctx context.Context, r Rule, batch int64) (int64, error) {
	keep, err := s.Duration(ctx, r.Setting, "")
	if err != nil {
		return 0, err
	}
	if keep <= 0 {
		return 0, nil
	}
	cutoff := s.now().Add(-keep).UnixMilli()
	cond := r.TimeColumn + " < ?"
	if r.Where != "" {
		cond += " AND (" + r.Where + ")"
	}
	q := fmt.Sprintf(`DELETE FROM %[1]s WHERE rowid IN (SELECT rowid FROM %[1]s WHERE %[2]s LIMIT ?)`, r.Table, cond)
	args := append([]any{cutoff}, r.Args...)
	args = append(args, batch)
	var total int64
	for {
		var n int64
		err := s.db.Write(ctx, func(tx *storage.Tx) error {
			res, err := tx.Exec(q, args...)
			if err != nil {
				return err
			}
			n, err = res.RowsAffected()
			return err
		})
		if err != nil {
			return total, err
		}
		total += n
		if n < batch {
			return total, nil
		}
	}
}

// Run calls PruneOnce now and then every retention.interval (read before each wait) until
// ctx ends. A failed run is passed to onError (which may be nil) and the loop continues.
func (s *Store) Run(ctx context.Context, onError func(error)) {
	for {
		if _, err := s.PruneOnce(ctx); err != nil && onError != nil && ctx.Err() == nil {
			onError(err)
		}
		wait, err := s.Duration(ctx, KeyRetentionInterval, "")
		if err != nil || wait <= 0 {
			if err != nil && onError != nil && ctx.Err() == nil {
				onError(err)
			}
			wait = time.Hour
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
