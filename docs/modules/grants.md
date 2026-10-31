# grants module

## Purpose

Per-consumer grants and policy rules, versioned.

## Owns

- Packages: `internal/grants/` (built by PLT-F-007).
- Tables: `grt_*`, migrations in `internal/grants/migrations/`.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-25, D-44. The module map is in `docs/design_docs/lld.md` section 1.
