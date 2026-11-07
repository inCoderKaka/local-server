package admin_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/admin"
	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/storage"
)

const noSuchID = "0192e5a0-ffff-7000-8000-00000000ffff"

func (r resp) obj(key string) map[string]any {
	m, _ := r.json()[key].(map[string]any)
	return m
}

func (e *env) createConsumer(token, body string) map[string]any {
	e.t.Helper()
	r := e.call(token, "POST", "/admin/v1/consumers", body)
	status(e.t, r, http.StatusCreated)
	return r.json()
}

// giveGrant gives a consumer a grant allowing the named models, with a token quota.
func (e *env) giveGrant(consumerID string, tokens int64, models ...string) {
	e.t.Helper()
	if err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		g, err := e.gr.Grants.Create(tx, consumerID)
		if err != nil {
			return err
		}
		v := grants.NewVersion{Quotas: []grants.QuotaEntry{{Metric: grants.MetricTokens, Limit: tokens, Window: 24 * time.Hour}}}
		for _, name := range models {
			m, err := e.gr.Models.Create(tx, name)
			if err != nil {
				return err
			}
			v.Models = append(v.Models, grants.VersionModel{ModelID: m.ID})
		}
		ver, err := e.gr.Versions.Create(tx, g.ID, v)
		if err != nil {
			return err
		}
		return e.gr.Grants.SetCurrentVersion(tx, g.ID, ver.ID)
	}); err != nil {
		e.t.Fatal(err)
	}
}

func TestConsumerRoutesNeedASession(t *testing.T) {
	e := newEnv(t)
	e.setup()
	for _, rt := range []struct{ method, path, body string }{
		{"GET", "/admin/v1/consumers", ""},
		{"POST", "/admin/v1/consumers", `{"kind":"application","display_name":"x"}`},
		{"GET", "/admin/v1/consumers/" + noSuchID, ""},
		{"PATCH", "/admin/v1/consumers/" + noSuchID, `{}`},
		{"DELETE", "/admin/v1/consumers/" + noSuchID, ""},
		{"POST", "/admin/v1/consumers/" + noSuchID + "/suspend", ""},
		{"POST", "/admin/v1/consumers/" + noSuchID + "/reactivate", ""},
		{"GET", "/admin/v1/consumers/" + noSuchID + "/access-keys", ""},
		{"POST", "/admin/v1/consumers/" + noSuchID + "/access-keys", `{}`},
		{"PATCH", "/admin/v1/access-keys/" + noSuchID, `{}`},
		{"POST", "/admin/v1/access-keys/" + noSuchID + "/revoke", ""},
		{"GET", "/admin/v1/invites", ""},
		{"POST", "/admin/v1/invites", `{}`},
		{"POST", "/admin/v1/invites/" + noSuchID + "/revoke", ""},
	} {
		r := e.do(req{method: rt.method, path: rt.path, body: rt.body, origin: adminOrigin})
		if r.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session = %d, want 401", rt.method, rt.path, r.Code)
			continue
		}
		r.errBody(t)
	}
	if n := e.count(`SELECT count(*) FROM idn_consumer`) + e.count(`SELECT count(*) FROM idn_invite`); n != 0 {
		t.Errorf("%d rows written without a session", n)
	}
}

func TestConsumerLifecycleOverHTTP(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	actor := e.adminPrincipal()

	c := e.createConsumer(token, `{"kind":"application","display_name":"Example App","email":"app@example.invalid"}`)
	id, _ := c["id"].(string)
	if c["kind"] != "application" || c["status"] != "active" || c["display_name"] != "Example App" || c["email"] != "app@example.invalid" ||
		c["has_login"] != false || c["username"] != nil || c["deleted_at"] != nil || id == "" || c["created_at"] == nil {
		t.Fatalf("created = %v", c)
	}
	if rows := e.auditRows(consumers.ActionConsumerCreated); len(rows) != 1 || rows[0].actor != actor || rows[0].detail["consumer_id"] != id {
		t.Fatalf("audit rows = %+v", rows)
	}

	r := e.call(token, "GET", "/admin/v1/consumers/"+id, "")
	status(t, r, http.StatusOK)
	if got := r.json(); got["id"] != id || got["display_name"] != "Example App" {
		t.Fatalf("get = %v", got)
	}

	r = e.call(token, "PATCH", "/admin/v1/consumers/"+id, `{"display_name":"Renamed","email":null}`)
	status(t, r, http.StatusOK)
	if got := r.json(); got["display_name"] != "Renamed" || got["email"] != nil {
		t.Fatalf("patch = %v", got)
	}

	r = e.call(token, "POST", "/admin/v1/consumers/"+id+"/suspend", "")
	status(t, r, http.StatusOK)
	if r.json()["status"] != "suspended" {
		t.Fatalf("suspend = %s", r.Body.String())
	}
	r = e.call(token, "POST", "/admin/v1/consumers/"+id+"/reactivate", "")
	status(t, r, http.StatusOK)
	if r.json()["status"] != "active" {
		t.Fatalf("reactivate = %s", r.Body.String())
	}

	r = e.call(token, "DELETE", "/admin/v1/consumers/"+id, "")
	status(t, r, http.StatusNoContent)
	if r.Body.Len() != 0 {
		t.Errorf("204 with a body: %s", r.Body.String())
	}
	r = e.call(token, "GET", "/admin/v1/consumers/"+id, "")
	status(t, r, http.StatusOK)
	got := r.json()
	if got["status"] != "deleted" || got["display_name"] != nil || got["email"] != nil || got["deleted_at"] == nil || got["id"] != id {
		t.Fatalf("deleted consumer = %v", got)
	}
	r = e.call(token, "POST", "/admin/v1/consumers/"+id+"/reactivate", "")
	status(t, r, http.StatusConflict)
	if inner := r.errBody(t); inner["type"] != "invalid_request_error" {
		t.Errorf("409 envelope = %v", inner)
	}
	status(t, e.call(token, "PATCH", "/admin/v1/consumers/"+id, `{"display_name":"Back"}`), http.StatusConflict)
	status(t, e.call(token, "POST", "/admin/v1/consumers/"+id+"/suspend", ""), http.StatusConflict)

	// The audit list shows the actions with the admin as the actor, and no personal string.
	r = e.call(token, "GET", "/admin/v1/audit-events?action=consumer.deleted", "")
	status(t, r, http.StatusOK)
	if data, _ := r.json()["data"].([]any); len(data) != 1 {
		t.Fatalf("audit list = %s", r.Body.String())
	}
	if all := e.call(token, "GET", "/admin/v1/audit-events", "").Body.String(); strings.Contains(all, "Example App") || strings.Contains(all, "Renamed") || strings.Contains(all, "example.invalid") {
		t.Errorf("the audit list holds personal data: %s", all)
	}
}

func TestConsumerNotFoundAndBadInput(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	hostile := "ZZhostile-marker-5519"

	for _, c := range []struct {
		name         string
		method, path string
		body         string
		status       int
		param        any
	}{
		{"unknown consumer", "GET", "/admin/v1/consumers/" + noSuchID, "", 404, nil},
		{"unknown consumer patch", "PATCH", "/admin/v1/consumers/" + noSuchID, `{"display_name":"x"}`, 404, nil},
		{"unknown consumer delete", "DELETE", "/admin/v1/consumers/" + noSuchID, "", 404, nil},
		{"unknown consumer suspend", "POST", "/admin/v1/consumers/" + noSuchID + "/suspend", "", 404, nil},
		{"unknown consumer keys", "GET", "/admin/v1/consumers/" + noSuchID + "/access-keys", "", 404, nil},
		{"unknown consumer issue", "POST", "/admin/v1/consumers/" + noSuchID + "/access-keys", `{}`, 404, nil},
		{"unknown key patch", "PATCH", "/admin/v1/access-keys/" + noSuchID, `{"name":"x"}`, 404, nil},
		{"unknown key revoke", "POST", "/admin/v1/access-keys/" + noSuchID + "/revoke", "", 404, nil},
		{"unknown invite revoke", "POST", "/admin/v1/invites/" + noSuchID + "/revoke", "", 404, nil},
		{"id that is not a uuid", "GET", "/admin/v1/consumers/" + hostile, "", 400, "consumer_id"},
		{"bad email", "POST", "/admin/v1/consumers", `{"kind":"individual","display_name":"x","email":"` + hostile + `"}`, 400, "email"},
		{"blank name", "POST", "/admin/v1/consumers", `{"kind":"individual","display_name":"  "}`, 400, "display_name"},
		{"bad kind", "POST", "/admin/v1/consumers", `{"kind":"` + hostile + `","display_name":"x"}`, 400, "kind"},
		{"missing name", "POST", "/admin/v1/consumers", `{"kind":"individual"}`, 400, "display_name"},
		{"bad status filter", "GET", "/admin/v1/consumers?status=" + hostile, "", 400, "status"},
		{"bad cursor", "GET", "/admin/v1/consumers?cursor=" + hostile, "", 400, "cursor"},
		{"limit zero", "GET", "/admin/v1/consumers?limit=0", "", 400, "limit"},
		{"limit text", "GET", "/admin/v1/invites?limit=" + hostile, "", 400, "limit"},
		{"invite bad cursor", "GET", "/admin/v1/invites?cursor=" + hostile, "", 400, "cursor"},
		{"invite past expiry", "POST", "/admin/v1/invites", `{"expires_at":"2020-01-01T00:00:00Z"}`, 400, "expires_at"},
		{"invite bad expiry", "POST", "/admin/v1/invites", `{"expires_at":"` + hostile + `"}`, 400, nil},
	} {
		r := e.call(token, c.method, c.path, c.body)
		if r.Code != c.status {
			t.Errorf("%s: status %d, want %d; body %s", c.name, r.Code, c.status, r.Body.String())
			continue
		}
		inner := r.errBody(t)
		if c.param != nil && inner["param"] != c.param {
			t.Errorf("%s: param = %v, want %v", c.name, inner["param"], c.param)
		}
		if strings.Contains(r.Body.String(), hostile) {
			t.Errorf("%s: the answer quotes the request: %s", c.name, r.Body.String())
		}
	}
	if strings.Contains(e.tel.buf.String(), hostile) {
		t.Error("request bytes reached the log")
	}
	if n := e.count(`SELECT count(*) FROM idn_consumer`); n != 0 {
		t.Errorf("%d consumers after refused requests", n)
	}
}

func TestConsumerListPagingOverHTTP(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	var ids []string
	for _, n := range []string{"A", "B", "C"} {
		e.clk.advance(1_000_000_000)
		ids = append(ids, e.createConsumer(token, `{"kind":"individual","display_name":"`+n+`"}`)["id"].(string))
	}
	status(t, e.call(token, "POST", "/admin/v1/consumers/"+ids[0]+"/suspend", ""), http.StatusOK)
	status(t, e.call(token, "DELETE", "/admin/v1/consumers/"+ids[1], ""), http.StatusNoContent)

	idsOf := func(r resp) []string {
		var out []string
		for _, x := range r.json()["data"].([]any) {
			out = append(out, x.(map[string]any)["id"].(string))
		}
		return out
	}
	r := e.call(token, "GET", "/admin/v1/consumers", "")
	status(t, r, http.StatusOK)
	if got := idsOf(r); len(got) != 2 || got[0] != ids[2] || got[1] != ids[0] {
		t.Fatalf("default list = %v", got)
	}
	if v, ok := r.json()["next_cursor"]; !ok || v != nil {
		t.Errorf("next_cursor = %v (present %v), want null", v, ok)
	}
	r = e.call(token, "GET", "/admin/v1/consumers?limit=1", "")
	status(t, r, http.StatusOK)
	cur, _ := r.json()["next_cursor"].(string)
	if cur == "" || len(idsOf(r)) != 1 {
		t.Fatalf("page 1 = %s", r.Body.String())
	}
	r = e.call(token, "GET", "/admin/v1/consumers?limit=1&cursor="+cur, "")
	status(t, r, http.StatusOK)
	if got := idsOf(r); len(got) != 1 || got[0] != ids[0] || r.json()["next_cursor"] != nil {
		t.Fatalf("page 2 = %s", r.Body.String())
	}
	for status_, want := range map[string]string{"suspended": ids[0], "deleted": ids[1], "active": ids[2]} {
		r = e.call(token, "GET", "/admin/v1/consumers?status="+status_, "")
		status(t, r, http.StatusOK)
		if got := idsOf(r); len(got) != 1 || got[0] != want {
			t.Errorf("status %s = %v, want [%s]", status_, got, want)
		}
	}
	e.set(consumers.KeyListPageSize, "1")
	if got := idsOf(e.call(token, "GET", "/admin/v1/consumers?limit=50", "")); len(got) != 1 {
		t.Errorf("limit above the setting returned %d rows", len(got))
	}
}

func TestAccessKeyOverHTTP(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	cid := e.createConsumer(token, `{"kind":"application","display_name":"Example App"}`)["id"].(string)
	e.giveGrant(cid, 1000, "example-model", "other-model")

	r := e.call(token, "POST", "/admin/v1/consumers/"+cid+"/access-keys",
		`{"name":"Example key","expires_at":"2027-01-01T00:00:00Z","narrowing":{"models":["example-model"],"quota":{"tokens":100},"max_output_tokens":512,"concurrency":2}}`)
	status(t, r, http.StatusCreated)
	issued := r.json()
	secret, _ := issued["secret"].(string)
	k := r.obj("access_key")
	prefix, _ := k["prefix"].(string)
	if !strings.HasPrefix(secret, "ls_"+prefix+"_") || k["status"] != "active" || k["name"] != "Example key" || k["consumer_id"] != cid ||
		k["expires_at"] != "2027-01-01T00:00:00Z" || k["bound_node"] != nil || k["revoked_at"] != nil {
		t.Fatalf("issued = %s", r.Body.String())
	}
	nar, _ := k["narrowing"].(map[string]any)
	if models, _ := nar["models"].([]any); len(models) != 1 || models[0] != "example-model" || nar["max_output_tokens"] != float64(512) || nar["concurrency"] != float64(2) ||
		nar["quota"].(map[string]any)["tokens"] != float64(100) {
		t.Fatalf("narrowing = %v", nar)
	}
	if strings.Contains(r.Body.String(), "secret_hash") {
		t.Error("the hash is in the answer")
	}

	// Shown once: neither a list, nor a patch, nor a revoke, nor the log repeats the secret.
	list := e.call(token, "GET", "/admin/v1/consumers/"+cid+"/access-keys", "")
	status(t, list, http.StatusOK)
	data, _ := list.json()["data"].([]any)
	if len(data) != 1 || data[0].(map[string]any)["prefix"] != prefix {
		t.Fatalf("list = %s", list.Body.String())
	}
	kid, _ := k["id"].(string)
	body := list.Body.String()

	r = e.call(token, "PATCH", "/admin/v1/access-keys/"+kid, `{"name":null,"narrowing":{"quota":{"tokens":10}},"bound_node":"example-node"}`)
	status(t, r, http.StatusOK)
	if got := r.json(); got["name"] != nil || got["bound_node"] != "example-node" || got["narrowing"].(map[string]any)["quota"].(map[string]any)["tokens"] != float64(10) {
		t.Fatalf("patch = %s", r.Body.String())
	}
	// A narrowing replaces the stored one whole: parts left out are removed.
	if nar := r.json()["narrowing"].(map[string]any); nar["models"] != nil || nar["max_output_tokens"] != nil || nar["concurrency"] != nil {
		t.Errorf("patched narrowing kept parts that were left out: %v", nar)
	}
	body += r.Body.String()
	status(t, e.call(token, "PATCH", "/admin/v1/access-keys/"+kid, `{"narrowing":null}`), http.StatusOK)
	r = e.call(token, "PATCH", "/admin/v1/access-keys/"+kid, `{"narrowing":{"quota":{"tokens":5000}}}`) // wider than the grant
	status(t, r, http.StatusBadRequest)
	if r.errBody(t)["param"] != "narrowing.quota.tokens" {
		t.Errorf("param = %v", r.errBody(t)["param"])
	}
	status(t, e.call(token, "PATCH", "/admin/v1/access-keys/"+kid, `{"narrowing":{"models":["no-such-model"]}}`), http.StatusBadRequest)
	status(t, e.call(token, "PATCH", "/admin/v1/access-keys/"+kid, `{"narrowing":{"quotas":[{"tokens":100}]}}`), http.StatusBadRequest) // an entry needs metric, limit and window_seconds
	// An unknown field is ignored, never refused (D-105), and the answer names it: the deprecated
	// quota has no window of its own, so the one asked for is not applied.
	r = e.call(token, "PATCH", "/admin/v1/access-keys/"+kid, `{"narrowing":{"quota":{"window_seconds":60,"tokens":100}}}`)
	status(t, r, http.StatusOK)
	if w, _ := r.json()["warnings"].([]any); len(w) != 1 || w[0] != "narrowing.quota.window_seconds is not a known field and was ignored" {
		t.Errorf("warnings = %s", r.Body.String())
	}

	r = e.call(token, "POST", "/admin/v1/access-keys/"+kid+"/revoke", "")
	status(t, r, http.StatusOK)
	if got := r.json(); got["status"] != "revoked" || got["revoked_at"] == nil {
		t.Fatalf("revoke = %s", r.Body.String())
	}
	body += r.Body.String()
	r = e.call(token, "PATCH", "/admin/v1/access-keys/"+kid, `{"name":"again"}`)
	status(t, r, http.StatusConflict)
	r.errBody(t)

	if strings.Contains(body, secret) || strings.Contains(e.tel.buf.String(), secret) {
		t.Error("the secret was repeated after it was shown")
	}
	if strings.Contains(e.tel.buf.String(), strings.TrimPrefix(secret, "ls_"+prefix+"_")) {
		t.Error("the secret part reached the log")
	}
	var audited string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT group_concat(detail) FROM aud_event`).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(audited, secret) || strings.Contains(audited, "Example key") || strings.Contains(audited, "Example App") {
		t.Errorf("audit rows hold a secret or a name: %s", audited)
	}
	for _, action := range []string{"access_key.issued", "access_key.updated", "access_key.revoked"} {
		if len(e.auditRows(action)) == 0 {
			t.Errorf("no %s audit row", action)
		}
	}

	// The key of a deleted consumer: the delete revoked it and issue is refused.
	k2 := e.call(token, "POST", "/admin/v1/consumers/"+cid+"/access-keys", `{}`)
	status(t, k2, http.StatusCreated)
	status(t, e.call(token, "DELETE", "/admin/v1/consumers/"+cid, ""), http.StatusNoContent)
	list = e.call(token, "GET", "/admin/v1/consumers/"+cid+"/access-keys", "")
	status(t, list, http.StatusOK)
	for _, x := range list.json()["data"].([]any) {
		if x.(map[string]any)["status"] != "revoked" {
			t.Errorf("key of a deleted consumer: %v", x)
		}
	}
	status(t, e.call(token, "POST", "/admin/v1/consumers/"+cid+"/access-keys", `{}`), http.StatusConflict)
}

// A key's quota is a list of windowed entries; the deprecated quota object is an alias for the
// entries with the host's default window, and the list wins when both are sent (req_001 D-107).
func TestAccessKeyQuotaEntriesOverHTTP(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	cid := e.createConsumer(token, `{"kind":"application","display_name":"Example App"}`)["id"].(string)
	e.giveGrant(cid, 1000, "example-model") // tokens 1000 per day
	keys := "/admin/v1/consumers/" + cid + "/access-keys"

	// Entries in: they come back as the list, and the day-long ones as the deprecated alias too.
	r := e.call(token, "POST", keys, `{"narrowing":{"quotas":[{"metric":"requests","limit":20,"window_seconds":3600},{"metric":"tokens","limit":100,"window_seconds":86400}]}}`)
	status(t, r, http.StatusCreated)
	k := r.obj("access_key")
	nar, _ := k["narrowing"].(map[string]any)
	list, _ := nar["quotas"].([]any)
	if len(list) != 2 || list[0].(map[string]any)["metric"] != "tokens" || list[0].(map[string]any)["limit"] != float64(100) ||
		list[0].(map[string]any)["window_seconds"] != float64(86400) || list[1].(map[string]any)["window_seconds"] != float64(3600) {
		t.Fatalf("quotas = %v", nar)
	}
	if alias, _ := nar["quota"].(map[string]any); alias["tokens"] != float64(100) || alias["requests"] != nil {
		t.Errorf("alias = %v", nar["quota"])
	}
	if k["warnings"] != nil {
		t.Errorf("warnings = %v", k["warnings"])
	}

	// The deprecated alias in: it becomes entries with the default window, no warning.
	r = e.call(token, "POST", keys, `{"narrowing":{"quota":{"tokens":50,"requests":9}}}`)
	status(t, r, http.StatusCreated)
	nar, _ = r.obj("access_key")["narrowing"].(map[string]any)
	list, _ = nar["quotas"].([]any)
	if len(list) != 2 || list[0].(map[string]any)["window_seconds"] != float64(86400) || nar["quota"].(map[string]any)["requests"] != float64(9) ||
		r.obj("access_key")["warnings"] != nil {
		t.Fatalf("alias in = %s", r.Body.String())
	}

	// Both: the list wins and a warning says so; on a change the warning is in the changed key.
	both := `{"quota":{"tokens":5},"quotas":[{"metric":"tokens","limit":7,"window_seconds":7200}]}`
	r = e.call(token, "POST", keys, `{"narrowing":`+both+`}`)
	status(t, r, http.StatusCreated)
	k = r.obj("access_key")
	nar, _ = k["narrowing"].(map[string]any)
	list, _ = nar["quotas"].([]any)
	warns, _ := k["warnings"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["limit"] != float64(7) || nar["quota"] != nil || len(warns) != 1 ||
		warns[0] != "quota is deprecated and ignored because quotas is given" {
		t.Fatalf("both = %s", r.Body.String())
	}
	if _, present := nar["quota"]; !present {
		t.Errorf("the narrowing leaves out quota, want null: %s", r.Body.String())
	}
	kid, _ := k["id"].(string)
	r = e.call(token, "PATCH", "/admin/v1/access-keys/"+kid, `{"narrowing":`+both+`}`)
	status(t, r, http.StatusOK)
	if warns, _ := r.json()["warnings"].([]any); len(warns) != 1 {
		t.Errorf("patch warnings = %s", r.Body.String())
	}
	if r := e.call(token, "GET", keys, ""); strings.Contains(r.Body.String(), `"warnings"`) {
		t.Errorf("a list repeats warnings: %s", r.Body.String())
	}

	// Refusals are 400 and name the entry: a window outside the host's bounds (1m to 720h by
	// default), a repeated metric and window, a limit above the grant's entry of the same window.
	for name, c := range map[string]struct{ narrowing, param string }{
		"window below the minimum": {`{"quotas":[{"metric":"tokens","limit":1,"window_seconds":30}]}`, "narrowing.quotas.0.window_seconds"},
		"window above the maximum": {`{"quotas":[{"metric":"tokens","limit":1,"window_seconds":9999999}]}`, "narrowing.quotas.0.window_seconds"},
		"window too long to hold":  {`{"quotas":[{"metric":"tokens","limit":1,"window_seconds":9223372036854775807}]}`, "narrowing.quotas.0.window_seconds"},
		"repeated entry": {`{"quotas":[{"metric":"requests","limit":1,"window_seconds":3600},{"metric":"requests","limit":2,"window_seconds":3600}]}`,
			"narrowing.quotas.1.window_seconds"},
		"wider than the grant": {`{"quotas":[{"metric":"tokens","limit":5000,"window_seconds":86400}]}`, "narrowing.quotas.0"},
	} {
		r := e.call(token, "POST", keys, `{"narrowing":`+c.narrowing+`}`)
		status(t, r, http.StatusBadRequest)
		if got := r.errBody(t)["param"]; got != c.param && !strings.HasPrefix(got.(string), c.param) {
			t.Errorf("%s: param = %v, want %s", name, got, c.param)
		}
	}
	// Schema bounds are refused before the service: a window of 0 and an unknown metric.
	status(t, e.call(token, "POST", keys, `{"narrowing":{"quotas":[{"metric":"tokens","limit":1,"window_seconds":0}]}}`), http.StatusBadRequest)
	status(t, e.call(token, "POST", keys, `{"narrowing":{"quotas":[{"metric":"jobs","limit":1,"window_seconds":60}]}}`), http.StatusBadRequest)
}

// A refusal that is a matter of who asks, not of the body, is 403 with the field named.
func TestForbiddenKindIs403(t *testing.T) {
	status, param, _ := admin.ClassifyError(&consumers.Error{Kind: consumers.KindForbidden, Param: "narrowing.quotas", Message: "Not allowed."})
	if status != http.StatusForbidden || param != "narrowing.quotas" {
		t.Errorf("status %d param %q, want 403 narrowing.quotas", status, param)
	}
}

func TestConsumerBillingModeOverHTTP(t *testing.T) {
	e := newEnv(t)
	token := e.setup()

	c := e.createConsumer(token, `{"kind":"application","display_name":"Example App"}`)
	id, _ := c["id"].(string)
	if c["billing_mode"] != "none" {
		t.Fatalf("default billing_mode = %v", c["billing_mode"])
	}
	c = e.createConsumer(token, `{"kind":"application","display_name":"Paying App","billing_mode":"postpaid"}`)
	if c["billing_mode"] != "postpaid" {
		t.Fatalf("created billing_mode = %v", c["billing_mode"])
	}

	r := e.call(token, "PATCH", "/admin/v1/consumers/"+id, `{"billing_mode":"prepaid"}`)
	status(t, r, http.StatusOK)
	if r.json()["billing_mode"] != "prepaid" || r.json()["display_name"] != "Example App" {
		t.Fatalf("patch = %s", r.Body.String())
	}
	r = e.call(token, "GET", "/admin/v1/consumers/"+id, "")
	status(t, r, http.StatusOK)
	if r.json()["billing_mode"] != "prepaid" {
		t.Fatalf("get = %s", r.Body.String())
	}
	if rows := e.auditRows(consumers.ActionConsumerUpdated); len(rows) == 0 {
		t.Error("no audit row for the billing mode change")
	}
	status(t, e.call(token, "PATCH", "/admin/v1/consumers/"+id, `{"billing_mode":"gold"}`), http.StatusBadRequest)
	status(t, e.call(token, "PATCH", "/admin/v1/consumers/"+id, `{"billing_mode":null}`), http.StatusBadRequest)
	status(t, e.call(token, "POST", "/admin/v1/consumers", `{"kind":"application","display_name":"X","billing_mode":"gold"}`), http.StatusBadRequest)
	if got := e.call(token, "GET", "/admin/v1/consumers/"+id, "").json()["billing_mode"]; got != "prepaid" {
		t.Errorf("a refused change was applied: %v", got)
	}
	if list := e.call(token, "GET", "/admin/v1/consumers", "").Body.String(); !strings.Contains(list, `"billing_mode":"postpaid"`) {
		t.Errorf("the list lacks billing_mode: %s", list)
	}
}

func TestConsumerUpdateKeepsBillingMode(t *testing.T) {
	e := newEnv(t)
	token := e.setup()

	c := e.createConsumer(token, `{"kind":"application","display_name":"Paying App","billing_mode":"postpaid"}`)
	id, _ := c["id"].(string)

	r := e.call(token, "PATCH", "/admin/v1/consumers/"+id, `{"display_name":"Renamed App"}`)
	status(t, r, http.StatusOK)
	if r.json()["billing_mode"] != "postpaid" || r.json()["display_name"] != "Renamed App" {
		t.Fatalf("rename = %s", r.Body.String())
	}
	r = e.call(token, "PATCH", "/admin/v1/consumers/"+id, `{"email":"user@example.invalid"}`)
	status(t, r, http.StatusOK)
	if r.json()["billing_mode"] != "postpaid" {
		t.Fatalf("email change = %s", r.Body.String())
	}
	r = e.call(token, "GET", "/admin/v1/consumers/"+id, "")
	status(t, r, http.StatusOK)
	if r.json()["billing_mode"] != "postpaid" {
		t.Fatalf("get = %s", r.Body.String())
	}
}

func TestInviteOverHTTP(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	e.set(consumers.KeyInviteLinkBaseURL, "https://my-host.example.ts.net")

	r := e.call(token, "POST", "/admin/v1/invites", `{"note":"For an example person"}`)
	status(t, r, http.StatusCreated)
	tok, _ := r.json()["token"].(string)
	link, _ := r.json()["link"].(string)
	inv := r.obj("invite")
	if !strings.HasPrefix(tok, "ls_invite_") || link != "https://my-host.example.ts.net/invite#"+tok || inv["status"] != "pending" ||
		inv["note"] != "For an example person" || inv["redeemed_consumer_id"] != nil || inv["expires_at"] == nil {
		t.Fatalf("created = %s", r.Body.String())
	}
	id, _ := inv["id"].(string)
	if want := e.clk.now().Add(24 * 3600e9).UTC().Format("2006-01-02T15:04:05Z"); inv["expires_at"] != want {
		t.Errorf("expires_at = %v, want %s (the invites.ttl default)", inv["expires_at"], want)
	}
	if rows := e.auditRows("invite.created"); len(rows) != 1 || rows[0].actor != e.adminPrincipal() || rows[0].detail["invite_id"] != id {
		t.Fatalf("audit = %+v", rows)
	}

	// A relative link when the base is empty.
	e.set(consumers.KeyInviteLinkBaseURL, "")
	r = e.call(token, "POST", "/admin/v1/invites", `{"expires_at":"2099-01-01T00:00:00Z"}`)
	status(t, r, http.StatusCreated)
	tok2, _ := r.json()["token"].(string)
	if r.json()["link"] != "/invite#"+tok2 || r.obj("invite")["expires_at"] != "2099-01-01T00:00:00Z" {
		t.Fatalf("relative = %s", r.Body.String())
	}

	// The list shows states and never a token.
	list := e.call(token, "GET", "/admin/v1/invites", "")
	status(t, list, http.StatusOK)
	if strings.Contains(list.Body.String(), tok) || strings.Contains(list.Body.String(), "ls_invite_") || strings.Contains(list.Body.String(), "token") {
		t.Errorf("the list repeats a token: %s", list.Body.String())
	}
	data, _ := list.json()["data"].([]any)
	if len(data) != 2 || data[1].(map[string]any)["id"] != id || list.json()["next_cursor"] != nil {
		t.Fatalf("list = %s", list.Body.String())
	}

	r = e.call(token, "POST", "/admin/v1/invites/"+id+"/revoke", "")
	status(t, r, http.StatusOK)
	if r.json()["status"] != "revoked" || r.json()["id"] != id {
		t.Fatalf("revoke = %s", r.Body.String())
	}
	r = e.call(token, "POST", "/admin/v1/invites/"+id+"/revoke", "")
	status(t, r, http.StatusConflict)
	r.errBody(t)

	list = e.call(token, "GET", "/admin/v1/invites", "")
	if !strings.Contains(list.Body.String(), `"status":"revoked"`) {
		t.Errorf("list = %s", list.Body.String())
	}
	for _, s := range []string{tok, tok2} {
		if strings.Contains(e.tel.buf.String(), s) || strings.Contains(e.tel.buf.String(), strings.TrimPrefix(s, "ls_invite_")) {
			t.Error("an invite token reached the log")
		}
		var audited string
		if err := e.db.Reader().QueryRow(context.Background(), `SELECT group_concat(detail) FROM aud_event`).Scan(&audited); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(audited, s) || strings.Contains(audited, "example person") {
			t.Errorf("audit rows hold a token or a note: %s", audited)
		}
	}
}
