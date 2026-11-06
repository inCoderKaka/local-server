# grants module

## Purpose

Per-consumer grants and policy rules, versioned.

## Owns

- Packages: `internal/grants/` (built by PLT-F-007).
- Tables: `grt_model`, `grt_grant`, `grt_version`, `grt_version_model`, `grt_version_quota`, `grt_policy_rule`; migrations in `internal/grants/migrations/`. Triggers refuse any update or delete of `grt_version`, `grt_version_model` and `grt_version_quota`, so versions are immutable. Register after identity. `grt_version_quota` (migration 2, req_001 D-107) holds a version's windowed quota entries `{metric, limit, window}`, at most one per metric and window (`grants.QuotaEntry`, `grants.ValidateQuotas`; `Version.Quotas`). The columns `grt_version.quota_tokens_per_day` and `quota_requests_per_day` stay as a deprecated read-only alias: new versions leave them empty and the limiter reads an old figure as one entry with the window `limits.quota_window`.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-25, D-44, D-107. The module map is in `docs/design_docs/lld.md` section 1.
