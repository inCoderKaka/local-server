package gateway

import (
	"context"

	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/policy"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// ruleSource reads the stored policy rules of a grant for the policy module.
type ruleSource struct {
	db    *storage.DB
	rules *grants.PolicyRuleRepo
}

// NewRuleSource returns the policy.RuleSource over the stored rules: a grant's own rules and
// the global ones (grants owns their storage; policy owns the rule format).
func NewRuleSource(db *storage.DB, repos *grants.Repos) policy.RuleSource {
	return ruleSource{db: db, rules: repos.PolicyRules}
}

func (s ruleSource) Rules(ctx context.Context, grantID string) ([]policy.StoredRule, error) {
	rows, err := s.rules.List(ctx, s.db.Reader(), &grantID)
	if err != nil {
		return nil, err
	}
	out := make([]policy.StoredRule, len(rows))
	for i, r := range rows {
		out[i] = policy.StoredRule{ID: r.ID, GrantID: r.GrantID, Name: r.Name, Position: r.Position,
			FormatVersion: r.FormatVersion, Body: r.Body, Enabled: r.Enabled}
	}
	return out, nil
}
