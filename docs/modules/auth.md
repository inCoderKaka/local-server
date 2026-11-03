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
- `internal/auth/credential` (built by PLT-F-024): the credential package both web surfaces share, the admin API now and the consumer web page later (PLT-F-027). It imports only the standard library, `golang.org/x/crypto/argon2` and `internal/settings`. It holds:
  - argon2id password hashing in PHC format (16-byte random salt, 32-byte key). The cost is read from settings and refused below OWASP's minimum (req_001 D-25); `Verify` reads the cost from the stored hash, so a cost change does not break old hashes, and refuses a malformed or oversized-cost hash without hashing. `Hasher.Burn` does one verify's work for an unknown login name, so timing does not tell which names exist.
  - The password policy (`auth.password.min_length`, 12). The longest password is bounded by the admin body limit, not here.
  - The login lockout tracker (req_001 D-54): failures by remote host and by lower-cased login name, in memory, bounded; `Check`, `Fail` (reports whether it started a lockout) and `Succeed` (never lifts a lockout in force, req_001 D-99).
  - Session tokens (32 random bytes, base64url; stored only as a SHA-256 hash) and the session cookie constructors (HttpOnly, Secure, SameSite=Strict). The cookie name is a parameter: `ls_admin_session` or `ls_session`.
  - `Guard`: the middleware that refuses a request whose Host is not `localhost`, a loopback address or a configured name (DNS rebinding), and a state-changing request whose Origin is missing (when it carries a session cookie) or not its own (CSRF) (req_001 D-10). It fails closed when the configured names cannot be read, and the refusal is written by a callback so each surface answers in its own error shape.
- Settings of the credential package (host-owned): `auth.password.memory_kib` (19456), `auth.password.iterations` (2), `auth.password.parallelism` (1), `auth.password.min_length` (12), `auth.login_lockout.failures` (5), `auth.login_lockout.window` (10m), `auth.login_lockout.duration` (15m), `auth.login_lockout.tracker_idle_ttl` (1h), `auth.login_lockout.tracker_max_entries` (10000). The cost settings are also refused above a safe maximum (1 GiB, 64 passes, 64 threads), because a hash that cannot be verified would lock the host out.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-25, D-60, D-10, D-54, D-99. The module map is in `docs/design_docs/lld.md` section 1.
