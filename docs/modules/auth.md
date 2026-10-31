# auth module

## Purpose

Access keys: issue (shown once), revoke, and verify the `Authorization` key header for the gateway. A verified key resolves to its consumer, current grant version and effective limits (the grant narrowed by the key). A key whose consumer is not active does not authenticate.

## Owns

- Packages: `internal/auth/` (built by PLT-F-010).
- Tables: none (it uses `idn_access_key` through `internal/identity`).
- Port: `WhoIs` (`NodeOf(ctx, remoteAddr)`), the bound-node check; transport adapters implement it later.
- Setting: `auth.cache_ttl` (host; default 60s; 0 turns the cache off).
- Events: subscribes to `access_key.revoked` and `grant.changed` to clear its cache; emits `access_key.revoked` from `Revoke`. A caller that updates a key, soft-deletes a consumer or changes a principal's status must call `Service.Invalidate()` after its unit of work commits; otherwise the change shows only when the cache TTL runs out. Whoever writes a new grant version emits `grant.changed` in the same unit of work.
- Audit actions: `access_key.issued`, `access_key.revoked`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-25, D-60. The module map is in `docs/design_docs/lld.md` section 1.
