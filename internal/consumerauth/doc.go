// Package consumerauth is the consumer accounts module (docs/modules/consumerauth.md, PLT-F-027): invite
// redemption, password login, sessions, consent and the consumer's own account routes. Its parts:
//
//   - the auth adapter port (Adapter) and its password adapter (PasswordAdapter): how a person
//     proves who they are, so an email or OAuth adapter can be added later without touching the
//     rest (req_001 D-13);
//   - Service: invite redemption (one-time token, one unit of work, consent rows for the policy
//     versions in force), login with lockout, logout, sessions and the /v1/me routes;
//   - Service.Handler: the consumer page listener's handler. It serves the /v1 account routes of
//     the page at the page's own origin, with the ls_session cookie, behind credential.Guard
//     (Host and Origin checks), through its own ogen server (req_001 D-10, D-148);
//   - Caller: who a call is for, as a consumer. A session puts it in the context on the page
//     listener; the consumer API (internal/gateway) puts the same Caller for an access key, so
//     the account handlers behind both are one set of methods. Later consumer routes (resource
//     requests) read it with CallerOf.
//
// A password, a token, a hash, a cookie, a Host or an Origin is never logged, never put in an
// error and never echoed. Prompts and responses never pass through here (req_001 D-31, D-40).
// The module adds no table and no migration: it uses the identity module's idn_ rows, and
// consent rows are never touched by a soft delete of a consumer (req_001 D-136).
package consumerauth
