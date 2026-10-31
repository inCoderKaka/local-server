# sharing module

## Purpose

Platform admin and sharing grants between parties.

## Owns

- Packages: `internal/sharing/` (built by PLT-F-047).
- Tables: `plt_*`, migrations in `internal/sharing/migrations/`.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-43, D-44. The module map is in `docs/design_docs/lld.md` section 1.
