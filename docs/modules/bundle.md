# bundle module

## Purpose

Export and import bundles.

## Owns

- Packages: `internal/bundle/` (designed: PLT-F-045; not built).
- Tables: none.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-60. The module map is in `docs/design_docs/lld.md` section 1.
