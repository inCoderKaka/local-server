# grants module

## Purpose

Per-consumer grants and policy rules, versioned.

## Owns

- Packages: `internal/grants/` (built by PLT-F-007).
- Tables: `grt_model`, `grt_grant`, `grt_version`, `grt_version_model`, `grt_policy_rule`; migrations in `internal/grants/migrations/`. Triggers refuse any update or delete of `grt_version` and `grt_version_model`, so versions are immutable. Register after identity.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-25, D-44. The module map is in `docs/design_docs/lld.md` section 1.
