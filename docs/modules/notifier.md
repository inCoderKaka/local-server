# notifier module

## Purpose

Notifications, preferences, webhook endpoints and the outbox, with desktop and webhook adapters.

## Owns

- Packages: `internal/notifier/` (built by PLT-F-029..F-031).
- Tables: `ntf_*`, migrations in `internal/notifier/migrations/`.
- Port: `Notifier`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-29, D-44. The module map is in `docs/design_docs/lld.md` section 1.
