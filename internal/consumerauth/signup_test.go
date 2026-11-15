package consumerauth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/consumerauth"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func (e *env) redeem(token, username, password string) resp {
	return e.do(req{method: "POST", path: "/v1/invite-redemptions/password", body: redeemBody(token, username, password)})
}

func TestRedeemCreatesTheConsumerAndStartsASession(t *testing.T) {
	e := newEnv(t)
	inv, invID := e.invite(time.Hour)
	r := e.redeem(inv, "example-user", goodPass)
	status(t, r, http.StatusCreated)
	var body map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["kind"] != "individual" || body["status"] != "active" || body["billing_mode"] != "none" || body["has_login"] != true ||
		body["username"] != "example-user" || body["display_name"] != "Example Consumer" || body["email"] != "user@example.invalid" {
		t.Fatalf("consumer = %v", body)
	}
	tok := r.token()
	if tok == "" {
		t.Fatal("no session cookie")
	}
	// The new session works, and the consumer is the one just created.
	me := e.get(tok, "/v1/me")
	status(t, me, http.StatusOK)
	if !strings.Contains(me.Body.String(), body["id"].(string)) {
		t.Fatalf("me = %s", me.Body.String())
	}
	// The invite is redeemed by that consumer; a credential, two consent rows and no grant exist.
	if n := e.count(`SELECT count(*) FROM idn_invite WHERE id = ? AND redeemed_at IS NOT NULL AND redeemed_consumer_id = ?`, invID, body["id"]); n != 1 {
		t.Fatalf("invite not marked redeemed by the consumer (%d)", n)
	}
	if n := e.count(`SELECT count(*) FROM grt_grant`); n != 0 {
		t.Fatalf("a new consumer got a grant (%d rows)", n)
	}
	// Only a hash of the password and of the token is stored.
	var secret, hash string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT secret FROM idn_credential WHERE identifier = 'example-user'`).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "$argon2id$") || strings.Contains(secret, goodPass) {
		t.Fatalf("stored credential = %q", secret)
	}
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT token_hash FROM idn_session`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == tok || strings.Contains(hash, tok) {
		t.Fatal("the session token is stored, not only its hash")
	}
}

func TestSessionCookieFlags(t *testing.T) {
	e := newEnv(t)
	inv, _ := e.invite(time.Hour)
	r := e.redeem(inv, "example-user", goodPass)
	status(t, r, http.StatusCreated)
	cookies := r.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %v", cookies)
	}
	c := cookies[0]
	if c.Name != "ls_session" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Fatalf("cookie = %+v", c)
	}
	if c.MaxAge != int((24 * time.Hour).Seconds()) { // consumer.session_ttl default
		t.Fatalf("max-age = %d, want the 24h default", c.MaxAge)
	}
	// The lifetime is the runtime setting, read at the moment a session starts.
	e.set(consumerauth.KeySessionTTL, "2h")
	l := e.do(req{method: "POST", path: "/v1/session/password", body: loginBody("example-user", goodPass)})
	status(t, l, http.StatusOK)
	if got := l.Result().Cookies()[0].MaxAge; got != int((2 * time.Hour).Seconds()) {
		t.Fatalf("max-age = %d after the setting changed, want 2h", got)
	}
	if err := e.st.Set(context.Background(), "host", consumerauth.KeySessionTTL, "", "0s"); err == nil {
		t.Fatal("a zero session lifetime was accepted")
	}
}

func TestInviteIsSingleUse(t *testing.T) {
	e := newEnv(t)
	inv, _ := e.invite(time.Hour)
	status(t, e.redeem(inv, "first-user", goodPass), http.StatusCreated)
	r := e.redeem(inv, "second-user", goodPass)
	status(t, r, http.StatusGone)
	if n := e.count(`SELECT count(*) FROM idn_consumer`); n != 1 {
		t.Fatalf("consumers = %d, want 1", n)
	}
	if n := e.count(`SELECT count(*) FROM idn_credential WHERE identifier = 'second-user'`); n != 0 {
		t.Fatal("the refused sign-up left a credential")
	}
}

func TestConcurrentRedemptionsOfOneInviteHaveOneWinner(t *testing.T) {
	e := newEnv(t)
	inv, _ := e.invite(time.Hour)
	const n = 8
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = e.redeem(inv, fmt.Sprintf("racer-%d", i), goodPass).Code
		}()
	}
	wg.Wait()
	won, gone := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusCreated:
			won++
		case http.StatusGone:
			gone++
		default:
			t.Fatalf("codes = %v", codes)
		}
	}
	if won != 1 || gone != n-1 {
		t.Fatalf("codes = %v, want one 201 and %d 410", codes, n-1)
	}
	for _, q := range []string{`idn_consumer`, `idn_session`, `idn_credential`} {
		if got := e.count(`SELECT count(*) FROM ` + q); got != 1 {
			t.Fatalf("%s has %d rows, want 1", q, got)
		}
	}
	if got := e.count(`SELECT count(*) FROM idn_consent`); got != 2 {
		t.Fatalf("consent rows = %d, want 2", got)
	}
}

func TestExpiredAndRevokedInvitesAreRefused(t *testing.T) {
	e := newEnv(t)
	expiring, _ := e.invite(time.Hour)
	revoked, revID := e.invite(time.Hour)
	if err := e.db.Write(context.Background(), func(tx *storage.Tx) error { return e.ir.Invites.Revoke(tx, revID) }); err != nil {
		t.Fatal(err)
	}
	status(t, e.redeem(revoked, "a-user", goodPass), http.StatusGone)
	e.clk.advance(2 * time.Hour)
	status(t, e.redeem(expiring, "b-user", goodPass), http.StatusGone)
	if n := e.count(`SELECT count(*) FROM idn_consumer`); n != 0 {
		t.Fatalf("consumers = %d, want 0", n)
	}
}

func TestInviteTokenThatIsNotValidIsNotFound(t *testing.T) {
	e := newEnv(t)
	inv, _ := e.invite(time.Hour)
	wrongSecret := inv[:len(inv)-4] + strings.Map(func(r rune) rune {
		if r == '0' {
			return '1'
		}
		return '0'
	}, inv[len(inv)-4:])
	for name, tok := range map[string]string{"malformed": "not-a-token", "empty-ish": "ls_invite_", "wrong secret": wrongSecret,
		"unknown prefix": "ls_invite_" + strings.Repeat("a", 16) + "_" + strings.Repeat("b", 64)} {
		r := e.redeem(tok, "a-user", goodPass)
		if r.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, body %s", name, r.Code, r.Body.String())
		}
		if strings.Contains(r.Body.String(), tok) && tok != "" {
			t.Fatalf("%s: the token is echoed", name)
		}
	}
	// The real invite is still redeemable after those attempts.
	status(t, e.redeem(inv, "a-user", goodPass), http.StatusCreated)
}

func TestConsentRowsCarryThePolicyVersions(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)
	if tok == "" {
		t.Fatal("no session")
	}
	rows, err := e.db.Reader().Query(context.Background(), `SELECT purpose, policy_version, granted_at, withdrawn_at, evidence FROM idn_consent ORDER BY purpose`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var purpose, version string
		var granted int64
		var withdrawn *int64
		var evidence *string
		if err := rows.Scan(&purpose, &version, &granted, &withdrawn, &evidence); err != nil {
			t.Fatal(err)
		}
		if withdrawn != nil || granted != t0.UnixMilli() || evidence == nil {
			t.Fatalf("consent row = %v %v %v %v", granted, withdrawn, evidence, purpose)
		}
		got[purpose] = version
	}
	if len(got) != 2 || got[consumerauth.PurposeTerms] != "terms-2026-11-v1" || got[consumerauth.PurposePrivacy] != "privacy-2026-11-v1" {
		t.Fatalf("consent = %v", got)
	}
}

// The evidence of a sign-up holds the two version ids, the acceptance time, the invite, the source
// address without its port and the browser user agent cut to the host's cap, and trusts no
// forwarding header (req_001 D-153).
func TestConsentEvidenceHoldsVersionsTimeInviteAddressAndUserAgent(t *testing.T) {
	e := newEnv(t)
	inv, invID := e.invite(time.Hour)
	ua := strings.Repeat("a", 300)
	r := e.do(req{method: "POST", path: "/v1/invite-redemptions/password", body: redeemBody(inv, "evidence-user", goodPass),
		header: map[string]string{"User-Agent": ua, "X-Forwarded-For": "203.0.113.9"}})
	status(t, r, http.StatusCreated)
	rows, err := e.db.Reader().Query(context.Background(), `SELECT evidence FROM idn_consent`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var ev map[string]string
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			t.Fatalf("evidence %q: %v", raw, err)
		}
		want := map[string]string{"via": "invite_redemption", "terms_version": consumerauth.TermsVersion, "privacy_version": consumerauth.PrivacyVersion,
			"accepted_at": t0.UTC().Format(time.RFC3339), "invite_id": invID, "ip": "100.64.0.7", "user_agent": strings.Repeat("a", 256)}
		if len(ev) != len(want) {
			t.Fatalf("evidence = %v, want %v", ev, want)
		}
		for k, v := range want {
			if ev[k] != v {
				t.Fatalf("evidence[%s] = %q, want %q", k, ev[k], v)
			}
		}
		if strings.Contains(raw, "203.0.113.9") {
			t.Fatalf("evidence trusts X-Forwarded-For: %s", raw)
		}
		n++
	}
	if n != 2 {
		t.Fatalf("%d consent rows, want 2", n)
	}
}

func TestSignUpThatDoesNotAcceptTheCurrentPoliciesIsRefused(t *testing.T) {
	e := newEnv(t)
	inv, _ := e.invite(time.Hour)
	body := func(terms, privacy string) string {
		return `{"invite_token":"` + inv + `","display_name":"A","username":"a-user","password":"` + goodPass +
			`","consent":{"terms_version":"` + terms + `","privacy_version":"` + privacy + `"}}`
	}
	for name, b := range map[string]string{
		"stale terms":     body("terms-2026-09-v0", consumerauth.PrivacyVersion),
		"stale privacy":   body(consumerauth.TermsVersion, "privacy-2026-09-v0"),
		"empty terms":     body("", consumerauth.PrivacyVersion),
		"empty privacy":   body(consumerauth.TermsVersion, ""),
		"no consent":      `{"invite_token":"` + inv + `","display_name":"A","username":"a-user","password":"` + goodPass + `"}`,
		"missing privacy": `{"invite_token":"` + inv + `","display_name":"A","username":"a-user","password":"` + goodPass + `","consent":{"terms_version":"` + consumerauth.TermsVersion + `"}}`,
	} {
		r := e.do(req{method: "POST", path: "/v1/invite-redemptions/password", body: b})
		if r.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, body %s", name, r.Code, r.Body.String())
		}
	}
	if e.count(`SELECT count(*) FROM idn_consumer`) != 0 || e.count(`SELECT count(*) FROM idn_consent`) != 0 {
		t.Fatal("a refused sign-up left rows")
	}
	// The invite was not spent by the refusals.
	status(t, e.redeem(inv, "a-user", goodPass), http.StatusCreated)
}

func TestSignUpFieldChecks(t *testing.T) {
	e := newEnv(t)
	inv, _ := e.invite(time.Hour)
	r := e.redeem(inv, "a-user", "short")
	status(t, r, http.StatusBadRequest)
	if !strings.Contains(r.Body.String(), `"password"`) || strings.Contains(r.Body.String(), "short\"") {
		t.Fatalf("body = %s", r.Body.String())
	}
	status(t, e.redeem(inv, "   ", goodPass), http.StatusBadRequest)
	status(t, e.redeem(inv, "bad\\u0007name", goodPass), http.StatusBadRequest)
	bad := strings.Replace(redeemBody(inv, "a-user", goodPass), "user@example.invalid", "not an email", 1)
	status(t, e.do(req{method: "POST", path: "/v1/invite-redemptions/password", body: bad}), http.StatusBadRequest)
	// The login name is taken by an earlier consumer.
	status(t, e.redeem(inv, "a-user", goodPass), http.StatusCreated)
	inv2, _ := e.invite(time.Hour)
	status(t, e.redeem(inv2, "a-user", goodPass), http.StatusConflict)
	// A refused name leaves the invite pending.
	status(t, e.redeem(inv2, "another-user", goodPass), http.StatusCreated)
}
