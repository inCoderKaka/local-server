package admin

import (
	"context"
	"slices"

	"github.com/go-faster/jx"
	"github.com/google/uuid"

	admingrants "github.com/inCoderKaka/local-server/internal/admin/grants"
	"github.com/inCoderKaka/local-server/internal/api"
)

// The policy rule operations are thin: package admin/grants owns the rules (req_001 D-87, D-73).
// The rule document is stored as the JSON it was sent in, keys sorted; the host checks it with the
// policy module and stores the format version that module reads, never one the caller gives.

// AdminListPolicyRules implements GET /admin/v1/policy-rules: every rule, or only one grant's own.
func (a *Admin) AdminListPolicyRules(ctx context.Context, params api.AdminListPolicyRulesParams) (*api.PolicyRuleList, error) {
	var grantID *string
	if v, ok := params.GrantID.Get(); ok {
		s := v.String()
		grantID = &s
	}
	rs, err := a.grs.ListRules(ctx, grantID)
	if err != nil {
		return nil, err
	}
	return ruleListOut(rs)
}

// AdminCreatePolicyRule implements POST /admin/v1/policy-rules: 201.
func (a *Admin) AdminCreatePolicyRule(ctx context.Context, req *api.PolicyRuleCreate) (*api.PolicyRule, error) {
	in := admingrants.NewRule{Body: ruleBody(req.Rule)}
	if v, ok := req.GrantID.Get(); ok {
		s := v.String()
		in.GrantID = &s
	}
	if v, ok := req.Name.Get(); ok {
		in.Name = &v
	}
	if v, ok := req.Position.Get(); ok {
		p := int64(v)
		in.Position = &p
	}
	r, err := a.grs.CreateRule(ctx, PrincipalIDOf(ctx), in)
	if err != nil {
		return nil, err
	}
	return ruleOut(r)
}

// AdminGetPolicyRule implements GET /admin/v1/policy-rules/{policy_rule_id}.
func (a *Admin) AdminGetPolicyRule(ctx context.Context, params api.AdminGetPolicyRuleParams) (*api.PolicyRule, error) {
	r, err := a.grs.GetRule(ctx, params.PolicyRuleID.String())
	if err != nil {
		return nil, err
	}
	return ruleOut(r)
}

// AdminReplacePolicyRule implements PUT /admin/v1/policy-rules/{policy_rule_id}: a new version.
func (a *Admin) AdminReplacePolicyRule(ctx context.Context, req *api.PolicyRuleUpdate, params api.AdminReplacePolicyRuleParams) (*api.PolicyRule, error) {
	ch := admingrants.RuleChange{Body: ruleBody(req.Rule)}
	if v, ok := req.Name.Get(); ok {
		ch.Name = &v
	}
	if v, ok := req.Position.Get(); ok {
		p := int64(v)
		ch.Position = &p
	}
	if v, ok := req.Enabled.Get(); ok {
		ch.Enabled = &v
	}
	r, err := a.grs.ReplaceRule(ctx, PrincipalIDOf(ctx), params.PolicyRuleID.String(), ch)
	if err != nil {
		return nil, err
	}
	return ruleOut(r)
}

// AdminDeletePolicyRule implements DELETE /admin/v1/policy-rules/{policy_rule_id}: 204.
func (a *Admin) AdminDeletePolicyRule(ctx context.Context, params api.AdminDeletePolicyRuleParams) error {
	return a.grs.DeleteRule(ctx, PrincipalIDOf(ctx), params.PolicyRuleID.String())
}

// AdminListPolicyRuleVersions implements GET /admin/v1/policy-rules/{policy_rule_id}/versions:
// newest first.
func (a *Admin) AdminListPolicyRuleVersions(ctx context.Context, params api.AdminListPolicyRuleVersionsParams) (*api.PolicyRuleList, error) {
	rs, err := a.grs.RuleVersions(ctx, params.PolicyRuleID.String())
	if err != nil {
		return nil, err
	}
	return ruleListOut(rs)
}

// ruleBody encodes a rule document with its keys in order, so the stored text does not depend on
// map order. Values are kept as they were sent.
func ruleBody[M ~map[string]jx.Raw](m M) []byte {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var e jx.Encoder
	e.ObjStart()
	for _, k := range keys {
		e.FieldStart(k)
		e.Raw(m[k])
	}
	e.ObjEnd()
	return e.Bytes()
}

func ruleListOut(rs []admingrants.PolicyRule) (*api.PolicyRuleList, error) {
	out := &api.PolicyRuleList{Data: make([]api.PolicyRule, 0, len(rs))}
	for _, r := range rs {
		o, err := ruleOut(r)
		if err != nil {
			return nil, err
		}
		out.Data = append(out.Data, *o)
	}
	return out, nil
}

func ruleOut(r admingrants.PolicyRule) (*api.PolicyRule, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, err
	}
	doc := api.PolicyRuleRule{}
	d := jx.DecodeStr(r.Body)
	// A stored document that is not an object (it cannot be one the host wrote) shows as empty.
	_ = d.ObjBytes(func(d *jx.Decoder, key []byte) error {
		raw, err := d.Raw()
		if err != nil {
			return err
		}
		doc[string(key)] = slices.Clone(raw)
		return nil
	})
	out := &api.PolicyRule{ID: id, Name: api.NewOptString(r.Name), Position: api.NewOptInt(int(r.Position)), Version: int(r.Version), Rule: doc, CreatedAt: r.CreatedAt.UTC()}
	if r.Enabled != nil {
		out.Enabled = api.NewOptBool(*r.Enabled)
	}
	if r.GrantID != nil {
		gid, err := uuid.Parse(*r.GrantID)
		if err != nil {
			return nil, err
		}
		out.GrantID = api.NewOptNilUUID(gid)
	} else {
		out.GrantID.SetToNull()
	}
	return out, nil
}
