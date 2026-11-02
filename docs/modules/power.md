# power module

## Purpose

The sleep lock while sharing is on.

## Owns

- Packages: `internal/power/` (designed: PLT-F-042; not built).
- Tables: none.
- Port: `Power`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-30. The module map is in `docs/design_docs/lld.md` section 1.
