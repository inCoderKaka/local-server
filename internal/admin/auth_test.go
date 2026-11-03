package admin_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/admin"
	"github.com/inCoderKaka/local-server/internal/auth/credential"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// First run creates exactly one admin, its session and one audit row; a second call is 409
// (req_001 D-10).
func TestSetupCreatesExactlyOneAdmin(t *testing.T) {
	e := newEnv(t)
	r := e.do(req{path: "/admin/v1/setup"})
	status(t, r, http.StatusOK)
	if r.json()["admin_exists"] != false {
		t.Fatalf("setup status = %s", r.Body.String())
	}

	r = e.do(req{method: "POST", path: "/admin/v1/setup", body: creds("  "+testUser+" ", testPass), origin: adminOrigin})
	status(t, r, http.StatusCreated)
	body := r.json()
	if body["username"] != testUser || body["id"] == "" || body["created_at"] == nil {
		t.Fatalf("body = %s", r.Body.String())
	}
	if strings.Contains(r.Body.String(), testPass) || strings.Contains(r.Body.String(), "argon2") {
		t.Fatalf("the response leaks a secret: %s", r.Body.String())
	}
	setCookie := r.Header().Get("Set-Cookie")
	for _, want := range []string{credential.AdminSessionCookie + "=", "HttpOnly", "Secure", "SameSite=Strict", "Path=/", "Max-Age=43200"} {
		if !strings.Contains(setCookie, want) {
			t.Errorf("Set-Cookie %q lacks %q", setCookie, want)
		}
	}
	token := tokenOf(t, r)

	r = e.do(req{path: "/admin/v1/setup"})
	if r.json()["admin_exists"] != true {
		t.Fatalf("setup status after = %s", r.Body.String())
	}
	r = e.do(req{method: "POST", path: "/admin/v1/setup", body: creds("second", testPass), origin: adminOrigin})
	status(t, r, http.StatusConflict)
	r.errBody(t)

	for q, want := range map[string]int{
		`SELECT count(*) FROM idn_admin_user`: 1, `SELECT count(*) FROM idn_principal`: 1, `SELECT count(*) FROM idn_credential`: 1,
		`SELECT count(*) FROM idn_session`: 1, `SELECT count(*) FROM aud_event WHERE action = 'admin.created'`: 1,
	} {
		if n := e.count(q); n != want {
			t.Errorf("%s = %d, want %d", q, n, want)
		}
	}
	// What is stored: the username trimmed, an argon2id hash (never the password), and only the
	// hash of the session token.
	var identifier, secret, kind, hash string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT identifier, secret, kind FROM idn_credential`).Scan(&identifier, &secret, &kind); err != nil {
		t.Fatal(err)
	}
	if identifier != testUser || kind != "password" || !strings.HasPrefix(secret, "$argon2id$v=19$m=19456,t=2,p=1$") || strings.Contains(secret, testPass) {
		t.Errorf("credential = %q %q %q", identifier, kind, secret)
	}
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT token_hash FROM idn_session`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == token || hash != credential.HashToken(token) {
		t.Errorf("token_hash = %q for token %q", hash, token)
	}
	var pk string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT kind FROM idn_principal`).Scan(&pk); err != nil || pk != "host_admin" {
		t.Errorf("principal kind = %q, %v", pk, err)
	}
	rows := e.auditRows("admin.created")
	pid := e.adminPrincipal()
	if len(rows) != 1 || rows[0].actor != pid || rows[0].target != pid || rows[0].detail["username"] != testUser {
		t.Errorf("audit rows = %+v", rows)
	}
	if _, ok := rows[0].detail["password"]; ok {
		t.Error("the audit row has a password")
	}
	// The cookie works.
	status(t, e.call(token, "GET", "/admin/v1/session", ""), http.StatusOK)
}

func TestSetupConcurrentCreatesExactlyOne(t *testing.T) {
	e := newEnv(t)
	const n = 16
	var created, conflict atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r := e.do(req{method: "POST", path: "/admin/v1/setup", body: creds("admin"+string(rune('a'+i)), testPass), origin: adminOrigin})
			switch r.Code {
			case http.StatusCreated:
				created.Add(1)
			case http.StatusConflict:
				conflict.Add(1)
			default:
				t.Errorf("status %d: %s", r.Code, r.Body.String())
			}
		}()
	}
	close(start)
	wg.Wait()
	if created.Load() != 1 || conflict.Load() != n-1 {
		t.Fatalf("created %d, conflict %d; want 1 and %d", created.Load(), conflict.Load(), n-1)
	}
	for _, q := range []string{`SELECT count(*) FROM idn_admin_user`, `SELECT count(*) FROM idn_credential`, `SELECT count(*) FROM idn_session`, `SELECT count(*) FROM aud_event WHERE action = 'admin.created'`} {
		if c := e.count(q); c != 1 {
			t.Errorf("%s = %d, want 1", q, c)
		}
	}
}

func TestSetupValidatesTheLogin(t *testing.T) {
	e := newEnv(t)
	for name, c := range map[string]struct{ user, pass, param string }{
		"short password":    {"admin", "short", "password"},
		"blank name":        {"   ", testPass, "username"},
		"control character": {"ad\x07min", testPass, "username"},
		"newline in name":   {"ad\nmin", testPass, "username"},
		"name too long":     {strings.Repeat("x", 129), testPass, "username"},
	} {
		r := e.do(req{method: "POST", path: "/admin/v1/setup", body: creds(c.user, c.pass), origin: adminOrigin})
		status(t, r, http.StatusBadRequest)
		if p := r.errBody(t)["param"]; p != c.param {
			t.Errorf("%s: param = %v, want %s", name, p, c.param)
		}
		if strings.Contains(r.Body.String(), c.pass) && c.pass != "" {
			t.Errorf("%s: the answer repeats the password", name)
		}
	}
	if n := e.count(`SELECT count(*) FROM idn_admin_user`); n != 0 {
		t.Fatalf("a refused setup created %d admins", n)
	}
	// 128 characters is the longest name; the longest message is bounded by the body setting.
	status(t, e.do(req{method: "POST", path: "/admin/v1/setup", body: creds(strings.Repeat("é", 128), testPass), origin: adminOrigin}), http.StatusCreated)
}

func TestSetupPasswordPolicyIsASetting(t *testing.T) {
	e := newEnv(t)
	e.set(credential.KeyMinLength, "30")
	r := e.do(req{method: "POST", path: "/admin/v1/setup", body: creds(testUser, testPass), origin: adminOrigin})
	status(t, r, http.StatusBadRequest)
	if msg := r.errBody(t)["message"].(string); !strings.Contains(msg, "30") {
		t.Errorf("message = %q; want the minimum length", msg)
	}
	e.set(credential.KeyMinLength, "8")
	status(t, e.do(req{method: "POST", path: "/admin/v1/setup", body: creds(testUser, "eight!!!"), origin: adminOrigin}), http.StatusCreated)
}

func TestLoginRightWrongAndUnknownLookAlike(t *testing.T) {
	e := newEnv(t)
	e.setup()

	wrong := e.do(req{method: "POST", path: "/admin/v1/session", body: creds(testUser, "not the password at all"), origin: adminOrigin})
	unknown := e.do(req{method: "POST", path: "/admin/v1/session", body: creds("nobody", "not the password at all"), origin: adminOrigin})
	status(t, wrong, http.StatusUnauthorized)
	status(t, unknown, http.StatusUnauthorized)
	if wrong.Body.String() != unknown.Body.String() {
		t.Fatalf("wrong password and unknown user differ:\n%s\n%s", wrong.Body.String(), unknown.Body.String())
	}
	if wrong.Header().Get("Set-Cookie") != "" || unknown.Header().Get("Set-Cookie") != "" {
		t.Error("a failed login set a cookie")
	}
	// A name is matched exactly: another case is another (unknown) name.
	status(t, e.do(req{method: "POST", path: "/admin/v1/session", body: creds(strings.ToUpper(testUser), testPass), origin: adminOrigin}), http.StatusUnauthorized)

	before := e.count(`SELECT count(*) FROM aud_event`)
	r := e.do(req{method: "POST", path: "/admin/v1/session", body: creds(testUser, testPass), origin: adminOrigin})
	status(t, r, http.StatusOK)
	token := tokenOf(t, r)
	if r.json()["username"] != testUser {
		t.Fatalf("body = %s", r.Body.String())
	}
	// Failed attempts are not audited here (req_001 D-89, PLT-F-055); a success audits
	// admin.session_started with the admin as the actor.
	if after := e.count(`SELECT count(*) FROM aud_event`); after != before+1 {
		t.Errorf("audit rows %d -> %d; want exactly the session_started row", before, after)
	}
	rows := e.auditRows("admin.session_started")
	if len(rows) != 1 || rows[0].actor != e.adminPrincipal() {
		t.Errorf("session_started rows = %+v", rows)
	}
	var used *int64
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT last_used_at FROM idn_credential`).Scan(&used); err != nil || used == nil {
		t.Errorf("last_used_at = %v, %v", used, err)
	}
	r = e.call(token, "GET", "/admin/v1/session", "")
	status(t, r, http.StatusOK)
	if r.json()["username"] != testUser {
		t.Errorf("session body = %s", r.Body.String())
	}
	// No password and no token reached the log.
	logs := e.tel.buf.String()
	for _, secret := range []string{testPass, "not the password at all", token} {
		if strings.Contains(logs, secret) {
			t.Errorf("the log contains a secret: %q", secret)
		}
	}
}

func TestLoginLockout(t *testing.T) {
	e := newEnv(t)
	e.setup()
	e.set(credential.KeyLockoutFailures, "3")
	e.set(credential.KeyLockoutDuration, "20m")
	login := func(user, pass, remote string) resp {
		return e.do(req{method: "POST", path: "/admin/v1/session", body: creds(user, pass), origin: adminOrigin, remote: remote})
	}
	for i := 0; i < 3; i++ {
		status(t, login(testUser, "wrong password!!", "203.0.113.5:1000"), http.StatusUnauthorized)
	}
	// Locked out: the right password is refused too, with 429 and Retry-After.
	r := login(testUser, testPass, "203.0.113.5:1000")
	status(t, r, http.StatusTooManyRequests)
	inner := r.errBody(t)
	if inner["code"] != "rate_limit_exceeded" || inner["type"] != "requests" {
		t.Errorf("error = %v", inner)
	}
	if ra := r.Header().Get("Retry-After"); ra != "1200" {
		t.Errorf("Retry-After = %q, want 1200", ra)
	}
	// The name is locked from any address, and the address for any name.
	status(t, login(testUser, testPass, "198.51.100.7:1"), http.StatusTooManyRequests)
	status(t, login("someone-else", "x", "203.0.113.5:2"), http.StatusTooManyRequests)
	// Another name from another address is not affected.
	status(t, login("someone-else", "x", "198.51.100.7:1"), http.StatusUnauthorized)
	// A lockout is not lifted by waiting less than its duration, and ends after it.
	e.clk.advance(19 * time.Minute)
	status(t, login(testUser, testPass, "203.0.113.5:1000"), http.StatusTooManyRequests)
	e.clk.advance(2 * time.Minute)
	status(t, login(testUser, testPass, "203.0.113.5:1000"), http.StatusOK)
}

func TestLogoutEndsTheSession(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	other := e.login() // a second session stays valid

	r := e.call(token, "DELETE", "/admin/v1/session", "")
	status(t, r, http.StatusNoContent)
	cleared := r.Header().Get("Set-Cookie")
	for _, want := range []string{credential.AdminSessionCookie + "=;", "Max-Age=0", "HttpOnly", "Secure", "SameSite=Strict"} {
		if !strings.Contains(cleared, want) && !(want == "Max-Age=0" && strings.Contains(cleared, "Max-Age=0")) {
			t.Errorf("clearing cookie %q lacks %q", cleared, want)
		}
	}
	status(t, e.call(token, "GET", "/admin/v1/session", ""), http.StatusUnauthorized)
	status(t, e.call(other, "GET", "/admin/v1/session", ""), http.StatusOK)
	rows := e.auditRows("admin.session_ended")
	if len(rows) != 1 || rows[0].actor != e.adminPrincipal() {
		t.Errorf("session_ended rows = %+v", rows)
	}
	var revoked *int64
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT revoked_at FROM idn_session WHERE token_hash = ?`, credential.HashToken(token)).Scan(&revoked); err != nil || revoked == nil {
		t.Errorf("revoked_at = %v, %v", revoked, err)
	}
	// A second logout with the dead cookie is a 401, not a second audit row.
	status(t, e.call(token, "DELETE", "/admin/v1/session", ""), http.StatusUnauthorized)
	if n := len(e.auditRows("admin.session_ended")); n != 1 {
		t.Errorf("session_ended rows = %d after a repeat", n)
	}
}

func TestSessionExpiresAndIsCleanedAtLogin(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	e.set(admin.KeySessionTTL, "1h")
	status(t, e.call(token, "GET", "/admin/v1/session", ""), http.StatusOK)
	e.clk.advance(12*time.Hour - time.Second)
	status(t, e.call(token, "GET", "/admin/v1/session", ""), http.StatusOK)
	e.clk.advance(2 * time.Second)
	status(t, e.call(token, "GET", "/admin/v1/session", ""), http.StatusUnauthorized)

	// The expired row is deleted by the next login, which starts a session of the new TTL.
	if n := e.count(`SELECT count(*) FROM idn_session`); n != 1 {
		t.Fatalf("sessions = %d", n)
	}
	r := e.do(req{method: "POST", path: "/admin/v1/session", body: creds(testUser, testPass), origin: adminOrigin})
	status(t, r, http.StatusOK)
	if c := r.Header().Get("Set-Cookie"); !strings.Contains(c, "Max-Age=3600") {
		t.Errorf("Set-Cookie = %q, want a one hour session", c)
	}
	if n := e.count(`SELECT count(*) FROM idn_session`); n != 1 {
		t.Errorf("sessions after login = %d, want the expired one deleted", n)
	}
}

func TestSessionOfASuspendedAdminIsRefused(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		return e.ir.Principals.SetStatus(tx, e.adminPrincipal(), identity.StatusSuspended)
	})
	if err != nil {
		t.Fatal(err)
	}
	status(t, e.call(token, "GET", "/admin/v1/session", ""), http.StatusUnauthorized)
	status(t, e.do(req{method: "POST", path: "/admin/v1/session", body: creds(testUser, testPass), origin: adminOrigin}), http.StatusUnauthorized)
}

func TestProtectedRoutesNeedASession(t *testing.T) {
	e := newEnv(t)
	e.setup()
	for _, rt := range []struct{ method, path, body string }{
		{"GET", "/admin/v1/session", ""},
		{"DELETE", "/admin/v1/session", ""},
		{"GET", "/admin/v1/host", ""},
		{"POST", "/admin/v1/host/pause", ""},
		{"POST", "/admin/v1/host/resume", ""},
		{"GET", "/admin/v1/host-metrics", ""},
		{"GET", "/admin/v1/host-metrics/history", ""},
		{"GET", "/admin/v1/settings", ""},
		{"PATCH", "/admin/v1/settings", `{"values":{}}`},
		{"GET", "/admin/v1/audit-events", ""},
		{"GET", "/admin/v1/usage?granularity=day", ""},
		{"GET", "/admin/v1/consumers", ""},
	} {
		for name, r := range map[string]resp{
			"no cookie":        e.do(req{method: rt.method, path: rt.path, body: rt.body, origin: adminOrigin}),
			"unknown cookie":   e.do(req{method: rt.method, path: rt.path, body: rt.body, origin: adminOrigin, cookie: "not-a-session-token"}),
			"huge cookie":      e.do(req{method: rt.method, path: rt.path, body: rt.body, origin: adminOrigin, cookie: strings.Repeat("A", 3000)}),
			"access key bears": e.do(req{method: rt.method, path: rt.path, body: rt.body, origin: adminOrigin, header: map[string]string{"Authorization": "Bearer ls_abc_def"}}),
		} {
			if r.Code != http.StatusUnauthorized {
				t.Errorf("%s %s (%s) = %d, want 401", rt.method, rt.path, name, r.Code)
				continue
			}
			r.errBody(t)
		}
	}
}

// Authenticate is the one function the HTTP login and the recovery-code command share.
func TestAuthenticateFunction(t *testing.T) {
	e := newEnv(t)
	e.setup()
	ctx := context.Background()
	res, err := e.admin.Authenticate(ctx, testUser, testPass, "127.0.0.1:1")
	if err != nil || res.PrincipalID != e.adminPrincipal() || res.AdminUser.Username != testUser {
		t.Fatalf("Authenticate = %+v, %v", res, err)
	}
	for _, c := range []struct{ user, pass string }{{testUser, "wrong"}, {"nobody", testPass}, {"", ""}, {strings.Repeat("x", 100000), "x"}} {
		if _, err := e.admin.Authenticate(ctx, c.user, c.pass, "127.0.0.1:1"); !errors.Is(err, admin.ErrInvalidLogin) {
			t.Errorf("Authenticate(%.10q) = %v; want ErrInvalidLogin", c.user, err)
		}
	}
	e.set(credential.KeyLockoutFailures, "1")
	_, _ = e.admin.Authenticate(ctx, testUser, "wrong", "127.0.0.1:2")
	var le *admin.LockedOutError
	if _, err := e.admin.Authenticate(ctx, testUser, testPass, "127.0.0.1:3"); !errors.As(err, &le) || le.RetryAfter <= 0 {
		t.Errorf("after the threshold: %v; want a *LockedOutError", err)
	}
}

func TestSessionContextCarriesThePrincipal(t *testing.T) {
	// PrincipalIDOf is "" without a session, and the settings audit row carries the real one
	// (TestSettingsWriteIsAudited).
	if id := admin.PrincipalIDOf(context.Background()); id != "" {
		t.Errorf("PrincipalIDOf(empty) = %q", id)
	}
}

// A flood of parallel guesses cannot all pass the lockout check before the first failure is
// counted: at most the threshold of them is checked against the password.
func TestParallelGuessesStopAtTheLockoutThreshold(t *testing.T) {
	e := newEnv(t)
	e.setup()
	e.set(credential.KeyLockoutFailures, "3")
	const n = 24
	var unauthorized, limited atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r := e.do(req{method: "POST", path: "/admin/v1/session", body: creds(testUser, "wrong password!!"), origin: adminOrigin})
			switch r.Code {
			case http.StatusUnauthorized:
				unauthorized.Add(1)
			case http.StatusTooManyRequests:
				limited.Add(1)
			default:
				t.Errorf("status %d", r.Code)
			}
		}()
	}
	close(start)
	wg.Wait()
	if unauthorized.Load() != 3 || limited.Load() != n-3 {
		t.Fatalf("%d got 401 and %d got 429; want 3 and %d", unauthorized.Load(), limited.Load(), n-3)
	}
}
