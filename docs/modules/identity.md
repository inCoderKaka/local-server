# identity module

## Purpose

Principals, admin users, consumers, credentials, sessions and consent records. Also defines the content-capture interface, which stays off and can only run behind a matching consent record.

## Owns

- Packages: `internal/identity/` (built by PLT-F-007).
- Tables: `idn_*`, migrations in `internal/identity/migrations/`.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-42, D-44, D-45. The module map is in `docs/design_docs/lld.md` section 1.
