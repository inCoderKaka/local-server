package grants

import (
	"context"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Grant is a grt_grant row.
type Grant struct {
	ID               string
	ConsumerID       string
	CurrentVersionID *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// GrantRepo reads and writes grt_grant.
type GrantRepo struct{ now Clock }

const grantCols = `id, consumer_id, current_version_id, created_at, updated_at`

func scanGrant(r storage.Row) (Grant, error) {
	var g Grant
	var c, u int64
	err := r.Scan(&g.ID, &g.ConsumerID, &g.CurrentVersionID, &c, &u)
	g.CreatedAt, g.UpdatedAt = tm(c), tm(u)
	return g, err
}

// Create inserts the grant of a consumer, with no version yet; a consumer has one grant.
func (r *GrantRepo) Create(tx *storage.Tx, consumerID string) (Grant, error) {
	id, err := newID()
	if err != nil {
		return Grant{}, err
	}
	now := ms(r.now())
	if _, err := tx.Exec(`INSERT INTO grt_grant (id, consumer_id, created_at, updated_at) VALUES (?, ?, ?, ?)`, id, consumerID, now, now); err != nil {
		return Grant{}, err
	}
	return Grant{ID: id, ConsumerID: consumerID, CreatedAt: tm(now), UpdatedAt: tm(now)}, nil
}

// Get returns one grant.
func (r *GrantRepo) Get(ctx context.Context, rd *storage.Reader, id string) (Grant, error) {
	return first[Grant](rd.Query(ctx, `SELECT `+grantCols+` FROM grt_grant WHERE id = ?`, id))(scanGrant)
}

// GetByConsumer returns the grant of a consumer.
func (r *GrantRepo) GetByConsumer(ctx context.Context, rd *storage.Reader, consumerID string) (Grant, error) {
	return first[Grant](rd.Query(ctx, `SELECT `+grantCols+` FROM grt_grant WHERE consumer_id = ?`, consumerID))(scanGrant)
}

// List returns every grant, oldest first.
func (r *GrantRepo) List(ctx context.Context, rd *storage.Reader) ([]Grant, error) {
	rows, err := rd.Query(ctx, `SELECT `+grantCols+` FROM grt_grant ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanGrant)
}

// SetCurrentVersion points the grant at one of its own versions (for example to roll back).
// Making a new version does this itself.
func (r *GrantRepo) SetCurrentVersion(tx *storage.Tx, grantID, versionID string) error {
	return mustChange(tx.Exec(`UPDATE grt_grant SET current_version_id = ?, updated_at = ?
WHERE id = ? AND EXISTS (SELECT 1 FROM grt_version WHERE id = ? AND grant_id = ?)`, versionID, ms(r.now()), grantID, versionID, grantID))
}
