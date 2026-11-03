// Package credential is the shared credential package of the auth module (docs/modules/auth.md).
// Both web surfaces use it: the admin API (internal/admin) and, later, the consumer web page
// (PLT-F-027). It holds what they must not each build differently:
//
//   - argon2id password hashing in PHC format, with the cost taken from host settings and never
//     below OWASP's minimum (req_001 D-25);
//   - the password policy (a minimum length setting);
//   - the lockout tracker for password logins (req_001 D-54);
//   - session tokens (random, stored only as a hash) and the session cookie (HttpOnly, Secure,
//     SameSite=Strict);
//   - the Guard middleware: the DNS-rebinding guard (the Host header must be a loopback name or
//     address, or a configured name) and the CSRF guard (a state-changing request that carries a
//     session cookie must carry a same-origin Origin header) (req_001 D-10).
//
// It imports only the standard library, golang.org/x/crypto/argon2 and internal/settings. It
// never logs a password, a token or a hash, and it never echoes a Host or Origin value back.
package credential
