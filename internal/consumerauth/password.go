package consumerauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/inCoderKaka/local-server/internal/auth/credential"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// PrincipalKind is the idn_principal.kind of a consumer.
const PrincipalKind = "consumer"

// maxLoginLen bounds a login name; it is a format limit, not a tuning value.
const maxLoginLen = 128

// PasswordAdapter is the password Adapter: argon2id hashes in idn_credential (kind password, the
// login name as identifier), the password policy and cost from the shared auth.password.*
// settings, and a login lockout from the shared auth.login_lockout.* settings (req_001 D-25,
// D-54). It keeps its own in-memory lockout tracker, so a consumer's failed logins never lock an
// admin out. It is safe for concurrent use.
type PasswordAdapter struct {
	db     *storage.DB
	st     *settings.Store
	ir     *identity.Repos
	now    func() time.Time
	hasher *credential.Hasher
	lock   *credential.Lockout
	// attempt serializes the lockout check, the password check and the failure count, so that a
	// flood of parallel guesses cannot all pass the lockout check before the first failure is
	// counted. Past the threshold the rest return at once, without hashing.
	attempt sync.Mutex
}

var _ Adapter = (*PasswordAdapter)(nil)

// NewPasswordAdapter registers the password and lockout settings and returns the adapter. now
// nil means time.Now.
func NewPasswordAdapter(db *storage.DB, st *settings.Store, ir *identity.Repos, now func() time.Time) (*PasswordAdapter, error) {
	if db == nil || st == nil || ir == nil {
		return nil, errors.New("consumerauth: DB, Settings and Identity are required")
	}
	if now == nil {
		now = time.Now
	}
	h, err := credential.NewHasher(st)
	if err != nil {
		return nil, err
	}
	if err := credential.RegisterLockout(st); err != nil {
		return nil, err
	}
	return &PasswordAdapter{db: db, st: st, ir: ir, now: now, hasher: h, lock: credential.NewLockout()}, nil
}

// Kind is identity.CredentialPassword.
func (a *PasswordAdapter) Kind() string { return identity.CredentialPassword }

// validLogin reports whether name can be a login name: 1 to 128 characters, valid UTF-8, no
// control character. The same rule as the admin's login name.
func validLogin(name string) bool {
	n := utf8.RuneCountInString(name)
	if n < 1 || n > maxLoginLen || !utf8.ValidString(name) {
		return false
	}
	return !strings.ContainsFunc(name, unicode.IsControl)
}

// checkPolicy maps a policy refusal to the API's answer.
func (a *PasswordAdapter) checkPolicy(ctx context.Context, password, param string) error {
	if err := a.hasher.CheckPolicy(ctx, password); err != nil {
		if errors.Is(err, credential.ErrPasswordTooShort) {
			n, _ := a.st.Int(ctx, credential.KeyMinLength, "")
			return badRequest(param, fmt.Sprintf("The password must have at least %d characters.", n))
		}
		return err
	}
	return nil
}

type passwordEnrollment struct {
	a     *PasswordAdapter
	login string
	hash  string
}

// Prepare checks the login name and the password policy, refuses a name that is taken and
// hashes the password.
func (a *PasswordAdapter) Prepare(ctx context.Context, login, password string) (Enrollment, error) {
	login = strings.TrimSpace(login)
	if !validLogin(login) {
		return nil, badRequest("username", "The login name must have 1 to 128 characters and no control characters.")
	}
	if err := a.checkPolicy(ctx, password, "password"); err != nil {
		return nil, err
	}
	// Refuse a taken name before spending the memory-hard hash; Store checks again in the unit.
	if _, err := a.ir.Credentials.GetByIdentifier(ctx, a.db.Reader(), identity.CredentialPassword, login); err == nil {
		return nil, errLoginTaken()
	} else if !errors.Is(err, identity.ErrNotFound) {
		return nil, err
	}
	hash, err := a.hasher.Hash(ctx, password)
	if err != nil {
		return nil, err
	}
	return &passwordEnrollment{a: a, login: login, hash: hash}, nil
}

func errLoginTaken() *Error { return conflict("That login name is already taken. Choose another.") }

func (e *passwordEnrollment) Store(tx *storage.Tx, principalID string) error {
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM idn_credential WHERE kind = ? AND identifier = ?`, identity.CredentialPassword, e.login).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return errLoginTaken()
	}
	_, err := e.a.ir.Credentials.Create(tx, principalID, identity.CredentialPassword, e.login, e.hash)
	return err
}

// Authenticate checks login and password for a client at remoteAddr. It returns ErrInvalidLogin
// for an unknown name, a wrong password, a revoked credential and a principal that is not an
// active consumer, all after the same argon2 work, so neither the answer nor its timing says
// which was wrong; and a *LockedOutError, before any hashing, while the address or the name is
// locked out. A failure counts toward the lockout; a success clears the name's counter only,
// never the address's, and does not lift a lockout in force (req_001 D-54, D-99).
func (a *PasswordAdapter) Authenticate(ctx context.Context, login, password, remoteAddr string) (Verified, error) {
	login = strings.TrimSpace(login)
	a.attempt.Lock()
	defer a.attempt.Unlock()
	cfg, err := credential.LoadLockout(ctx, a.st)
	if err != nil {
		return Verified{}, err
	}
	keys := credential.LoginKeys(remoteAddr, login)
	if wait := a.lock.Check(keys, a.now()); wait > 0 {
		return Verified{}, &LockedOutError{RetryAfter: wait}
	}
	reject := func() (Verified, error) {
		a.lock.Fail(keys, a.now(), cfg)
		return Verified{}, ErrInvalidLogin
	}
	rd := a.db.Reader()
	var cred identity.Credential
	known := validLogin(login)
	if known {
		cred, err = a.ir.Credentials.GetByIdentifier(ctx, rd, identity.CredentialPassword, login)
		switch {
		case errors.Is(err, identity.ErrNotFound):
			known = false
		case err != nil:
			return Verified{}, err
		case cred.RevokedAt != nil:
			known = false
		}
	}
	if !known {
		// Verify at the cost a consumer's stored hash really has, which differs from the current
		// cost settings after a change; no consumer yet, or a malformed row, falls back to Burn.
		ref, err := a.referenceHash(ctx, rd)
		if err != nil {
			return Verified{}, err
		}
		if ref == "" {
			err = a.hasher.Burn(ctx, password)
		} else if _, verr := credential.Verify(password, ref); verr != nil {
			err = a.hasher.Burn(ctx, password)
		}
		if err != nil {
			return Verified{}, err
		}
		return reject()
	}
	ok, err := credential.Verify(password, cred.Secret)
	if err != nil {
		return Verified{}, fmt.Errorf("consumerauth: stored password hash: %w", err)
	}
	if !ok {
		return reject()
	}
	p, err := a.ir.Principals.Get(ctx, rd, cred.PrincipalID)
	if err != nil {
		return Verified{}, err
	}
	if p.Kind != PrincipalKind || p.Status != identity.StatusActive {
		return reject()
	}
	// Only the name's counter is cleared: a success proves the name, not the address.
	a.lock.Succeed(credential.LoginKeys("", login), a.now())
	return Verified{PrincipalID: cred.PrincipalID, CredentialID: cred.ID}, nil
}

// referenceHash returns the stored password hash of the oldest live consumer credential, or ""
// when there is none.
func (a *PasswordAdapter) referenceHash(ctx context.Context, rd *storage.Reader) (string, error) {
	var secret string
	err := rd.QueryRow(ctx, `SELECT c.secret FROM idn_credential c JOIN idn_consumer u ON u.principal_id = c.principal_id
WHERE c.kind = ? AND c.revoked_at IS NULL ORDER BY c.created_at, c.id LIMIT 1`, identity.CredentialPassword).Scan(&secret)
	if errors.Is(err, storage.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return secret, nil
}

// CheckCurrent verifies the current password of a logged-in consumer before it is changed. It
// counts toward the same lockout as a login, for the client's address and the login name, and
// refuses with a *LockedOutError while locked out. A wrong password returns ErrInvalidLogin.
func (a *PasswordAdapter) CheckCurrent(ctx context.Context, cred identity.Credential, current, remoteAddr string) error {
	a.attempt.Lock()
	defer a.attempt.Unlock()
	cfg, err := credential.LoadLockout(ctx, a.st)
	if err != nil {
		return err
	}
	keys := credential.LoginKeys(remoteAddr, cred.Identifier)
	if wait := a.lock.Check(keys, a.now()); wait > 0 {
		return &LockedOutError{RetryAfter: wait}
	}
	ok, err := credential.Verify(current, cred.Secret)
	if err != nil {
		return fmt.Errorf("consumerauth: stored password hash: %w", err)
	}
	if !ok {
		a.lock.Fail(keys, a.now(), cfg)
		return ErrInvalidLogin
	}
	a.lock.Succeed(credential.LoginKeys("", cred.Identifier), a.now())
	return nil
}
