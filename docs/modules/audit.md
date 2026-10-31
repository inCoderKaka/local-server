# audit module

## Purpose

The append-only audit trail written in the same unit of work as the change.

## Owns

- Packages: `internal/audit/` (built by PLT-F-008).
- Tables: `aud_event` (triggers refuse updates and deletes), migrations in `internal/audit/migrations/`. Callers add a row with `audit.Record(tx, Event)` inside their own unit of work. `detail` refuses content-like keys at any depth, and list pages are capped by the host setting `audit.list_page_size`. Register after identity.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-28, D-44. The module map is in `docs/design_docs/lld.md` section 1.
