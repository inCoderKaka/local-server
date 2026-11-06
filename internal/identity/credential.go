package identity

import (
	"context"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Credential kinds.
const (
	CredentialPassword = "password"
	CredentialPasskey  = "passkey"
)

// Credential is an idn_credential row. Secret is a password hash or a passkey public key,
// as given by the auth module; this package does not hash.
type Credential struct {
	ID          string
	PrincipalID string
	Kind        string
	Identifier  string
	Secret      string
	CreatedAt   time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
}

// CredentialRepo reads and writes idn_credential.
type CredentialRepo struct{ now Clock }

const credCols = `id, principal_id, kind, identifier, secret, created_at, last_used_at, revoked_at`

func scanCredential(r storage.Row) (Credential, error) {
	var c Credential
	var cr int64
	var lu, rv *int64
	err := r.Scan(&c.ID, &c.PrincipalID, &c.Kind, &c.Identifier, &c.Secret, &cr, &lu, &rv)
	c.CreatedAt, c.LastUsedAt, c.RevokedAt = tm(cr), tmp(lu), tmp(rv)
	return c, err
}

// Create inserts a credential.
func (r *CredentialRepo) Create(tx *storage.Tx, principalID, kind, identifier, secret string) (Credential, error) {
	id, err := newID()
	if err != nil {
		return Credential{}, err
	}
	now := ms(r.now())
	if _, err := tx.Exec(`INSERT INTO idn_credential (id, principal_id, kind, identifier, secret, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, principalID, kind, identifier, secret, now); err != nil {
		return Credential{}, err
	}
	return Credential{ID: id, PrincipalID: principalID, Kind: kind, Identifier: identifier, Secret: secret, CreatedAt: tm(now)}, nil
}

// Get returns one credential.
func (r *CredentialRepo) Get(ctx context.Context, rd *storage.Reader, id string) (Credential, error) {
	return first[Credential](rd.Query(ctx, `SELECT `+credCols+` FROM idn_credential WHERE id = ?`, id))(scanCredential)
}

// GetByIdentifier returns the credential of a kind with a login identifier.
func (r *CredentialRepo) GetByIdentifier(ctx context.Context, rd *storage.Reader, kind, identifier string) (Credential, error) {
	return first[Credential](rd.Query(ctx, `SELECT `+credCols+` FROM idn_credential WHERE kind = ? AND identifier = ?`, kind, identifier))(scanCredential)
}

// ListByPrincipal returns a principal's credentials, oldest first.
func (r *CredentialRepo) ListByPrincipal(ctx context.Context, rd *storage.Reader, principalID string) ([]Credential, error) {
	rows, err := rd.Query(ctx, `SELECT `+credCols+` FROM idn_credential WHERE principal_id = ? ORDER BY created_at, id`, principalID)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanCredential)
}

// ListByPrincipalTx is ListByPrincipal as the unit of work sees it.
func (r *CredentialRepo) ListByPrincipalTx(tx *storage.Tx, principalID string) ([]Credential, error) {
	rows, err := tx.Query(`SELECT `+credCols+` FROM idn_credential WHERE principal_id = ? ORDER BY created_at, id`, principalID)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanCredential)
}

// UpdateSecret replaces the stored hash or public key.
func (r *CredentialRepo) UpdateSecret(tx *storage.Tx, id, secret string) error {
	return mustChange(tx.Exec(`UPDATE idn_credential SET secret = ? WHERE id = ?`, secret, id))
}

// MarkUsed records a successful login.
func (r *CredentialRepo) MarkUsed(tx *storage.Tx, id string) error {
	return mustChange(tx.Exec(`UPDATE idn_credential SET last_used_at = ? WHERE id = ?`, ms(r.now()), id))
}

// Revoke ends a credential. It stays for history.
func (r *CredentialRepo) Revoke(tx *storage.Tx, id string) error {
	return mustChange(tx.Exec(`UPDATE idn_credential SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, ms(r.now()), id))
}
