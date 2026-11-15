package consumerauth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/inCoderKaka/local-server/internal/auth/credential"
	"github.com/inCoderKaka/local-server/internal/consumerauth"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/storage"
)

var b64 = base64.RawURLEncoding

// softKey is a software authenticator: one ES256 passkey that answers a registration with a
// "none" attestation and a login with a signed assertion, the way a platform authenticator does
// (user presence and user verification set).
type softKey struct {
	t      *testing.T
	priv   *ecdsa.PrivateKey
	id     []byte
	handle []byte
	count  uint32
}

func newSoftKey(t *testing.T) *softKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		t.Fatal(err)
	}
	return &softKey{t: t, priv: priv, id: id}
}

// ceremony is a begin response.
type ceremony struct {
	ID      string                     `json:"ceremony_id"`
	Options map[string]json.RawMessage `json:"options"`
}

func (c ceremony) challenge() string {
	var s string
	_ = json.Unmarshal(c.Options["challenge"], &s)
	return s
}

func (c ceremony) rpID() string {
	var rp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(c.Options["rp"], &rp)
	return rp.ID
}

func (c ceremony) userID() []byte {
	var u struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(c.Options["user"], &u)
	b, _ := b64.DecodeString(u.ID)
	return b
}

func (c ceremony) excluded() []string {
	var ex []struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(c.Options["excludeCredentials"], &ex)
	var out []string
	for _, x := range ex {
		out = append(out, x.ID)
	}
	return out
}

func parseCeremony(t *testing.T, r resp) ceremony {
	t.Helper()
	status(t, r, http.StatusOK)
	var c ceremony
	if err := json.Unmarshal(r.Body.Bytes(), &c); err != nil || c.ID == "" || c.challenge() == "" {
		t.Fatalf("not a ceremony: %v %s", err, r.Body.String())
	}
	return c
}

func (k *softKey) clientData(typ, origin, challenge string) []byte {
	b, err := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": origin, "crossOrigin": false})
	if err != nil {
		k.t.Fatal(err)
	}
	return b
}

func rpHash(rpID string) []byte { h := sha256.Sum256([]byte(rpID)); return h[:] }

// create answers a registration ceremony for rpID at origin.
func (k *softKey) create(c ceremony, rpID, origin string) map[string]any {
	k.t.Helper()
	k.handle = c.userID()
	x := k.priv.PublicKey.X.FillBytes(make([]byte, 32))
	y := k.priv.PublicKey.Y.FillBytes(make([]byte, 32))
	cose, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
	if err != nil {
		k.t.Fatal(err)
	}
	auth := append(rpHash(rpID), 0x01|0x04|0x40) // UP, UV, AT
	auth = binary.BigEndian.AppendUint32(auth, k.count)
	auth = append(auth, make([]byte, 16)...) // AAGUID
	auth = binary.BigEndian.AppendUint16(auth, uint16(len(k.id)))
	auth = append(auth, k.id...)
	auth = append(auth, cose...)
	obj, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": auth})
	if err != nil {
		k.t.Fatal(err)
	}
	return map[string]any{
		"id": b64.EncodeToString(k.id), "rawId": b64.EncodeToString(k.id), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(k.clientData("webauthn.create", origin, c.challenge())),
			"attestationObject": b64.EncodeToString(obj),
			"transports":        []string{"internal"},
		},
		"clientExtensionResults":  map[string]any{},
		"authenticatorAttachment": "platform",
	}
}

// assert answers a login ceremony for rpID at origin, with the next counter value.
func (k *softKey) assert(c ceremony, rpID, origin string) map[string]any {
	k.t.Helper()
	k.count++
	auth := append(rpHash(rpID), 0x01|0x04) // UP, UV
	auth = binary.BigEndian.AppendUint32(auth, k.count)
	cd := k.clientData("webauthn.get", origin, c.challenge())
	cdHash := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, auth...), cdHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, k.priv, digest[:])
	if err != nil {
		k.t.Fatal(err)
	}
	return map[string]any{
		"id": b64.EncodeToString(k.id), "rawId": b64.EncodeToString(k.id), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(cd),
			"authenticatorData": b64.EncodeToString(auth),
			"signature":         b64.EncodeToString(sig),
			"userHandle":        b64.EncodeToString(k.handle),
		},
		"clientExtensionResults": map[string]any{},
	}
}

func finishBody(t *testing.T, c ceremony, cred map[string]any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"ceremony_id": c.ID, "credential": cred})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func signupBeginBody(token string) string {
	return `{"invite_token":"` + token + `","display_name":"Passkey Consumer","email":"user@example.invalid","consent":{"terms_version":"` +
		consumerauth.TermsVersion + `","privacy_version":"` + consumerauth.PrivacyVersion + `"}}`
}

const (
	pathSignupBegin  = "/v1/invite-redemptions/passkey/begin"
	pathSignupFinish = "/v1/invite-redemptions/passkey/finish"
	pathLoginBegin   = "/v1/session/passkey/begin"
	pathLoginFinish  = "/v1/session/passkey/finish"
	pathAddBegin     = "/v1/me/passkeys/begin"
	pathAddFinish    = "/v1/me/passkeys/finish"
)

func (e *env) post(path, body string) resp { return e.do(req{method: "POST", path: path, body: body}) }

// passkeySignUp signs a new consumer up with k from a fresh invite and returns the response of
// the finish route.
func (e *env) passkeySignUp(k *softKey) (token string, r resp) {
	e.t.Helper()
	inv, _ := e.invite(time.Hour)
	c := parseCeremony(e.t, e.post(pathSignupBegin, signupBeginBody(inv)))
	r = e.post(pathSignupFinish, finishBody(e.t, c, k.create(c, pageHost, pageOrigin)))
	status(e.t, r, http.StatusCreated)
	return r.token(), r
}

// passkeyLogin logs in with k at the page's own host.
func (e *env) passkeyLogin(k *softKey) resp {
	e.t.Helper()
	c := parseCeremony(e.t, e.post(pathLoginBegin, ""))
	return e.post(pathLoginFinish, finishBody(e.t, c, k.assert(c, pageHost, pageOrigin)))
}

func TestPasskeySignUpCreatesTheConsumerAndStartsASession(t *testing.T) {
	e := newEnv(t)
	k := newSoftKey(t)
	inv, invID := e.invite(time.Hour)
	begin := e.post(pathSignupBegin, signupBeginBody(inv))
	c := parseCeremony(t, begin)
	// The options carry the relying party of the request's Host, a discoverable credential with
	// user verification, and the browser's timeout from the ceremony TTL setting (5m).
	opts := begin.Body.String()
	if c.rpID() != pageHost || !strings.Contains(opts, `"residentKey":"required"`) || !strings.Contains(opts, `"userVerification":"required"`) ||
		!strings.Contains(opts, `"timeout":300000`) {
		t.Fatalf("options = %s", opts)
	}
	// Nothing exists until the ceremony finishes.
	if e.count(`SELECT count(*) FROM idn_consumer`) != 0 || e.count(`SELECT count(*) FROM idn_credential`) != 0 ||
		e.count(`SELECT count(*) FROM idn_invite WHERE redeemed_at IS NOT NULL`) != 0 {
		t.Fatal("the begin route created something")
	}
	r := e.post(pathSignupFinish, finishBody(t, c, k.create(c, pageHost, pageOrigin)))
	status(t, r, http.StatusCreated)
	var body map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// A passkey-only consumer: it has a login, and no login name.
	if body["kind"] != "individual" || body["status"] != "active" || body["billing_mode"] != "none" || body["has_login"] != true ||
		body["username"] != nil || body["display_name"] != "Passkey Consumer" || body["email"] != "user@example.invalid" {
		t.Fatalf("consumer = %v", body)
	}
	tok := r.token()
	if tok == "" {
		t.Fatal("no session cookie")
	}
	cookie := r.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.MaxAge != int((12*time.Hour).Seconds()) {
		t.Fatalf("cookie = %+v", cookie)
	}
	me := e.get(tok, "/v1/me")
	status(t, me, http.StatusOK)
	if !strings.Contains(me.Body.String(), body["id"].(string)) || !strings.Contains(me.Body.String(), `"has_login":true`) {
		t.Fatalf("me = %s", me.Body.String())
	}
	// The invite is redeemed by the consumer; the two consent rows name the versions in force.
	if e.count(`SELECT count(*) FROM idn_invite WHERE id = ? AND redeemed_at IS NOT NULL AND redeemed_consumer_id = ?`, invID, body["id"]) != 1 {
		t.Fatal("invite not marked redeemed by the consumer")
	}
	if e.count(`SELECT count(*) FROM idn_consent WHERE policy_version IN (?, ?)`, consumerauth.TermsVersion, consumerauth.PrivacyVersion) != 2 {
		t.Fatal("consent rows missing")
	}
	// One passkey credential: the credential id is the identifier, and the row holds a public key.
	var ident, secret string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT identifier, secret FROM idn_credential WHERE kind = 'passkey'`).Scan(&ident, &secret); err != nil {
		t.Fatal(err)
	}
	if ident != b64.EncodeToString(k.id) || !strings.Contains(secret, `"publicKey"`) || strings.Contains(secret, c.challenge()) {
		t.Fatalf("credential = %q %q", ident, secret)
	}
	// Single use: the same ceremony cannot finish twice, and the invite cannot start another.
	status(t, e.post(pathSignupFinish, finishBody(t, c, k.create(c, pageHost, pageOrigin))), http.StatusBadRequest)
	status(t, e.post(pathSignupBegin, signupBeginBody(inv)), http.StatusGone)
	if e.count(`SELECT count(*) FROM idn_consumer`) != 1 {
		t.Fatal("a second consumer was created")
	}
}

func TestPasskeySignUpChecksTheInviteAtBeginAndAgainAtFinish(t *testing.T) {
	e := newEnv(t)
	// The checks of a password sign-up apply at begin: same answers.
	status(t, e.post(pathSignupBegin, signupBeginBody("ls_invite_nope_nope")), http.StatusNotFound)
	inv, _ := e.invite(time.Hour)
	bad := strings.Replace(signupBeginBody(inv), consumerauth.TermsVersion, "terms-0", 1)
	status(t, e.post(pathSignupBegin, bad), http.StatusBadRequest)
	status(t, e.post(pathSignupBegin, strings.Replace(signupBeginBody(inv), `"Passkey Consumer"`, `"  "`, 1)), http.StatusBadRequest)
	status(t, e.post(pathSignupBegin, strings.Replace(signupBeginBody(inv), "user@example.invalid", "not-an-address", 1)), http.StatusBadRequest)

	// Two ceremonies of one invite: the first to finish wins, the other gets 410 at finish, and
	// the loser leaves no consumer.
	k1, k2 := newSoftKey(t), newSoftKey(t)
	c1 := parseCeremony(t, e.post(pathSignupBegin, signupBeginBody(inv)))
	c2 := parseCeremony(t, e.post(pathSignupBegin, signupBeginBody(inv)))
	status(t, e.post(pathSignupFinish, finishBody(t, c1, k1.create(c1, pageHost, pageOrigin))), http.StatusCreated)
	status(t, e.post(pathSignupFinish, finishBody(t, c2, k2.create(c2, pageHost, pageOrigin))), http.StatusGone)
	if e.count(`SELECT count(*) FROM idn_consumer`) != 1 || e.count(`SELECT count(*) FROM idn_credential`) != 1 {
		t.Fatal("the refused sign-up left rows")
	}

	// An invite withdrawn between begin and finish is refused at finish.
	inv2, id2 := e.invite(time.Hour)
	c3 := parseCeremony(t, e.post(pathSignupBegin, signupBeginBody(inv2)))
	e.revokeInvite(id2)
	k3 := newSoftKey(t)
	status(t, e.post(pathSignupFinish, finishBody(t, c3, k3.create(c3, pageHost, pageOrigin))), http.StatusGone)
	if e.count(`SELECT count(*) FROM idn_consumer`) != 1 {
		t.Fatal("a consumer was created from a withdrawn invite")
	}
}

func (e *env) revokeInvite(id string) {
	e.t.Helper()
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error { return e.ir.Invites.Revoke(tx, id) })
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) revokeCredentials() {
	e.t.Helper()
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := tx.Exec(`UPDATE idn_credential SET revoked_at = 1 WHERE kind = 'passkey'`)
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

func TestPasskeyLoginWithoutAUsername(t *testing.T) {
	e := newEnv(t)
	k := newSoftKey(t)
	signed, _ := e.passkeySignUp(k)
	begin := e.post(pathLoginBegin, "")
	c := parseCeremony(t, begin)
	if strings.Contains(begin.Body.String(), "allowCredentials") {
		t.Fatalf("a discoverable login lists credentials: %s", begin.Body.String())
	}
	r := e.post(pathLoginFinish, finishBody(t, c, k.assert(c, pageHost, pageOrigin)))
	status(t, r, http.StatusOK)
	tok := r.token()
	if tok == "" || tok == signed {
		t.Fatalf("token = %q", tok)
	}
	status(t, e.get(tok, "/v1/me"), http.StatusOK)
	if e.count(`SELECT count(*) FROM idn_credential WHERE kind = 'passkey' AND last_used_at IS NOT NULL`) != 1 {
		t.Fatal("the login was not marked on the credential")
	}
	// The signature counter is kept, so a replay of an old counter is noticed.
	var secret string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT secret FROM idn_credential WHERE kind = 'passkey'`).Scan(&secret); err != nil || !strings.Contains(secret, `"signCount":1`) {
		t.Fatalf("secret = %q (%v)", secret, err)
	}
	k.count = 0 // the authenticator answers with a counter that went backwards: a cloned key
	status(t, e.passkeyLogin(k), http.StatusUnauthorized)
	k.count = 5
	status(t, e.passkeyLogin(k), http.StatusOK)
	// The ceremony TTL setting reaches the browser's timeout.
	e.set(consumerauth.KeyPasskeyCeremonyTTL, "90s")
	if !strings.Contains(e.post(pathLoginBegin, "").Body.String(), `"timeout":90000`) {
		t.Fatal("the browser's timeout is not the setting")
	}
}

func TestPasskeyLoginRefusalsAnswerTheSame(t *testing.T) {
	e := newEnv(t)
	k := newSoftKey(t)
	e.passkeySignUp(k)
	e.set(credential.KeyLockoutFailures, "50") // this test is about the answers, not the lockout
	login := func(mutate func(c ceremony, cred map[string]any)) resp {
		c := parseCeremony(t, e.post(pathLoginBegin, ""))
		cred := k.assert(c, pageHost, pageOrigin)
		if mutate != nil {
			mutate(c, cred)
		}
		return e.post(pathLoginFinish, finishBody(t, c, cred))
	}
	badSig := login(func(_ ceremony, cred map[string]any) {
		cred["response"].(map[string]any)["signature"] = b64.EncodeToString([]byte("not a signature"))
	})
	status(t, badSig, http.StatusUnauthorized)
	other := newSoftKey(t) // a passkey the host does not know
	unknown := func() resp {
		c := parseCeremony(t, e.post(pathLoginBegin, ""))
		other.handle = []byte("some other handle")
		return e.post(pathLoginFinish, finishBody(t, c, other.assert(c, pageHost, pageOrigin)))
	}()
	status(t, unknown, http.StatusUnauthorized)
	wrongHandle := login(func(_ ceremony, cred map[string]any) {
		cred["response"].(map[string]any)["userHandle"] = b64.EncodeToString([]byte("someone else"))
	})
	status(t, wrongHandle, http.StatusUnauthorized)
	e.setStatus(identity.StatusSuspended)
	suspended := login(nil)
	status(t, suspended, http.StatusUnauthorized)
	for name, r := range map[string]resp{"unknown": unknown, "handle": wrongHandle, "suspended": suspended} {
		if r.Body.String() != badSig.Body.String() {
			t.Fatalf("%s is told apart from a bad signature: %s vs %s", name, r.Body.String(), badSig.Body.String())
		}
	}
	// A deleted consumer is refused the same way, and an active one is let in again.
	e.setStatus(identity.StatusDeleted)
	deleted := login(nil)
	status(t, deleted, http.StatusUnauthorized)
	if deleted.Body.String() != badSig.Body.String() {
		t.Fatal("a deleted consumer is told apart")
	}
	e.setStatus(identity.StatusActive)
	status(t, login(nil), http.StatusOK)
	// A revoked passkey is refused the same way.
	e.revokeCredentials()
	revoked := login(nil)
	status(t, revoked, http.StatusUnauthorized)
	if revoked.Body.String() != badSig.Body.String() {
		t.Fatal("a revoked passkey is told apart")
	}
}

func TestPasskeyFailuresCountTowardTheSameLockoutAsPasswords(t *testing.T) {
	e := newEnv(t)
	e.signUp("example-user", goodPass)
	k := newSoftKey(t)
	e.passkeySignUp(k)
	e.set(credential.KeyLockoutFailures, "3")
	e.set(credential.KeyLockoutDuration, "15m")
	// Two bad passkey assertions and one wrong password lock the address out of both.
	for range 2 {
		c := parseCeremony(t, e.post(pathLoginBegin, ""))
		cred := k.assert(c, pageHost, pageOrigin)
		cred["response"].(map[string]any)["signature"] = b64.EncodeToString([]byte("nope"))
		status(t, e.post(pathLoginFinish, finishBody(t, c, cred)), http.StatusUnauthorized)
	}
	status(t, e.login("example-user", "wrong password!!"), http.StatusUnauthorized)
	c := parseCeremony(t, e.post(pathLoginBegin, ""))
	locked := e.post(pathLoginFinish, finishBody(t, c, k.assert(c, pageHost, pageOrigin)))
	status(t, locked, http.StatusTooManyRequests)
	if got := locked.Header().Get("Retry-After"); got != strconv.Itoa(15*60) {
		t.Fatalf("Retry-After = %q, want 900", got)
	}
	status(t, e.login("example-user", goodPass), http.StatusTooManyRequests)
	// A success does not clear the address; after the lockout it works again.
	e.clk.advance(15*time.Minute + time.Second)
	c = parseCeremony(t, e.post(pathLoginBegin, ""))
	status(t, e.post(pathLoginFinish, finishBody(t, c, k.assert(c, pageHost, pageOrigin))), http.StatusOK)
}

func TestAddAPasskeyToALoggedInConsumerThenLogInWithIt(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)
	// The routes need the session.
	status(t, e.do(req{method: "POST", path: pathAddBegin, origin: "-"}), http.StatusUnauthorized)
	status(t, e.do(req{method: "POST", path: pathAddFinish, body: `{"ceremony_id":"x","credential":{}}`, origin: "-"}), http.StatusUnauthorized)

	k := newSoftKey(t)
	c := parseCeremony(t, e.send(tok, "POST", pathAddBegin, ""))
	if len(c.excluded()) != 0 {
		t.Fatalf("excluded = %v", c.excluded())
	}
	status(t, e.send(tok, "POST", pathAddFinish, finishBody(t, c, k.create(c, pageHost, pageOrigin))), http.StatusNoContent)
	if e.count(`SELECT count(*) FROM idn_credential WHERE kind = 'passkey'`) != 1 || e.count(`SELECT count(*) FROM idn_consumer`) != 1 {
		t.Fatal("the passkey was not stored for the one consumer")
	}
	// Logging in with the passkey reaches the same consumer.
	me := e.get(tok, "/v1/me")
	r := e.passkeyLogin(k)
	status(t, r, http.StatusOK)
	var a, b map[string]any
	if json.Unmarshal(me.Body.Bytes(), &a) != nil || json.Unmarshal(r.Body.Bytes(), &b) != nil || a["id"] != b["id"] || b["username"] != "example-user" {
		t.Fatalf("me = %s, login = %s", me.Body.String(), r.Body.String())
	}
	// A second passkey excludes the first and keeps the user handle.
	c2 := parseCeremony(t, e.send(tok, "POST", pathAddBegin, ""))
	if ex := c2.excluded(); len(ex) != 1 || ex[0] != b64.EncodeToString(k.id) {
		t.Fatalf("excluded = %v", ex)
	}
	if string(c2.userID()) != string(k.handle) {
		t.Fatal("a second passkey got a new user handle")
	}
	k2 := newSoftKey(t)
	status(t, e.send(tok, "POST", pathAddFinish, finishBody(t, c2, k2.create(c2, pageHost, pageOrigin))), http.StatusNoContent)
	status(t, e.passkeyLogin(k2), http.StatusOK)
	// The same authenticator cannot be registered twice, nor by another consumer.
	c3 := parseCeremony(t, e.send(tok, "POST", pathAddBegin, ""))
	status(t, e.send(tok, "POST", pathAddFinish, finishBody(t, c3, k2.create(c3, pageHost, pageOrigin))), http.StatusConflict)
	tok2 := e.signUp("other-user", goodPass)
	c4 := parseCeremony(t, e.send(tok2, "POST", pathAddBegin, ""))
	status(t, e.send(tok2, "POST", pathAddFinish, finishBody(t, c4, k2.create(c4, pageHost, pageOrigin))), http.StatusConflict)
	if e.count(`SELECT count(*) FROM idn_credential WHERE kind = 'passkey'`) != 2 {
		t.Fatal("a refused registration left a credential")
	}
}

func TestAnAddCeremonyBelongsToTheSessionThatBeganIt(t *testing.T) {
	e := newEnv(t)
	tokA := e.signUp("user-a", goodPass)
	tokB := e.signUp("user-b", goodPass)
	k := newSoftKey(t)
	c := parseCeremony(t, e.send(tokA, "POST", pathAddBegin, ""))
	status(t, e.send(tokB, "POST", pathAddFinish, finishBody(t, c, k.create(c, pageHost, pageOrigin))), http.StatusBadRequest)
	// It was consumed by the attempt: the right session cannot use it either.
	status(t, e.send(tokA, "POST", pathAddFinish, finishBody(t, c, k.create(c, pageHost, pageOrigin))), http.StatusBadRequest)
	if e.count(`SELECT count(*) FROM idn_credential WHERE kind = 'passkey'`) != 0 {
		t.Fatal("a passkey was stored")
	}
}

func TestCeremonyErrors(t *testing.T) {
	e := newEnv(t)
	k := newSoftKey(t)
	inv, _ := e.invite(time.Hour)
	begin := func() ceremony { return parseCeremony(t, e.post(pathSignupBegin, signupBeginBody(inv))) }
	finish := func(c ceremony, cred map[string]any) resp { return e.post(pathSignupFinish, finishBody(t, c, cred)) }
	wantCeremonyErr := func(r resp) {
		t.Helper()
		status(t, r, http.StatusBadRequest)
		if !strings.Contains(r.Body.String(), `"param":"ceremony_id"`) {
			t.Fatalf("body = %s", r.Body.String())
		}
	}
	wantResponseErr := func(r resp) {
		t.Helper()
		status(t, r, http.StatusBadRequest)
		if !strings.Contains(r.Body.String(), `"param":"credential"`) {
			t.Fatalf("body = %s", r.Body.String())
		}
	}

	// An id that was never issued.
	c := begin()
	wantCeremonyErr(finish(ceremony{ID: "never-issued", Options: c.Options}, k.create(c, pageHost, pageOrigin)))
	// Expired: the host's own clock decides, and the ceremony is gone afterwards.
	c = begin()
	e.clk.advance(5*time.Minute + time.Second)
	wantCeremonyErr(finish(c, k.create(c, pageHost, pageOrigin)))
	// The TTL is the setting: a ceremony begun after a change lives that long.
	e.set(consumerauth.KeyPasskeyCeremonyTTL, "30s")
	c = begin()
	e.clk.advance(31 * time.Second)
	wantCeremonyErr(finish(c, k.create(c, pageHost, pageOrigin)))
	// Wrong origin, wrong challenge, wrong relying party, wrong type of ceremony: refused, and
	// each attempt consumes its ceremony.
	c = begin()
	wantResponseErr(finish(c, k.create(c, pageHost, "https://evil.example")))
	wantCeremonyErr(finish(c, k.create(c, pageHost, pageOrigin))) // replay after a failure
	c = begin()
	other := begin()
	wantResponseErr(finish(c, k.create(other, pageHost, pageOrigin))) // another ceremony's challenge
	c = begin()
	wantResponseErr(finish(c, k.create(c, "evil.example", pageOrigin))) // rpIdHash of another site
	c = begin()
	login := parseCeremony(t, e.post(pathLoginBegin, ""))
	wantCeremonyErr(e.post(pathSignupFinish, finishBody(t, login, k.create(login, pageHost, pageOrigin)))) // a login ceremony is not a sign-up
	// A response that is not a credential.
	c = begin()
	wantResponseErr(finish(c, map[string]any{"id": "x"}))
	// Nothing was created by any of this, and the invite is still good.
	if e.count(`SELECT count(*) FROM idn_consumer`) != 0 || e.count(`SELECT count(*) FROM idn_credential`) != 0 {
		t.Fatal("a refused ceremony left rows")
	}
	c = begin()
	status(t, finish(c, k.create(c, pageHost, pageOrigin)), http.StatusCreated)
}

func TestCeremonyStateIsBounded(t *testing.T) {
	e := newEnv(t)
	k := newSoftKey(t)
	if err := e.st.Set(context.Background(), "host", consumerauth.KeyPasskeyMaxCeremonies, "", "0"); err == nil {
		t.Fatal("a zero ceremony limit was accepted")
	}
	if err := e.st.Set(context.Background(), "host", consumerauth.KeyPasskeyCeremonyTTL, "", "0s"); err == nil {
		t.Fatal("a zero ceremony TTL was accepted")
	}
	e.set(consumerauth.KeyPasskeyMaxCeremonies, "2")
	first := parseCeremony(t, e.post(pathLoginBegin, ""))
	e.clk.advance(time.Second)
	second := parseCeremony(t, e.post(pathLoginBegin, ""))
	e.clk.advance(time.Second)
	third := parseCeremony(t, e.post(pathLoginBegin, ""))
	// The oldest was dropped to make room; the two newest are still there.
	status(t, e.post(pathLoginFinish, finishBody(t, first, k.assert(first, pageHost, pageOrigin))), http.StatusBadRequest)
	k.handle = []byte("h")
	if r := e.post(pathLoginFinish, finishBody(t, second, k.assert(second, pageHost, pageOrigin))); r.Code != http.StatusUnauthorized {
		t.Fatalf("second = %d %s", r.Code, r.Body.String())
	}
	if r := e.post(pathLoginFinish, finishBody(t, third, k.assert(third, pageHost, pageOrigin))); r.Code != http.StatusUnauthorized {
		t.Fatalf("third = %d %s", r.Code, r.Body.String())
	}
}

func TestRelyingPartyComesFromTheRequestHost(t *testing.T) {
	e := newEnv(t)
	k := newSoftKey(t)
	// Loopback is a relying party of its own: localhost, over http, with the port in the origin.
	inv, _ := e.invite(time.Hour)
	c := parseCeremony(t, e.do(req{method: "POST", path: pathSignupBegin, body: signupBeginBody(inv), host: "localhost:8443"}))
	if c.rpID() != "localhost" {
		t.Fatalf("rp id = %q", c.rpID())
	}
	cred := k.create(c, "localhost", "http://localhost:8443")
	// A ceremony begun at one name finishes at the same name only.
	status(t, e.do(req{method: "POST", path: pathSignupFinish, body: finishBody(t, c, cred), host: pageHost}), http.StatusBadRequest)
	c = parseCeremony(t, e.do(req{method: "POST", path: pathSignupBegin, body: signupBeginBody(inv), host: "localhost:8443"}))
	cred = k.create(c, "localhost", "http://localhost:8443")
	status(t, e.do(req{method: "POST", path: pathSignupFinish, body: finishBody(t, c, cred), host: "localhost:9000"}), http.StatusBadRequest)
	// The browser's origin must be the request's: another port is another origin.
	c = parseCeremony(t, e.do(req{method: "POST", path: pathSignupBegin, body: signupBeginBody(inv), host: "localhost:8443"}))
	status(t, e.do(req{method: "POST", path: pathSignupFinish, body: finishBody(t, c, k.create(c, "localhost", "http://localhost:9000")), host: "localhost:8443"}), http.StatusBadRequest)
	c = parseCeremony(t, e.do(req{method: "POST", path: pathSignupBegin, body: signupBeginBody(inv), host: "localhost:8443"}))
	status(t, e.do(req{method: "POST", path: pathSignupFinish, body: finishBody(t, c, k.create(c, "localhost", "http://localhost:8443")), host: "localhost:8443"}), http.StatusCreated)

	// Over TLS the origin is https, with the default port left out, as a browser writes it.
	k2 := newSoftKey(t)
	inv2, _ := e.invite(time.Hour)
	c = parseCeremony(t, e.do(req{method: "POST", path: pathSignupBegin, body: signupBeginBody(inv2), host: pageHost + ":443", tls: true}))
	if c.rpID() != pageHost {
		t.Fatalf("rp id = %q", c.rpID())
	}
	status(t, e.do(req{method: "POST", path: pathSignupFinish, body: finishBody(t, c, k2.create(c, pageHost, pageOrigin)), host: pageHost, tls: true}), http.StatusBadRequest) // http origin over TLS
	c = parseCeremony(t, e.do(req{method: "POST", path: pathSignupBegin, body: signupBeginBody(inv2), host: pageHost + ":443", tls: true}))
	status(t, e.do(req{method: "POST", path: pathSignupFinish, body: finishBody(t, c, k2.create(c, pageHost, "https://"+pageHost)), host: pageHost, tls: true}), http.StatusCreated)

	// An IP address cannot be a relying party; the guard lets loopback through, so say why.
	r := e.do(req{method: "POST", path: pathLoginBegin, host: "127.0.0.1:8443"})
	status(t, r, http.StatusBadRequest)
	if !strings.Contains(r.Body.String(), "IP address") {
		t.Fatalf("body = %s", r.Body.String())
	}
	// A host the guard refuses never reaches a ceremony.
	status(t, e.do(req{method: "POST", path: pathLoginBegin, host: "evil.example"}), http.StatusForbidden)
}

func TestPasskeyRoutesAreBuiltAndKeepToTheirSecurity(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{pathSignupBegin, pathSignupFinish, pathLoginBegin, pathLoginFinish} {
		r := e.do(req{method: "POST", path: p, body: `{}`})
		if r.Code == http.StatusNotImplemented {
			t.Fatalf("%s is still 501", p)
		}
	}
	// Malformed bodies are 400s with the envelope, never a panic or 500.
	status(t, e.post(pathLoginFinish, `{"ceremony_id":"x","credential":{"id":1}}`), http.StatusBadRequest)
	status(t, e.post(pathLoginFinish, `{"ceremony_id":"x"}`), http.StatusBadRequest)
	// The me routes need a session; an access key is not one on the page listener.
	for _, p := range []string{pathAddBegin, pathAddFinish} {
		status(t, e.do(req{method: "POST", path: p, body: `{"ceremony_id":"x","credential":{}}`, origin: "-"}), http.StatusUnauthorized)
	}
}

func TestNoChallengeCredentialOrPublicKeyReachesTheLogOrAnAnswer(t *testing.T) {
	e := newEnv(t)
	k := newSoftKey(t)
	inv, _ := e.invite(time.Hour)
	_, invSecret, _ := strings.Cut(strings.TrimPrefix(inv, "ls_invite_"), "_")
	var bodies []string
	collect := func(r resp) resp { bodies = append(bodies, r.Body.String()); return r }
	c := parseCeremony(t, collect(e.post(pathSignupBegin, signupBeginBody(inv))))
	collect(e.post(pathSignupFinish, finishBody(t, c, k.create(c, pageHost, "https://evil.example")))) // refused
	c2 := parseCeremony(t, collect(e.post(pathSignupBegin, signupBeginBody(inv))))
	cred := k.create(c2, pageHost, pageOrigin)
	status(t, collect(e.post(pathSignupFinish, finishBody(t, c2, cred))), http.StatusCreated)
	l := parseCeremony(t, collect(e.post(pathLoginBegin, "")))
	bad := k.assert(l, pageHost, pageOrigin)
	bad["response"].(map[string]any)["signature"] = b64.EncodeToString([]byte("nope"))
	collect(e.post(pathLoginFinish, finishBody(t, l, bad)))
	var secret string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT secret FROM idn_credential WHERE kind = 'passkey'`).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	var pub struct {
		Credential struct {
			PublicKey []byte `json:"publicKey"`
		} `json:"credential"`
	}
	if err := json.Unmarshal([]byte(secret), &pub); err != nil || len(pub.Credential.PublicKey) == 0 {
		t.Fatalf("stored key: %v", err)
	}
	logs := e.tel.buf.String()
	for _, s := range []string{c.challenge(), c2.challenge(), l.challenge(), b64.EncodeToString(k.id), b64.EncodeToString(pub.Credential.PublicKey),
		b64.EncodeToString(pub.Credential.PublicKey[:12]), invSecret, "evil.example", "attestationObject", "clientDataJSON"} {
		if strings.Contains(logs, s) {
			t.Fatalf("the log holds %q: %s", s, logs)
		}
	}
	for _, b := range bodies[len(bodies)-1:] { // the last answer is a refusal: no echo of the request
		for _, s := range []string{b64.EncodeToString(k.id), "nope", "signature"} {
			if strings.Contains(b, s) {
				t.Fatalf("an answer echoes %q: %s", s, b)
			}
		}
	}
}
