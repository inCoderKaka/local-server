# consumerauth module

## Purpose

Consumer accounts: invites, login, sessions and consent.

## Owns

- Packages: `internal/consumerauth/` (designed: PLT-F-027; not built).
- Tables: none.
- Port: `auth adapter`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-21. The module map is in `docs/design_docs/lld.md` section 1.
