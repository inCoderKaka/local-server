package grants

import (
	"context"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// PolicyRule is a grt_policy_rule row. GrantID nil means the rule is global (D-25).
type PolicyRule struct {
	ID            string
	GrantID       *string
	Name          string
	Position      int64
	FormatVersion int64
	Body          string
	Enabled       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PolicyRuleRepo reads and writes grt_policy_rule. Evaluating rules is the policy module's job.
type PolicyRuleRepo struct{ now Clock }

const ruleCols = `id, grant_id, name, position, format_version, body, enabled, created_at, updated_at`

func scanRule(r storage.Row) (PolicyRule, error) {
	var p PolicyRule
	var en, c, u int64
	err := r.Scan(&p.ID, &p.GrantID, &p.Name, &p.Position, &p.FormatVersion, &p.Body, &en, &c, &u)
	p.Enabled, p.CreatedAt, p.UpdatedAt = en == 1, tm(c), tm(u)
	return p, err
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// Create inserts an enabled rule.
func (r *PolicyRuleRepo) Create(tx *storage.Tx, grantID *string, name string, position, formatVersion int64, body string) (PolicyRule, error) {
	id, err := newID()
	if err != nil {
		return PolicyRule{}, err
	}
	now := ms(r.now())
	if _, err := tx.Exec(`INSERT INTO grt_policy_rule (`+ruleCols+`) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		id, grantID, name, position, formatVersion, body, now, now); err != nil {
		return PolicyRule{}, err
	}
	return PolicyRule{ID: id, GrantID: grantID, Name: name, Position: position, FormatVersion: formatVersion, Body: body, Enabled: true, CreatedAt: tm(now), UpdatedAt: tm(now)}, nil
}

// Get returns one rule.
func (r *PolicyRuleRepo) Get(ctx context.Context, rd *storage.Reader, id string) (PolicyRule, error) {
	return first[PolicyRule](rd.Query(ctx, `SELECT `+ruleCols+` FROM grt_policy_rule WHERE id = ?`, id))(scanRule)
}

// List returns the rules that apply to a grant: its own and the global ones, in evaluation
// order. A nil grantID returns only the global rules.
func (r *PolicyRuleRepo) List(ctx context.Context, rd *storage.Reader, grantID *string) ([]PolicyRule, error) {
	rows, err := rd.Query(ctx, `SELECT `+ruleCols+` FROM grt_policy_rule WHERE grant_id IS NULL OR grant_id = ? ORDER BY position, created_at, id`, grantID)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanRule)
}

// Update changes a rule's name, order, format version, body and switch.
func (r *PolicyRuleRepo) Update(tx *storage.Tx, id, name string, position, formatVersion int64, body string, enabled bool) error {
	return mustChange(tx.Exec(`UPDATE grt_policy_rule SET name = ?, position = ?, format_version = ?, body = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		name, position, formatVersion, body, b2i(enabled), ms(r.now()), id))
}

// Delete removes a rule.
func (r *PolicyRuleRepo) Delete(tx *storage.Tx, id string) error {
	return mustChange(tx.Exec(`DELETE FROM grt_policy_rule WHERE id = ?`, id))
}
