# api module

## Purpose

Server code generated from `api/openapi.yaml`.

## Owns

- Packages: `internal/api/` (built by PLT-F-001).
- Tables: none.
- Port: `server interfaces`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-32, D-57. The module map is in `docs/design_docs/lld.md` section 1.
