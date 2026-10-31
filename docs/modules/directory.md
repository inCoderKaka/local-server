# directory module

## Purpose

The seam for a future central server or marketplace. Phase 1 ships only the no-op.

## Owns

- Packages: `internal/directory/` (built by PLT-C-001).
- Tables: none.
- Port: `directory (no-op)`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-02, D-21, D-45. The module map is in `docs/design_docs/lld.md` section 1.
