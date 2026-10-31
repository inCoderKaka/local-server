# audit module

## Purpose

The append-only audit trail written in the same unit of work as the change.

## Owns

- Packages: `internal/audit/` (built by PLT-F-008).
- Tables: `aud_*`, migrations in `internal/audit/migrations/`.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-28, D-44. The module map is in `docs/design_docs/lld.md` section 1.
