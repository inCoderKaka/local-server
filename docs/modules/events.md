# events module

## Purpose

Domain events written in the unit of work and handed to subscribers.

## Owns

- Packages: `internal/notifier/events/` (built by PLT-F-009).
- Tables: `ntf_*` (`ntf_event`), migrations in `internal/notifier/events/migrations/`. Rows are pruned after `events.retention` (host setting, 30 d, D-83) by `events.RegisterRetention`, which startup (PLT-F-020) calls (PLT-B-015).
- Port: `domain events`. `Emit(tx, Event)` works only inside `Bus.Write`, so the event row commits with the caller's unit of work. Subscribers run after commit, synchronously and at most once, and a failing one never undoes the action. Payloads refuse content-like keys at any depth. Event names match the webhook contract (for example `resource_request.created`). Three types are internal and not on the webhook event list (I-028), so notification adapters filter on that list: `access_key.revoked` and `grant.changed` clear the access-key cache, and `policy.rule_invalid` (rule id and version, failure code and count; D-73, D-87) tells the host admin that an enabled policy rule does not parse.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-29. The module map is in `docs/design_docs/lld.md` section 1.
