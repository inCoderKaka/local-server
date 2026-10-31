package identity

import (
	"context"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Session is an idn_session row; TokenHash is a hash, never the token.
type Session struct {
	ID          string
	PrincipalID string
	TokenHash   string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	RevokedAt   *time.Time
}

// SessionRepo reads and writes idn_session.
type SessionRepo struct{ now Clock }

const sessionCols = `id, principal_id, token_hash, created_at, expires_at, revoked_at`

func scanSession(r storage.Row) (Session, error) {
	var s Session
	var cr, ex int64
	var rv *int64
	err := r.Scan(&s.ID, &s.PrincipalID, &s.TokenHash, &cr, &ex, &rv)
	s.CreatedAt, s.ExpiresAt, s.RevokedAt = tm(cr), tm(ex), tmp(rv)
	return s, err
}

// Create inserts a session.
func (r *SessionRepo) Create(tx *storage.Tx, principalID, tokenHash string, expiresAt time.Time) (Session, error) {
	id, err := newID()
	if err != nil {
		return Session{}, err
	}
	now := ms(r.now())
	if _, err := tx.Exec(`INSERT INTO idn_session (id, principal_id, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		id, principalID, tokenHash, now, ms(expiresAt)); err != nil {
		return Session{}, err
	}
	return Session{ID: id, PrincipalID: principalID, TokenHash: tokenHash, CreatedAt: tm(now), ExpiresAt: tm(ms(expiresAt))}, nil
}

// Get returns one session.
func (r *SessionRepo) Get(ctx context.Context, rd *storage.Reader, id string) (Session, error) {
	return first[Session](rd.Query(ctx, `SELECT `+sessionCols+` FROM idn_session WHERE id = ?`, id))(scanSession)
}

// GetByTokenHash returns the session with a token hash.
func (r *SessionRepo) GetByTokenHash(ctx context.Context, rd *storage.Reader, tokenHash string) (Session, error) {
	return first[Session](rd.Query(ctx, `SELECT `+sessionCols+` FROM idn_session WHERE token_hash = ?`, tokenHash))(scanSession)
}

// ListByPrincipal returns a principal's sessions, newest first.
func (r *SessionRepo) ListByPrincipal(ctx context.Context, rd *storage.Reader, principalID string) ([]Session, error) {
	rows, err := rd.Query(ctx, `SELECT `+sessionCols+` FROM idn_session WHERE principal_id = ? ORDER BY created_at DESC, id DESC`, principalID)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanSession)
}

// Extend sets a later expiry.
func (r *SessionRepo) Extend(tx *storage.Tx, id string, expiresAt time.Time) error {
	return mustChange(tx.Exec(`UPDATE idn_session SET expires_at = ? WHERE id = ? AND revoked_at IS NULL`, ms(expiresAt), id))
}

// Revoke ends a session early.
func (r *SessionRepo) Revoke(tx *storage.Tx, id string) error {
	return mustChange(tx.Exec(`UPDATE idn_session SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, ms(r.now()), id))
}

// DeleteExpired removes sessions that expired before the cutoff and returns how many.
func (r *SessionRepo) DeleteExpired(tx *storage.Tx, before time.Time) (int64, error) {
	res, err := tx.Exec(`DELETE FROM idn_session WHERE expires_at < ?`, ms(before))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
