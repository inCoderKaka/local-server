package admin_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/admin"
	"github.com/inCoderKaka/local-server/internal/api"
)

// An unknown request field is ignored, never refused, and the answer names it (req_001 D-105). The
// value never reaches an answer, a row or the log (D-31, D-40).

const hostileValue = "ZZvalue-5519"

func warningsOf(t *testing.T, m map[string]any) []string {
	t.Helper()
	var out []string
	w, ok := m["warnings"]
	if !ok {
		return nil
	}
	list, _ := w.([]any)
	for _, x := range list {
		out = append(out, x.(string))
	}
	if out == nil {
		t.Fatalf("warnings = %v, want absent or a non-empty list", w)
	}
	return out
}

func wantWarnings(t *testing.T, where string, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: warnings = %q, want %q", where, got, want)
	}
}

func TestUnknownFieldsInConsumerBodiesAreIgnoredAndNamed(t *testing.T) {
	e := newEnv(t)
	token := e.setup()

	r := e.call(token, "POST", "/admin/v1/consumers", `{"kind":"individual","display_name":"Example","nickname":"`+hostileValue+`"}`)
	status(t, r, http.StatusCreated)
	wantWarnings(t, "create", warningsOf(t, r.json()), "nickname is not a known field and was ignored")
	id, _ := r.json()["id"].(string)
	if strings.Contains(r.Body.String(), hostileValue) {
		t.Errorf("the answer quotes the value: %s", r.Body.String())
	}
	if n := e.count(`SELECT count(*) FROM idn_consumer`); n != 1 {
		t.Errorf("%d consumers, want 1", n)
	}

	r = e.call(token, "PATCH", "/admin/v1/consumers/"+id, `{"display_name":"Renamed","emial":"`+hostileValue+`","billing_mode":"none"}`)
	status(t, r, http.StatusOK)
	if r.json()["display_name"] != "Renamed" {
		t.Errorf("the known field was not applied: %s", r.Body.String())
	}
	wantWarnings(t, "update", warningsOf(t, r.json()), "emial is not a known field and was ignored")

	// A body that is all known fields has no warnings, and a read never has any.
	r = e.call(token, "PATCH", "/admin/v1/consumers/"+id, `{"display_name":"Again"}`)
	status(t, r, http.StatusOK)
	if _, has := r.json()["warnings"]; has {
		t.Errorf("update without an unknown field has warnings: %s", r.Body.String())
	}
	r = e.call(token, "GET", "/admin/v1/consumers/"+id, "")
	status(t, r, http.StatusOK)
	if _, has := r.json()["warnings"]; has {
		t.Errorf("a read has warnings: %s", r.Body.String())
	}
	r = e.call(token, "GET", "/admin/v1/consumers", "")
	status(t, r, http.StatusOK)
	if strings.Contains(r.Body.String(), "warnings") || strings.Contains(r.Body.String(), "nickname") {
		t.Errorf("the list has warnings: %s", r.Body.String())
	}
	for _, path := range []string{"suspend", "reactivate"} {
		r = e.call(token, "POST", "/admin/v1/consumers/"+id+"/"+path, "")
		status(t, r, http.StatusOK)
		if _, has := r.json()["warnings"]; has {
			t.Errorf("%s has warnings: %s", path, r.Body.String())
		}
	}
	if strings.Contains(e.tel.buf.String(), hostileValue) {
		t.Error("a request value reached the log")
	}
}

func TestUnknownFieldsInAccessKeyBodiesAreIgnoredAndNamed(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	cid := e.createConsumer(token, `{"kind":"application","display_name":"Example App"}`)["id"].(string)
	e.giveGrant(cid, 1000, "example-model")
	keys := "/admin/v1/consumers/" + cid + "/access-keys"

	// Top level, inside the narrowing, and inside a quota entry, in the order of the walk.
	r := e.call(token, "POST", keys, `{"name":"Example key","expiry":"`+hostileValue+`","narrowing":{"models":["example-model"],"cap":"`+hostileValue+`",
"quotas":[{"metric":"tokens","limit":10,"window_seconds":86400,"burst":"`+hostileValue+`"}]}}`)
	status(t, r, http.StatusCreated)
	k := r.obj("access_key")
	wantWarnings(t, "issue", warningsOf(t, k),
		"expiry is not a known field and was ignored",
		"narrowing.cap is not a known field and was ignored",
		"narrowing.quotas.0.burst is not a known field and was ignored")
	if strings.Contains(r.Body.String(), hostileValue) {
		t.Errorf("the answer quotes a value: %s", r.Body.String())
	}
	if _, has := r.json()["warnings"]; has {
		t.Errorf("the warnings are beside the key, not in it: %s", r.Body.String())
	}
	// Nothing is stored from an unknown field: the key has no expiry and the entry its three fields.
	if k["expires_at"] != nil {
		t.Errorf("expires_at = %v", k["expires_at"])
	}
	kid, _ := k["id"].(string)
	list := e.call(token, "GET", keys, "")
	status(t, list, http.StatusOK)
	if strings.Contains(list.Body.String(), "warnings") || strings.Contains(list.Body.String(), "burst") || strings.Contains(list.Body.String(), "cap") {
		t.Errorf("the list carries the ignored fields: %s", list.Body.String())
	}

	// The service's own warnings come first, then the ignored fields.
	r = e.call(token, "POST", keys, `{"narrowing":{"quota":{"tokens":5},"quotas":[{"metric":"tokens","limit":7,"window_seconds":86400}],"cap":1}}`)
	status(t, r, http.StatusCreated)
	wantWarnings(t, "issue with both", warningsOf(t, r.obj("access_key")),
		"quota is deprecated and ignored because quotas is given",
		"narrowing.cap is not a known field and was ignored")

	r = e.call(token, "PATCH", "/admin/v1/access-keys/"+kid, `{"name":"Renamed","expiry":null,"narrowing":{"models":["example-model"],"cap":1}}`)
	status(t, r, http.StatusOK)
	if r.json()["name"] != "Renamed" {
		t.Errorf("the known field was not applied: %s", r.Body.String())
	}
	wantWarnings(t, "update", warningsOf(t, r.json()),
		"expiry is not a known field and was ignored",
		"narrowing.cap is not a known field and was ignored")

	// A key without an unknown field has no warnings; revoke is not a write of a body.
	r = e.call(token, "PATCH", "/admin/v1/access-keys/"+kid, `{"name":"Plain"}`)
	status(t, r, http.StatusOK)
	if _, has := r.json()["warnings"]; has {
		t.Errorf("update without an unknown field has warnings: %s", r.Body.String())
	}
	r = e.call(token, "POST", "/admin/v1/access-keys/"+kid+"/revoke", "")
	status(t, r, http.StatusOK)
	if _, has := r.json()["warnings"]; has {
		t.Errorf("revoke has warnings: %s", r.Body.String())
	}
	if strings.Contains(e.tel.buf.String(), hostileValue) {
		t.Error("a request value reached the log")
	}
}

func TestUnknownFieldsInInviteBodyAreIgnoredAndNamed(t *testing.T) {
	e := newEnv(t)
	token := e.setup()

	r := e.call(token, "POST", "/admin/v1/invites", `{"note":"For an example person","ttl":"`+hostileValue+`"}`)
	status(t, r, http.StatusCreated)
	wantWarnings(t, "invite", warningsOf(t, r.obj("invite")), "ttl is not a known field and was ignored")
	if strings.Contains(r.Body.String(), hostileValue) {
		t.Errorf("the answer quotes the value: %s", r.Body.String())
	}
	if n := e.count(`SELECT count(*) FROM idn_invite`); n != 1 {
		t.Errorf("%d invites, want 1", n)
	}

	r = e.call(token, "GET", "/admin/v1/invites", "")
	status(t, r, http.StatusOK)
	if strings.Contains(r.Body.String(), "warnings") || strings.Contains(r.Body.String(), "ttl") {
		t.Errorf("the list has warnings: %s", r.Body.String())
	}
	r = e.call(token, "POST", "/admin/v1/invites", `{"note":"Plain"}`)
	status(t, r, http.StatusCreated)
	if _, has := r.obj("invite")["warnings"]; has {
		t.Errorf("an invite without an unknown field has warnings: %s", r.Body.String())
	}
}

func TestUnknownFieldsInGrantBodyAreIgnoredAndNamed(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	cid := e.createConsumer(token, `{"kind":"application","display_name":"Example App"}`)["id"].(string)
	e.openAllModels(token)
	path := "/admin/v1/consumers/" + cid + "/grant"

	body := `{"models":[{"model":"fake-chat","context_limit":4096,"max_output_tokens":1024,"note":"` + hostileValue + `"}],
"quotas":[{"metric":"tokens","limit":100000,"window_seconds":86400},{"metric":"requests","limit":500,"window_seconds":3600,"burst":1}],
"concurrency":2,"on_context_overflow":"refuse","allowed_request_properties":null,"expires_at":null,"zz top":"` + hostileValue + `"}`
	r := e.call(token, "PUT", path, body)
	status(t, r, http.StatusOK)
	wantWarnings(t, "grant", warningsOf(t, r.json()),
		"models.0.note is not a known field and was ignored",
		"quotas.1.burst is not a known field and was ignored",
		"zz top is not a known field and was ignored")
	if strings.Contains(r.Body.String(), hostileValue) || strings.Contains(e.tel.buf.String(), hostileValue) {
		t.Errorf("the answer or the log quotes a value: %s", r.Body.String())
	}
	if n := e.count(`SELECT count(*) FROM grt_version`); n != 1 {
		t.Errorf("%d versions, want 1", n)
	}

	// The same body without the unknown fields: no warnings; and the read shows none.
	r = e.call(token, "PUT", path, grantBody)
	status(t, r, http.StatusOK)
	if _, has := r.json()["warnings"]; has {
		t.Errorf("a grant without an unknown field has warnings: %s", r.Body.String())
	}
	r = e.call(token, "GET", path+"/versions", "")
	status(t, r, http.StatusOK)
	if strings.Contains(r.Body.String(), "warnings") || strings.Contains(r.Body.String(), "burst") {
		t.Errorf("the history has warnings: %s", r.Body.String())
	}
}

// A name that could carry arbitrary text is not echoed: too long, a control character or a byte
// that is not UTF-8. The answer is still 2xx and still says that a field was ignored.
func TestUnknownFieldWithAnUnsafeNameIsNotEchoed(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	long := strings.Repeat("x", 65)

	r := e.call(token, "POST", "/admin/v1/consumers", `{"kind":"individual","display_name":"Example","`+long+`":1,"bad\u0001name":2,"bad‮name":3}`)
	status(t, r, http.StatusCreated)
	const text = "a field with a name that is empty, unprintable or too long was ignored"
	wantWarnings(t, "unsafe", warningsOf(t, r.json()), text, text, text)
	if strings.Contains(r.Body.String(), "xxxx") || strings.Contains(r.Body.String(), "bad") {
		t.Errorf("an unsafe name is echoed: %s", r.Body.String())
	}

	// One level down the safe parent is named.
	cid := e.createConsumer(token, `{"kind":"application","display_name":"Example App"}`)["id"].(string)
	r = e.call(token, "POST", "/admin/v1/consumers/"+cid+"/access-keys", `{"narrowing":{"`+long+`":1}}`)
	status(t, r, http.StatusCreated)
	wantWarnings(t, "unsafe nested", warningsOf(t, r.obj("access_key")), "a field of narrowing with a name that is empty, unprintable or too long was ignored")
}

func TestIgnoredFieldsWalker(t *testing.T) {
	entry := `{"metric":"tokens","limit":5,"window_seconds":60`
	for _, tc := range []struct {
		name string
		raw  string
		req  any
		want []string
	}{
		{"no unknown field", `{"models":[{"model":"m","context_limit":1,"max_output_tokens":1}],"quotas":[` + entry + `}],"concurrency":1,"on_context_overflow":"refuse","allowed_request_properties":[],"expires_at":null}`,
			&api.GrantLimits{}, nil},
		{"nested arrays", `{"models":[{"model":"m","a":1},{"model":"m","b":{"c":1}}],"quotas":[` + entry + `,"d":1},` + entry + `}],"e":[1]}`,
			&api.GrantLimits{}, []string{"e is not a known field and was ignored", "models.0.a is not a known field and was ignored",
				"models.1.b is not a known field and was ignored", "quotas.0.d is not a known field and was ignored"}},
		{"opt wrappers", `{"narrowing":{"quota":{"tokens":1,"z":1},"context_limit":3,"y":1},"expires_at":"2027-01-01T00:00:00Z"}`,
			&api.AccessKeyCreate{}, []string{"narrowing.quota.z is not a known field and was ignored", "narrowing.y is not a known field and was ignored"}},
		{"null wrappers", `{"narrowing":null,"name":null,"expires_at":null}`, &api.AccessKeyUpdate{}, nil},
		{"update with a narrowing", `{"narrowing":{"quotas":[` + entry + `,"q":1}]}}`, &api.AccessKeyUpdate{},
			[]string{"narrowing.quotas.0.q is not a known field and was ignored"}},
		{"case sensitive", `{"Display_Name":"x"}`, &api.ConsumerUpdate{}, []string{"Display_Name is not a known field and was ignored"}},
		{"free-form map", `{"rule":{"anything":{"goes":1}},"name":"n"}`, &api.PolicyRuleCreate{}, nil},
		{"unsafe name", "{\"bad\\u0001\":1,\"narrowing\":{\"" + strings.Repeat("x", 65) + "\":1}}", &api.AccessKeyCreate{},
			[]string{"a field with a name that is empty, unprintable or too long was ignored",
				"a field of narrowing with a name that is empty, unprintable or too long was ignored"}},
		{"empty name", `{"":1}`, &api.InviteCreate{}, []string{"a field with a name that is empty, unprintable or too long was ignored"}},
		{"not an object", `[1,2]`, &api.InviteCreate{}, nil},
		{"not json", `{"a":`, &api.InviteCreate{}, nil},
		{"empty body", ``, &api.InviteCreate{}, nil},
		{"no request", `{"a":1}`, nil, nil},
		{"a type with no body", `{"a":1}`, "text", nil},
	} {
		got := admin.IgnoredFields([]byte(tc.raw), tc.req)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
