package grants

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/policy"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// MaxRuleNameLength is the longest rule name, in characters (api/openapi.yaml, PolicyRuleCreate).
const MaxRuleNameLength = 120

// PolicyRule is a policy rule as the admin API shows it: one version of it. Body is the rule
// document as stored, JSON.
type PolicyRule struct {
	ID      string
	GrantID *string
	Name    string
	// Position is the evaluation order, lowest first.
	Position int64
	// Enabled is the rule's switch; nil for an older version, because the switch belongs to the
	// rule and not to a version.
	Enabled   *bool
	Version   int64
	Body      string
	CreatedAt time.Time
}

// NewRule is a rule to create. A nil Name means a name made from the rule's id; a nil Position
// means 0.
type NewRule struct {
	GrantID  *string
	Name     *string
	Position *int64
	Body     []byte
}

// RuleChange replaces a rule with a new version. A field left out keeps its current value; Body
// is the whole new document.
type RuleChange struct {
	Name     *string
	Position *int64
	Enabled  *bool
	Body     []byte
}

func actorPtr(actor string) *string { return ptrOrNil(actor) }

// checkRule validates a rule document with the policy module and returns the format version it
// found, which is the one stored: a caller never gives it (PLT-F-011). The refusal has fixed text
// chosen by the error's code, never the validator's message, which can quote the document.
func (s *Service) checkRule(ctx context.Context, body []byte) (int64, error) {
	lim, err := policy.LoadLimits(ctx, s.cfg.Settings)
	if err != nil {
		return 0, err
	}
	fv, err := policy.ValidateRule(body, lim.RuleMaxBytes)
	if err == nil {
		return fv, nil
	}
	var pe *policy.Error
	if errors.As(err, &pe) {
		if pe.Code == policy.CodeUnknownFormatVersion {
			return 0, invalid("rule.format_version", "This host does not know that rule format version.")
		}
		return 0, invalid("rule", "The rule is not valid under the policy rule format, or it is longer than this host allows.")
	}
	return 0, err
}

func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxRuleNameLength {
		return "", invalid("name", "The name must be 1 to 120 characters.")
	}
	return name, nil
}

func checkPosition(p int64) error {
	if p < 0 {
		return invalid("position", "The value is below the lowest allowed.")
	}
	return nil
}

func ruleView(r grants.PolicyRule) PolicyRule {
	en := r.Enabled
	return PolicyRule{ID: r.ID, GrantID: r.GrantID, Name: r.Name, Position: r.Position, Enabled: &en, Version: r.VersionNo, Body: r.Body, CreatedAt: r.VersionAt}
}

// GetRule returns the current version of a rule. A rule that does not exist or is deleted is not
// found.
func (s *Service) GetRule(ctx context.Context, id string) (PolicyRule, error) {
	r, err := s.cfg.Grants.PolicyRules.Get(ctx, s.cfg.DB.Reader(), id)
	if isNotFound(err) || (err == nil && r.DeletedAt != nil) {
		return PolicyRule{}, errNoRule
	}
	if err != nil {
		return PolicyRule{}, err
	}
	return ruleView(r), nil
}

// ListRules returns the rules that are not deleted, in evaluation order: every rule when grantID
// is nil, otherwise only that grant's own rules.
func (s *Service) ListRules(ctx context.Context, grantID *string) ([]PolicyRule, error) {
	rs, err := s.cfg.Grants.PolicyRules.ListScoped(ctx, s.cfg.DB.Reader(), grantID)
	if err != nil {
		return nil, err
	}
	out := make([]PolicyRule, len(rs))
	for i, r := range rs {
		out[i] = ruleView(r)
	}
	return out, nil
}

// RuleVersions returns every version of a rule, newest first. A rule that does not exist or is
// deleted is not found.
func (s *Service) RuleVersions(ctx context.Context, id string) ([]PolicyRule, error) {
	r, err := s.cfg.Grants.PolicyRules.Get(ctx, s.cfg.DB.Reader(), id)
	if isNotFound(err) || (err == nil && r.DeletedAt != nil) {
		return nil, errNoRule
	}
	if err != nil {
		return nil, err
	}
	vs, err := s.cfg.Grants.PolicyRules.Versions(ctx, s.cfg.DB.Reader(), id)
	if err != nil {
		return nil, err
	}
	out := make([]PolicyRule, len(vs))
	for i, v := range vs {
		out[i] = PolicyRule{ID: r.ID, GrantID: r.GrantID, Name: v.Name, Position: v.Position, Version: v.VersionNo, Body: v.Body, CreatedAt: v.CreatedAt}
		if v.VersionNo == r.VersionNo {
			en := r.Enabled
			out[i].Enabled = &en
		}
	}
	return out, nil
}

// CreateRule validates and stores a new rule, enabled, as version 1, and audits it. A grant id
// must be an existing grant.
func (s *Service) CreateRule(ctx context.Context, actor string, in NewRule) (PolicyRule, error) {
	fv, err := s.checkRule(ctx, in.Body)
	if err != nil {
		return PolicyRule{}, err
	}
	var position int64
	if in.Position != nil {
		if err := checkPosition(*in.Position); err != nil {
			return PolicyRule{}, err
		}
		position = *in.Position
	}
	name := ""
	if in.Name != nil {
		if name, err = checkName(*in.Name); err != nil {
			return PolicyRule{}, err
		}
	}
	if in.GrantID != nil {
		if _, err := s.cfg.Grants.Grants.Get(ctx, s.cfg.DB.Reader(), *in.GrantID); isNotFound(err) {
			return PolicyRule{}, invalid("grant_id", "No such grant.")
		} else if err != nil {
			return PolicyRule{}, err
		}
	}
	var created grants.PolicyRule
	err = s.write(ctx, func(tx *storage.Tx) error {
		var err error
		// No name given: the repository names the rule by its own id, which is unique and in no language.
		created, err = s.cfg.Grants.PolicyRules.CreateBy(tx, actorPtr(actor), in.GrantID, name, position, fv, string(in.Body))
		if err != nil {
			return err
		}
		return s.record(tx, audit.Event{Action: ActionPolicyRuleCreated, ActorID: actor,
			Detail: map[string]any{"rule_id": created.ID, "grant_id": in.GrantID, "version": created.VersionNo}})
	})
	if err != nil {
		return PolicyRule{}, wrap("create rule", err)
	}
	return ruleView(created), nil
}

// ReplaceRule stores a new immutable version of a rule (when its name, order or document differ
// from the current one), applies the switch, and audits it. After the commit it forgets the
// rule's failed evaluations when its document changed or the rule is off (req_001 D-73).
func (s *Service) ReplaceRule(ctx context.Context, actor, id string, ch RuleChange) (PolicyRule, error) {
	cur, err := s.cfg.Grants.PolicyRules.Get(ctx, s.cfg.DB.Reader(), id)
	if isNotFound(err) || (err == nil && cur.DeletedAt != nil) {
		return PolicyRule{}, errNoRule
	}
	if err != nil {
		return PolicyRule{}, err
	}
	fv, err := s.checkRule(ctx, ch.Body)
	if err != nil {
		return PolicyRule{}, err
	}
	name, position, enabled := cur.Name, cur.Position, cur.Enabled
	if ch.Name != nil {
		if name, err = checkName(*ch.Name); err != nil {
			return PolicyRule{}, err
		}
	}
	if ch.Position != nil {
		if err := checkPosition(*ch.Position); err != nil {
			return PolicyRule{}, err
		}
		position = *ch.Position
	}
	if ch.Enabled != nil {
		enabled = *ch.Enabled
	}
	var out grants.PolicyRule
	err = s.write(ctx, func(tx *storage.Tx) error {
		no, err := s.cfg.Grants.PolicyRules.UpdateBy(tx, actorPtr(actor), id, name, position, fv, string(ch.Body), enabled)
		if err != nil {
			return err
		}
		if out, err = s.cfg.Grants.PolicyRules.GetTx(tx, id); err != nil {
			return err
		}
		return s.record(tx, audit.Event{Action: ActionPolicyRuleUpdated, ActorID: actor,
			Detail: map[string]any{"rule_id": id, "version": no, "enabled": enabled}})
	})
	if isNotFound(err) {
		return PolicyRule{}, errNoRule
	}
	if err != nil {
		return PolicyRule{}, wrap("replace rule", err)
	}
	if s.cfg.RuleFailures != nil && (cur.Body != string(ch.Body) || !enabled) {
		s.cfg.RuleFailures.ClearRuleFailures(id)
	}
	return ruleView(out), nil
}

// DeleteRule soft-deletes a rule: it stops applying at once, and its versions stay.
func (s *Service) DeleteRule(ctx context.Context, actor, id string) error {
	err := s.write(ctx, func(tx *storage.Tx) error {
		if err := s.cfg.Grants.PolicyRules.Delete(tx, id); err != nil {
			return err
		}
		return s.record(tx, audit.Event{Action: ActionPolicyRuleDeleted, ActorID: actor, Detail: map[string]any{"rule_id": id}})
	})
	if isNotFound(err) {
		return errNoRule
	}
	if err != nil {
		return wrap("delete rule", err)
	}
	if s.cfg.RuleFailures != nil {
		s.cfg.RuleFailures.ClearRuleFailures(id)
	}
	return nil
}
