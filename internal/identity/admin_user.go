package identity

import (
	"context"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// AdminUser is an idn_admin_user row.
type AdminUser struct {
	ID          string
	PrincipalID string
	Username    string
	DisplayName *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// AdminUserRepo reads and writes idn_admin_user.
type AdminUserRepo struct{ now Clock }

const adminCols = `id, principal_id, username, display_name, created_at, updated_at`

func scanAdmin(r storage.Row) (AdminUser, error) {
	var a AdminUser
	var c, u int64
	err := r.Scan(&a.ID, &a.PrincipalID, &a.Username, &a.DisplayName, &c, &u)
	a.CreatedAt, a.UpdatedAt = tm(c), tm(u)
	return a, err
}

// Create inserts the admin user of an existing principal. It fails if the principal does not
// exist or already has an admin user row (principal_id is UNIQUE, D-53).
func (r *AdminUserRepo) Create(tx *storage.Tx, principalID, username string, displayName *string) (AdminUser, error) {
	id, err := newID()
	if err != nil {
		return AdminUser{}, err
	}
	now := ms(r.now())
	if _, err := tx.Exec(`INSERT INTO idn_admin_user (id, principal_id, username, display_name, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, principalID, username, displayName, now, now); err != nil {
		return AdminUser{}, err
	}
	return AdminUser{ID: id, PrincipalID: principalID, Username: username, DisplayName: displayName, CreatedAt: tm(now), UpdatedAt: tm(now)}, nil
}

// Get returns one admin user.
func (r *AdminUserRepo) Get(ctx context.Context, rd *storage.Reader, id string) (AdminUser, error) {
	return first[AdminUser](rd.Query(ctx, `SELECT `+adminCols+` FROM idn_admin_user WHERE id = ?`, id))(scanAdmin)
}

// GetByUsername returns the admin user with a login name.
func (r *AdminUserRepo) GetByUsername(ctx context.Context, rd *storage.Reader, username string) (AdminUser, error) {
	return first[AdminUser](rd.Query(ctx, `SELECT `+adminCols+` FROM idn_admin_user WHERE username = ?`, username))(scanAdmin)
}

// GetByPrincipal returns the admin user of a principal.
func (r *AdminUserRepo) GetByPrincipal(ctx context.Context, rd *storage.Reader, principalID string) (AdminUser, error) {
	return first[AdminUser](rd.Query(ctx, `SELECT `+adminCols+` FROM idn_admin_user WHERE principal_id = ?`, principalID))(scanAdmin)
}

// List returns every admin user, oldest first.
func (r *AdminUserRepo) List(ctx context.Context, rd *storage.Reader) ([]AdminUser, error) {
	rows, err := rd.Query(ctx, `SELECT `+adminCols+` FROM idn_admin_user ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanAdmin)
}

// Update changes the display name.
func (r *AdminUserRepo) Update(tx *storage.Tx, id string, displayName *string) error {
	return mustChange(tx.Exec(`UPDATE idn_admin_user SET display_name = ?, updated_at = ? WHERE id = ?`, displayName, ms(r.now()), id))
}
