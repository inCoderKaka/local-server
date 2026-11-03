package settings

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Definitions returns every registered definition, sorted by key. The slice is a copy.
func (s *Store) Definitions() []Def {
	s.mu.RLock()
	out := make([]Def, 0, len(s.defs))
	for _, d := range s.defs {
		out = append(out, d)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Update is one setting Apply sets. Reset returns the setting to its default and ignores Value.
type Update struct {
	Key   string
	Value string
	Reset bool
}

// Change is a setting whose effective value Apply changed. Old and New are the text forms of the
// effective values (the stored value, or the default when none is stored).
type Change struct {
	Key string
	Old string
	New string
}

// UpdateError says which setting an Apply refused. Err is one of the Err values of this package
// (ErrUnknownKey, ErrNotOwner, ErrInvalidValue, ErrScope, ErrRejected).
type UpdateError struct {
	Key string
	Err error
}

func (e *UpdateError) Error() string { return fmt.Sprintf("settings: %s: %v", e.Key, e.Err) }
func (e *UpdateError) Unwrap() error { return e.Err }

// Apply sets or resets several non-scoped settings on behalf of by in one unit of work: either
// every change is written or none is. It checks every update first (the key exists, by owns it,
// the value parses as the setting's type, and the validators of SetValidator and SetTxValidator
// accept it) and only then writes.
//
// A setting whose new effective value equals its current one is not a change: nothing is written
// for it and it is not in the returned list. After all writes, and in the same unit of work,
// Apply calls after (when not nil) with the changes in key order, so a caller can add the rows
// that must commit or roll back with them, an audit row for example; an error from after rolls
// everything back. A refusal is an *UpdateError naming the key. The same key twice is refused.
func (s *Store) Apply(ctx context.Context, by Owner, updates []Update, after func(tx *storage.Tx, changes []Change) error) ([]Change, error) {
	type plan struct {
		def   Def
		value string
	}
	plans := make(map[string]plan, len(updates))
	for _, u := range updates {
		fail := func(err error) error { return &UpdateError{Key: u.Key, Err: err} }
		if _, dup := plans[u.Key]; dup {
			return nil, fail(fmt.Errorf("%w: key given twice", ErrInvalidValue))
		}
		d, err := s.lookup(u.Key, "")
		if err != nil {
			return nil, fail(errors.Unwrap(err))
		}
		if d.Owner != by {
			return nil, fail(fmt.Errorf("%w: owned by %s, not %s", ErrNotOwner, d.Owner, by))
		}
		value := u.Value
		if u.Reset {
			value = d.Default
		} else if err := checkValue(d.Kind, value); err != nil {
			return nil, fail(fmt.Errorf("%w: %v", ErrInvalidValue, err))
		}
		if err := s.validate(ctx, u.Key, value); err != nil {
			return nil, fail(err)
		}
		plans[u.Key] = plan{d, value}
	}
	keys := make([]string, 0, len(plans))
	for k := range plans {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var changes []Change
	at := s.now().UnixMilli()
	err := s.db.Write(ctx, func(tx *storage.Tx) error {
		changes = changes[:0]
		for _, k := range keys {
			p := plans[k]
			old, err := effectiveTx(tx, p.def)
			if err != nil {
				return err
			}
			if old == p.value {
				continue
			}
			if err := s.validateTx(tx, k, p.value); err != nil {
				return &UpdateError{Key: k, Err: err}
			}
			if p.value == p.def.Default {
				_, err = tx.Exec(`DELETE FROM hst_setting WHERE key = ? AND scope = ''`, k)
			} else {
				_, err = tx.Exec(`INSERT INTO hst_setting (key, scope, value, updated_by, updated_at) VALUES (?, '', ?, ?, ?)
					ON CONFLICT (key, scope) DO UPDATE SET value = excluded.value, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
					k, p.value, string(by), at)
			}
			if err != nil {
				return err
			}
			changes = append(changes, Change{Key: k, Old: old, New: p.value})
		}
		if after != nil {
			return after(tx, changes)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changes, nil
}

// effectiveTx reads the effective text value of a non-scoped setting inside a unit of work.
func effectiveTx(tx *storage.Tx, d Def) (string, error) {
	rows, err := tx.Query(`SELECT value FROM hst_setting WHERE key = ? AND scope = ''`, d.Key)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return d.Default, rows.Err()
	}
	var v string
	if err := rows.Scan(&v); err != nil {
		return "", err
	}
	return v, nil
}
