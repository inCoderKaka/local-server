// Package policy is the policy module (docs/modules/policy.md): the Policy port, the
// versioned declarative rule format, the LS-Request-Properties parser and the rule engine
// behind the port (D-15, D-25, D-33, D-39 step 3).
//
// Decide is the pure core: given the stored rules, the request's properties header, model,
// time and grant, and the limits, it returns the priority class and caps or a typed *Error.
// Service implements Policy by reading the limits from settings and the rules from a
// RuleSource (grants owns their storage) and calling Decide.
//
// Request property values come from consumers. They are never logged or stored, and no error
// quotes them (D-31, D-40).
package policy
