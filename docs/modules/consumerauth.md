# consumerauth module

## Purpose

Consumer accounts: invites, login, sessions and consent.

## Owns

- Packages: `internal/consumerauth/` (built by PLT-F-027, password only; passkeys come later behind the same port). Invite redemption, password login with lockout, sessions, consent rows, the `/v1/me` routes and the consumer page listener's handler (`Service.Handler`).
- Tables: none.
- Port: `auth adapter` (`Adapter`, with the password adapter). The caller of an account route is `Caller`, read with `CallerOf`.
- Settings: `consumer.session_ttl`; the login lockout and password cost reuse the host's `auth.login_lockout.*` and `auth.password.*`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-21. The module map is in `docs/design_docs/lld.md` section 1.
