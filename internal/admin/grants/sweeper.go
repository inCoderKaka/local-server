package grants

import (
	"context"
	"encoding/json"
	"time"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// hasDeny reports whether a rule document has a top-level `deny` field. A document that cannot be
// read as a JSON object has none. Any `deny` field counts, whatever its value, so a rule that may
// be refusing requests on purpose is never switched off by the host.
func hasDeny(body string) bool {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return false
	}
	_, ok := doc["deny"]
	return ok
}

// DisableFailingRules switches off every enabled rule without `deny` whose failed evaluations
// have reached policy.rule.max_failures, and returns their ids (req_001 D-73, PLT-B-020). Each
// rule is one unit of work: the switch and a policy_rule.auto_disabled audit row (the system as
// actor; the rule id and version, the failure count and the limit, no name, no document). The
// host admin sees the policy.rule_invalid event the gateway raised when the rule first failed,
// and this row. The rule's failures are forgotten afterwards. A rule with `deny` is never
// switched off: a broken deny rule keeps refusing. With no RuleFailures it does nothing.
func (s *Service) DisableFailingRules(ctx context.Context) ([]string, error) {
	if s.cfg.RuleFailures == nil {
		return nil, nil
	}
	limit, err := s.cfg.Settings.Int(ctx, KeyRuleMaxFailures, "")
	if err != nil {
		return nil, err
	}
	rules, err := s.cfg.Grants.PolicyRules.ListEnabled(ctx, s.cfg.DB.Reader())
	if err != nil {
		return nil, err
	}
	var disabled []string
	for _, r := range rules {
		n := s.cfg.RuleFailures.RuleFailures(r.ID)
		if n < limit || hasDeny(r.Body) {
			continue
		}
		did := false
		var failures int64
		err := s.write(ctx, func(tx *storage.Tx) error {
			did = false
			cur, err := s.cfg.Grants.PolicyRules.GetTx(tx, r.ID)
			if err != nil {
				return err
			}
			if cur.DeletedAt != nil || !cur.Enabled || hasDeny(cur.Body) {
				return nil // changed since it was listed
			}
			if cur.VersionNo != r.VersionNo {
				return nil // edited since it was listed: the count belongs to an older document
			}
			m := s.cfg.RuleFailures.RuleFailures(r.ID)
			if m < limit {
				return nil // forgotten since it was counted
			}
			failures = m
			if err := s.cfg.Grants.PolicyRules.SetEnabled(tx, r.ID, false); err != nil {
				return err
			}
			did = true
			return s.record(tx, audit.Event{Action: ActionPolicyRuleDisabled,
				Detail: map[string]any{"rule_id": r.ID, "version": cur.VersionNo, "failures": m, "max_failures": limit}})
		})
		if err != nil {
			return disabled, wrap("disable rule", err)
		}
		if !did {
			continue
		}
		s.log.WarnContext(ctx, "admin grants: policy rule switched off after failed evaluations", "rule_id", r.ID, "failures", failures)
		s.cfg.RuleFailures.ClearRuleFailures(r.ID)
		disabled = append(disabled, r.ID)
	}
	return disabled, nil
}

// RunRuleSweeper calls DisableFailingRules every policy.rule.check_interval until ctx is done.
// The interval is read before every wait, so a change applies to the next one. A failure is
// logged and the loop goes on.
func (s *Service) RunRuleSweeper(ctx context.Context) {
	for {
		wait, err := s.cfg.Settings.Duration(ctx, KeyRuleCheckInterval, "")
		if err != nil || wait <= 0 {
			wait = defaultRuleCheckInterval
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		if _, err := s.DisableFailingRules(ctx); err != nil && ctx.Err() == nil {
			s.log.ErrorContext(ctx, "admin grants: policy rule check failed", "err", err)
		}
	}
}
