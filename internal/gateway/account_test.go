package gateway_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/auth/credential"
	"github.com/inCoderKaka/local-server/internal/consumerauth"
	"github.com/inCoderKaka/local-server/internal/gateway"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/requests"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/usage"
)

const pageHost = "host.example-tailnet.ts.net"

// accountEnv is a gateway whose /v1/me routes are served by a real accounts service, and the
// consumer page handler of that same service.
func accountEnv(t *testing.T) (e *env, page http.Handler) {
	t.Helper()
	var svc *consumerauth.Service
	e = newEnv(t, func(o *envOpts) {
		o.account = func(e *env) gateway.Account {
			if err := e.st.Register(settings.Def{Key: "x.pref", Kind: settings.Duration, Default: "24h", Owner: settings.Consumer, Scoped: true, Description: "A consumer preference."}); err != nil {
				t.Fatal(err)
			}
			var err error
			e.reqs, err = requests.New(requests.Config{DB: e.db, Settings: e.st, Identity: e.ir, Events: e.bus, Logger: e.tel.Logger()})
			if err != nil {
				t.Fatal(err)
			}
			svc, err = consumerauth.New(consumerauth.Config{DB: e.db, Settings: e.st, Identity: e.ir, Grants: e.gr, Telemetry: e.tel, Requests: e.reqs,
				Keys: func(ctx context.Context, id uuid.UUID) (*api.AccessKeyList, error) {
					keys, err := e.ir.AccessKeys.ListByConsumer(ctx, e.db.Reader(), id.String())
					if err != nil {
						return nil, err
					}
					out := &api.AccessKeyList{Data: []api.AccessKey{}}
					for _, k := range keys {
						kid, _ := uuid.Parse(k.ID)
						out.Data = append(out.Data, api.AccessKey{ID: kid, ConsumerID: id, Prefix: k.Prefix, Status: api.AccessKeyStatusActive, CreatedAt: k.CreatedAt})
					}
					return out, nil
				},
				Limits: func(_ context.Context, id uuid.UUID) (*api.GrantVersion, error) {
					return &api.GrantVersion{ConsumerID: id, Version: 1}, nil
				},
				LimitBody: e.lim.LimitBody,
				HostNames: func(context.Context) ([]string, error) { return []string{pageHost}, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			return svc
		}
	})
	return e, svc.Handler()
}

// session gives the env's consumer a live page session and returns its token.
func (e *env) session() string {
	e.t.Helper()
	token, err := credential.NewToken()
	if err != nil {
		e.t.Fatal(err)
	}
	err = e.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := e.ir.Sessions.Create(tx, e.cons.PrincipalID, credential.HashToken(token), time.Now().Add(time.Hour))
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return token
}

func TestAccountRoutesTakeAnAccessKey(t *testing.T) {
	e, _ := accountEnv(t)
	me := e.do(http.MethodGet, "/v1/me", "")
	statusIs(t, me, http.StatusOK)
	if me.json()["id"] != e.cons.ID || me.json()["kind"] != "application" {
		t.Fatalf("me = %v", me.json())
	}
	lim := e.do(http.MethodGet, "/v1/me/limits", "")
	statusIs(t, lim, http.StatusOK)
	if lim.json()["consumer_id"] != e.cons.ID {
		t.Fatalf("limits = %v", lim.json())
	}
	keys := e.do(http.MethodGet, "/v1/me/access-keys", "")
	statusIs(t, keys, http.StatusOK)
	if !strings.Contains(keys.Body.String(), e.keyID) || strings.Contains(keys.Body.String(), strings.TrimPrefix(e.key, "Bearer ")) {
		t.Fatalf("keys = %s", keys.Body.String())
	}
	statusIs(t, e.do(http.MethodGet, "/v1/me/settings", ""), http.StatusOK)
	up := e.do(http.MethodPatch, "/v1/me/settings", `{"values":{"x.pref":"1h"}}`)
	statusIs(t, up, http.StatusOK)
	if !strings.Contains(up.Body.String(), `"value":"1h"`) {
		t.Fatalf("update = %s", up.Body.String())
	}
	if v, err := e.st.Duration(context.Background(), "x.pref", e.cons.ID); err != nil || v != time.Hour {
		t.Fatalf("stored = %v, %v; want 1h for the key's consumer", v, err)
	}
	// A host-owned key is refused with the same 403 as on the page.
	r := e.do(http.MethodPatch, "/v1/me/settings", `{"values":{"limits.rate_capacity":1}}`)
	statusIs(t, r, http.StatusForbidden)
	// The password route is session-only: a key is not enough.
	statusIs(t, e.do(http.MethodPut, "/v1/me/password", `{"new_password":"another long passphrase"}`), http.StatusUnauthorized)
	// No key, no account.
	statusIs(t, e.doAuth("", http.MethodGet, "/v1/me", ""), http.StatusUnauthorized)
}

func TestAccountRoutesStay501WithoutTheAccountsService(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/v1/me", "/v1/me/limits", "/v1/me/access-keys", "/v1/me/settings"} {
		statusIs(t, e.do(http.MethodGet, p, ""), http.StatusNotImplemented)
	}
}

func TestACookieNeverWorksOnTheConsumerAPI(t *testing.T) {
	e, page := accountEnv(t)
	token := e.session()
	cookie := credential.ConsumerSessionCookie + "=" + token
	// The cookie is valid on the page listener...
	rec := httptest.NewRecorder()
	pr := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	pr.Host = pageHost
	pr.Header.Set("Cookie", cookie)
	page.ServeHTTP(rec, pr)
	if rec.Code != http.StatusOK {
		t.Fatalf("page listener: status = %d, body %s", rec.Code, rec.Body.String())
	}
	// ...and never on the API listener, on every route that names a session.
	for _, c := range []struct{ m, p string }{
		{http.MethodGet, "/v1/me"}, {http.MethodGet, "/v1/me/limits"}, {http.MethodGet, "/v1/me/access-keys"},
		{http.MethodGet, "/v1/me/settings"}, {http.MethodGet, "/v1/usage?granularity=day"}, {http.MethodDelete, "/v1/session"},
		{http.MethodPut, "/v1/me/password"},
	} {
		r := e.doAuth("", c.m, c.p, "", "Cookie", cookie)
		statusIs(t, r, http.StatusUnauthorized)
		if strings.Contains(r.Body.String(), token) {
			t.Fatal("the cookie is echoed")
		}
	}
	// The session is intact: the API listener did not use it up.
	pr = httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	pr.Host = pageHost
	pr.Header.Set("Cookie", cookie)
	rec = httptest.NewRecorder()
	page.ServeHTTP(rec, pr)
	if rec.Code != http.StatusOK {
		t.Fatalf("page listener after: status = %d", rec.Code)
	}
}

func TestPublicAccountRoutesAreNotServedOnTheConsumerAPI(t *testing.T) {
	e, _ := accountEnv(t)
	consent := `"consent":{"terms_version":"terms-2026-10-v1","privacy_version":"privacy-2026-10-v1"}`
	for _, c := range []struct{ p, body string }{
		{"/v1/invite-redemptions/password", `{"invite_token":"x","display_name":"A","username":"a","password":"a long passphrase",` + consent + `}`},
		{"/v1/session/password", `{"username":"a","password":"a long passphrase"}`},
		{"/v1/invite-redemptions/passkey/begin", `{"invite_token":"x","display_name":"A",` + consent + `}`},
		{"/v1/session/passkey/begin", ``},
	} {
		statusIs(t, e.doAuth("", http.MethodPost, c.p, c.body), http.StatusNotImplemented)
	}
}

// GET /v1/usage answers the same body with an access key on the consumer API and with a session
// on the consumer page: both call usage.ConsumerRollups for the caller's own consumer.
func TestUsageBodyIsTheSameWithAKeyAndWithASession(t *testing.T) {
	e, page := accountEnv(t)
	if _, err := usage.NewRollups(e.db, e.st, e.tel); err != nil { // registers usage.page_size
		t.Fatal(err)
	}
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		for _, g := range []string{"minute", "hour", "day"} {
			for i, consumer := range []string{e.cons.ID, uuid.NewString()} {
				if _, err := tx.Exec(`INSERT INTO use_rollup (granularity, bucket_start, consumer_id, access_key_id, model, requests, prompt_tokens, completion_tokens, total_tokens)
VALUES (?, 60000, ?, ?, ?, ?, 3, 4, 7)`, g, consumer, e.keyID, "model-"+string(rune('a'+i)), 5+i); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cookie := credential.ConsumerSessionCookie + "=" + e.session()
	for _, q := range []string{"granularity=day", "granularity=hour&limit=1", "granularity=minute&model=model-a"} {
		byKey := e.do(http.MethodGet, "/v1/usage?"+q, "")
		statusIs(t, byKey, http.StatusOK)
		rec := httptest.NewRecorder()
		pr := httptest.NewRequest(http.MethodGet, "/v1/usage?"+q, nil)
		pr.Host = pageHost
		pr.Header.Set("Cookie", cookie)
		page.ServeHTTP(rec, pr)
		if rec.Code != http.StatusOK || rec.Body.String() != byKey.Body.String() {
			t.Fatalf("%s:\nsession %d %s\nkey     %s", q, rec.Code, rec.Body.String(), byKey.Body.String())
		}
		if strings.Contains(rec.Body.String(), "model-b") {
			t.Fatalf("%s: another consumer's row", q)
		}
	}
}

// The resource request routes take a key on the consumer API, through the same service as the
// page: the caller's own consumer only, the ask's ignored fields named, a final request 409, and
// never a cookie (req_001 D-39, D-105, D-148).
func TestResourceRequestRoutesTakeAnAccessKey(t *testing.T) {
	e, _ := accountEnv(t)
	r := e.do(http.MethodPost, "/v1/resource-requests", `{"message":"More please","ask":{"models":["fake-chat"],"bogus":1}}`)
	statusIs(t, r, http.StatusCreated)
	m := r.json()
	id, _ := m["id"].(string)
	if m["status"] != "open" || m["consumer_id"] != e.cons.ID {
		t.Fatalf("created = %s", r.Body.String())
	}
	if w, _ := m["warnings"].([]any); len(w) != 1 || w[0] != "ask.bogus is not a known field and was ignored" {
		t.Errorf("warnings = %v, want the ignored ask field named on the consumer listener", m["warnings"])
	}

	statusIs(t, e.do(http.MethodPost, "/v1/resource-requests/"+id+"/comments", `{"body":"I run long documents"}`), http.StatusCreated)
	got := e.do(http.MethodGet, "/v1/resource-requests/"+id, "")
	statusIs(t, got, http.StatusOK)
	c, _ := got.json()["comments"].([]any)
	if len(c) != 1 || c[0].(map[string]any)["author"].(map[string]any)["principal_id"] != e.cons.PrincipalID {
		t.Errorf("comments = %v", got.json()["comments"])
	}
	list := e.do(http.MethodGet, "/v1/resource-requests?status=open", "")
	statusIs(t, list, http.StatusOK)
	if d, _ := list.json()["data"].([]any); len(d) != 1 {
		t.Errorf("list = %s", list.Body.String())
	}

	// Another consumer's request is 404 with a key too.
	var otherID string
	if err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		p, err := e.ir.Principals.Create(tx, "consumer", nil, nil)
		if err != nil {
			return err
		}
		o, err := e.ir.Consumers.Create(tx, p.ID, identity.ConsumerApplication, nil, nil)
		otherID = o.ID
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var otherPrincipal string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT principal_id FROM idn_consumer WHERE id = ?`, otherID).Scan(&otherPrincipal); err != nil {
		t.Fatal(err)
	}
	theirs, err := e.reqs.Create(context.Background(), otherID, otherPrincipal, requests.NewRequest{Message: "Theirs"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ m, p, body string }{
		{http.MethodGet, "/v1/resource-requests/" + theirs.ID, ""},
		{http.MethodPost, "/v1/resource-requests/" + theirs.ID + "/comments", `{"body":"x"}`},
		{http.MethodPost, "/v1/resource-requests/" + theirs.ID + "/withdraw", ""},
	} {
		statusIs(t, e.do(c.m, c.p, c.body), http.StatusNotFound)
	}

	w := e.do(http.MethodPost, "/v1/resource-requests/"+id+"/withdraw", "")
	statusIs(t, w, http.StatusOK)
	statusIs(t, e.do(http.MethodPost, "/v1/resource-requests/"+id+"/comments", `{"body":"again"}`), http.StatusConflict)
	statusIs(t, e.do(http.MethodPost, "/v1/resource-requests/"+id+"/withdraw", ""), http.StatusConflict)

	// No key, no requests; a cookie is never a credential on this listener.
	statusIs(t, e.doAuth("", http.MethodGet, "/v1/resource-requests", ""), http.StatusUnauthorized)
	cookie := credential.ConsumerSessionCookie + "=" + e.session()
	statusIs(t, e.doAuth("", http.MethodGet, "/v1/resource-requests", "", "Cookie", cookie), http.StatusUnauthorized)
	statusIs(t, e.doAuth("", http.MethodPost, "/v1/resource-requests", `{"message":"x"}`, "Cookie", cookie), http.StatusUnauthorized)
}

func TestResourceRequestRoutesStay501WithoutTheAccountsService(t *testing.T) {
	e := newEnv(t)
	statusIs(t, e.do(http.MethodGet, "/v1/resource-requests", ""), http.StatusNotImplemented)
	statusIs(t, e.do(http.MethodPost, "/v1/resource-requests", `{"message":"x"}`), http.StatusNotImplemented)
}
