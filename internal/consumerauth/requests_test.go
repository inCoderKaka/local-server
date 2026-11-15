package consumerauth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/requests"
)

// The consumer's resource request routes on the page listener, with the session cookie. The same
// service answers the consumer API with a key (internal/gateway tests).

const noSuchRequest = "0192e5a0-0006-7000-8000-000000000006"

const hostileText = "ZZsecret-text-4412"

func obj(t *testing.T, r resp) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
		t.Fatalf("body is not a JSON object: %s", r.Body.String())
	}
	return m
}

func warningsOf(t *testing.T, m map[string]any) []string {
	t.Helper()
	w, ok := m["warnings"]
	if !ok {
		return nil
	}
	var out []string
	for _, x := range w.([]any) {
		out = append(out, x.(string))
	}
	return out
}

// open creates a request over the page and returns its id.
func (e *env) open(token, message string) string {
	e.t.Helper()
	r := e.send(token, "POST", "/v1/resource-requests", `{"message":"`+message+`"}`)
	status(e.t, r, http.StatusCreated)
	id, _ := obj(e.t, r)["id"].(string)
	return id
}

func TestResourceRequestRoutesNeedASession(t *testing.T) {
	e := newEnv(t)
	for _, rt := range []struct{ method, path, body string }{
		{"GET", "/v1/resource-requests", ""},
		{"POST", "/v1/resource-requests", `{"message":"x"}`},
		{"GET", "/v1/resource-requests/" + noSuchRequest, ""},
		{"POST", "/v1/resource-requests/" + noSuchRequest + "/comments", `{"body":"x"}`},
		{"POST", "/v1/resource-requests/" + noSuchRequest + "/withdraw", ""},
	} {
		// No cookie, and an access key, which the page listener never takes (req_001 D-148).
		for name, h := range map[string]map[string]string{"no credential": nil, "access key": {"Authorization": "Bearer ls_abcdefgh_secretsecretsecret"}} {
			r := e.do(req{method: rt.method, path: rt.path, body: rt.body, origin: "-", header: h})
			if r.Code != http.StatusUnauthorized {
				t.Errorf("%s %s with %s = %d, want 401", rt.method, rt.path, name, r.Code)
			}
		}
	}
	if n := e.count(`SELECT count(*) FROM req_resource_request`); n != 0 {
		t.Errorf("%d requests written without a session", n)
	}
}

func TestResourceRequestWritesNeedTheSameOrigin(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)
	id := e.open(tok, "More please")
	for _, rt := range []struct{ path, body string }{
		{"/v1/resource-requests", `{"message":"x"}`},
		{"/v1/resource-requests/" + id + "/comments", `{"body":"x"}`},
		{"/v1/resource-requests/" + id + "/withdraw", ""},
	} {
		for _, origin := range []string{"", "https://evil.example"} {
			status(t, e.do(req{method: "POST", path: rt.path, body: rt.body, cookie: tok, origin: origin}), http.StatusForbidden)
		}
	}
	if n := e.count(`SELECT count(*) FROM req_resource_request`); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}
	if got := obj(t, e.get(tok, "/v1/resource-requests/"+id)); got["status"] != "open" {
		t.Errorf("request = %v", got)
	}
}

func TestConsumerOpensCommentsWithdrawsAndLists(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)

	r := e.send(tok, "POST", "/v1/resource-requests", `{"message":"More context please","ask":{"models":["fake-chat"],"context_limit":8192}}`)
	status(t, r, http.StatusCreated)
	m := obj(t, r)
	id, _ := m["id"].(string)
	if m["status"] != "open" || m["message"] != "More context please" || m["expires_at"] == nil || m["decided_at"] != nil || m["resulting_grant_version_id"] != nil {
		t.Fatalf("created = %s", r.Body.String())
	}
	if c, ok := m["comments"].([]any); !ok || len(c) != 0 {
		t.Errorf("comments = %v, want an empty list", m["comments"])
	}
	ask, _ := m["ask"].(map[string]any)
	if ask["context_limit"].(float64) != 8192 || ask["models"].([]any)[0] != "fake-chat" {
		t.Errorf("ask = %v", ask)
	}
	if _, has := m["warnings"]; has {
		t.Errorf("a create without an unknown field has warnings: %s", r.Body.String())
	}

	r = e.send(tok, "POST", "/v1/resource-requests/"+id+"/comments", `{"body":"I run long documents"}`)
	status(t, r, http.StatusCreated)
	if c, _ := obj(t, r)["comments"].([]any); len(c) != 1 || c[0].(map[string]any)["author"].(map[string]any)["kind"] != "consumer" {
		t.Errorf("comment answer = %s", r.Body.String())
	}
	// The host's comment shows with its author kind.
	if _, err := e.reqs.Comment(context.Background(), "", e.admin.ID, id, "How long?"); err != nil {
		t.Fatal(err)
	}
	got := obj(t, e.get(tok, "/v1/resource-requests/"+id))
	if c, _ := got["comments"].([]any); len(c) != 2 || c[1].(map[string]any)["author"].(map[string]any)["kind"] != "host_admin" {
		t.Errorf("get = %v", got)
	}

	// A list has no comments and holds the caller's requests, newest first; the filter works.
	second := e.open(tok, "Another")
	r = e.get(tok, "/v1/resource-requests")
	status(t, r, http.StatusOK)
	data, _ := obj(t, r)["data"].([]any)
	if len(data) != 2 || data[0].(map[string]any)["id"] != second || data[1].(map[string]any)["id"] != id {
		t.Fatalf("list = %s", r.Body.String())
	}
	if _, has := data[0].(map[string]any)["comments"]; has {
		t.Errorf("a list entry has comments: %v", data[0])
	}

	r = e.send(tok, "POST", "/v1/resource-requests/"+id+"/withdraw", "")
	status(t, r, http.StatusOK)
	if m := obj(t, r); m["status"] != "withdrawn" || m["expires_at"] != nil || m["decided_at"] == nil {
		t.Errorf("withdrawn = %s", r.Body.String())
	}
	for status_, want := range map[string]int{"open": 1, "withdrawn": 1, "approved": 0} {
		r = e.get(tok, "/v1/resource-requests?status="+status_)
		status(t, r, http.StatusOK)
		if d, _ := obj(t, r)["data"].([]any); len(d) != want {
			t.Errorf("status=%s lists %d, want %d", status_, len(d), want)
		}
	}
	status(t, e.get(tok, "/v1/resource-requests?status=bogus"), http.StatusBadRequest)
}

// A final request refuses a comment and a decision with 409; the host's decision makes it final.
func TestFinalRequestsAnswer409OnThePage(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)
	id := e.open(tok, "More please")
	if _, err := e.reqs.Reject(context.Background(), e.admin.ID, id, "Not now"); err != nil {
		t.Fatal(err)
	}
	before := e.count(`SELECT count(*) FROM req_comment`)
	status(t, e.send(tok, "POST", "/v1/resource-requests/"+id+"/comments", `{"body":"please?"}`), http.StatusConflict)
	status(t, e.send(tok, "POST", "/v1/resource-requests/"+id+"/withdraw", ""), http.StatusConflict)
	if n := e.count(`SELECT count(*) FROM req_comment`); n != before {
		t.Errorf("a refused comment was stored: %d, %d", n, before)
	}
	r := e.get(tok, "/v1/resource-requests/"+id)
	if m := obj(t, r); m["status"] != "rejected" {
		t.Errorf("request = %s", r.Body.String())
	}
	// The answer is the error envelope with a null code.
	r = e.send(tok, "POST", "/v1/resource-requests/"+id+"/withdraw", "")
	if inner, _ := obj(t, r)["error"].(map[string]any); inner == nil || inner["code"] != nil || inner["type"] != "invalid_request_error" {
		t.Errorf("409 body = %s", r.Body.String())
	}
}

func TestAnotherConsumersRequestIsNotFound(t *testing.T) {
	e := newEnv(t)
	a := e.signUp("user-a", goodPass)
	b := e.signUp("user-b", goodPass)
	id := e.open(a, "A's request")
	e.open(b, "B's request")

	for name, r := range map[string]resp{
		"get":      e.get(b, "/v1/resource-requests/"+id),
		"comment":  e.send(b, "POST", "/v1/resource-requests/"+id+"/comments", `{"body":"hi"}`),
		"withdraw": e.send(b, "POST", "/v1/resource-requests/"+id+"/withdraw", ""),
	} {
		if r.Code != http.StatusNotFound {
			t.Errorf("%s of another consumer's request = %d, want 404 (not 403): %s", name, r.Code, r.Body.String())
		}
	}
	// The same body as an id that does not exist.
	other := e.get(b, "/v1/resource-requests/"+id)
	missing := e.get(b, "/v1/resource-requests/"+noSuchRequest)
	if other.Body.String() != missing.Body.String() {
		t.Errorf("another consumer's id answers %s, an unknown id %s", other.Body.String(), missing.Body.String())
	}
	if d, _ := obj(t, e.get(b, "/v1/resource-requests"))["data"].([]any); len(d) != 1 || d[0].(map[string]any)["message"] != "B's request" {
		t.Errorf("B's list = %v", d)
	}
	if got := obj(t, e.get(a, "/v1/resource-requests/"+id)); got["status"] != "open" || len(got["comments"].([]any)) != 0 {
		t.Errorf("A's request was changed: %v", got)
	}
}

func TestOpenRequestCapAndTextCapOnThePage(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)
	for i := 0; i < 5; i++ {
		e.open(tok, "ask")
	}
	r := e.send(tok, "POST", "/v1/resource-requests", `{"message":"a sixth"}`)
	status(t, r, http.StatusConflict)
	if inner, _ := obj(t, r)["error"].(map[string]any); inner == nil || inner["code"] != nil || !strings.Contains(inner["message"].(string), "open resource requests") {
		t.Errorf("6th create body = %s", r.Body.String())
	}
	if n := e.count(`SELECT count(*) FROM req_resource_request`); n != 5 {
		t.Errorf("%d requests, want 5", n)
	}
	e.set(requests.KeyMaxOpen, "20")
	e.open(tok, "a sixth")

	e.set(requests.KeyMaxCommentChars, "8")
	r = e.send(tok, "POST", "/v1/resource-requests", `{"message":"123456789"}`)
	status(t, r, http.StatusBadRequest)
	if inner, _ := obj(t, r)["error"].(map[string]any); inner["param"] != "message" {
		t.Errorf("long message body = %s", r.Body.String())
	}
	id := e.open(tok, "12345678")
	r = e.send(tok, "POST", "/v1/resource-requests/"+id+"/comments", `{"body":"123456789"}`)
	status(t, r, http.StatusBadRequest)
	if inner, _ := obj(t, r)["error"].(map[string]any); inner["param"] != "body" {
		t.Errorf("long comment body = %s", r.Body.String())
	}
	status(t, e.send(tok, "POST", "/v1/resource-requests/"+id+"/comments", `{"body":""}`), http.StatusBadRequest)
}

// An unknown field of the ask is ignored, named in warnings by its path and never quoted
// (req_001 D-105). The consumer listener needs the same walker as the admin listener.
func TestCreateNamesIgnoredAskFields(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)
	body := `{"message":"More please","ask":{"models":["fake-chat"],"bogus":"` + hostileText + `","quotas":[{"metric":"tokens","limit":5,"window_seconds":60,"burst":1}]},"priority":"high"}`
	r := e.send(tok, "POST", "/v1/resource-requests", body)
	status(t, r, http.StatusCreated)
	got := warningsOf(t, obj(t, r))
	want := []string{"ask.bogus is not a known field and was ignored", "ask.quotas.0.burst is not a known field and was ignored", "priority is not a known field and was ignored"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("warnings = %q, want %q", got, want)
	}
	if strings.Contains(r.Body.String(), hostileText) || strings.Contains(e.tel.buf.String(), hostileText) {
		t.Error("a request value reached the answer or the log")
	}
	// Only the known fields were kept, and a read shows no warnings.
	id, _ := obj(t, r)["id"].(string)
	var ask string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT ask FROM req_resource_request WHERE id = ?`, id).Scan(&ask); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ask, "bogus") || strings.Contains(ask, "burst") || !strings.Contains(ask, "fake-chat") {
		t.Errorf("stored ask = %s", ask)
	}
	if _, has := obj(t, e.get(tok, "/v1/resource-requests/"+id))["warnings"]; has {
		t.Error("a read has warnings")
	}
	// A comment body is not walked: the middleware runs for create only.
	r = e.send(tok, "POST", "/v1/resource-requests/"+id+"/comments", `{"body":"hi","bogus":1}`)
	status(t, r, http.StatusCreated)
	if w := warningsOf(t, obj(t, r)); w != nil {
		t.Errorf("a comment answers warnings %q", w)
	}
}

func TestTheRequestTextIsNeverLogged(t *testing.T) {
	e := newEnv(t)
	tok := e.signUp("example-user", goodPass)
	id := e.open(tok, hostileText)
	status(t, e.send(tok, "POST", "/v1/resource-requests/"+id+"/comments", `{"body":"`+hostileText+`"}`), http.StatusCreated)
	e.set(requests.KeyMaxCommentChars, "5")
	status(t, e.send(tok, "POST", "/v1/resource-requests/"+id+"/comments", `{"body":"`+hostileText+`"}`), http.StatusBadRequest)
	status(t, e.send(tok, "POST", "/v1/resource-requests", `{"message":"`+hostileText+`"}`), http.StatusBadRequest)
	status(t, e.send(tok, "POST", "/v1/resource-requests/"+id+"/withdraw", ""), http.StatusOK)
	e.set(requests.KeyMaxCommentChars, "4000")
	status(t, e.send(tok, "POST", "/v1/resource-requests/"+id+"/comments", `{"body":"`+hostileText+`"}`), http.StatusConflict)
	if strings.Contains(e.tel.buf.String(), hostileText) {
		t.Error("the request text is in the log")
	}
}
