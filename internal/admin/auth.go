package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/auth/credential"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// PrincipalKind is the idn_principal.kind of a host admin.
const PrincipalKind = "host_admin"

// Audit actions written by this package (req_001 D-07). The host module writes host.paused and
// host.resumed; the recovery-code command writes ActionRecoveryCodeShown.
const (
	ActionAdminCreated      = "admin.created"
	ActionSessionStarted    = "admin.session_started"
	ActionSessionEnded      = "admin.session_ended"
	ActionSettingChanged    = "setting.changed"
	ActionRecoveryCodeShown = "host.recovery_code_shown"
)

const (
	maxUsernameLen = 128
	maxTokenLen    = 256 // a cookie value longer than this cannot be a token
)

// Errors of Authenticate.
var (
	// ErrInvalidLogin means the login name or the password is wrong. It never says which.
	ErrInvalidLogin = errors.New("admin: login name or password is wrong")
	// ErrSetupDone means the first admin already exists.
	ErrSetupDone = errors.New("admin: an admin already exists")
)

// LockedOutError means the login is locked out: too many failed attempts from this address or
// for this login name. RetryAfter is how long the longest lockout still runs.
type LockedOutError struct{ RetryAfter time.Duration }

func (e *LockedOutError) Error() string { return "admin: login is locked out" }

// AuthConfig is what NewAuthenticator needs.
type AuthConfig struct {
	DB       *storage.DB
	Settings *settings.Store
	Identity *identity.Repos
	// Clock nil means time.Now.
	Clock func() time.Time
}

// Authenticator checks admin logins: argon2id password verification with the login lockout
// (req_001 D-25, D-54). The admin API and the recovery-code command (req_001 D-80, D-101) use
// the same one, so the same lockout and the same timing apply to both. It is safe for concurrent
// use.
type Authenticator struct {
	cfg    AuthConfig
	now    func() time.Time
	hasher *credential.Hasher
	lock   *credential.Lockout
	// attempt serializes the lockout check, the password check and the failure count, so that a
	// flood of parallel guesses cannot all pass the lockout check before the first failure is
	// counted. Past the threshold the rest return at once, without hashing.
	attempt sync.Mutex
}

// NewAuthenticator registers the password and lockout settings and returns the authenticator.
func NewAuthenticator(cfg AuthConfig) (*Authenticator, error) {
	if cfg.DB == nil || cfg.Settings == nil || cfg.Identity == nil {
		return nil, errors.New("admin: DB, Settings and Identity are required")
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	h, err := credential.NewHasher(cfg.Settings)
	if err != nil {
		return nil, err
	}
	if err := credential.RegisterLockout(cfg.Settings); err != nil {
		return nil, err
	}
	return &Authenticator{cfg: cfg, now: cfg.Clock, hasher: h, lock: credential.NewLockout()}, nil
}

// Authenticated is a verified admin login.
type Authenticated struct {
	PrincipalID  string
	CredentialID string
	AdminUser    identity.AdminUser
}

// Authenticate checks username and password for the admin whose login name is username (an exact
// match after trimming), for a client at remoteAddr ("host:port", or any string naming where the
// attempt came from). It returns ErrInvalidLogin for an unknown name, a wrong password, a revoked
// credential or an inactive principal, all after the same argon2 work, so neither the answer nor
// its timing says which was wrong; and a *LockedOutError, before any hashing, while the address
// or the name is locked out. A failure counts toward the lockout; a success clears the name's
// counter only, never the address's, and does not lift a lockout in force (req_001 D-99). It
// does not audit: failed logins are PLT-F-055 (req_001 D-89).
func (a *Authenticator) Authenticate(ctx context.Context, username, password, remoteAddr string) (Authenticated, error) {
	username = strings.TrimSpace(username)
	a.attempt.Lock()
	defer a.attempt.Unlock()
	cfg, err := credential.LoadLockout(ctx, a.cfg.Settings)
	if err != nil {
		return Authenticated{}, err
	}
	keys := credential.LoginKeys(remoteAddr, username)
	if wait := a.lock.Check(keys, a.now()); wait > 0 {
		return Authenticated{}, &LockedOutError{RetryAfter: wait}
	}
	reject := func() (Authenticated, error) {
		a.lock.Fail(keys, a.now(), cfg)
		return Authenticated{}, ErrInvalidLogin
	}

	rd := a.cfg.DB.Reader()
	var cred identity.Credential
	known := validUsername(username)
	if known {
		cred, err = a.cfg.Identity.Credentials.GetByIdentifier(ctx, rd, identity.CredentialPassword, username)
		switch {
		case errors.Is(err, identity.ErrNotFound):
			known = false
		case err != nil:
			return Authenticated{}, err
		case cred.RevokedAt != nil:
			known = false
		}
	}
	if !known {
		if err := a.hasher.Burn(ctx, password); err != nil {
			return Authenticated{}, err
		}
		return reject()
	}
	ok, err := credential.Verify(password, cred.Secret)
	if err != nil {
		return Authenticated{}, fmt.Errorf("admin: stored password hash: %w", err)
	}
	if !ok {
		return reject()
	}
	p, err := a.cfg.Identity.Principals.Get(ctx, rd, cred.PrincipalID)
	if err != nil {
		return Authenticated{}, err
	}
	if p.Kind != PrincipalKind || p.Status != identity.StatusActive {
		return reject()
	}
	au, err := a.cfg.Identity.AdminUsers.GetByPrincipal(ctx, rd, cred.PrincipalID)
	if err != nil {
		return Authenticated{}, err
	}
	// Only the name's counter is cleared: a success proves the name, not the address.
	a.lock.Succeed(credential.LoginKeys("", username), a.now())
	return Authenticated{PrincipalID: cred.PrincipalID, CredentialID: cred.ID, AdminUser: au}, nil
}

// validUsername reports whether name can be an admin login name: 1 to 128 characters, valid
// UTF-8, no control character.
func validUsername(name string) bool {
	n := utf8.RuneCountInString(name)
	if n < 1 || n > maxUsernameLen || !utf8.ValidString(name) {
		return false
	}
	return !strings.ContainsFunc(name, unicode.IsControl)
}

// Session is the logged-in admin of a request.
type Session struct {
	ID          string
	PrincipalID string
	AdminUser   identity.AdminUser
	ExpiresAt   time.Time
}

type (
	sessionKey struct{}
	remoteKey  struct{}
)

// SessionOf returns the admin session of a request that passed the session security check.
func SessionOf(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(Session)
	return s, ok
}

// PrincipalIDOf returns the principal id of the logged-in admin of a request, the actor of every
// audited write; "" when the request is not authenticated.
func PrincipalIDOf(ctx context.Context) string {
	s, _ := SessionOf(ctx)
	return s.PrincipalID
}

func remoteOf(ctx context.Context) string {
	s, _ := ctx.Value(remoteKey{}).(string)
	return s
}

// HandleAdminSessionAuth checks the session cookie: its token's hash must name a session that is
// not revoked or expired, of an active host admin. Any other cookie, or none, is a 401.
func (a *Admin) HandleAdminSessionAuth(ctx context.Context, _ api.OperationName, t api.AdminSessionAuth) (context.Context, error) {
	if t.APIKey == "" || len(t.APIKey) > maxTokenLen {
		return nil, unauthenticated()
	}
	rd := a.cfg.DB.Reader()
	ir := a.cfg.Identity
	s, err := ir.Sessions.GetByTokenHash(ctx, rd, credential.HashToken(t.APIKey))
	if errors.Is(err, identity.ErrNotFound) {
		return nil, unauthenticated()
	}
	if err != nil {
		return nil, err
	}
	if s.RevokedAt != nil || !a.now().Before(s.ExpiresAt) {
		return nil, unauthenticated()
	}
	p, err := ir.Principals.Get(ctx, rd, s.PrincipalID)
	if errors.Is(err, identity.ErrNotFound) {
		return nil, unauthenticated()
	}
	if err != nil {
		return nil, err
	}
	if p.Kind != PrincipalKind || p.Status != identity.StatusActive {
		return nil, unauthenticated()
	}
	au, err := ir.AdminUsers.GetByPrincipal(ctx, rd, s.PrincipalID)
	if errors.Is(err, identity.ErrNotFound) {
		return nil, unauthenticated()
	}
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, sessionKey{}, Session{ID: s.ID, PrincipalID: s.PrincipalID, AdminUser: au, ExpiresAt: s.ExpiresAt}), nil
}

// startSession adds a session row for principalID inside tx and returns the cookie value and the
// Set-Cookie header. The token is shown only here; the row holds its hash.
func (a *Admin) startSession(ctx context.Context, tx *storage.Tx, principalID string) (id, cookie string, err error) {
	ttl, err := a.durationSetting(ctx, KeySessionTTL)
	if err != nil {
		return "", "", err
	}
	token, err := credential.NewToken()
	if err != nil {
		return "", "", err
	}
	now := a.now()
	expires := now.Add(ttl)
	s, err := a.cfg.Identity.Sessions.Create(tx, principalID, credential.HashToken(token), expires)
	if err != nil {
		return "", "", err
	}
	return s.ID, credential.SessionCookie(credential.AdminSessionCookie, token, expires, now), nil
}

func (a *Admin) record(tx *storage.Tx, ev audit.Event) error {
	_, err := audit.RecordAt(tx, audit.Clock(a.now), ev)
	return err
}

func toAdminUser(au identity.AdminUser) (api.AdminUser, error) {
	id, err := uuid.Parse(au.ID)
	if err != nil {
		return api.AdminUser{}, err
	}
	return api.AdminUser{ID: id, Username: au.Username, CreatedAt: au.CreatedAt}, nil
}

// adminExists reports whether any admin user exists.
func (a *Admin) adminExists(ctx context.Context) (bool, error) {
	var n int
	if err := a.cfg.DB.Reader().QueryRow(ctx, `SELECT count(*) FROM idn_admin_user`).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// AdminGetSetupStatus implements GET /admin/v1/setup.
func (a *Admin) AdminGetSetupStatus(ctx context.Context) (*api.AdminSetupStatus, error) {
	exists, err := a.adminExists(ctx)
	if err != nil {
		return nil, err
	}
	return &api.AdminSetupStatus{AdminExists: exists}, nil
}

// AdminCreateFirstAdmin implements POST /admin/v1/setup (req_001 D-10): it creates the one host
// admin of a first run and starts its session. The check that no admin exists, the creation and
// the audit row are one unit of work, and the single writer orders concurrent calls, so exactly
// one wins and every other gets 409.
func (a *Admin) AdminCreateFirstAdmin(ctx context.Context, req *api.AdminCredentials) (*api.AdminUserHeaders, error) {
	username := strings.TrimSpace(req.Username)
	if !validUsername(username) {
		return nil, badRequest("username", "The login name must have 1 to 128 characters and no control characters.")
	}
	if err := a.auth.hasher.CheckPolicy(ctx, req.Password); err != nil {
		if errors.Is(err, credential.ErrPasswordTooShort) {
			n, _ := a.cfg.Settings.Int(ctx, credential.KeyMinLength, "")
			return nil, badRequest("password", fmt.Sprintf("The password must have at least %d characters.", n))
		}
		return nil, err
	}
	// Refuse a repeat before spending the memory-hard hash; the unit below checks again.
	if exists, err := a.adminExists(ctx); err != nil {
		return nil, err
	} else if exists {
		return nil, setupDone()
	}
	hash, err := a.auth.hasher.Hash(ctx, req.Password)
	if err != nil {
		return nil, err
	}
	var au identity.AdminUser
	var cookie string
	err = a.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM idn_admin_user`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrSetupDone
		}
		ir := a.cfg.Identity
		p, err := ir.Principals.Create(tx, PrincipalKind, nil, nil)
		if err != nil {
			return err
		}
		if au, err = ir.AdminUsers.Create(tx, p.ID, username, nil); err != nil {
			return err
		}
		if _, err = ir.Credentials.Create(tx, p.ID, identity.CredentialPassword, username, hash); err != nil {
			return err
		}
		if _, cookie, err = a.startSession(ctx, tx, p.ID); err != nil {
			return err
		}
		return a.record(tx, audit.Event{Action: ActionAdminCreated, ActorID: p.ID, TargetID: p.ID, Detail: map[string]string{"username": username}})
	})
	if errors.Is(err, ErrSetupDone) {
		return nil, setupDone()
	}
	if err != nil {
		return nil, err
	}
	body, err := toAdminUser(au)
	if err != nil {
		return nil, err
	}
	return &api.AdminUserHeaders{SetCookie: api.NewOptString(cookie), Response: body}, nil
}

func setupDone() *apiError {
	return newErr(http.StatusConflict, typeInvalidRequest, "", "", "An admin already exists on this host. Log in instead.")
}

// AdminGetSession implements GET /admin/v1/session.
func (a *Admin) AdminGetSession(ctx context.Context) (*api.AdminUser, error) {
	s, ok := SessionOf(ctx)
	if !ok {
		return nil, unauthenticated()
	}
	body, err := toAdminUser(s.AdminUser)
	if err != nil {
		return nil, err
	}
	return &body, nil
}

// AdminCreateSession implements POST /admin/v1/session (login). The lockout check comes first
// (429, Retry-After); a wrong password and an unknown name give the same 401. A success marks the
// credential used, drops expired sessions, starts the session and audits admin.session_started
// in one unit of work.
func (a *Admin) AdminCreateSession(ctx context.Context, req *api.AdminCredentials) (*api.AdminUserHeaders, error) {
	res, err := a.auth.Authenticate(ctx, req.Username, req.Password, remoteOf(ctx))
	if errors.Is(err, ErrInvalidLogin) {
		return nil, newErr(http.StatusUnauthorized, typeInvalidRequest, "", "", "The login name or password is wrong.")
	}
	if err != nil {
		return nil, err
	}
	var cookie string
	err = a.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
		ir := a.cfg.Identity
		if err := ir.Credentials.MarkUsed(tx, res.CredentialID); err != nil {
			return err
		}
		if _, err := ir.Sessions.DeleteExpired(tx, a.now()); err != nil {
			return err
		}
		sid, c, err := a.startSession(ctx, tx, res.PrincipalID)
		if err != nil {
			return err
		}
		cookie = c
		return a.record(tx, audit.Event{Action: ActionSessionStarted, ActorID: res.PrincipalID, Detail: map[string]string{"session_id": sid}})
	})
	if err != nil {
		return nil, err
	}
	body, err := toAdminUser(res.AdminUser)
	if err != nil {
		return nil, err
	}
	return &api.AdminUserHeaders{SetCookie: api.NewOptString(cookie), Response: body}, nil
}

// AdminDeleteSession implements DELETE /admin/v1/session (logout): it revokes the session,
// audits admin.session_ended and clears the cookie.
func (a *Admin) AdminDeleteSession(ctx context.Context) (*api.AdminDeleteSessionNoContent, error) {
	s, ok := SessionOf(ctx)
	if !ok {
		return nil, unauthenticated()
	}
	err := a.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
		if err := a.cfg.Identity.Sessions.Revoke(tx, s.ID); err != nil {
			return err
		}
		return a.record(tx, audit.Event{Action: ActionSessionEnded, ActorID: s.PrincipalID, Detail: map[string]string{"session_id": s.ID}})
	})
	if err != nil {
		return nil, err
	}
	return &api.AdminDeleteSessionNoContent{SetCookie: api.NewOptString(credential.ClearCookie(credential.AdminSessionCookie))}, nil
}
