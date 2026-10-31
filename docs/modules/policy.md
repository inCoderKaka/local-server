# policy module

## Purpose

Decides whether a request is allowed; the gateway enforces the decision.

## Owns

- Packages: `internal/policy/` (built by PLT-F-011).
- Tables: none.
- Port: `Policy`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-25, D-39. The module map is in `docs/design_docs/lld.md` section 1.
