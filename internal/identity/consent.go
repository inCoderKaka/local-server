package identity

import (
	"context"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Consent is an idn_consent row (D-44, D-45).
type Consent struct {
	ID            string
	PrincipalID   string
	Purpose       string
	PolicyVersion string
	GrantedAt     time.Time
	WithdrawnAt   *time.Time
	Evidence      *string
}

// ConsentRepo reads and writes idn_consent.
type ConsentRepo struct{ now Clock }

const consentCols = `id, principal_id, purpose, policy_version, granted_at, withdrawn_at, evidence`

func scanConsent(r storage.Row) (Consent, error) {
	var c Consent
	var g int64
	var w *int64
	err := r.Scan(&c.ID, &c.PrincipalID, &c.Purpose, &c.PolicyVersion, &g, &w, &c.Evidence)
	c.GrantedAt, c.WithdrawnAt = tm(g), tmp(w)
	return c, err
}

// Record stores a consent given now.
func (r *ConsentRepo) Record(tx *storage.Tx, ref ConsentRef, evidence *string) (Consent, error) {
	id, err := newID()
	if err != nil {
		return Consent{}, err
	}
	now := ms(r.now())
	if _, err := tx.Exec(`INSERT INTO idn_consent (id, principal_id, purpose, policy_version, granted_at, evidence) VALUES (?, ?, ?, ?, ?, ?)`,
		id, ref.PrincipalID, ref.Purpose, ref.PolicyVersion, now, evidence); err != nil {
		return Consent{}, err
	}
	return Consent{ID: id, PrincipalID: ref.PrincipalID, Purpose: ref.Purpose, PolicyVersion: ref.PolicyVersion, GrantedAt: tm(now), Evidence: evidence}, nil
}

// Get returns one consent.
func (r *ConsentRepo) Get(ctx context.Context, rd *storage.Reader, id string) (Consent, error) {
	return first[Consent](rd.Query(ctx, `SELECT `+consentCols+` FROM idn_consent WHERE id = ?`, id))(scanConsent)
}

// ListByPrincipal returns a principal's consents, oldest first.
func (r *ConsentRepo) ListByPrincipal(ctx context.Context, rd *storage.Reader, principalID string) ([]Consent, error) {
	rows, err := rd.Query(ctx, `SELECT `+consentCols+` FROM idn_consent WHERE principal_id = ? ORDER BY granted_at, id`, principalID)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanConsent)
}

// Find returns the unwithdrawn consent that matches the reference, or ErrNoConsent (D-45).
func (r *ConsentRepo) Find(ctx context.Context, rd *storage.Reader, ref ConsentRef) (Consent, error) {
	c, err := first[Consent](rd.Query(ctx, `SELECT `+consentCols+` FROM idn_consent
WHERE principal_id = ? AND purpose = ? AND policy_version = ? AND withdrawn_at IS NULL ORDER BY granted_at DESC, id DESC LIMIT 1`,
		ref.PrincipalID, ref.Purpose, ref.PolicyVersion))(scanConsent)
	if err == ErrNotFound {
		return Consent{}, ErrNoConsent
	}
	return c, err
}

// Withdraw marks a consent withdrawn. The record stays.
func (r *ConsentRepo) Withdraw(tx *storage.Tx, id string) error {
	return mustChange(tx.Exec(`UPDATE idn_consent SET withdrawn_at = ? WHERE id = ? AND withdrawn_at IS NULL`, ms(r.now()), id))
}
