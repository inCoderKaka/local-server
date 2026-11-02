# offhost module

## Purpose

Backups and opt-in platform sync with per-dataset keys.

## Owns

- Packages: `internal/offhost/` (designed: PLT-F-044..F-046; not built).
- Tables: `syn_*`, migrations in `internal/offhost/migrations/`.
- Port: `OffHostSync`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-44. The module map is in `docs/design_docs/lld.md` section 1.
