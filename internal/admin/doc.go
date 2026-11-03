// Package admin is the admin module (docs/modules/admin.md): the handler of the admin API,
// /admin/v1 (api/openapi.yaml), served on the admin listener only. It also serves /metrics on
// that listener, behind the same guard.
//
// # Surface
//
// Handler is the whole admin listener: a request id on every response, then the credential
// guard (the DNS-rebinding Host check and the CSRF Origin check of internal/auth/credential,
// req_001 D-10), then a router: GET /metrics, /admin/v1/..., and a 404 in the error envelope for
// everything else, including every consumer route. Every error is the OpenAI-style envelope; no
// error quotes request bytes.
//
// PLT-F-024 implements these operations: the first-run setup, login, logout and current admin,
// host state with pause and resume, host metrics now and over time, host settings read and
// change, the audit list and the usage rollups (req_001 D-104). Every other admin operation
// stays on api.UnimplementedHandler and answers 501 once the caller is logged in. A later ticket
// adds an operation by implementing its method on Admin; routing, security and error mapping
// need no change.
//
// # Sessions and the admin principal
//
// A session is an idn_session row whose token_hash is the hash of the cookie value
// (ls_admin_session). HandleAdminSessionAuth checks it before any protected operation runs and
// puts the Session in the context: SessionOf and PrincipalIDOf read it. Every write that is
// audited carries that principal id as the actor, in the same unit of work as the change.
//
// # Authentication
//
// Authenticator.Authenticate checks a login name and password with the lockout of
// internal/auth/credential (req_001 D-54). The HTTP login and the recovery-code command
// (req_001 D-80, D-101) both use it. Failed logins and lockout triggers are not audited here
// (req_001 D-89: PLT-F-055).
package admin
