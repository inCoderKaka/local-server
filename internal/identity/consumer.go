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

// Billing modes of a consumer (req_001 D-107). The mode is a label: Phase 1 keeps no balances,
// invoices or prices (req_001 D-02).
const (
	BillingNone     = "none"
	BillingPrepaid  = "prepaid"
	BillingPostpaid = "postpaid"
)

// ValidBillingMode reports whether m is none, prepaid or postpaid.
func ValidBillingMode(m string) bool {
	return m == BillingNone || m == BillingPrepaid || m == BillingPostpaid
}

// ConsumerDataset is the syn_change dataset of consumer rows (the "consumer directory", D-40).
const ConsumerDataset = "consumer_directory"

// Consumer is an idn_consumer row.
type Consumer struct {
	ID          string
	PrincipalID string
	Kind        string
	DisplayName *string
	Email       *string
	// BillingMode is BillingNone, BillingPrepaid or BillingPostpaid. Only a chargeable
	// consumer may set quota windows on its own keys (req_001 D-107).
	BillingMode string
	RowVersion  int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Chargeable reports whether the consumer is prepaid or postpaid.
func (c Consumer) Chargeable() bool {
	return c.BillingMode == BillingPrepaid || c.BillingMode == BillingPostpaid
}

// ConsumerRepo reads and writes idn_consumer. Every write records the row in syn_change
// through Tx.Capture, in the same unit of work (D-40).
type ConsumerRepo struct{ now Clock }

const consumerCols = `id, principal_id, kind, display_name, email, billing_mode, row_version, created_at, updated_at`

func scanConsumer(r storage.Row) (Consumer, error) {
	var c Consumer
	var cr, up int64
	err := r.Scan(&c.ID, &c.PrincipalID, &c.Kind, &c.DisplayName, &c.Email, &c.BillingMode, &c.RowVersion, &cr, &up)
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

// Create inserts the consumer of an existing principal with billing mode none. It fails if the
// principal does not exist or already has a consumer row (principal_id is UNIQUE, D-53).
func (r *ConsumerRepo) Create(tx *storage.Tx, principalID, kind string, displayName, email *string) (Consumer, error) {
	return r.CreateWithBilling(tx, principalID, kind, BillingNone, displayName, email)
}

// CreateWithBilling is Create with a billing mode: BillingNone, BillingPrepaid or BillingPostpaid.
func (r *ConsumerRepo) CreateWithBilling(tx *storage.Tx, principalID, kind, billingMode string, displayName, email *string) (Consumer, error) {
	id, err := newID()
	if err != nil {
		return Consumer{}, err
	}
	now := ms(r.now())
	if _, err := tx.Exec(`INSERT INTO idn_consumer (id, principal_id, kind, display_name, email, billing_mode, row_version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		id, principalID, kind, displayName, email, billingMode, now, now); err != nil {
		return Consumer{}, fmt.Errorf("identity: create consumer: %w", err)
	}
	c := Consumer{ID: id, PrincipalID: principalID, Kind: kind, DisplayName: displayName, Email: email, BillingMode: billingMode, RowVersion: 1, CreatedAt: tm(now), UpdatedAt: tm(now)}
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
	rows, err := rd.Query(ctx, `SELECT c.id, c.principal_id, c.kind, c.display_name, c.email, c.billing_mode, c.row_version, c.created_at, c.updated_at
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

// SetBillingMode changes the billing mode (BillingNone, BillingPrepaid or BillingPostpaid) and
// records the row in syn_change like any consumer edit.
func (r *ConsumerRepo) SetBillingMode(tx *storage.Tx, id, mode string) error {
	if err := mustChange(tx.Exec(`UPDATE idn_consumer SET billing_mode = ?, row_version = row_version + 1, updated_at = ? WHERE id = ?`,
		mode, ms(r.now()), id)); err != nil {
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

// ConsumerAccount is a consumer with the state of its principal and of its login.
type ConsumerAccount struct {
	Consumer
	// Status is the principal's status: active, suspended or deleted.
	Status string
	// StatusChangedAt is when the principal row last changed. For a deleted consumer that is
	// the moment of deletion, because deleted is final.
	StatusChangedAt time.Time
	// Username is the identifier of the consumer's first password credential that is not
	// revoked, or nil for a key-only consumer.
	Username *string
	// HasLogin is whether the consumer has any credential that is not revoked.
	HasLogin bool
}

const accountSelect = `SELECT c.id, c.principal_id, c.kind, c.display_name, c.email, c.billing_mode, c.row_version, c.created_at, c.updated_at,
p.status, p.updated_at,
(SELECT k.identifier FROM idn_credential k WHERE k.principal_id = p.id AND k.kind = 'password' AND k.revoked_at IS NULL ORDER BY k.created_at, k.id LIMIT 1),
EXISTS (SELECT 1 FROM idn_credential k WHERE k.principal_id = p.id AND k.revoked_at IS NULL)
FROM idn_consumer c JOIN idn_principal p ON p.id = c.principal_id`

func scanAccount(r storage.Row) (ConsumerAccount, error) {
	var a ConsumerAccount
	var cr, up, st int64
	err := r.Scan(&a.ID, &a.PrincipalID, &a.Kind, &a.DisplayName, &a.Email, &a.BillingMode, &a.RowVersion, &cr, &up,
		&a.Status, &st, &a.Username, &a.HasLogin)
	a.CreatedAt, a.UpdatedAt, a.StatusChangedAt = tm(cr), tm(up), tm(st)
	return a, err
}

// GetAccount returns one consumer with its principal status and login.
func (r *ConsumerRepo) GetAccount(ctx context.Context, rd *storage.Reader, id string) (ConsumerAccount, error) {
	return first[ConsumerAccount](rd.Query(ctx, accountSelect+` WHERE c.id = ?`, id))(scanAccount)
}

// GetAccountTx is GetAccount as the unit of work sees it.
func (r *ConsumerRepo) GetAccountTx(tx *storage.Tx, id string) (ConsumerAccount, error) {
	return first[ConsumerAccount](tx.Query(accountSelect+` WHERE c.id = ?`, id))(scanAccount)
}

// ListAccounts returns up to limit consumers, newest first, that come after the given position
// (from the start when after is nil). A status keeps the consumers whose principal has it; an
// empty status keeps every consumer that is not deleted.
func (r *ConsumerRepo) ListAccounts(ctx context.Context, rd *storage.Reader, status string, after *PageKey, limit int) ([]ConsumerAccount, error) {
	q := accountSelect + ` WHERE ` + statusClause(status)
	var args []any
	if status != "" {
		args = append(args, status)
	}
	if after != nil {
		q += ` AND (c.created_at < ? OR (c.created_at = ? AND c.id < ?))`
		args = append(args, ms(after.CreatedAt), ms(after.CreatedAt), after.ID)
	}
	q += ` ORDER BY c.created_at DESC, c.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := rd.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanAccount)
}

func statusClause(status string) string {
	if status == "" {
		return `p.status <> 'deleted'`
	}
	return `p.status = ?`
}
