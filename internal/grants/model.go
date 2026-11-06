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
	// Allowed is whether a new grant may include the model (the host opens it for sharing).
	// Closing it does not change grant versions that already include it.
	Allowed bool
	// KeepAliveSeconds is how long the model stays loaded when idle; nil means the host's
	// default keep-alive setting.
	KeepAliveSeconds *int64
}

// ModelRepo reads and writes grt_model.
type ModelRepo struct{ now Clock }

const modelCols = `id, name, created_at, retired_at, allowed, keep_alive_seconds`

func scanModel(r storage.Row) (Model, error) {
	var m Model
	var c, al int64
	var rt *int64
	err := r.Scan(&m.ID, &m.Name, &c, &rt, &al, &m.KeepAliveSeconds)
	m.CreatedAt, m.RetiredAt, m.Allowed = tm(c), tmp(rt), al == 1
	return m, err
}

// Create inserts a model that grants may include; the name is unique.
func (r *ModelRepo) Create(tx *storage.Tx, name string) (Model, error) {
	return r.CreateWithAllowed(tx, name, true)
}

// CreateWithAllowed inserts a model with the given sharing switch; the name is unique.
func (r *ModelRepo) CreateWithAllowed(tx *storage.Tx, name string, allowed bool) (Model, error) {
	id, err := newID()
	if err != nil {
		return Model{}, err
	}
	now := ms(r.now())
	if _, err := tx.Exec(`INSERT INTO grt_model (id, name, created_at, allowed) VALUES (?, ?, ?, ?)`, id, name, now, b2i(allowed)); err != nil {
		return Model{}, err
	}
	return Model{ID: id, Name: name, CreatedAt: tm(now), Allowed: allowed}, nil
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

// SetConfig sets the host's sharing configuration of a model: whether grants may include it and
// its keep-alive (nil clears it to the host default). A retired model is ErrNotFound.
func (r *ModelRepo) SetConfig(tx *storage.Tx, id string, allowed bool, keepAliveSeconds *int64) error {
	return mustChange(tx.Exec(`UPDATE grt_model SET allowed = ?, keep_alive_seconds = ? WHERE id = ? AND retired_at IS NULL`, b2i(allowed), keepAliveSeconds, id))
}

// GetTx is Get inside a unit of work.
func (r *ModelRepo) GetTx(tx *storage.Tx, id string) (Model, error) {
	return first[Model](tx.Query(`SELECT `+modelCols+` FROM grt_model WHERE id = ?`, id))(scanModel)
}

// GetByNameTx is GetByName inside a unit of work.
func (r *ModelRepo) GetByNameTx(tx *storage.Tx, name string) (Model, error) {
	return first[Model](tx.Query(`SELECT `+modelCols+` FROM grt_model WHERE name = ?`, name))(scanModel)
}
