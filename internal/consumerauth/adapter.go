package consumerauth

import (
	"context"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Adapter is the auth adapter port (req_001 D-13): one way a person proves who they are. The
// password and passkey adapters are built; the email (magic link) and Google or Apple sign-in
// adapters are added behind the same port, and the Service, the sessions and the invite
// redemption do not change for them. An adapter owns its credential rows and what it needs to
// check them; it never creates a session, which is the Service's. The passkey adapter keeps the
// port's Kind and its two string methods only to say it has no string login: a passkey is two
// round trips with state the host holds between them, so its routes call the ceremony methods of
// PasskeyAdapter and join the Service at Enrollment (sign-up) and Verified (login).
type Adapter interface {
	// Kind is the idn_credential.kind the adapter stores and checks.
	Kind() string
	// Prepare checks a new login and does the expensive work for it (a password is checked
	// against the policy and hashed) outside any unit of work, so a refused sign-up spends
	// nothing and the single writer is never held during a hash. It returns an *Error for a
	// login that is not acceptable.
	Prepare(ctx context.Context, login, secret string) (Enrollment, error)
	// Authenticate checks a login for a client at remoteAddr. It returns ErrInvalidLogin for
	// every wrong answer without saying which part was wrong, and a *LockedOutError while the
	// address or the login is locked out.
	Authenticate(ctx context.Context, login, secret, remoteAddr string) (Verified, error)
}

// Enrollment is a prepared credential that is stored in the unit of work that creates the
// consumer.
type Enrollment interface {
	// Store inserts the credential of principalID inside tx. It returns an *Error when the
	// login name is taken.
	Store(tx *storage.Tx, principalID string) error
}

// Verified is a successful Authenticate.
type Verified struct {
	PrincipalID  string
	CredentialID string
	// Persist, when set, runs in the unit of work that starts the session, for what a login
	// changes in the credential's own row (a passkey's signature counter). Nil for a password.
	Persist func(tx *storage.Tx) error
}
