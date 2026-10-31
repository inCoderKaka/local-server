package identity

import (
	"context"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Invite is an idn_invite row. Its state is derived: redeemed, revoked, expired or pending.
type Invite struct {
	ID                 string
	Prefix             string
	TokenHash          string
	CreatedBy          string
	Note               *string
	CreatedAt          time.Time
	ExpiresAt          time.Time
	RedeemedAt         *time.Time
	RedeemedConsumerID *string
	RevokedAt          *time.Time
}

// InviteRepo reads and writes idn_invite.
type InviteRepo struct{ now Clock }

const inviteCols = `id, prefix, token_hash, created_by, note, created_at, expires_at, redeemed_at, redeemed_consumer_id, revoked_at`

func scanInvite(r storage.Row) (Invite, error) {
	var i Invite
	var cr, ex int64
	var rd, rv *int64
	err := r.Scan(&i.ID, &i.Prefix, &i.TokenHash, &i.CreatedBy, &i.Note, &cr, &ex, &rd, &i.RedeemedConsumerID, &rv)
	i.CreatedAt, i.ExpiresAt, i.RedeemedAt, i.RevokedAt = tm(cr), tm(ex), tmp(rd), tmp(rv)
	return i, err
}

// Create inserts a pending invite.
func (r *InviteRepo) Create(tx *storage.Tx, prefix, tokenHash, createdBy string, note *string, expiresAt time.Time) (Invite, error) {
	id, err := newID()
	if err != nil {
		return Invite{}, err
	}
	now := ms(r.now())
	if _, err := tx.Exec(`INSERT INTO idn_invite (id, prefix, token_hash, created_by, note, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, prefix, tokenHash, createdBy, note, now, ms(expiresAt)); err != nil {
		return Invite{}, err
	}
	return Invite{ID: id, Prefix: prefix, TokenHash: tokenHash, CreatedBy: createdBy, Note: note, CreatedAt: tm(now), ExpiresAt: tm(ms(expiresAt))}, nil
}

// Get returns one invite.
func (r *InviteRepo) Get(ctx context.Context, rd *storage.Reader, id string) (Invite, error) {
	return first[Invite](rd.Query(ctx, `SELECT `+inviteCols+` FROM idn_invite WHERE id = ?`, id))(scanInvite)
}

// GetByPrefix returns the invite with a token prefix.
func (r *InviteRepo) GetByPrefix(ctx context.Context, rd *storage.Reader, prefix string) (Invite, error) {
	return first[Invite](rd.Query(ctx, `SELECT `+inviteCols+` FROM idn_invite WHERE prefix = ?`, prefix))(scanInvite)
}

// ListPending returns invites that are not redeemed, not revoked and not expired at now.
func (r *InviteRepo) ListPending(ctx context.Context, rd *storage.Reader, now time.Time) ([]Invite, error) {
	rows, err := rd.Query(ctx, `SELECT `+inviteCols+` FROM idn_invite WHERE redeemed_at IS NULL AND revoked_at IS NULL AND expires_at > ? ORDER BY created_at, id`, ms(now))
	if err != nil {
		return nil, err
	}
	return collect(rows, scanInvite)
}

// Redeem marks a pending invite redeemed as a consumer. It returns ErrNotFound if the invite
// is missing, already redeemed, revoked or expired.
func (r *InviteRepo) Redeem(tx *storage.Tx, id, consumerID string) error {
	now := ms(r.now())
	return mustChange(tx.Exec(`UPDATE idn_invite SET redeemed_at = ?, redeemed_consumer_id = ?
WHERE id = ? AND redeemed_at IS NULL AND revoked_at IS NULL AND expires_at > ?`, now, consumerID, id, now))
}

// Revoke ends a pending invite.
func (r *InviteRepo) Revoke(tx *storage.Tx, id string) error {
	return mustChange(tx.Exec(`UPDATE idn_invite SET revoked_at = ? WHERE id = ? AND redeemed_at IS NULL AND revoked_at IS NULL`, ms(r.now()), id))
}
