# gateway module

## Purpose

The consumer API: OpenAI-compatible endpoints and the request pipeline. Built (PLT-F-018, PLT-F-019): `/v1/chat/completions` (streamed with `stream: true` in `internal/gateway/stream`: SSE flushed per chunk, usage chunk with `include_usage`, receipt comment line before `[DONE]`, `gateway.stream_first_token_timeout`, `gateway.stream_idle_timeout` and `gateway.stream_total_timeout`, disconnect cancels the engine call and meters aborted) and `/v1/embeddings`, the ledger endpoints `/v1/keys`, `/v1/ledger/checkpoint` and `/v1/usage/{usage_id}/proof` (owner-only), the F1 pipeline (auth, limits and lockout in the security handler, policy, validate, admit, engine, meter), a body limit on every `/v1` route, a signed receipt in every answer (500 when none can be signed), metering of aborted calls, and the policy refusal codes `policy_denied`, `outside_time_window` and `invalid_request_properties` (D-79). The async jobs API (PLT-F-022): `POST /v1/jobs`, `GET /v1/jobs/{job_id}`, `POST /v1/jobs/{job_id}/cancel` and `GET /v1/jobs/{job_id}/result` over `internal/jobs`, with `Gateway.JobExecutor()` giving the job worker the same policy, admission, engine, metering and receipt steps; a job result is deleted only after its response body was written whole (D-85, `jobs.Delivery`). Startup wiring and `/v1/health`, `/v1/capabilities`, `/v1/models` are built (PLT-F-020, PLT-F-021). Settings are the `gateway.*` keys in `Defs()`; the call order is in the package doc of `internal/gateway`.

## Owns

- Packages: `internal/gateway/` (built by PLT-F-018, PLT-F-019, PLT-F-021, PLT-F-022).
- Tables: none.
- Metrics and events: emits `metering.failed` and `policy.rule_invalid` (internal, D-73); exposes `Gateway.RuleFailures` / `ClearRuleFailures`, used by the rule sweeper of `internal/admin/grants` (PLT-F-026, built).
- Quotas (req_001 D-107): the gateway calls `Limiter.CountAdmitted` once when the scheduler admits a call, so the persisted request counter counts every admitted call, metered or not; a call is charged to the quota window that holds that moment. `/v1/capabilities` lists one quota status per scope and window (a window's tokens and requests together), from `Limiter.StatusOf`.
- Port: none defined here. `NewRuleSource(db, repos)` is the `policy.RuleSource` over stored rules.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-16, D-21, D-24, D-26, D-32, D-38, D-39, D-51, D-73, D-79, D-85, D-107. The module map is in `docs/design_docs/lld.md` section 1.
