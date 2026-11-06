package admin_test

import (
	"net/http"
	"strings"
	"testing"
)

const grantBody = `{"models":[{"model":"fake-chat","context_limit":4096,"max_output_tokens":1024}],
"quotas":[{"metric":"tokens","limit":100000,"window_seconds":86400},{"metric":"requests","limit":500,"window_seconds":3600}],
"concurrency":2,"on_context_overflow":"refuse","allowed_request_properties":null,"expires_at":null}`

const ruleDoc = `{"format_version":1,"when":{"properties":{"background":true}},"priority":"background","caps":{"max_output_tokens":512}}`

func TestGrantModelAndPolicyRoutesNeedASession(t *testing.T) {
	e := newEnv(t)
	e.setup()
	for _, rt := range []struct{ method, path, body string }{
		{"GET", "/admin/v1/consumers/" + noSuchID + "/grant", ""},
		{"PUT", "/admin/v1/consumers/" + noSuchID + "/grant", grantBody},
		{"GET", "/admin/v1/consumers/" + noSuchID + "/grant/versions", ""},
		{"GET", "/admin/v1/models", ""},
		{"PATCH", "/admin/v1/models/" + noSuchID, `{}`},
		{"GET", "/admin/v1/policy-rules", ""},
		{"POST", "/admin/v1/policy-rules", `{"rule":` + ruleDoc + `}`},
		{"GET", "/admin/v1/policy-rules/" + noSuchID, ""},
		{"PUT", "/admin/v1/policy-rules/" + noSuchID, `{"rule":` + ruleDoc + `}`},
		{"DELETE", "/admin/v1/policy-rules/" + noSuchID, ""},
		{"GET", "/admin/v1/policy-rules/" + noSuchID + "/versions", ""},
	} {
		r := e.do(req{method: rt.method, path: rt.path, body: rt.body, origin: adminOrigin})
		if r.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session = %d, want 401", rt.method, rt.path, r.Code)
			continue
		}
		r.errBody(t)
	}
	if n := e.count(`SELECT count(*) FROM grt_grant`) + e.count(`SELECT count(*) FROM grt_policy_rule`) + e.count(`SELECT count(*) FROM grt_model`); n != 0 {
		t.Errorf("%d rows written without a session", n)
	}
}

func TestHostModelsOverHTTP(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	r := e.call(token, "GET", "/admin/v1/models", "")
	status(t, r, http.StatusOK)
	data, _ := r.json()["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("models = %s", r.Body.String())
	}
	m := data[0].(map[string]any) // fake-chat, by name
	id, _ := m["id"].(string)
	if m["name"] != "fake-chat" || m["engine"] != "ollama" || m["allowed"] != false || m["preload"] != false || m["loaded"] != false || m["keep_alive_seconds"] != nil || id == "" {
		t.Fatalf("model = %v", m)
	}
	r = e.call(token, "PATCH", "/admin/v1/models/"+id, `{"allowed":true,"preload":true,"keep_alive_seconds":300}`)
	status(t, r, http.StatusOK)
	if got := r.json(); got["allowed"] != true || got["preload"] != true || got["keep_alive_seconds"] != float64(300) || got["id"] != id {
		t.Fatalf("patched = %v", got)
	}
	if rows := e.auditRows("model.updated"); len(rows) != 1 || rows[0].actor != e.adminPrincipal() {
		t.Fatalf("audit rows = %+v", rows)
	}
	r = e.call(token, "PATCH", "/admin/v1/models/"+id, `{"keep_alive_seconds":null}`)
	status(t, r, http.StatusOK)
	if got := r.json(); got["keep_alive_seconds"] != nil || got["allowed"] != true {
		t.Fatalf("patched = %v", got)
	}

	r = e.call(token, "PATCH", "/admin/v1/models/"+noSuchID, `{"allowed":true}`)
	status(t, r, http.StatusNotFound)
	r.errBody(t)
	r = e.call(token, "PATCH", "/admin/v1/models/"+id, `{"keep_alive_seconds":-1}`)
	status(t, r, http.StatusBadRequest)
	if r.errBody(t)["param"] != "keep_alive_seconds" {
		t.Fatalf("error = %s", r.Body.String())
	}
}

func TestGrantOverHTTP(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	c := e.createConsumer(token, `{"kind":"application","display_name":"Example App"}`)
	cid, _ := c["id"].(string)
	path := "/admin/v1/consumers/" + cid + "/grant"

	// No grant yet: 404 in the envelope. Open fake-chat, then set the grant.
	r := e.call(token, "GET", path, "")
	status(t, r, http.StatusNotFound)
	r.errBody(t)
	r = e.call(token, "PUT", path, grantBody)
	status(t, r, http.StatusBadRequest) // the model is not open yet
	if inner := r.errBody(t); inner["param"] != "limits.models.0.model" || inner["type"] != "invalid_request_error" {
		t.Fatalf("error = %s", r.Body.String())
	}
	e.openAllModels(token)

	r = e.call(token, "PUT", path, grantBody)
	status(t, r, http.StatusOK)
	v := r.json()
	limits, _ := v["limits"].(map[string]any)
	if v["version"] != float64(1) || v["consumer_id"] != cid || v["resource_request_id"] != nil || v["warnings"] != nil || v["id"] == nil || v["grant_id"] == nil {
		t.Fatalf("grant version = %s", r.Body.String())
	}
	if limits["allowed_request_properties"] != nil || limits["expires_at"] != nil || limits["concurrency"] != float64(2) || limits["on_context_overflow"] != "refuse" {
		t.Fatalf("limits = %v", limits)
	}
	if qs, _ := limits["quotas"].([]any); len(qs) != 2 || qs[0].(map[string]any)["metric"] != "tokens" || qs[0].(map[string]any)["window_seconds"] != float64(86400) {
		t.Fatalf("quotas = %v", limits["quotas"])
	}
	// The 24h entry is also the deprecated alias; the 1h one is not.
	if q, _ := limits["quota"].(map[string]any); q["tokens"] != float64(100000) || q["requests"] != nil {
		t.Fatalf("quota = %v", limits["quota"])
	}
	if ms, _ := limits["models"].([]any); len(ms) != 1 || ms[0].(map[string]any)["model"] != "fake-chat" || ms[0].(map[string]any)["context_limit"] != float64(4096) {
		t.Fatalf("models = %v", limits["models"])
	}
	if rows := e.auditRows("grant.changed"); len(rows) != 1 || rows[0].actor != e.adminPrincipal() {
		t.Fatalf("audit rows = %+v", rows)
	}

	// An empty list means no property is allowed, and it is not the same as null.
	r = e.call(token, "PUT", path, strings.Replace(grantBody, `"allowed_request_properties":null`, `"allowed_request_properties":[]`, 1))
	status(t, r, http.StatusOK)
	r = e.call(token, "GET", path, "")
	status(t, r, http.StatusOK)
	if got := r.obj("limits")["allowed_request_properties"]; got == nil || len(got.([]any)) != 0 || r.json()["version"] != float64(2) {
		t.Fatalf("grant = %s", r.Body.String())
	}

	// Quota and quotas together: quotas wins and the answer warns.
	both := strings.Replace(grantBody, `"concurrency":2`, `"quota":{"tokens":5},"concurrency":2`, 1)
	r = e.call(token, "PUT", path, both)
	status(t, r, http.StatusOK)
	if w, _ := r.json()["warnings"].([]any); len(w) != 1 {
		t.Fatalf("warnings = %s", r.Body.String())
	}

	// History: newest first, paged by next_cursor.
	r = e.call(token, "GET", path+"/versions?limit=2", "")
	status(t, r, http.StatusOK)
	page := r.json()
	data, _ := page["data"].([]any)
	if len(data) != 2 || data[0].(map[string]any)["version"] != float64(3) || page["next_cursor"] == nil {
		t.Fatalf("page = %s", r.Body.String())
	}
	r = e.call(token, "GET", path+"/versions?limit=2&cursor="+page["next_cursor"].(string), "")
	status(t, r, http.StatusOK)
	if data, _ = r.json()["data"].([]any); len(data) != 1 || data[0].(map[string]any)["version"] != float64(1) || r.json()["next_cursor"] != nil {
		t.Fatalf("page 2 = %s", r.Body.String())
	}
	r = e.call(token, "GET", path+"/versions?cursor=nonsense", "")
	status(t, r, http.StatusBadRequest)
	if r.errBody(t)["param"] != "cursor" {
		t.Fatalf("error = %s", r.Body.String())
	}

	// Refusals: a window outside the host's bounds, an unknown consumer, a field the host does not
	// know, a missing required field. None quotes the request.
	r = e.call(token, "PUT", path, strings.Replace(grantBody, `"window_seconds":3600`, `"window_seconds":5`, 1))
	status(t, r, http.StatusBadRequest)
	if r.errBody(t)["param"] != "limits.quotas.1.window_seconds" {
		t.Fatalf("error = %s", r.Body.String())
	}
	r = e.call(token, "PUT", "/admin/v1/consumers/"+noSuchID+"/grant", grantBody)
	status(t, r, http.StatusNotFound)
	r.errBody(t)
	r = e.call(token, "PUT", path, strings.Replace(grantBody, `"concurrency":2`, `"concurrency":2,"SECRET FIELD":1`, 1))
	status(t, r, http.StatusBadRequest)
	if strings.Contains(r.Body.String(), "SECRET") {
		t.Fatalf("the refusal quotes the request: %s", r.Body.String())
	}
	r = e.call(token, "PUT", path, strings.Replace(grantBody, `"allowed_request_properties":null,`, ``, 1))
	status(t, r, http.StatusBadRequest)
	r.errBody(t)
	if n := e.count(`SELECT count(*) FROM grt_version`); n != 3 {
		t.Fatalf("%d versions after the refusals, want 3", n)
	}
}

func TestPolicyRulesOverHTTP(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	c := e.createConsumer(token, `{"kind":"application","display_name":"Example App"}`)
	cid, _ := c["id"].(string)
	e.openAllModels(token)
	r := e.call(token, "PUT", "/admin/v1/consumers/"+cid+"/grant", grantBody)
	status(t, r, http.StatusOK)
	gid, _ := r.json()["grant_id"].(string)

	r = e.call(token, "POST", "/admin/v1/policy-rules", `{"rule":`+ruleDoc+`}`)
	status(t, r, http.StatusCreated)
	rule := r.json()
	id, _ := rule["id"].(string)
	if rule["version"] != float64(1) || rule["grant_id"] != nil || rule["name"] != id || rule["position"] != float64(0) || rule["enabled"] != true || rule["created_at"] == nil {
		t.Fatalf("created = %s", r.Body.String())
	}
	if doc, _ := rule["rule"].(map[string]any); doc["format_version"] != float64(1) || doc["priority"] != "background" {
		t.Fatalf("rule document = %v", rule["rule"])
	}
	r = e.call(token, "POST", "/admin/v1/policy-rules", `{"grant_id":"`+gid+`","name":"Mine","position":2,"rule":{"format_version":1,"deny":true}}`)
	status(t, r, http.StatusCreated)
	mine, _ := r.json()["id"].(string)

	r = e.call(token, "GET", "/admin/v1/policy-rules", "")
	status(t, r, http.StatusOK)
	if d, _ := r.json()["data"].([]any); len(d) != 2 {
		t.Fatalf("list = %s", r.Body.String())
	}
	r = e.call(token, "GET", "/admin/v1/policy-rules?grant_id="+gid, "")
	status(t, r, http.StatusOK)
	if d, _ := r.json()["data"].([]any); len(d) != 1 || d[0].(map[string]any)["id"] != mine || d[0].(map[string]any)["grant_id"] != gid {
		t.Fatalf("list by grant = %s", r.Body.String())
	}

	r = e.call(token, "PUT", "/admin/v1/policy-rules/"+id, `{"name":"Capped","enabled":false,"rule":{"format_version":1,"priority":"interactive"}}`)
	status(t, r, http.StatusOK)
	if got := r.json(); got["version"] != float64(2) || got["name"] != "Capped" || got["enabled"] != false || got["position"] != float64(0) {
		t.Fatalf("replaced = %s", r.Body.String())
	}
	r = e.call(token, "GET", "/admin/v1/policy-rules/"+id+"/versions", "")
	status(t, r, http.StatusOK)
	d, _ := r.json()["data"].([]any)
	if len(d) != 2 || d[0].(map[string]any)["version"] != float64(2) || d[1].(map[string]any)["version"] != float64(1) || d[1].(map[string]any)["name"] != id {
		t.Fatalf("versions = %s", r.Body.String())
	}
	if rows := e.auditRows("policy_rule.updated"); len(rows) != 1 || rows[0].actor != e.adminPrincipal() || rows[0].detail["rule_id"] != id {
		t.Fatalf("audit rows = %+v", rows)
	}

	// A document the policy module refuses is 400, and nothing of it comes back.
	for _, body := range []string{
		`{"rule":{"format_version":1,"priority":"SECRET-WORD"}}`,
		`{"rule":{"format_version":9,"SECRET-WORD":1}}`,
		`{"rule":{"SECRET-WORD":1}}`,
	} {
		r = e.call(token, "POST", "/admin/v1/policy-rules", body)
		status(t, r, http.StatusBadRequest)
		if inner := r.errBody(t); !strings.HasPrefix(inner["param"].(string), "rule") || strings.Contains(r.Body.String(), "SECRET") {
			t.Fatalf("error = %s", r.Body.String())
		}
	}
	r = e.call(token, "POST", "/admin/v1/policy-rules", `{"grant_id":"`+noSuchID+`","rule":`+ruleDoc+`}`)
	status(t, r, http.StatusBadRequest)
	if r.errBody(t)["param"] != "grant_id" {
		t.Fatalf("error = %s", r.Body.String())
	}
	r = e.call(token, "PUT", "/admin/v1/policy-rules/"+noSuchID, `{"rule":`+ruleDoc+`}`)
	status(t, r, http.StatusNotFound)
	r.errBody(t)

	r = e.call(token, "DELETE", "/admin/v1/policy-rules/"+id, "")
	status(t, r, http.StatusNoContent)
	for _, p := range []string{"", "/versions"} {
		r = e.call(token, "GET", "/admin/v1/policy-rules/"+id+p, "")
		status(t, r, http.StatusNotFound)
		r.errBody(t)
	}
	r = e.call(token, "DELETE", "/admin/v1/policy-rules/"+id, "")
	status(t, r, http.StatusNotFound)
	if rows := e.auditRows("policy_rule.deleted"); len(rows) != 1 {
		t.Fatalf("audit rows = %+v", rows)
	}
	if n := e.count(`SELECT count(*) FROM grt_policy_rule_version WHERE rule_id = ?`, id); n != 2 {
		t.Fatalf("%d versions kept after the delete, want 2", n)
	}
}

// openAllModels opens every engine model for sharing, as the host does on the models page.
func (e *env) openAllModels(token string) {
	e.t.Helper()
	r := e.call(token, "GET", "/admin/v1/models", "")
	status(e.t, r, http.StatusOK)
	data, _ := r.json()["data"].([]any)
	for _, m := range data {
		id, _ := m.(map[string]any)["id"].(string)
		status(e.t, e.call(token, "PATCH", "/admin/v1/models/"+id, `{"allowed":true}`), http.StatusOK)
	}
}
