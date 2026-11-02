# requests module

## Purpose

Resource requests from consumers to the host and their comments.

## Owns

- Packages: `internal/requests/` (designed: PLT-F-028; not built).
- Tables: `req_*`, migrations in `internal/requests/migrations/`.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-38, D-44. The module map is in `docs/design_docs/lld.md` section 1.
