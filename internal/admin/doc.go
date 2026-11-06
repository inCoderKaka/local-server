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
// change, the audit list and the usage rollups (req_001 D-104).
//
// PLT-F-025 implements the consumer, access key and invite operations: list, create, get,
// change, delete, suspend and reactivate a consumer; list, issue, change and revoke an access
// key; list, create and revoke an invite. They are thin: package admin/consumers owns the rules
// (soft delete with erasure of personal fields, key secrets and invite tokens shown once,
// narrowing that can only narrow the grant, terminal states never reopened, req_001 D-25, D-40,
// D-54) and package admin maps its types onto the generated ones and its *consumers.Error onto
// the envelope (400, 404, 409).
//
// PLT-F-026 implements the grant, model and policy rule operations: get, set and list the versions
// of a consumer's grant; list the engine's models and configure one; list, create, get, replace,
// delete and list the versions of policy rules. They are thin too: package admin/grants owns the
// rules (a grant edit is always a new immutable version with its audit row and grant.changed event
// in one unit of work, policy rules are immutable versions checked by the policy module, a broken
// rule without deny is switched off after policy.rule.max_failures failures, req_001 D-25, D-73,
// D-87, D-107) and package admin maps its types and its *grants.Error onto the generated ones and
// the envelope. RunRuleSweeper is the worker that switches off broken rules.
//
// Every other admin operation (resource requests, notification preferences and webhook endpoints)
// stays on api.UnimplementedHandler and answers 501 once the caller is logged in. A later ticket
// adds an operation by implementing its method on Admin; routing, security and error mapping need
// no change.
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
