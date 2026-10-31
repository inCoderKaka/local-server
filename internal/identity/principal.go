package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Principal statuses. Deleted is the soft delete (D-42).
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusDeleted   = "deleted"
)

// Principal is an idn_principal row.
type Principal struct {
	ID        string
	Kind      string
	Category  *string
	Segment   *string
	PlanID    *string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PrincipalRepo reads and writes idn_principal.
type PrincipalRepo struct{ now Clock }

const principalCols = `id, kind, category, segment, plan_id, status, created_at, updated_at`

func scanPrincipal(r storage.Row) (Principal, error) {
	var p Principal
	var c, u int64
	err := r.Scan(&p.ID, &p.Kind, &p.Category, &p.Segment, &p.PlanID, &p.Status, &c, &u)
	p.CreatedAt, p.UpdatedAt = tm(c), tm(u)
	return p, err
}

// Create inserts an active principal of the given kind and returns it.
func (r *PrincipalRepo) Create(tx *storage.Tx, kind string, category, segment *string) (Principal, error) {
	id, err := newID()
	if err != nil {
		return Principal{}, err
	}
	now := r.now()
	_, err = tx.Exec(`INSERT INTO idn_principal (id, kind, category, segment, status, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', ?, ?)`,
		id, kind, category, segment, ms(now), ms(now))
	if err != nil {
		return Principal{}, fmt.Errorf("identity: create principal: %w", err)
	}
	return Principal{ID: id, Kind: kind, Category: category, Segment: segment, Status: StatusActive, CreatedAt: tm(ms(now)), UpdatedAt: tm(ms(now))}, nil
}

// Get returns one principal.
func (r *PrincipalRepo) Get(ctx context.Context, rd *storage.Reader, id string) (Principal, error) {
	return first[Principal](rd.Query(ctx, `SELECT `+principalCols+` FROM idn_principal WHERE id = ?`, id))(scanPrincipal)
}

// List returns principals of a kind (all kinds if kind is empty), oldest first.
func (r *PrincipalRepo) List(ctx context.Context, rd *storage.Reader, kind string) ([]Principal, error) {
	rows, err := rd.Query(ctx, `SELECT `+principalCols+` FROM idn_principal WHERE (? = '' OR kind = ?) ORDER BY created_at, id`, kind, kind)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanPrincipal)
}

// Update changes the tags and plan of a principal.
func (r *PrincipalRepo) Update(tx *storage.Tx, id string, category, segment, planID *string) error {
	return mustChange(tx.Exec(`UPDATE idn_principal SET category = ?, segment = ?, plan_id = ?, updated_at = ? WHERE id = ?`,
		category, segment, planID, ms(r.now()), id))
}

// SetStatus moves a principal between active, suspended and deleted (the soft delete).
func (r *PrincipalRepo) SetStatus(tx *storage.Tx, id, status string) error {
	return mustChange(tx.Exec(`UPDATE idn_principal SET status = ?, updated_at = ? WHERE id = ?`, status, ms(r.now()), id))
}

// Delete removes the row for good. It is refused while any type row, credential, session,
// consent or other row refers to the principal (ON DELETE RESTRICT, D-42); soft-delete with
// SetStatus instead.
func (r *PrincipalRepo) Delete(tx *storage.Tx, id string) error {
	return mustChange(tx.Exec(`DELETE FROM idn_principal WHERE id = ?`, id))
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
