package consumerauth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/auth/credential"
	"github.com/inCoderKaka/local-server/internal/consumerauth"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func TestGuardRefusesAForeignHost(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)
	evil := "rebound.evil.example"
	for _, h := range []string{evil, evil + ":8485", "100.64.0.1", ""} {
		r := e.do(req{method: "GET", path: "/v1/me", cookie: tok, host: h})
		if h == "" {
			status(t, r, http.StatusOK) // the page's own name works
			continue
		}
		status(t, r, http.StatusForbidden)
		if strings.Contains(r.Body.String(), "evil") {
			t.Fatalf("the Host value is echoed: %s", r.Body.String())
		}
	}
	// Loopback is the machine itself, like the admin listener.
	status(t, e.do(req{method: "GET", path: "/v1/me", cookie: tok, host: "127.0.0.1:8485"}), http.StatusOK)
	if strings.Contains(e.tel.buf.String(), evil) {
		t.Fatal("the Host value is logged")
	}
}

func TestGuardChecksTheOriginOfStateChangingRequestsWithACookie(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)
	body := `{"values":{"x.pref":"1h"}}`
	cases := []struct {
		name   string
		origin string
		want   int
	}{
		{"no origin with a cookie", "", http.StatusForbidden},
		{"foreign origin", "https://evil.example", http.StatusForbidden},
		{"null origin", "null", http.StatusForbidden},
		{"wrong scheme", "https://" + pageHost, http.StatusForbidden},
		{"own origin", "-", http.StatusOK},
	}
	for _, c := range cases {
		r := e.do(req{method: "PATCH", path: "/v1/me/settings", body: body, cookie: tok, origin: c.origin})
		if r.Code != c.want {
			t.Fatalf("%s: status = %d, want %d; %s", c.name, r.Code, c.want, r.Body.String())
		}
		if strings.Contains(r.Body.String(), "evil") {
			t.Fatalf("%s: the Origin value is echoed", c.name)
		}
	}
	// A foreign origin is refused for login and sign-up too, with or without a cookie.
	status(t, e.do(req{method: "POST", path: "/v1/session/password", body: loginBody("example-user", goodPass), origin: "https://evil.example"}), http.StatusForbidden)
	status(t, e.do(req{method: "POST", path: "/v1/session/password", body: loginBody("example-user", goodPass), origin: "-"}), http.StatusOK)
	if strings.Contains(e.tel.buf.String(), "evil.example") {
		t.Fatal("the Origin value is logged")
	}
}

func TestPageListenerServes404ForTheRest(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/", "/index.html", "/assets/app.js", "/admin/v1/session", "/metrics"} {
		status(t, e.do(req{method: "GET", path: p}), http.StatusNotFound)
	}
	status(t, e.do(req{method: "GET", path: "/v1/nothing-here"}), http.StatusNotFound)
	status(t, e.do(req{method: "POST", path: "/v1/me", body: "{}", origin: "-"}), http.StatusMethodNotAllowed)
	// The passkey routes are built (passkey_test.go). A session does not open the key routes.
	tok := e.signUp("example-user", goodPass)
	status(t, e.send(tok, "POST", "/v1/chat/completions", `{"model":"m","messages":[{"role":"user","content":"x"}]}`), http.StatusUnauthorized)
}

func TestAnAccessKeyIsNotAcceptedOnThePageListener(t *testing.T) {
	e := newEnv(t)
	e.signUp("example-user", goodPass)
	prefix, secret, err := auth.NewInviteToken() // same shape as a key: prefix and secret
	if err != nil {
		t.Fatal(err)
	}
	key := "ls_" + prefix + "_" + secret
	r := e.do(req{method: "GET", path: "/v1/me", header: map[string]string{"Authorization": "Bearer " + key}})
	status(t, r, http.StatusUnauthorized)
	r = e.do(req{method: "GET", path: "/v1/usage?granularity=day", header: map[string]string{"Authorization": "Bearer " + key}})
	status(t, r, http.StatusUnauthorized)
	if strings.Contains(r.Body.String(), secret) {
		t.Fatal("the key is echoed")
	}
}

func TestBodyCapAndErrorEnvelope(t *testing.T) {
	e := newEnv(t)
	big := `{"username":"` + strings.Repeat("x", maxBody+10) + `","password":"p"}`
	r := e.do(req{method: "POST", path: "/v1/session/password", body: big})
	status(t, r, http.StatusRequestEntityTooLarge)
	var env struct {
		Error struct {
			Message string  `json:"message"`
			Type    string  `json:"type"`
			Param   *string `json:"param"`
			Code    *string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &env); err != nil || env.Error.Type != "invalid_request_error" || env.Error.Message == "" {
		t.Fatalf("not the error envelope: %s", r.Body.String())
	}
	if r.Header().Get("X-Request-Id") == "" {
		t.Fatal("no request id")
	}
	// A hostile body is refused without being echoed.
	hostile := "<script>alert(1)</script>"
	for _, b := range []string{`{"username": ` + hostile, `{"username": 5, "password": "` + hostile + `"}`, `{"` + hostile + `": 1}`, `{"username":"` + hostile + `"}`} {
		r := e.do(req{method: "POST", path: "/v1/session/password", body: b})
		if r.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d", b, r.Code)
		}
		if strings.Contains(r.Body.String(), "script") {
			t.Fatalf("the request is echoed: %s", r.Body.String())
		}
	}
	status(t, e.do(req{method: "POST", path: "/v1/session/password", body: loginBody("a", "b"), header: map[string]string{"Content-Type": "text/plain"}}), http.StatusUnsupportedMediaType)
}

func TestMeLimitsAndKeys(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)
	me := e.get(tok, "/v1/me")
	status(t, me, http.StatusOK)
	var c map[string]any
	if err := json.Unmarshal(me.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	id := c["id"].(string)

	// No grant yet: 404, not an empty limit set.
	status(t, e.get(tok, "/v1/me/limits"), http.StatusNotFound)
	// The host gives a grant and a key.
	var keyPrefix, keySecret string
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		g, err := e.gr.Grants.Create(tx, id)
		if err != nil {
			return err
		}
		m, err := e.gr.Models.Create(tx, "example-model")
		if err != nil {
			return err
		}
		if _, err := e.gr.Versions.Create(tx, g.ID, grants.NewVersion{Models: []grants.VersionModel{{ModelID: m.ID}}}); err != nil {
			return err
		}
		var secret string
		if keyPrefix, secret, err = auth.NewInviteToken(); err != nil {
			return err
		}
		keySecret = secret
		_, err = e.ir.AccessKeys.Create(tx, identity.NewAccessKey{ConsumerID: id, Prefix: keyPrefix, SecretHash: auth.HashSecret(secret)})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	lim := e.get(tok, "/v1/me/limits")
	status(t, lim, http.StatusOK)
	if !strings.Contains(lim.Body.String(), id) {
		t.Fatalf("limits = %s", lim.Body.String())
	}
	keys := e.get(tok, "/v1/me/access-keys")
	status(t, keys, http.StatusOK)
	if !strings.Contains(keys.Body.String(), keyPrefix) {
		t.Fatalf("keys = %s", keys.Body.String())
	}
	if strings.Contains(keys.Body.String(), keySecret) || strings.Contains(keys.Body.String(), auth.HashSecret(keySecret)) {
		t.Fatalf("a secret or hash is listed: %s", keys.Body.String())
	}
}

func TestSettingsAreScopedToTheCallerAndOwnedByTheConsumer(t *testing.T) {
	e := newEnv(t)
	a := e.signUp("user-a", goodPass)
	b := e.signUp("user-b", goodPass)
	list := func(tok string) map[string]map[string]any {
		r := e.get(tok, "/v1/me/settings")
		status(t, r, http.StatusOK)
		var body struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		out := map[string]map[string]any{}
		for _, s := range body.Data {
			out[s["key"].(string)] = s
		}
		return out
	}
	got := list(a)
	if len(got) != 2 || got["x.pref"]["value"] != "24h" || got["x.pref"]["default_value"] != "24h" || got["x.pref"]["owner"] != "consumer" || got["x.count"]["value"] != float64(3) {
		t.Fatalf("settings = %v", got)
	}
	if _, ok := got["limits.rate_capacity"]; ok {
		t.Fatal("a host-owned setting is listed")
	}
	// Set, then the other consumer is untouched.
	status(t, e.send(a, "PATCH", "/v1/me/settings", `{"values":{"x.pref":"1h","x.count":9}}`), http.StatusOK)
	got = list(a)
	if got["x.pref"]["value"] != "1h" || got["x.count"]["value"] != float64(9) {
		t.Fatalf("after set = %v", got)
	}
	if list(b)["x.pref"]["value"] != "24h" {
		t.Fatal("another consumer's value changed")
	}
	// A null resets to the default.
	r := e.send(a, "PATCH", "/v1/me/settings", `{"values":{"x.pref":null}}`)
	status(t, r, http.StatusOK)
	if list(a)["x.pref"]["value"] != "24h" || list(a)["x.count"]["value"] != float64(9) {
		t.Fatalf("after reset = %v", list(a))
	}
	// A host-owned key is 403, and nothing in the request is applied.
	r = e.send(a, "PATCH", "/v1/me/settings", `{"values":{"x.count":1,"`+credential.KeyLockoutFailures+`":1}}`)
	status(t, r, http.StatusForbidden)
	if list(a)["x.count"]["value"] != float64(9) {
		t.Fatal("a request with a refused key applied part of it")
	}
	if v, err := e.st.Int(context.Background(), credential.KeyLockoutFailures, ""); err != nil || v == 1 {
		t.Fatalf("the host setting changed: %v %v", v, err)
	}
	// Unknown key and wrong types are 400; so is an empty update.
	status(t, e.send(a, "PATCH", "/v1/me/settings", `{"values":{"no.such.key":1}}`), http.StatusBadRequest)
	status(t, e.send(a, "PATCH", "/v1/me/settings", `{"values":{"x.pref":5}}`), http.StatusBadRequest)
	status(t, e.send(a, "PATCH", "/v1/me/settings", `{"values":{"x.count":"nine"}}`), http.StatusBadRequest)
	status(t, e.send(a, "PATCH", "/v1/me/settings", `{"values":{"x.pref":"not a duration"}}`), http.StatusBadRequest)
	status(t, e.send(a, "PATCH", "/v1/me/settings", `{"values":{}}`), http.StatusBadRequest)
}

func TestUsageWithASessionReturnsOnlyTheCallersRows(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)
	var c struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(e.get(tok, "/v1/me").Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	other := uuid.NewString()
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		for _, g := range []string{"minute", "hour", "day"} {
			for _, row := range []struct {
				consumer, model string
				requests        int
			}{{c.ID, "my-model", 2}, {other, "secret-model", 99}} {
				if _, err := tx.Exec(`INSERT INTO use_rollup (granularity, bucket_start, consumer_id, access_key_id, model, requests, prompt_tokens, completion_tokens, total_tokens)
VALUES (?, 60000, ?, ?, ?, ?, 1, 1, 2)`, g, row.consumer, uuid.NewString(), row.model, row.requests); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range []string{"minute", "hour", "day"} {
		r := e.get(tok, "/v1/usage?granularity="+g)
		status(t, r, http.StatusOK)
		var body struct {
			Data       []map[string]any `json:"data"`
			NextCursor any              `json:"next_cursor"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Data) != 1 || body.Data[0]["consumer_id"] != c.ID || body.Data[0]["model"] != "my-model" || body.Data[0]["requests"] != float64(2) || body.NextCursor != nil {
			t.Fatalf("%s: usage = %s", g, r.Body.String())
		}
	}
	// Naming another consumer's key narrows to nothing; there is no consumer parameter to widen.
	r := e.get(tok, "/v1/usage?granularity=day&access_key_id="+uuid.NewString())
	status(t, r, http.StatusOK)
	if !strings.Contains(r.Body.String(), `"data":[]`) {
		t.Fatalf("body = %s", r.Body.String())
	}
	status(t, e.get(tok, "/v1/usage?granularity=week"), http.StatusBadRequest)
	status(t, e.get(tok, "/v1/usage?granularity=day&cursor=garbage"), http.StatusBadRequest)
	status(t, e.get("", "/v1/usage?granularity=day"), http.StatusUnauthorized)
}

func TestPasswordChangeEndsTheOtherSessionsAndKeepsTheCurrent(t *testing.T) {
	e := newEnv(t)
	a := e.signUp("example-user", goodPass)
	b := e.login("example-user", goodPass).token()
	c := e.login("example-user", goodPass).token()
	const newPass = "a brand new passphrase!"
	r := e.send(a, "PUT", "/v1/me/password", `{"current_password":"`+goodPass+`","new_password":"`+newPass+`"}`)
	status(t, r, http.StatusNoContent)
	status(t, e.get(a, "/v1/me"), http.StatusOK)
	status(t, e.get(b, "/v1/me"), http.StatusUnauthorized)
	status(t, e.get(c, "/v1/me"), http.StatusUnauthorized)
	status(t, e.login("example-user", goodPass), http.StatusUnauthorized)
	status(t, e.login("example-user", newPass), http.StatusOK)
	// Only the hash of the new password is stored.
	var secret string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT secret FROM idn_credential`).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "$argon2id$") || strings.Contains(secret, newPass) {
		t.Fatal("the new password is not stored as a hash")
	}
}

func TestPasswordChangeChecks(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)
	put := func(body string) resp { return e.send(tok, "PUT", "/v1/me/password", body) }
	status(t, put(`{"new_password":"a brand new passphrase!"}`), http.StatusBadRequest)                       // current required
	status(t, put(`{"current_password":"","new_password":"a brand new passphrase!"}`), http.StatusBadRequest) // empty current
	status(t, put(`{"current_password":"`+goodPass+`","new_password":"short"}`), http.StatusBadRequest)       // policy
	wrong := put(`{"current_password":"not my password at all","new_password":"a brand new passphrase!"}`)
	status(t, wrong, http.StatusBadRequest)
	if strings.Contains(wrong.Body.String(), "not my password") || strings.Contains(wrong.Body.String(), "brand new") {
		t.Fatal("a password is echoed")
	}
	status(t, e.login("example-user", goodPass), http.StatusOK) // unchanged
	status(t, e.do(req{method: "PUT", path: "/v1/me/password", body: `{"new_password":"x"}`, origin: "-"}), http.StatusUnauthorized)
}

func TestWrongCurrentPasswordCountsTowardTheLockout(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)
	e.set(credential.KeyLockoutFailures, "3")
	for i := range 3 {
		status(t, e.send(tok, "PUT", "/v1/me/password", `{"current_password":"wrong password `+strconv.Itoa(i)+`!!","new_password":"a brand new passphrase!"}`), http.StatusBadRequest)
	}
	r := e.send(tok, "PUT", "/v1/me/password", `{"current_password":"`+goodPass+`","new_password":"a brand new passphrase!"}`)
	status(t, r, http.StatusTooManyRequests)
	if r.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	// The same lockout stops the login, and ends with the window.
	status(t, e.login("example-user", goodPass), http.StatusTooManyRequests)
	e.clk.advance(16 * time.Minute)
	status(t, e.send(tok, "PUT", "/v1/me/password", `{"current_password":"`+goodPass+`","new_password":"a brand new passphrase!"}`), http.StatusNoContent)
}

func TestNoPasswordTokenHashOrCookieReachesTheLogOrAnAnswer(t *testing.T) {
	e := newEnv(t)
	secrets := []string{"a very secret passphrase 1", "another secret passphrase 2", "wrong guess passphrase 3"}
	inv, _ := e.invite(time.Hour)
	_, invSecret, _ := strings.Cut(strings.TrimPrefix(inv, "ls_invite_"), "_")
	var bodies []string
	collect := func(r resp) resp { bodies = append(bodies, r.Body.String()); return r }
	su := collect(e.redeem(inv, "secret-user", secrets[0]))
	status(t, su, http.StatusCreated)
	tok := su.token()
	collect(e.redeem(inv, "again-user", secrets[1])) // 410: used
	collect(e.login("secret-user", secrets[2]))
	collect(e.login("nobody", secrets[2]))
	collect(e.send(tok, "PUT", "/v1/me/password", `{"current_password":"`+secrets[2]+`","new_password":"`+secrets[1]+`"}`))
	collect(e.send(tok, "PUT", "/v1/me/password", `{"current_password":"`+secrets[0]+`","new_password":"`+secrets[1]+`"}`))
	collect(e.do(req{method: "POST", path: "/v1/session/password", body: `{"username": ` + secrets[2]}))
	collect(e.do(req{method: "GET", path: "/v1/me", cookie: tok, host: "evil.example"}))
	var hash string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT secret FROM idn_credential`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	logs := e.tel.buf.String()
	for _, s := range append(secrets, inv, invSecret, tok, hash, "$argon2id", "evil.example") {
		if s == "" {
			continue
		}
		if strings.Contains(logs, s) {
			t.Fatalf("the log holds a secret: %q", s)
		}
		for _, b := range bodies {
			if strings.Contains(b, s) {
				t.Fatalf("an answer holds a secret %q: %s", s, b)
			}
		}
	}
}

var _ = consumerauth.TermsVersion
