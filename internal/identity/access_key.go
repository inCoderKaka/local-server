package identity

import (
	"context"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// AccessKey is an idn_access_key row: a prefix and a hash, never the secret (D-25, D-40).
type AccessKey struct {
	ID            string
	ConsumerID    string
	Prefix        string
	SecretHash    string
	Label         *string
	NarrowingJSON *string
	BoundNode     *string
	CreatedAt     time.Time
	ExpiresAt     *time.Time
	RevokedAt     *time.Time
}

// AccessKeyRepo reads and writes idn_access_key.
type AccessKeyRepo struct{ now Clock }

const keyCols = `id, consumer_id, prefix, secret_hash, label, narrowing_json, bound_node, created_at, expires_at, revoked_at`

func scanAccessKey(r storage.Row) (AccessKey, error) {
	var k AccessKey
	var cr int64
	var ex, rv *int64
	err := r.Scan(&k.ID, &k.ConsumerID, &k.Prefix, &k.SecretHash, &k.Label, &k.NarrowingJSON, &k.BoundNode, &cr, &ex, &rv)
	k.CreatedAt, k.ExpiresAt, k.RevokedAt = tm(cr), tmp(ex), tmp(rv)
	return k, err
}

// NewAccessKey is what Create stores. The caller (the auth module) generates the key and
// passes only its prefix and hash.
type NewAccessKey struct {
	ConsumerID    string
	Prefix        string
	SecretHash    string
	Label         *string
	NarrowingJSON *string
	BoundNode     *string
	ExpiresAt     *time.Time
}

// Create inserts an access key.
func (r *AccessKeyRepo) Create(tx *storage.Tx, n NewAccessKey) (AccessKey, error) {
	id, err := newID()
	if err != nil {
		return AccessKey{}, err
	}
	now := ms(r.now())
	if _, err := tx.Exec(`INSERT INTO idn_access_key (id, consumer_id, prefix, secret_hash, label, narrowing_json, bound_node, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, n.ConsumerID, n.Prefix, n.SecretHash, n.Label, n.NarrowingJSON, n.BoundNode, now, msp(n.ExpiresAt)); err != nil {
		return AccessKey{}, err
	}
	return AccessKey{ID: id, ConsumerID: n.ConsumerID, Prefix: n.Prefix, SecretHash: n.SecretHash, Label: n.Label,
		NarrowingJSON: n.NarrowingJSON, BoundNode: n.BoundNode, CreatedAt: tm(now), ExpiresAt: n.ExpiresAt}, nil
}

// Get returns one access key.
func (r *AccessKeyRepo) Get(ctx context.Context, rd *storage.Reader, id string) (AccessKey, error) {
	return first[AccessKey](rd.Query(ctx, `SELECT `+keyCols+` FROM idn_access_key WHERE id = ?`, id))(scanAccessKey)
}

// GetTx returns one access key as the unit of work sees it.
func (r *AccessKeyRepo) GetTx(tx *storage.Tx, id string) (AccessKey, error) {
	return first[AccessKey](tx.Query(`SELECT `+keyCols+` FROM idn_access_key WHERE id = ?`, id))(scanAccessKey)
}

// GetByPrefix returns the access key with a prefix.
func (r *AccessKeyRepo) GetByPrefix(ctx context.Context, rd *storage.Reader, prefix string) (AccessKey, error) {
	return first[AccessKey](rd.Query(ctx, `SELECT `+keyCols+` FROM idn_access_key WHERE prefix = ?`, prefix))(scanAccessKey)
}

// ListByConsumer returns a consumer's access keys, oldest first.
func (r *AccessKeyRepo) ListByConsumer(ctx context.Context, rd *storage.Reader, consumerID string) ([]AccessKey, error) {
	rows, err := rd.Query(ctx, `SELECT `+keyCols+` FROM idn_access_key WHERE consumer_id = ? ORDER BY created_at, id`, consumerID)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanAccessKey)
}

// Update changes the label, the narrowing, the node binding and the expiry of a key.
func (r *AccessKeyRepo) Update(tx *storage.Tx, id string, label, narrowingJSON, boundNode *string, expiresAt *time.Time) error {
	return mustChange(tx.Exec(`UPDATE idn_access_key SET label = ?, narrowing_json = ?, bound_node = ?, expires_at = ? WHERE id = ?`,
		label, narrowingJSON, boundNode, msp(expiresAt), id))
}

// Revoke ends a key. It stays for metering history.
func (r *AccessKeyRepo) Revoke(tx *storage.Tx, id string) error {
	return mustChange(tx.Exec(`UPDATE idn_access_key SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, ms(r.now()), id))
}
