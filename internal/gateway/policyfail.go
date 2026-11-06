package gateway

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/policy"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// ruleInvalidPayload carries the rule id and version, the failure kind and the failure count: no
// rule body, no name, no request content (req_001 D-31, D-73, D-87).
type ruleInvalidPayload struct {
	RuleID      string `json:"rule_id"`
	RuleVersion int64  `json:"rule_version"`
	Code        string `json:"code"`
	Failures    int64  `json:"failures"`
}

// noteRuleFailure handles an Evaluate error that means an enabled policy rule does not parse
// (req_001 D-73, PLT-B-020): it counts the failure, logs it, and emits policy.rule_invalid at
// most once per rule per gateway.rule_invalid_event_interval. Every other error is ignored.
// The request fails closed regardless. Disabling a broken non-deny rule in storage is
// PLT-F-026's job, which reads RuleFailures.
func (g *Gateway) noteRuleFailure(ctx context.Context, cfg config, err error) {
	var pe *policy.Error
	if !errors.As(err, &pe) || (pe.Code != policy.CodeInvalidRule && pe.Code != policy.CodeUnknownFormatVersion) {
		return
	}
	g.ruleFailuresCnt.Inc(string(pe.Code))
	now := g.now()
	g.mu.Lock()
	g.ruleFails[pe.RuleID]++
	failures := g.ruleFails[pe.RuleID]
	last, seen := g.ruleEventAt[pe.RuleID]
	emit := !seen || now.Sub(last) >= cfg.ruleInvalidInterval
	if emit {
		g.ruleEventAt[pe.RuleID] = now
	}
	g.mu.Unlock()
	g.log.WarnContext(ctx, "gateway: a policy rule failed to parse and refused a request", "rule_id", pe.RuleID, "rule_version", pe.RuleVersion, "code", string(pe.Code), "failures", failures)
	if !emit {
		return
	}
	payload, merr := json.Marshal(ruleInvalidPayload{RuleID: pe.RuleID, RuleVersion: pe.RuleVersion, Code: string(pe.Code), Failures: failures})
	if merr == nil {
		actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.meteringTimeout)
		defer cancel()
		merr = g.cfg.Events.Write(actx, g.cfg.DB, func(tx *storage.Tx) error {
			_, err := g.cfg.Events.Emit(tx, events.Event{Type: events.PolicyRuleInvalid, Payload: payload})
			return err
		})
	}
	if merr != nil {
		g.mu.Lock()
		if g.ruleEventAt[pe.RuleID].Equal(now) {
			delete(g.ruleEventAt, pe.RuleID) // not raised: the next failure tries again
		}
		g.mu.Unlock()
		g.log.ErrorContext(ctx, "gateway: policy.rule_invalid event not emitted", "rule_id", pe.RuleID, "err", merr)
	}
}

// RuleFailures returns how many evaluations a rule has failed by not parsing, since the
// process started or since ClearRuleFailures. PLT-F-026 compares it with
// policy.rule.max_failures (req_001 D-73).
func (g *Gateway) RuleFailures(ruleID string) int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.ruleFails[ruleID]
}

// ClearRuleFailures forgets a rule's failures, for when the rule was fixed or disabled.
func (g *Gateway) ClearRuleFailures(ruleID string) {
	g.mu.Lock()
	delete(g.ruleFails, ruleID)
	delete(g.ruleEventAt, ruleID)
	g.mu.Unlock()
}
