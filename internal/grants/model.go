package grants

import (
	"context"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Model is a grt_model row.
type Model struct {
	ID        string
	Name      string
	CreatedAt time.Time
	RetiredAt *time.Time
}

// ModelRepo reads and writes grt_model.
type ModelRepo struct{ now Clock }

const modelCols = `id, name, created_at, retired_at`

func scanModel(r storage.Row) (Model, error) {
	var m Model
	var c int64
	var rt *int64
	err := r.Scan(&m.ID, &m.Name, &c, &rt)
	m.CreatedAt, m.RetiredAt = tm(c), tmp(rt)
	return m, err
}

// Create inserts a model; the name is unique.
func (r *ModelRepo) Create(tx *storage.Tx, name string) (Model, error) {
	id, err := newID()
	if err != nil {
		return Model{}, err
	}
	now := ms(r.now())
	if _, err := tx.Exec(`INSERT INTO grt_model (id, name, created_at) VALUES (?, ?, ?)`, id, name, now); err != nil {
		return Model{}, err
	}
	return Model{ID: id, Name: name, CreatedAt: tm(now)}, nil
}

// Get returns one model.
func (r *ModelRepo) Get(ctx context.Context, rd *storage.Reader, id string) (Model, error) {
	return first[Model](rd.Query(ctx, `SELECT `+modelCols+` FROM grt_model WHERE id = ?`, id))(scanModel)
}

// GetByName returns the model with an engine name.
func (r *ModelRepo) GetByName(ctx context.Context, rd *storage.Reader, name string) (Model, error) {
	return first[Model](rd.Query(ctx, `SELECT `+modelCols+` FROM grt_model WHERE name = ?`, name))(scanModel)
}

// List returns models by name; retired ones only if includeRetired.
func (r *ModelRepo) List(ctx context.Context, rd *storage.Reader, includeRetired bool) ([]Model, error) {
	rows, err := rd.Query(ctx, `SELECT `+modelCols+` FROM grt_model WHERE (? OR retired_at IS NULL) ORDER BY name`, includeRetired)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanModel)
}

// Retire soft-deletes a model: old versions keep referring to it.
func (r *ModelRepo) Retire(tx *storage.Tx, id string) error {
	return mustChange(tx.Exec(`UPDATE grt_model SET retired_at = ? WHERE id = ? AND retired_at IS NULL`, ms(r.now()), id))
}
