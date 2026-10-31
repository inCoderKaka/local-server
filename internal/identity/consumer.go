package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Consumer kinds.
const (
	ConsumerIndividual  = "individual"
	ConsumerApplication = "application"
)

// ConsumerDataset is the syn_change dataset of consumer rows (the "consumer directory", D-40).
const ConsumerDataset = "consumer_directory"

// Consumer is an idn_consumer row.
type Consumer struct {
	ID          string
	PrincipalID string
	Kind        string
	DisplayName *string
	Email       *string
	RowVersion  int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ConsumerRepo reads and writes idn_consumer. Every write records the row in syn_change
// through Tx.Capture, in the same unit of work (D-40).
type ConsumerRepo struct{ now Clock }

const consumerCols = `id, principal_id, kind, display_name, email, row_version, created_at, updated_at`

func scanConsumer(r storage.Row) (Consumer, error) {
	var c Consumer
	var cr, up int64
	err := r.Scan(&c.ID, &c.PrincipalID, &c.Kind, &c.DisplayName, &c.Email, &c.RowVersion, &cr, &up)
	c.CreatedAt, c.UpdatedAt = tm(cr), tm(up)
	return c, err
}

func capture(tx *storage.Tx, c Consumer, op string) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return tx.Capture(storage.Change{Dataset: ConsumerDataset, Table: "idn_consumer", PK: c.ID, Version: c.RowVersion, Op: op, Payload: b})
}

// Create inserts the consumer of an existing principal. It fails if the principal does not
// exist or already has a consumer row (principal_id is UNIQUE, D-53).
func (r *ConsumerRepo) Create(tx *storage.Tx, principalID, kind string, displayName, email *string) (Consumer, error) {
	id, err := newID()
	if err != nil {
		return Consumer{}, err
	}
	now := ms(r.now())
	if _, err := tx.Exec(`INSERT INTO idn_consumer (id, principal_id, kind, display_name, email, row_version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		id, principalID, kind, displayName, email, now, now); err != nil {
		return Consumer{}, fmt.Errorf("identity: create consumer: %w", err)
	}
	c := Consumer{ID: id, PrincipalID: principalID, Kind: kind, DisplayName: displayName, Email: email, RowVersion: 1, CreatedAt: tm(now), UpdatedAt: tm(now)}
	return c, capture(tx, c, "insert")
}

// Get returns one consumer.
func (r *ConsumerRepo) Get(ctx context.Context, rd *storage.Reader, id string) (Consumer, error) {
	return first[Consumer](rd.Query(ctx, `SELECT `+consumerCols+` FROM idn_consumer WHERE id = ?`, id))(scanConsumer)
}

// GetByPrincipal returns the consumer of a principal.
func (r *ConsumerRepo) GetByPrincipal(ctx context.Context, rd *storage.Reader, principalID string) (Consumer, error) {
	return first[Consumer](rd.Query(ctx, `SELECT `+consumerCols+` FROM idn_consumer WHERE principal_id = ?`, principalID))(scanConsumer)
}

// List returns consumers whose principal has the given status (any status if empty), oldest first.
func (r *ConsumerRepo) List(ctx context.Context, rd *storage.Reader, status string) ([]Consumer, error) {
	rows, err := rd.Query(ctx, `SELECT c.id, c.principal_id, c.kind, c.display_name, c.email, c.row_version, c.created_at, c.updated_at
FROM idn_consumer c JOIN idn_principal p ON p.id = c.principal_id WHERE (? = '' OR p.status = ?) ORDER BY c.created_at, c.id`, status, status)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanConsumer)
}

// Update changes the personal fields.
func (r *ConsumerRepo) Update(tx *storage.Tx, id string, displayName, email *string) error {
	if err := mustChange(tx.Exec(`UPDATE idn_consumer SET display_name = ?, email = ?, row_version = row_version + 1, updated_at = ? WHERE id = ?`,
		displayName, email, ms(r.now()), id)); err != nil {
		return err
	}
	return r.captureCurrent(tx, id, "update")
}

// SoftDelete erases the personal fields, revokes the consumer's access keys and marks its
// principal deleted. The rows stay for ledger references (D-40, D-42).
func (r *ConsumerRepo) SoftDelete(tx *storage.Tx, id string) error {
	now := ms(r.now())
	if err := mustChange(tx.Exec(`UPDATE idn_consumer SET display_name = NULL, email = NULL, row_version = row_version + 1, updated_at = ? WHERE id = ?`, now, id)); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE idn_access_key SET revoked_at = ? WHERE consumer_id = ? AND revoked_at IS NULL`, now, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE idn_principal SET status = 'deleted', updated_at = ? WHERE id = (SELECT principal_id FROM idn_consumer WHERE id = ?)`, now, id); err != nil {
		return err
	}
	return r.captureCurrent(tx, id, "update")
}

func (r *ConsumerRepo) captureCurrent(tx *storage.Tx, id, op string) error {
	c, err := first[Consumer](tx.Query(`SELECT `+consumerCols+` FROM idn_consumer WHERE id = ?`, id))(scanConsumer)
	if err != nil {
		return err
	}
	return capture(tx, c, op)
}
