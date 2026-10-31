# service module

## Purpose

Running in the background as a user or system service.

## Owns

- Packages: `internal/service/` (built by PLT-F-042).
- Tables: none.
- Port: `service`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-30. The module map is in `docs/design_docs/lld.md` section 1.
