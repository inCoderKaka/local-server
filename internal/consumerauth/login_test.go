package consumerauth_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/auth/credential"
	"github.com/inCoderKaka/local-server/internal/consumerauth"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func (e *env) login(user, pass string) resp {
	return e.do(req{method: "POST", path: "/v1/session/password", body: loginBody(user, pass)})
}

func TestLoginStartsASession(t *testing.T) {
	e := newEnv(t)
	e.signUp("example-user", goodPass)
	r := e.login("example-user", goodPass)
	status(t, r, http.StatusOK)
	tok := r.token()
	status(t, e.get(tok, "/v1/me"), http.StatusOK)
	if e.count(`SELECT count(*) FROM idn_credential WHERE last_used_at IS NOT NULL`) != 1 {
		t.Fatal("the login was not marked on the credential")
	}
	// The login name is trimmed but exact otherwise.
	status(t, e.login(" example-user ", goodPass), http.StatusOK)
	status(t, e.login("EXAMPLE-USER", goodPass), http.StatusUnauthorized)
}

func TestWrongPasswordAndUnknownLoginAnswerTheSame(t *testing.T) {
	e := newEnv(t)
	e.signUp("example-user", goodPass)
	wrong := e.login("example-user", "not the password at all")
	unknown := e.login("nobody-here", "not the password at all")
	status(t, wrong, http.StatusUnauthorized)
	status(t, unknown, http.StatusUnauthorized)
	if wrong.Body.String() != unknown.Body.String() {
		t.Fatalf("answers differ:\n%s\n%s", wrong.Body.String(), unknown.Body.String())
	}
	if wrong.Header().Get("Retry-After") != unknown.Header().Get("Retry-After") || wrong.token() != "" || unknown.token() != "" {
		t.Fatal("the failed logins differ in headers or set a cookie")
	}
	// A malformed login name is the same answer too.
	bad := e.login(strings.Repeat("x", 500), "not the password at all")
	status(t, bad, http.StatusUnauthorized)
	if bad.Body.String() != wrong.Body.String() {
		t.Fatal("a malformed login name is told apart")
	}
}

func TestSuspendedConsumerCannotLogInAndIsToldTheSameAsAWrongPassword(t *testing.T) {
	e := newEnv(t)
	e.signUp("example-user", goodPass)
	e.setStatus(identity.StatusSuspended)
	r := e.login("example-user", goodPass)
	status(t, r, http.StatusUnauthorized)
	wrong := e.login("example-user", "not the password at all")
	if r.Body.String() != wrong.Body.String() {
		t.Fatal("a suspended consumer is told apart from a wrong password")
	}
}

func (e *env) setStatus(st string) {
	e.t.Helper()
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := tx.Exec(`UPDATE idn_principal SET status = ? WHERE kind = 'consumer'`, st)
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

func TestLockoutAfterNFailuresAndRetryAfter(t *testing.T) {
	e := newEnv(t)
	e.signUp("example-user", goodPass)
	e.set(credential.KeyLockoutFailures, "3") // N is a runtime setting
	e.set(credential.KeyLockoutDuration, "15m")
	for i := range 3 {
		status(t, e.login("example-user", "wrong password "+strconv.Itoa(i)+"!!"), http.StatusUnauthorized)
	}
	locked := e.login("example-user", goodPass) // even the right password is refused while locked out
	status(t, locked, http.StatusTooManyRequests)
	if got := locked.Header().Get("Retry-After"); got != strconv.Itoa(15*60) {
		t.Fatalf("Retry-After = %q, want 900", got)
	}
	if !strings.Contains(locked.Body.String(), `"code":"rate_limit_exceeded"`) || !strings.Contains(locked.Body.String(), `"type":"requests"`) {
		t.Fatalf("body = %s", locked.Body.String())
	}
	// Another address is locked out too for this login name, but a different login name from the
	// locked address is locked out by the address.
	status(t, e.do(req{method: "POST", path: "/v1/session/password", body: loginBody("example-user", goodPass), remote: "100.64.0.99:1"}), http.StatusTooManyRequests)
	status(t, e.do(req{method: "POST", path: "/v1/session/password", body: loginBody("someone-else", goodPass)}), http.StatusTooManyRequests)
	// After the lockout window the right password works again.
	e.clk.advance(15*time.Minute + time.Second)
	status(t, e.login("example-user", goodPass), http.StatusOK)
}

func TestLockoutIsSeparateFromTheAdminsAndCountsAreSettings(t *testing.T) {
	e := newEnv(t)
	e.signUp("example-user", goodPass)
	e.set(credential.KeyLockoutFailures, "2")
	status(t, e.login("example-user", "wrong password one!"), http.StatusUnauthorized)
	status(t, e.login("example-user", "wrong password two!"), http.StatusUnauthorized)
	status(t, e.login("example-user", goodPass), http.StatusTooManyRequests)
	// A raised count lets the next failures through; the tracker holds, so use a new address and name.
	e.set(credential.KeyLockoutFailures, "50")
	status(t, e.do(req{method: "POST", path: "/v1/session/password", body: loginBody("other-name", "wrong password one!"), remote: "100.64.0.50:1"}), http.StatusUnauthorized)
}

func TestLogoutRevokesTheSessionAndClearsTheCookie(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)
	other := e.login("example-user", goodPass).token()
	r := e.send(tok, "DELETE", "/v1/session", "")
	status(t, r, http.StatusNoContent)
	cs := r.Result().Cookies()
	if len(cs) != 1 || cs[0].Name != "ls_session" || cs[0].Value != "" || cs[0].MaxAge >= 0 || !cs[0].HttpOnly || !cs[0].Secure {
		t.Fatalf("cleared cookie = %+v", cs)
	}
	status(t, e.get(tok, "/v1/me"), http.StatusUnauthorized)
	status(t, e.send(tok, "DELETE", "/v1/session", ""), http.StatusUnauthorized)
	status(t, e.get(other, "/v1/me"), http.StatusOK) // other sessions stay
	if e.count(`SELECT count(*) FROM idn_session WHERE revoked_at IS NOT NULL`) != 1 {
		t.Fatal("the session row is not marked revoked")
	}
	// Without a cookie there is nothing to log out.
	status(t, e.do(req{method: "DELETE", path: "/v1/session", origin: "-"}), http.StatusUnauthorized)
}

func TestExpiredRevokedAndInactiveSessionsAreRejected(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)
	status(t, e.get(tok, "/v1/me"), http.StatusOK)
	// Expired.
	e.clk.advance(12*time.Hour + time.Second)
	status(t, e.get(tok, "/v1/me"), http.StatusUnauthorized)

	// Revoked.
	fresh := e.login("example-user", goodPass).token()
	status(t, e.get(fresh, "/v1/me"), http.StatusOK)
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		ss, err := e.ir.Sessions.ListByPrincipalTx(tx, e.consumerPrincipal())
		if err != nil {
			return err
		}
		for _, s := range ss {
			if s.RevokedAt == nil && e.clk.now().Before(s.ExpiresAt) {
				if err := e.ir.Sessions.Revoke(tx, s.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	status(t, e.get(fresh, "/v1/me"), http.StatusUnauthorized)

	// A session of a suspended consumer, then of a soft-deleted one.
	live := e.login("example-user", goodPass).token()
	status(t, e.get(live, "/v1/me"), http.StatusOK)
	e.setStatus(identity.StatusSuspended)
	status(t, e.get(live, "/v1/me"), http.StatusUnauthorized)
	e.setStatus(identity.StatusActive)
	status(t, e.get(live, "/v1/me"), http.StatusOK)
	err = e.db.Write(context.Background(), func(tx *storage.Tx) error {
		c, err := e.ir.Consumers.GetByPrincipal(context.Background(), e.db.Reader(), e.consumerPrincipal())
		if err != nil {
			return err
		}
		return e.ir.Consumers.SoftDelete(tx, c.ID)
	})
	if err != nil {
		t.Fatal(err)
	}
	status(t, e.get(live, "/v1/me"), http.StatusUnauthorized)
	status(t, e.login("example-user", goodPass), http.StatusUnauthorized)
}

func (e *env) consumerPrincipal() string {
	e.t.Helper()
	var id string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT id FROM idn_principal WHERE kind = 'consumer' LIMIT 1`).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func TestAnAdminSessionIsNotAConsumerSession(t *testing.T) {
	e := newEnv(t)
	token, err := credential.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	err = e.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := e.ir.Sessions.Create(tx, e.admin.ID, credential.HashToken(token), e.clk.now().Add(time.Hour))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	status(t, e.get(token, "/v1/me"), http.StatusUnauthorized)
	// And the admin's login name does not log in here.
	err = e.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := e.ir.Credentials.Create(tx, e.admin.ID, identity.CredentialPassword, "the-admin", "$argon2id$v=19$m=19456,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	status(t, e.login("the-admin", goodPass), http.StatusUnauthorized)
}

func TestSessionAuthNeedsTheCookie(t *testing.T) {
	e := newEnv(t)
	status(t, e.get("", "/v1/me"), http.StatusUnauthorized)
	status(t, e.get("garbage", "/v1/me"), http.StatusUnauthorized)
	status(t, e.get(strings.Repeat("x", 1000), "/v1/me"), http.StatusUnauthorized)
	_ = consumerauth.KeySessionTTL
}
