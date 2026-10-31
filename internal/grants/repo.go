package grants

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("grants: not found")

// Clock returns the current time; a repository calls it for every created_at and updated_at.
type Clock func() time.Time

func newID() (string, error) {
	u, err := uuid.NewV7()
	return u.String(), err
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func msp(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	v := t.UnixMilli()
	return &v
}

func tm(v int64) time.Time { return time.UnixMilli(v).UTC() }

func tmp(v *int64) *time.Time {
	if v == nil {
		return nil
	}
	t := tm(*v)
	return &t
}

// Repos holds every grants repository.
type Repos struct {
	Models      *ModelRepo
	Grants      *GrantRepo
	Versions    *VersionRepo
	PolicyRules *PolicyRuleRepo
}

// NewRepos builds the repositories. A nil clock means time.Now.
func NewRepos(clock Clock) *Repos {
	if clock == nil {
		clock = time.Now
	}
	return &Repos{Models: &ModelRepo{clock}, Grants: &GrantRepo{clock}, Versions: &VersionRepo{clock}, PolicyRules: &PolicyRuleRepo{clock}}
}

func mustChange(res storage.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func collect[T any](rows storage.Rows, scan func(storage.Row) (T, error)) ([]T, error) {
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, v)
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

// first returns the only row of a query, or ErrNotFound. (storage does not export its
// no-rows error and this package may not import database/sql, so reads use Query.)
func first[T any](rows storage.Rows, err error) func(func(storage.Row) (T, error)) (T, error) {
	return func(scan func(storage.Row) (T, error)) (T, error) {
		var zero T
		if err != nil {
			return zero, err
		}
		out, err := collect(rows, scan)
		if err != nil {
			return zero, err
		}
		if len(out) == 0 {
			return zero, ErrNotFound
		}
		return out[0], nil
	}
}
