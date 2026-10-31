# limits module

## Purpose

Rate and quota limits applied at admission.

## Owns

- Packages: `internal/limits/` (built by PLT-F-017).
- Tables: none.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-25. The module map is in `docs/design_docs/lld.md` section 1.
