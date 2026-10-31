package grants

import (
	"context"
	"fmt"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Context-overflow behaviours (D-48).
const (
	OverflowRefuse   = "refuse"
	OverflowTruncate = "truncate"
)

// VersionModel is a grt_version_model row: one allowed model with its caps.
type VersionModel struct {
	ModelID         string
	ContextCap      *int64
	MaxOutputTokens *int64
}

// Version is an immutable grt_version row with its allowed models.
type Version struct {
	ID                  string
	GrantID             string
	VersionNo           int64
	QuotaTokensPerDay   *int64
	QuotaRequestsPerDay *int64
	MaxConcurrency      *int64
	ExpiresAt           *time.Time
	// AllowedProperties: nil (NULL) allows any request property; otherwise a JSON array of
	// property names, where `[]` allows none. Any other text, including an empty string, makes
	// every request on the grant fail with policy `invalid_grant` (I-113).
	AllowedProperties *string
	OnContextOverflow string
	Reason            *string
	CreatedBy         *string
	CreatedAt         time.Time
	Models            []VersionModel
}

// NewVersion is what Create stores. An empty OnContextOverflow means refuse (D-48).
type NewVersion struct {
	QuotaTokensPerDay   *int64
	QuotaRequestsPerDay *int64
	MaxConcurrency      *int64
	ExpiresAt           *time.Time
	// AllowedProperties: nil (NULL) allows any request property; otherwise a JSON array of
	// property names, where `[]` allows none. Any other text, including an empty string, makes
	// every request on the grant fail with policy `invalid_grant` (I-113).
	AllowedProperties *string
	OnContextOverflow string
	Reason            *string
	CreatedBy         *string
	Models            []VersionModel
}

// VersionRepo reads and writes grt_version and grt_version_model. There is no update: a
// version is immutable, and triggers refuse UPDATE and DELETE.
type VersionRepo struct{ now Clock }

const versionCols = `id, grant_id, version_no, quota_tokens_per_day, quota_requests_per_day, max_concurrency, expires_at, allowed_properties, on_context_overflow, reason, created_by, created_at`

func scanVersion(r storage.Row) (Version, error) {
	var v Version
	var ex *int64
	var cr int64
	err := r.Scan(&v.ID, &v.GrantID, &v.VersionNo, &v.QuotaTokensPerDay, &v.QuotaRequestsPerDay, &v.MaxConcurrency, &ex,
		&v.AllowedProperties, &v.OnContextOverflow, &v.Reason, &v.CreatedBy, &cr)
	v.ExpiresAt, v.CreatedAt = tmp(ex), tm(cr)
	return v, err
}

// Create makes the next version of a grant (number = highest + 1), stores its allowed
// models and moves the grant's current_version_id to it, all in the caller's unit of work.
// Earlier versions are not touched.
func (r *VersionRepo) Create(tx *storage.Tx, grantID string, n NewVersion) (Version, error) {
	id, err := newID()
	if err != nil {
		return Version{}, err
	}
	overflow := n.OnContextOverflow
	if overflow == "" {
		overflow = OverflowRefuse
	}
	var no int64
	rows, err := tx.Query(`SELECT COALESCE(MAX(version_no), 0) + 1 FROM grt_version WHERE grant_id = ?`, grantID)
	if err != nil {
		return Version{}, err
	}
	nos, err := collect(rows, func(r storage.Row) (int64, error) { var v int64; return v, r.Scan(&v) })
	if err != nil {
		return Version{}, err
	}
	no = nos[0]
	now := ms(r.now())
	if _, err := tx.Exec(`INSERT INTO grt_version (`+versionCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, grantID, no, n.QuotaTokensPerDay, n.QuotaRequestsPerDay, n.MaxConcurrency, msp(n.ExpiresAt),
		n.AllowedProperties, overflow, n.Reason, n.CreatedBy, now); err != nil {
		return Version{}, fmt.Errorf("grants: create version: %w", err)
	}
	for _, m := range n.Models {
		if _, err := tx.Exec(`INSERT INTO grt_version_model (version_id, model_id, context_cap, max_output_tokens) VALUES (?, ?, ?, ?)`,
			id, m.ModelID, m.ContextCap, m.MaxOutputTokens); err != nil {
			return Version{}, fmt.Errorf("grants: allow model %s: %w", m.ModelID, err)
		}
	}
	if err := mustChange(tx.Exec(`UPDATE grt_grant SET current_version_id = ?, updated_at = ? WHERE id = ?`, id, now, grantID)); err != nil {
		return Version{}, err
	}
	return Version{ID: id, GrantID: grantID, VersionNo: no, QuotaTokensPerDay: n.QuotaTokensPerDay, QuotaRequestsPerDay: n.QuotaRequestsPerDay,
		MaxConcurrency: n.MaxConcurrency, ExpiresAt: n.ExpiresAt, AllowedProperties: n.AllowedProperties, OnContextOverflow: overflow,
		Reason: n.Reason, CreatedBy: n.CreatedBy, CreatedAt: tm(now), Models: n.Models}, nil
}

func scanVersionModel(r storage.Row) (VersionModel, error) {
	var m VersionModel
	err := r.Scan(&m.ModelID, &m.ContextCap, &m.MaxOutputTokens)
	return m, err
}

func (r *VersionRepo) models(ctx context.Context, rd *storage.Reader, versionID string) ([]VersionModel, error) {
	rows, err := rd.Query(ctx, `SELECT model_id, context_cap, max_output_tokens FROM grt_version_model WHERE version_id = ? ORDER BY model_id`, versionID)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanVersionModel)
}

// Get returns one version with its allowed models.
func (r *VersionRepo) Get(ctx context.Context, rd *storage.Reader, id string) (Version, error) {
	v, err := first[Version](rd.Query(ctx, `SELECT `+versionCols+` FROM grt_version WHERE id = ?`, id))(scanVersion)
	if err != nil {
		return Version{}, err
	}
	v.Models, err = r.models(ctx, rd, id)
	return v, err
}

// ListByGrant returns every version of a grant, oldest first, with allowed models.
func (r *VersionRepo) ListByGrant(ctx context.Context, rd *storage.Reader, grantID string) ([]Version, error) {
	rows, err := rd.Query(ctx, `SELECT `+versionCols+` FROM grt_version WHERE grant_id = ? ORDER BY version_no`, grantID)
	if err != nil {
		return nil, err
	}
	vs, err := collect(rows, scanVersion)
	if err != nil {
		return nil, err
	}
	for i := range vs {
		if vs[i].Models, err = r.models(ctx, rd, vs[i].ID); err != nil {
			return nil, err
		}
	}
	return vs, nil
}

// Current returns the live version of a grant, or ErrNotFound if it has none.
func (r *VersionRepo) Current(ctx context.Context, rd *storage.Reader, grantID string) (Version, error) {
	v, err := first[Version](rd.Query(ctx, `SELECT v.id, v.grant_id, v.version_no, v.quota_tokens_per_day, v.quota_requests_per_day, v.max_concurrency, v.expires_at,
v.allowed_properties, v.on_context_overflow, v.reason, v.created_by, v.created_at
FROM grt_version v JOIN grt_grant g ON g.current_version_id = v.id WHERE g.id = ?`, grantID))(scanVersion)
	if err != nil {
		return Version{}, err
	}
	v.Models, err = r.models(ctx, rd, v.ID)
	return v, err
}
