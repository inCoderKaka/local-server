package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// Policy is the policy decision port (D-25, D-39 step 3). It decides only; the gateway
// enforces the decision. An expression-language engine can replace the rule engine behind it.
type Policy interface {
	// Evaluate decides one request. A nil error means the request is allowed; a refusal or a
	// malformed input is an *Error.
	Evaluate(ctx context.Context, in Input) (Decision, error)
}

// Input is everything one decision depends on.
type Input struct {
	// Properties are the LS-Request-Properties field lines as received (for example
	// http.Header.Values(HeaderName)); none means the request has no properties.
	Properties []string
	// Model is the engine model name the request asks for.
	Model string
	// Time is when the request is decided; the policy never reads a clock.
	Time time.Time
	// Grant is the caller's grant in force, after the access key's narrowing.
	Grant Grant
}

// Grant is what the policy needs from the caller's grant version (D-25).
type Grant struct {
	// ID is the grant's id. Rules scoped to it apply, with the global ones.
	ID string
	// AllowedProperties is the grant version's allowed_properties text: a JSON array of
	// property names. nil means any property is allowed; "[]" means none.
	AllowedProperties *string
	// Models are the models the grant allows, by engine model name, with their caps.
	Models []GrantModel
	// MaxConcurrency is the grant's concurrency limit; nil means no limit from the grant.
	MaxConcurrency *int64
}

// GrantModel is one model a grant allows. A nil cap means no cap from the grant.
type GrantModel struct {
	Name            string
	ContextCap      *int64
	MaxOutputTokens *int64
}

// PriorityClass is interactive (someone is waiting) or background (can wait) (D-26).
type PriorityClass string

// The priority classes.
const (
	Interactive PriorityClass = "interactive"
	Background  PriorityClass = "background"
)

// Caps are the limits that apply to one request, after the grant and every matching rule. A
// nil value means no limit was set; the caller applies its own default.
type Caps struct {
	ContextCap      *int64
	MaxOutputTokens *int64
	MaxConcurrency  *int64
}

// Decision is an allowed request's priority class and caps.
type Decision struct {
	Priority PriorityClass
	Caps     Caps
}

// StoredRule is one stored policy rule as the rule source returns it (grants owns the
// grt_policy_rule table). GrantID nil means the rule is global.
type StoredRule struct {
	ID            string
	GrantID       *string
	Name          string
	Position      int64
	FormatVersion int64
	Body          string
	Enabled       bool
}

// RuleSource loads the stored rules that apply to a grant: its own and the global ones.
type RuleSource interface {
	Rules(ctx context.Context, grantID string) ([]StoredRule, error)
}

// Limits are the policy's input bounds, read from settings (D-54).
type Limits struct {
	Properties   PropertyLimits
	RuleMaxBytes int
}

// Decide is the pure rule engine: it depends only on its arguments. It checks, in order: the
// properties header, the model against the grant, each property against the grant's allowed
// properties, then every enabled rule that applies to in.Grant.ID (global or the grant's own),
// in Position order (the order given breaks ties).
//
// Rules combine as follows. A matching rule with deny refuses the request (CodeDenied); a
// matching rule with windows refuses it outside all of them (CodeOutsideWindow). Refusals do
// not depend on order, and the first refusing rule is reported. Caps take the smallest value
// among the grant and every matching rule, so a rule can only narrow. The priority class is
// set by the first matching rule that sets one; with none it is interactive. Any enabled rule
// that does not parse fails the decision (CodeInvalidRule or CodeUnknownFormatVersion), so a
// broken rule never lets a request through.
func Decide(rules []StoredRule, in Input, lim Limits) (Decision, error) {
	props, err := ParseProperties(in.Properties, lim.Properties)
	if err != nil {
		return Decision{}, err
	}
	gm, ok := grantModel(in.Grant, in.Model)
	if !ok {
		return Decision{}, &Error{Code: CodeModelNotGranted, Message: "the model is not in the grant"}
	}
	if err := checkAllowed(in.Grant.AllowedProperties, props); err != nil {
		return Decision{}, err
	}
	parsed, err := applicable(rules, in.Grant.ID, lim.RuleMaxBytes)
	if err != nil {
		return Decision{}, err
	}
	dec := Decision{Caps: Caps{ContextCap: gm.ContextCap, MaxOutputTokens: gm.MaxOutputTokens, MaxConcurrency: in.Grant.MaxConcurrency}}
	for _, r := range parsed {
		if !r.matches(props, in.Model) {
			continue
		}
		if r.deny {
			return Decision{}, &Error{Code: CodeDenied, RuleID: r.id, RuleName: r.name, Message: "a rule refuses this request"}
		}
		if r.windows != nil && !r.inWindows(in.Time) {
			return Decision{}, &Error{Code: CodeOutsideWindow, RuleID: r.id, RuleName: r.name, Opens: r.nextOpen(in.Time),
				Message: "this request is allowed only inside the rule's time windows"}
		}
		if dec.Priority == "" && r.priority != "" {
			dec.Priority = r.priority
		}
		dec.Caps.ContextCap = minCap(dec.Caps.ContextCap, r.caps.ContextCap)
		dec.Caps.MaxOutputTokens = minCap(dec.Caps.MaxOutputTokens, r.caps.MaxOutputTokens)
		dec.Caps.MaxConcurrency = minCap(dec.Caps.MaxConcurrency, r.caps.MaxConcurrency)
	}
	if dec.Priority == "" {
		dec.Priority = Interactive
	}
	return dec, nil
}

func grantModel(g Grant, model string) (GrantModel, bool) {
	if model == "" {
		return GrantModel{}, false
	}
	for _, m := range g.Models {
		if m.Name == model {
			return m, true
		}
	}
	return GrantModel{}, false
}

func checkAllowed(text *string, props Properties) error {
	if text == nil {
		return nil
	}
	var names []string
	dec := json.NewDecoder(bytes.NewReader([]byte(*text)))
	if err := dec.Decode(&names); err != nil || names == nil || dec.More() {
		return &Error{Code: CodeInvalidGrant, Message: "allowed_properties is not a JSON array of names"}
	}
	for _, k := range slices.Sorted(maps.Keys(props)) {
		if !slices.Contains(names, k) {
			return &Error{Code: CodePropertyNotAllowed, Property: k, Message: "the grant does not allow this property"}
		}
	}
	return nil
}

// applicable parses the enabled rules that apply to grantID, sorted by Position.
func applicable(rules []StoredRule, grantID string, maxBytes int) ([]*rule, error) {
	var out []*rule
	for _, sr := range rules {
		if !sr.Enabled || (sr.GrantID != nil && *sr.GrantID != grantID) {
			continue
		}
		r, err := parseRule(sr, maxBytes)
		if err != nil {
			var e *Error
			if errors.As(err, &e) {
				e.RuleID, e.RuleName = sr.ID, sr.Name
			}
			return nil, err
		}
		out = append(out, r)
	}
	slices.SortStableFunc(out, func(a, b *rule) int {
		switch {
		case a.position < b.position:
			return -1
		case a.position > b.position:
			return 1
		}
		return 0
	})
	return out, nil
}

func minCap(a, b *int64) *int64 {
	switch {
	case a == nil:
		return b
	case b == nil || *a <= *b:
		return a
	}
	return b
}

// Keys of the policy settings (D-38, D-54). All are owned by the host.
const (
	KeyPropertiesMaxBytes = "policy.request_properties.max_bytes"
	KeyPropertiesMaxCount = "policy.request_properties.max_count"
	KeyRuleMaxBytes       = "policy.rule.max_bytes"
)

// Defs are the policy settings; register them with settings.Store.Register (New does).
func Defs() []settings.Def {
	return []settings.Def{
		{Key: KeyPropertiesMaxBytes, Kind: settings.Int, Default: "1024", Owner: settings.Host,
			Description: "The most bytes the LS-Request-Properties header may hold; a longer header is refused. Zero or less refuses any properties."},
		{Key: KeyPropertiesMaxCount, Kind: settings.Int, Default: "16", Owner: settings.Host,
			Description: "The most request properties one request may carry; more are refused. Zero or less refuses any properties."},
		{Key: KeyRuleMaxBytes, Kind: settings.Int, Default: "8192", Owner: settings.Host,
			Description: "The most bytes one policy rule document may hold. A larger rule is refused when written, and an existing rule above it fails every request it applies to until the rule is shortened or this value raised."},
	}
}

// LoadLimits reads the policy settings.
func LoadLimits(ctx context.Context, st *settings.Store) (Limits, error) {
	var vals [3]int
	for i, k := range []string{KeyPropertiesMaxBytes, KeyPropertiesMaxCount, KeyRuleMaxBytes} {
		v, err := st.Int(ctx, k, "")
		if err != nil {
			return Limits{}, err
		}
		vals[i] = int(max(min(v, math.MaxInt32), 0))
	}
	return Limits{Properties: PropertyLimits{MaxBytes: vals[0], MaxCount: vals[1]}, RuleMaxBytes: vals[2]}, nil
}

// Service is the rule engine behind the Policy port: it reads the limits from settings and the
// rules from a RuleSource on every call, then runs Decide.
type Service struct {
	settings *settings.Store
	rules    RuleSource
}

var _ Policy = (*Service)(nil)

// New registers the policy settings with st and returns the service.
func New(st *settings.Store, rules RuleSource) (*Service, error) {
	if err := st.Register(Defs()...); err != nil {
		return nil, err
	}
	return &Service{settings: st, rules: rules}, nil
}

// Evaluate implements Policy.
func (s *Service) Evaluate(ctx context.Context, in Input) (Decision, error) {
	lim, err := LoadLimits(ctx, s.settings)
	if err != nil {
		return Decision{}, err
	}
	rules, err := s.rules.Rules(ctx, in.Grant.ID)
	if err != nil {
		return Decision{}, err
	}
	return Decide(rules, in, lim)
}
