# events module

## Purpose

Domain events written in the unit of work and handed to subscribers.

## Owns

- Packages: `internal/notifier/events/` (built by PLT-F-009).
- Tables: `ntf_*` (`ntf_event`), migrations in `internal/notifier/events/migrations/`.
- Port: `domain events`. `Emit(tx, Event)` works only inside `Bus.Write`, so the event row commits with the caller's unit of work. Subscribers run after commit, synchronously and at most once, and a failing one never undoes the action. Payloads refuse content-like keys at any depth. Event names match the webhook contract (for example `resource_request.created`). Two types are internal and never sent to webhooks: `access_key.revoked` and `grant.changed`, which clear the access-key cache.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-29. The module map is in `docs/design_docs/lld.md` section 1.
