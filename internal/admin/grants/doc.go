// Package grants is the logic behind the admin API's grant, model and policy rule operations
// (docs/modules/admin.md, PLT-F-026). Package admin maps its types onto the generated ones and
// its *Error onto the error envelope.
//
// # Grants
//
// SetGrant makes a new immutable grant version for a consumer and makes it current, always, even
// when nothing changed (req_001 D-25). It creates the consumer's grant row first when the consumer
// has none: consumers are created without a grant. The version, its audit row (grant.changed) and
// the grant.changed event are one unit of work; once it has committed the key cache is cleared.
// The quota entries are windowed (req_001 D-107): each window is checked against the host's
// bounds, and the deprecated single quota is expanded to entries with the host's legacy window.
//
// # Models
//
// ListModels shows the engine's models with the host's sharing configuration, adding a row for a
// model the host has not seen yet (allowed only when admin.model_default_allowed is on).
// UpdateModel changes whether grants may include a model, its keep-alive and whether it is
// preloaded. Preloading is the scheduler's setting scheduler.preload_models, which this package
// rewrites; the per-model keep-alive is stored and shown but the scheduler does not read it yet.
//
// # Policy rules
//
// A rule is immutable versions (req_001 D-87): an edit stores a new version and the rule's head
// copies it, so the gateway reads one row per rule; switching a rule on or off makes no version;
// delete is soft. Every document is checked with policy.ValidateRule and the format version it
// returns is the one stored. DisableFailingRules switches off a broken rule without `deny` after
// policy.rule.max_failures failed evaluations and audits it (req_001 D-73); RunRuleSweeper does
// that every policy.rule.check_interval.
//
// Errors are *Error with fixed text: no prompt, response, rule document or other request bytes
// ever reach one (req_001 D-31, D-40).
package grants
