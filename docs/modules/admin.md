# admin module

## Purpose

The admin UI's API handlers: the admin API at `/admin/v1` and `/metrics`, served on the admin listener only. Writes go through the owning modules; every write is audited in the same unit of work as the change.

## Owns

- Packages: `internal/admin/` (built by PLT-F-024; PLT-F-025 and PLT-F-026 add the consumer, grant and request operations).
- Tables: none (it uses `idn_*` through `internal/identity`, `aud_event` through `internal/audit`).
- Port: none defined here. It takes narrow interfaces for the sharing state, the host-metrics sampler, the host keys (host kid) and the model engine.
- Handler: `admin.New(Config)` then `Handler()`. The handler is the request id middleware, then the credential guard of `internal/auth/credential` (the Host check against DNS rebinding, the Origin check against CSRF), then a router: `GET /metrics`, `/admin/v1/...`, and a 404 in the error envelope for everything else, including every consumer route. The admin routes are not on the consumer listener, and the consumer routes are not on the admin listener.
- Routes implemented by PLT-F-024: `GET` and `POST /admin/v1/setup` (first-run admin; exactly one), `GET`, `POST` and `DELETE /admin/v1/session` (current admin, login, logout), `GET /admin/v1/host`, `POST /admin/v1/host/pause`, `POST /admin/v1/host/resume`, `GET /admin/v1/host-metrics`, `GET /admin/v1/host-metrics/history`, `GET` and `PATCH /admin/v1/settings`, `GET /admin/v1/audit-events`, and `GET /admin/v1/usage` (the usage module's query function, served a second time, req_001 D-104). Every other admin operation answers `501` in the error envelope once the caller is logged in.
- Sessions: the cookie `ls_admin_session` (HttpOnly, Secure, SameSite=Strict, Path `/`) holds a random token; `idn_session.token_hash` holds its hash. A session is valid while it is not revoked, not expired (`admin.session_ttl`) and its principal is an active `host_admin`. `admin.SessionOf` and `admin.PrincipalIDOf` give a handler the logged-in admin.
- `admin.Authenticator` (`Authenticate(ctx, username, password, remoteAddr)`): the one password check, with the login lockout; the HTTP login and the `recovery-code` command both use it.
- Command `local-server recovery-code [--data-dir DIR]` (in `cmd/local-server`): shows the recovery code again after an admin login, on a terminal only, and only after its audit row has committed (req_001 D-80, D-101).
- Settings (host-owned, req_001 D-38, D-54): `admin.max_body_bytes` (1048576), `admin.session_ttl` (12h), `admin.engine_probe_timeout` (2s), `admin.metrics_history_default_range` (1h), `admin.metrics_history_max_points` (1440), `admin.tailnet_name` (empty; the extra Host name the guard accepts, read per request). The password and login lockout settings are the auth module's (`auth.password.*`, `auth.login_lockout.*`).
- Audit actions: `admin.created`, `admin.session_started`, `admin.session_ended`, `setting.changed` (detail: key, old value, new value), `host.recovery_code_shown`; `host.paused` and `host.resumed` are written by the host module with the admin as the actor. Failed logins and lockout triggers are not audited yet (PLT-F-055, req_001 D-89).

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters. The admin module imports `internal/api` (the generated server), `internal/auth/credential`, `internal/identity`, `internal/audit`, `internal/host`, `internal/hostmetrics`, `internal/settings`, `internal/usage`, `internal/engine` (the port) and `internal/telemetry`. It does not import the gateway.

## Decisions

D-21, D-10, D-07, D-25, D-31, D-38, D-54, D-80, D-101, D-104. The module map is in `docs/design_docs/lld.md` section 1.
