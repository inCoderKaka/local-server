# gateway module

## Purpose

The consumer API: OpenAI-compatible endpoints and the request pipeline. Built (PLT-F-018): non-streaming `/v1/chat/completions` and `/v1/embeddings`, the ledger endpoints `/v1/keys`, `/v1/ledger/checkpoint` and `/v1/usage/{usage_id}/proof` (owner-only), the F1 pipeline (auth, limits and lockout in the security handler, policy, validate, admit, engine, meter), a body limit on every `/v1` route, a signed receipt in every answer (500 when none can be signed), metering of aborted calls, and the policy refusal codes `policy_denied`, `outside_time_window` and `invalid_request_properties` (D-79). Streaming, startup wiring and `/v1/health`, `/v1/capabilities`, `/v1/models` are planned (PLT-F-019, PLT-F-020, PLT-F-021). Settings are the `gateway.*` keys in `Defs()`; the call order is in the package doc of `internal/gateway`.

## Owns

- Packages: `internal/gateway/` (built by PLT-F-018, PLT-F-019).
- Tables: none.
- Metrics and events: emits `metering.failed` and `policy.rule_invalid` (internal, D-73); exposes `Gateway.RuleFailures` / `ClearRuleFailures` for PLT-F-026.
- Port: none defined here. `NewRuleSource(db, repos)` is the `policy.RuleSource` over stored rules.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-16, D-21, D-24, D-32, D-39, D-51, D-73, D-79, D-100. The module map is in `docs/design_docs/lld.md` section 1.
