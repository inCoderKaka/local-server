# events module

## Purpose

Domain events written in the unit of work and handed to subscribers.

## Owns

- Packages: `internal/notifier/events/` (built by PLT-F-009).
- Tables: `ntf_*` (`ntf_event`), migrations in `internal/notifier/events/migrations/`.
- Port: `domain events`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-29. The module map is in `docs/design_docs/lld.md` section 1.
