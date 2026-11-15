package consumerauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/inCoderKaka/local-server/internal/auth/credential"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// PasskeyAdapter is the passkey Adapter (req_001 D-13, D-135): WebAuthn credentials in
// idn_credential (kind passkey, the base64url credential id as identifier, the public key and
// counters as JSON in secret; nothing in a row can log anyone in). A ceremony is two round trips,
// so the adapter holds the state between them in memory, single use and with the end time of the
// consumer.passkey_ceremony_ttl setting; a restart of the host abandons ceremonies in flight, and
// the person starts again. It shares the password adapter's lockout tracker: a failed assertion
// counts against the client's address, and an address that is locked out cannot log in with a
// passkey either. It is safe for concurrent use.
type PasskeyAdapter struct {
	db  *storage.DB
	st  *settings.Store
	ir  *identity.Repos
	now func() time.Time
	log *slog.Logger
	// lock is the password adapter's tracker (see New).
	lock *credential.Lockout
	cer  ceremonies
}

var _ Adapter = (*PasskeyAdapter)(nil)

// NewPasskeyAdapter returns the adapter. lock is the tracker that counts failed logins; now nil
// means time.Now.
func NewPasskeyAdapter(db *storage.DB, st *settings.Store, ir *identity.Repos, lock *credential.Lockout, now func() time.Time, log *slog.Logger) (*PasskeyAdapter, error) {
	if db == nil || st == nil || ir == nil || lock == nil || log == nil {
		return nil, errors.New("consumerauth: DB, Settings, Identity, a lockout tracker and a logger are required")
	}
	if now == nil {
		now = time.Now
	}
	return &PasskeyAdapter{db: db, st: st, ir: ir, now: now, log: log, lock: lock, cer: ceremonies{m: map[string]*ceremony{}}}, nil
}

// Kind is identity.CredentialPasskey.
func (a *PasskeyAdapter) Kind() string { return identity.CredentialPasskey }

var errNoStringLogin = errors.New("consumerauth: a passkey has no string login; use the ceremony methods")

// Prepare is not supported: a passkey sign-up is a ceremony (BeginSignup and FinishSignup).
func (a *PasskeyAdapter) Prepare(context.Context, string, string) (Enrollment, error) {
	return nil, errNoStringLogin
}

// Authenticate is not supported: a passkey login is a ceremony (BeginLogin and FinishLogin).
func (a *PasskeyAdapter) Authenticate(context.Context, string, string, string) (Verified, error) {
	return Verified{}, errNoStringLogin
}

// relyingParty is the WebAuthn relying party of a request: the Host the person's browser used,
// which the credential guard has already checked is the host's own name. A passkey is bound to it
// by the browser, so a ceremony begun at one name cannot be finished at another.
type relyingParty struct {
	ID     string // the host name, without port
	Origin string // scheme://host[:port], the form a browser puts in clientDataJSON
}

// pageHost is what serve records of the request for the relying party.
type pageHost struct {
	host string // the Host header
	tls  bool
}

type pageHostKey struct{}

func pageHostOf(ctx context.Context) (pageHost, bool) {
	h, ok := ctx.Value(pageHostKey{}).(pageHost)
	return h, ok
}

// relyingPartyOf derives the relying party from the request's Host and its scheme, the way the
// credential guard reads them (https when the connection is TLS). A host that is an IP address
// cannot be a relying party: browsers refuse it.
func relyingPartyOf(ctx context.Context) (relyingParty, error) {
	h, ok := pageHostOf(ctx)
	if !ok {
		return relyingParty{}, errors.New("consumerauth: request carries no host")
	}
	host, port := h.host, ""
	if hh, p, err := net.SplitHostPort(h.host); err == nil {
		host, port = hh, p
	}
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	scheme, def := "http", "80"
	if h.tls {
		scheme, def = "https", "443"
	}
	if protocol.ValidateRPID(host) != nil {
		return relyingParty{}, errRelyingParty()
	}
	origin := scheme + "://" + host
	if port != "" && port != def {
		origin += ":" + port
	}
	return relyingParty{ID: host, Origin: origin}, nil
}

func errRelyingParty() *Error {
	return badRequest("", "Passkeys need the host's name in the address, not an IP address.")
}

func errCeremony() *Error {
	return badRequest("ceremony_id", "This passkey ceremony is not valid or has expired. Start again.")
}

func errPasskeyResponse() *Error {
	return badRequest("credential", "The passkey could not be verified. Start again.")
}

func errPasskeyLogin() *Error {
	return newErr(401, typeInvalidRequest, "", "", "The passkey could not be verified. Try again or use another way to log in.")
}

func errPasskeyTaken() *Error { return conflict("This passkey is already registered.") }

// Ceremony is a started ceremony: the id to send back with the authenticator's response and the
// WebAuthn options as JSON.
type Ceremony struct {
	ID      string
	Options json.RawMessage
}

type ceremonyKind string

const (
	ceremonySignup ceremonyKind = "signup"
	ceremonyAdd    ceremonyKind = "add"
	ceremonyLogin  ceremonyKind = "login"
)

// ceremony is what the host remembers between the two round trips. It holds the WebAuthn session
// (the challenge), the relying party the ceremony began at and, for a sign-up, the invite and the
// profile that were checked at the start; never the invite token.
type ceremony struct {
	kind       ceremonyKind
	session    webauthn.SessionData
	rp         relyingParty
	started    time.Time
	expires    time.Time
	userHandle []byte
	addr       string            // the client host that began it, port removed; "" when unknown
	signup     checkedRedemption // ceremonySignup
	principal  string            // ceremonyAdd: the principal that began it
}

// ceremonies is the in-memory store of started ceremonies, by id.
type ceremonies struct {
	mu sync.Mutex
	m  map[string]*ceremony
}

// put stores c under id. Expired ceremonies are dropped first; while the client address of c has
// perAddress ceremonies stored, that address's oldest is dropped, so one address cannot push out
// the ceremonies of others; when the store is still at the limit, the oldest is dropped to make
// room.
func (s *ceremonies) put(id string, c *ceremony, now time.Time, limit, perAddress int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.m {
		if !now.Before(v.expires) {
			delete(s.m, k)
		}
	}
	if c.addr != "" {
		for {
			var n int64
			oldest, first := "", true
			var at time.Time
			for k, v := range s.m {
				if v.addr != c.addr {
					continue
				}
				n++
				if first || v.started.Before(at) {
					oldest, at, first = k, v.started, false
				}
			}
			if n < perAddress {
				break
			}
			delete(s.m, oldest)
		}
	}
	for int64(len(s.m)) >= limit {
		oldest, first := "", true
		var at time.Time
		for k, v := range s.m {
			if first || v.started.Before(at) {
				oldest, at, first = k, v.started, false
			}
		}
		delete(s.m, oldest)
	}
	s.m[id] = c
}

// clientHost is addr with the port removed, or addr itself when it has none.
func clientHost(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// take removes and returns the ceremony with id, if it is there and has not expired. A ceremony
// is single use: finishing it, with any outcome, consumes it.
func (s *ceremonies) take(id string, now time.Time) (*ceremony, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.m[id]
	if !ok {
		return nil, false
	}
	delete(s.m, id)
	return c, now.Before(c.expires)
}

func newCeremonyID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func newUserHandle() ([]byte, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return b, err
}

// storedPasskey is the JSON in idn_credential.secret: the credential record go-webauthn needs to
// check an assertion and the user handle the passkey was made with. The attestation the
// authenticator sent at registration is not kept.
type storedPasskey struct {
	UserHandle []byte              `json:"user_handle"`
	Credential webauthn.Credential `json:"credential"`
}

func encodePasskey(handle []byte, c webauthn.Credential) (string, error) {
	c.Attestation = webauthn.CredentialAttestation{}
	b, err := json.Marshal(storedPasskey{UserHandle: handle, Credential: c})
	return string(b), err
}

func decodePasskey(secret string) (storedPasskey, error) {
	var p storedPasskey
	err := json.Unmarshal([]byte(secret), &p)
	return p, err
}

func credentialIdentifier(id []byte) string { return base64.RawURLEncoding.EncodeToString(id) }

// passkeyUser is the WebAuthn user of a ceremony.
type passkeyUser struct {
	handle []byte
	name   string
	creds  []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte                         { return u.handle }
func (u *passkeyUser) WebAuthnName() string                       { return u.name }
func (u *passkeyUser) WebAuthnDisplayName() string                { return u.name }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// relyingPartyConfig builds the WebAuthn configuration for rp: the one origin the browser used,
// a discoverable credential with user verification required (a passkey is the only thing that
// proves the person, so it must prove more than possession), and no attestation. timeout is the
// time the browser is told; the host enforces the ceremony's end time itself, with its own clock.
func relyingPartyConfig(rp relyingParty, timeout time.Duration) (*webauthn.WebAuthn, error) {
	return webauthn.New(&webauthn.Config{
		RPID:          rp.ID,
		RPDisplayName: rp.ID,
		RPOrigins:     []string{rp.Origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
		AttestationPreference: protocol.PreferNoAttestation,
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Timeout: timeout, TimeoutUVD: timeout},
			Registration: webauthn.TimeoutConfig{Timeout: timeout, TimeoutUVD: timeout},
		},
	})
}

// remember stores c and returns the Ceremony the client gets, with options as JSON.
func (a *PasskeyAdapter) remember(ctx context.Context, c *ceremony, ttl time.Duration, options any) (Ceremony, error) {
	limit, err := a.st.Int(ctx, KeyPasskeyMaxCeremonies, "")
	if err != nil {
		return Ceremony{}, err
	}
	perAddress, err := a.st.Int(ctx, KeyPasskeyMaxCeremoniesPerAddress, "")
	if err != nil {
		return Ceremony{}, err
	}
	raw, err := json.Marshal(options)
	if err != nil {
		return Ceremony{}, err
	}
	id, err := newCeremonyID()
	if err != nil {
		return Ceremony{}, err
	}
	now := a.now()
	c.started, c.expires = now, now.Add(ttl)
	c.addr = clientHost(remoteOf(ctx))
	a.cer.put(id, c, now, limit, perAddress)
	return Ceremony{ID: id, Options: raw}, nil
}

// beginRegistration starts a registration for user at rp, excluding the credentials in exclude.
func (a *PasskeyAdapter) beginRegistration(ctx context.Context, rp relyingParty, user *passkeyUser, exclude []protocol.CredentialDescriptor, c *ceremony) (Ceremony, error) {
	ttl, err := a.st.Duration(ctx, KeyPasskeyCeremonyTTL, "")
	if err != nil {
		return Ceremony{}, err
	}
	wa, err := relyingPartyConfig(rp, ttl)
	if err != nil {
		return Ceremony{}, err
	}
	creation, session, err := wa.BeginRegistration(user, webauthn.WithExclusions(exclude))
	if err != nil {
		return Ceremony{}, fmt.Errorf("consumerauth: begin passkey registration: %w", err)
	}
	c.session, c.rp, c.userHandle = *session, rp, user.handle
	return a.remember(ctx, c, ttl, creation.Response)
}

// BeginSignup starts the registration of the passkey of a new consumer. chk is a sign-up that has
// passed Service.checkRedemption; nothing is created until FinishSignup.
func (a *PasskeyAdapter) BeginSignup(ctx context.Context, rp relyingParty, chk checkedRedemption) (Ceremony, error) {
	handle, err := newUserHandle()
	if err != nil {
		return Ceremony{}, err
	}
	user := &passkeyUser{handle: handle, name: chk.Name}
	return a.beginRegistration(ctx, rp, user, nil, &ceremony{kind: ceremonySignup, signup: chk})
}

// BeginAdd starts the registration of another passkey for the consumer behind principalID. The
// consumer's passkeys are excluded, so one authenticator is not registered twice, and a new
// passkey uses the user handle the consumer's others have.
func (a *PasskeyAdapter) BeginAdd(ctx context.Context, rp relyingParty, principalID, label string) (Ceremony, error) {
	creds, err := a.ir.Credentials.ListByPrincipal(ctx, a.db.Reader(), principalID)
	if err != nil {
		return Ceremony{}, err
	}
	var handle []byte
	var exclude []protocol.CredentialDescriptor
	for _, k := range creds {
		if k.Kind != identity.CredentialPasskey || k.RevokedAt != nil {
			continue
		}
		id, err := base64.RawURLEncoding.DecodeString(k.Identifier)
		if err != nil {
			continue
		}
		exclude = append(exclude, protocol.CredentialDescriptor{Type: protocol.PublicKeyCredentialType, CredentialID: id})
		if handle == nil {
			if sp, err := decodePasskey(k.Secret); err == nil && len(sp.UserHandle) > 0 {
				handle = sp.UserHandle
			}
		}
	}
	if handle == nil {
		if handle, err = newUserHandle(); err != nil {
			return Ceremony{}, err
		}
	}
	user := &passkeyUser{handle: handle, name: label}
	return a.beginRegistration(ctx, rp, user, exclude, &ceremony{kind: ceremonyAdd, principal: principalID})
}

// finishRegistration consumes the ceremony id of the wanted kind begun at rp, parses the
// authenticator's response and verifies it. Every refusal is an *Error; the library's text is
// never answered or logged, only its fixed type.
func (a *PasskeyAdapter) finishRegistration(ctx context.Context, rp relyingParty, id string, body []byte, want ceremonyKind, principalID string) (*webauthn.Credential, *ceremony, error) {
	c, ok := a.cer.take(id, a.now())
	if !ok || c.kind != want || c.rp != rp || (want == ceremonyAdd && c.principal != principalID) {
		return nil, nil, errCeremony()
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(body)
	if err != nil {
		a.refused(ctx, "registration", err)
		return nil, nil, errPasskeyResponse()
	}
	wa, err := relyingPartyConfig(rp, 0)
	if err != nil {
		return nil, nil, err
	}
	cred, err := wa.CreateCredential(&passkeyUser{handle: c.userHandle}, c.session, parsed)
	if err != nil {
		a.refused(ctx, "registration", err)
		return nil, nil, errPasskeyResponse()
	}
	return cred, c, nil
}

// refused logs that a ceremony response was refused, by the library's error type only: its text
// and debug fields can hold the challenge or the client's bytes.
func (a *PasskeyAdapter) refused(ctx context.Context, step string, err error) {
	typ := "unknown"
	var pe *protocol.Error
	if errors.As(err, &pe) && pe.Type != "" {
		typ = pe.Type
	}
	a.log.InfoContext(ctx, "consumerauth: passkey response refused", "step", step, "error_type", typ)
}

type passkeyEnrollment struct {
	a          *PasskeyAdapter
	identifier string
	secret     string
}

// Store inserts the passkey of principalID inside tx. It returns an *Error when the credential id
// is already registered, by anyone.
func (e *passkeyEnrollment) Store(tx *storage.Tx, principalID string) error {
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM idn_credential WHERE kind = ? AND identifier = ?`, identity.CredentialPasskey, e.identifier).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return errPasskeyTaken()
	}
	_, err := e.a.ir.Credentials.Create(tx, principalID, identity.CredentialPasskey, e.identifier, e.secret)
	return err
}

func (a *PasskeyAdapter) enrollment(handle []byte, cred *webauthn.Credential) (Enrollment, error) {
	secret, err := encodePasskey(handle, *cred)
	if err != nil {
		return nil, err
	}
	return &passkeyEnrollment{a: a, identifier: credentialIdentifier(cred.ID), secret: secret}, nil
}

// FinishSignup verifies the authenticator's response of a sign-up ceremony and returns the
// credential to store with the new consumer and the sign-up that was checked when the ceremony
// began. The Service finishes it with Service.completeRedemption, which checks the invite again.
func (a *PasskeyAdapter) FinishSignup(ctx context.Context, rp relyingParty, id string, body []byte) (Enrollment, checkedRedemption, error) {
	cred, c, err := a.finishRegistration(ctx, rp, id, body, ceremonySignup, "")
	if err != nil {
		return nil, checkedRedemption{}, err
	}
	enrol, err := a.enrollment(c.userHandle, cred)
	if err != nil {
		return nil, checkedRedemption{}, err
	}
	return enrol, c.signup, nil
}

// FinishAdd verifies the authenticator's response of an add ceremony begun by principalID and
// stores the passkey for it.
func (a *PasskeyAdapter) FinishAdd(ctx context.Context, rp relyingParty, principalID, id string, body []byte) error {
	cred, c, err := a.finishRegistration(ctx, rp, id, body, ceremonyAdd, principalID)
	if err != nil {
		return err
	}
	enrol, err := a.enrollment(c.userHandle, cred)
	if err != nil {
		return err
	}
	return a.db.Write(ctx, func(tx *storage.Tx) error { return enrol.Store(tx, principalID) })
}

// BeginLogin starts a discoverable login: no login name, the authenticator offers the passkeys
// it holds for this host.
func (a *PasskeyAdapter) BeginLogin(ctx context.Context, rp relyingParty) (Ceremony, error) {
	ttl, err := a.st.Duration(ctx, KeyPasskeyCeremonyTTL, "")
	if err != nil {
		return Ceremony{}, err
	}
	wa, err := relyingPartyConfig(rp, ttl)
	if err != nil {
		return Ceremony{}, err
	}
	assertion, session, err := wa.BeginDiscoverableLogin()
	if err != nil {
		return Ceremony{}, fmt.Errorf("consumerauth: begin passkey login: %w", err)
	}
	return a.remember(ctx, &ceremony{kind: ceremonyLogin, session: *session, rp: rp}, ttl, assertion.Response)
}

// FinishLogin verifies the assertion of a login ceremony for a client at remoteAddr. It returns
// the same 401 for every wrong answer without saying which part was wrong (an unknown
// passkey, a revoked one, a bad signature, a suspended or deleted consumer), a
// *LockedOutError while the address is locked out, and an *Error for a ceremony that is not
// valid. A wrong answer counts toward the address's lockout; a success does not clear it, as for
// a password (req_001 D-54, D-99). The returned Persist stores the new signature counter.
func (a *PasskeyAdapter) FinishLogin(ctx context.Context, rp relyingParty, id string, body []byte, remoteAddr string) (Verified, error) {
	cfg, err := credential.LoadLockout(ctx, a.st)
	if err != nil {
		return Verified{}, err
	}
	keys := credential.LoginKeys(remoteAddr, "")
	if wait := a.lock.Check(keys, a.now()); wait > 0 {
		return Verified{}, &LockedOutError{RetryAfter: wait}
	}
	c, ok := a.cer.take(id, a.now())
	if !ok || c.kind != ceremonyLogin || c.rp != rp {
		return Verified{}, errCeremony()
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body)
	if err != nil {
		a.refused(ctx, "login", err)
		return Verified{}, errPasskeyResponse()
	}
	wa, err := relyingPartyConfig(rp, 0)
	if err != nil {
		return Verified{}, err
	}
	reject := func(err error) (Verified, error) {
		a.refused(ctx, "login", err)
		a.lock.Fail(keys, a.now(), cfg)
		return Verified{}, errPasskeyLogin()
	}
	errUnknown := errors.New("unknown passkey")
	var row identity.Credential
	var internal error
	handler := func(rawID, _ []byte) (webauthn.User, error) {
		rd := a.db.Reader()
		var err error
		row, err = a.ir.Credentials.GetByIdentifier(ctx, rd, identity.CredentialPasskey, credentialIdentifier(rawID))
		if errors.Is(err, identity.ErrNotFound) || (err == nil && row.RevokedAt != nil) {
			return nil, errUnknown
		}
		if err != nil {
			internal = err
			return nil, err
		}
		p, err := a.ir.Principals.Get(ctx, rd, row.PrincipalID)
		if err != nil && !errors.Is(err, identity.ErrNotFound) {
			internal = err
			return nil, err
		}
		if err != nil || p.Kind != PrincipalKind || p.Status != identity.StatusActive {
			return nil, errUnknown
		}
		sp, err := decodePasskey(row.Secret)
		if err != nil {
			internal = fmt.Errorf("consumerauth: stored passkey: %w", err)
			return nil, internal
		}
		return &passkeyUser{handle: sp.UserHandle, creds: []webauthn.Credential{sp.Credential}}, nil
	}
	user, cred, err := wa.ValidatePasskeyLogin(handler, c.session, parsed)
	if internal != nil {
		return Verified{}, internal
	}
	if err != nil {
		return reject(err)
	}
	if cred.Authenticator.CloneWarning {
		return reject(errors.New("signature counter went backwards"))
	}
	secret, err := encodePasskey(user.WebAuthnID(), *cred)
	if err != nil {
		return Verified{}, err
	}
	return Verified{PrincipalID: row.PrincipalID, CredentialID: row.ID, Persist: func(tx *storage.Tx) error {
		return a.ir.Credentials.UpdateSecret(tx, row.ID, secret)
	}}, nil
}
