# admin module

## Purpose

The admin UI's API handlers. Writes go through the owning modules.

## Owns

- Packages: `internal/admin/` (designed: PLT-F-024..F-026; not built).
- Tables: none.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-21. The module map is in `docs/design_docs/lld.md` section 1.
