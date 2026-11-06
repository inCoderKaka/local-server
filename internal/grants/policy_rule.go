package grants

import (
	"context"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// PolicyRule is a grt_policy_rule row, the head of a rule: its scope, its switch and a copy of
// its live version (req_001 D-25, D-87). GrantID nil means the rule is global.
type PolicyRule struct {
	ID            string
	GrantID       *string
	Name          string
	Position      int64
	FormatVersion int64
	Body          string
	Enabled       bool
	// VersionNo is the live version, in grt_policy_rule_version. Name, Position, FormatVersion
	// and Body are that version's.
	VersionNo int64
	// VersionAt is when the live version was stored.
	VersionAt time.Time
	// DeletedAt is set once the rule is deleted (soft delete): it no longer applies and is not
	// listed, but its versions stay.
	DeletedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PolicyRuleVersion is an immutable grt_policy_rule_version row.
type PolicyRuleVersion struct {
	RuleID        string
	VersionNo     int64
	Name          string
	Position      int64
	FormatVersion int64
	Body          string
	CreatedBy     *string
	CreatedAt     time.Time
}

// PolicyRuleRepo reads and writes grt_policy_rule and grt_policy_rule_version. Evaluating rules
// is the policy module's job.
type PolicyRuleRepo struct{ now Clock }

const ruleCols = `r.id, r.grant_id, r.name, r.position, r.format_version, r.body, r.enabled, r.version_no,
COALESCE((SELECT v.created_at FROM grt_policy_rule_version v WHERE v.rule_id = r.id AND v.version_no = r.version_no), r.updated_at),
r.deleted_at, r.created_at, r.updated_at`

func scanRule(r storage.Row) (PolicyRule, error) {
	var p PolicyRule
	var en, va, c, u int64
	var del *int64
	err := r.Scan(&p.ID, &p.GrantID, &p.Name, &p.Position, &p.FormatVersion, &p.Body, &en, &p.VersionNo, &va, &del, &c, &u)
	p.Enabled, p.VersionAt, p.DeletedAt, p.CreatedAt, p.UpdatedAt = en == 1, tm(va), tmp(del), tm(c), tm(u)
	return p, err
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// Create inserts an enabled rule as version 1, made by nobody in particular.
func (r *PolicyRuleRepo) Create(tx *storage.Tx, grantID *string, name string, position, formatVersion int64, body string) (PolicyRule, error) {
	return r.CreateBy(tx, nil, grantID, name, position, formatVersion, body)
}

// CreateBy inserts an enabled rule as version 1, made by the principal actor (nil for the system).
func (r *PolicyRuleRepo) CreateBy(tx *storage.Tx, actor, grantID *string, name string, position, formatVersion int64, body string) (PolicyRule, error) {
	id, err := newID()
	if err != nil {
		return PolicyRule{}, err
	}
	if name == "" {
		name = id // a rule given no name is named by its id
	}
	now := ms(r.now())
	if _, err := tx.Exec(`INSERT INTO grt_policy_rule (id, grant_id, name, position, format_version, body, enabled, version_no, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 1, 1, ?, ?)`,
		id, grantID, name, position, formatVersion, body, now, now); err != nil {
		return PolicyRule{}, err
	}
	if err := insertRuleVersion(tx, id, 1, name, position, formatVersion, body, actor, now); err != nil {
		return PolicyRule{}, err
	}
	return PolicyRule{ID: id, GrantID: grantID, Name: name, Position: position, FormatVersion: formatVersion, Body: body, Enabled: true,
		VersionNo: 1, VersionAt: tm(now), CreatedAt: tm(now), UpdatedAt: tm(now)}, nil
}

func insertRuleVersion(tx *storage.Tx, id string, no int64, name string, position, formatVersion int64, body string, actor *string, now int64) error {
	_, err := tx.Exec(`INSERT INTO grt_policy_rule_version (rule_id, version_no, name, position, format_version, body, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, no, name, position, formatVersion, body, actor, now)
	return err
}

// Get returns one rule, deleted or not; a caller that must not see a deleted rule checks
// DeletedAt.
func (r *PolicyRuleRepo) Get(ctx context.Context, rd *storage.Reader, id string) (PolicyRule, error) {
	return first[PolicyRule](rd.Query(ctx, `SELECT `+ruleCols+` FROM grt_policy_rule r WHERE r.id = ?`, id))(scanRule)
}

// GetTx is Get inside a unit of work.
func (r *PolicyRuleRepo) GetTx(tx *storage.Tx, id string) (PolicyRule, error) {
	return first[PolicyRule](tx.Query(`SELECT `+ruleCols+` FROM grt_policy_rule r WHERE r.id = ?`, id))(scanRule)
}

// List returns the rules that apply to a grant: its own and the global ones that are not
// deleted, in evaluation order. A nil grantID returns only the global rules.
func (r *PolicyRuleRepo) List(ctx context.Context, rd *storage.Reader, grantID *string) ([]PolicyRule, error) {
	rows, err := rd.Query(ctx, `SELECT `+ruleCols+` FROM grt_policy_rule r WHERE r.deleted_at IS NULL AND (r.grant_id IS NULL OR r.grant_id = ?) ORDER BY r.position, r.created_at, r.id`, grantID)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanRule)
}

// ListScoped is the list the admin sees: with a nil grantID every rule that is not deleted, global
// and per grant; with a grantID only the rules of that grant itself. Evaluation order.
func (r *PolicyRuleRepo) ListScoped(ctx context.Context, rd *storage.Reader, grantID *string) ([]PolicyRule, error) {
	rows, err := rd.Query(ctx, `SELECT `+ruleCols+` FROM grt_policy_rule r WHERE r.deleted_at IS NULL AND (? IS NULL OR r.grant_id = ?) ORDER BY r.position, r.created_at, r.id`, grantID, grantID)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanRule)
}

// ListEnabled returns every enabled rule that is not deleted.
func (r *PolicyRuleRepo) ListEnabled(ctx context.Context, rd *storage.Reader) ([]PolicyRule, error) {
	rows, err := rd.Query(ctx, `SELECT `+ruleCols+` FROM grt_policy_rule r WHERE r.deleted_at IS NULL AND r.enabled = 1 ORDER BY r.position, r.created_at, r.id`)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanRule)
}

// Update changes a rule's name, order, format version, body and switch, made by nobody in
// particular. See UpdateBy.
func (r *PolicyRuleRepo) Update(tx *storage.Tx, id, name string, position, formatVersion int64, body string, enabled bool) error {
	_, err := r.UpdateBy(tx, nil, id, name, position, formatVersion, body, enabled)
	return err
}

// UpdateBy changes a rule on behalf of the principal actor (nil for the system). When the name,
// order, format version or body differ from the live version it stores a new immutable version
// (number + 1) and copies it to the head; the switch always changes on the head and never makes a
// version. It returns the live version number afterwards. A deleted rule is ErrNotFound.
func (r *PolicyRuleRepo) UpdateBy(tx *storage.Tx, actor *string, id, name string, position, formatVersion int64, body string, enabled bool) (int64, error) {
	cur, err := r.GetTx(tx, id)
	if err != nil {
		return 0, err
	}
	if cur.DeletedAt != nil {
		return 0, ErrNotFound
	}
	now := ms(r.now())
	no := cur.VersionNo
	if cur.Name != name || cur.Position != position || cur.FormatVersion != formatVersion || cur.Body != body {
		no++
		if err := insertRuleVersion(tx, id, no, name, position, formatVersion, body, actor, now); err != nil {
			return 0, err
		}
	}
	return no, mustChange(tx.Exec(`UPDATE grt_policy_rule SET name = ?, position = ?, format_version = ?, body = ?, enabled = ?, version_no = ?, updated_at = ? WHERE id = ?`,
		name, position, formatVersion, body, b2i(enabled), no, now, id))
}

// SetEnabled switches a rule on or off without making a version. A deleted rule is ErrNotFound.
func (r *PolicyRuleRepo) SetEnabled(tx *storage.Tx, id string, enabled bool) error {
	return mustChange(tx.Exec(`UPDATE grt_policy_rule SET enabled = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, b2i(enabled), ms(r.now()), id))
}

// Delete soft-deletes a rule: it stops applying and is no longer listed, and its versions stay.
// An already deleted rule is ErrNotFound.
func (r *PolicyRuleRepo) Delete(tx *storage.Tx, id string) error {
	now := ms(r.now())
	return mustChange(tx.Exec(`UPDATE grt_policy_rule SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, now, now, id))
}

const ruleVersionCols = `rule_id, version_no, name, position, format_version, body, created_by, created_at`

func scanRuleVersion(r storage.Row) (PolicyRuleVersion, error) {
	var v PolicyRuleVersion
	var c int64
	err := r.Scan(&v.RuleID, &v.VersionNo, &v.Name, &v.Position, &v.FormatVersion, &v.Body, &v.CreatedBy, &c)
	v.CreatedAt = tm(c)
	return v, err
}

// Versions returns every version of a rule, newest first.
func (r *PolicyRuleRepo) Versions(ctx context.Context, rd *storage.Reader, id string) ([]PolicyRuleVersion, error) {
	rows, err := rd.Query(ctx, `SELECT `+ruleVersionCols+` FROM grt_policy_rule_version WHERE rule_id = ? ORDER BY version_no DESC`, id)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanRuleVersion)
}
