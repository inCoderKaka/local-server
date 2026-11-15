package admin_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/requests"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// requestEnv is an env with a logged-in admin, a consumer, and every model open for sharing.
type requestEnv struct {
	*env
	token, consumerID, principalID string
}

func newRequestEnv(t *testing.T) *requestEnv {
	t.Helper()
	e := newEnv(t)
	token := e.setup()
	cid, _ := e.createConsumer(token, `{"kind":"application","display_name":"Example App"}`)["id"].(string)
	e.openAllModels(token)
	var pid string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT principal_id FROM idn_consumer WHERE id = ?`, cid).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	return &requestEnv{env: e, token: token, consumerID: cid, principalID: pid}
}

// ask opens a request for the consumer, the way the consumer routes do.
func (e *requestEnv) ask(message string) string {
	e.t.Helper()
	q, err := e.reqs.Create(context.Background(), e.consumerID, e.principalID, requests.NewRequest{Message: message, Ask: []byte(`{"models":["fake-chat"]}`)})
	if err != nil {
		e.t.Fatal(err)
	}
	return q.ID
}

func (e *requestEnv) rows() map[string]int {
	return map[string]int{
		"grants":   e.count(`SELECT count(*) FROM grt_grant`),
		"versions": e.count(`SELECT count(*) FROM grt_version`),
		"comments": e.count(`SELECT count(*) FROM req_comment`),
		"audit":    e.count(`SELECT count(*) FROM aud_event`),
		"events":   e.count(`SELECT count(*) FROM ntf_event`),
		"sync":     e.count(`SELECT count(*) FROM syn_change WHERE dataset = 'resource_requests'`),
	}
}

func sameRows(t *testing.T, where string, got, want map[string]int) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %s = %d, want %d", where, k, got[k], v)
		}
	}
}

func TestResourceRequestRoutesNeedASession(t *testing.T) {
	e := newEnv(t)
	e.setup()
	for _, rt := range []struct{ method, path, body string }{
		{"GET", "/admin/v1/resource-requests", ""},
		{"GET", "/admin/v1/resource-requests/" + noSuchID, ""},
		{"POST", "/admin/v1/resource-requests/" + noSuchID + "/approve", `{"limits":` + grantBody + `}`},
		{"POST", "/admin/v1/resource-requests/" + noSuchID + "/reject", `{}`},
		{"POST", "/admin/v1/resource-requests/" + noSuchID + "/comments", `{"body":"x"}`},
	} {
		r := e.do(req{method: rt.method, path: rt.path, body: rt.body, origin: adminOrigin})
		if r.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a session = %d, want 401", rt.method, rt.path, r.Code)
			continue
		}
		r.errBody(t)
	}
}

// A write from another origin with the session cookie is refused by the guard, as on every admin
// route, and changes nothing.
func TestResourceRequestWritesNeedTheSameOrigin(t *testing.T) {
	e := newRequestEnv(t)
	id := e.ask("More please")
	before := e.rows()
	for _, rt := range []struct{ path, body string }{
		{"/approve", `{"limits":` + grantBody + `}`}, {"/reject", `{}`}, {"/comments", `{"body":"x"}`},
	} {
		for _, origin := range []string{"", "http://evil.example"} {
			r := e.do(req{method: "POST", path: "/admin/v1/resource-requests/" + id + rt.path, body: rt.body, cookie: e.token, origin: origin})
			status(t, r, http.StatusForbidden)
		}
	}
	sameRows(t, "after refused writes", e.rows(), before)
}

func TestAdminListRealDataWithTheStatusFilter(t *testing.T) {
	e := newRequestEnv(t)
	other, _ := e.createConsumer(e.token, `{"kind":"individual","display_name":"Other"}`)["id"].(string)
	var otherPrincipal string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT principal_id FROM idn_consumer WHERE id = ?`, other).Scan(&otherPrincipal); err != nil {
		t.Fatal(err)
	}
	first, second := e.ask("First"), e.ask("Second")
	third, err := e.reqs.Create(context.Background(), other, otherPrincipal, requests.NewRequest{Message: "Third"})
	if err != nil {
		t.Fatal(err)
	}
	status(t, e.call(e.token, "POST", "/admin/v1/resource-requests/"+first+"/reject", `{"comment":"No"}`), http.StatusOK)

	ids := func(r resp) []string {
		t.Helper()
		status(t, r, http.StatusOK)
		var out []string
		data, _ := r.json()["data"].([]any)
		for _, x := range data {
			m := x.(map[string]any)
			out = append(out, m["id"].(string))
			if _, has := m["comments"]; has {
				t.Errorf("a list entry has comments: %v", m)
			}
		}
		return out
	}
	open := ids(e.call(e.token, "GET", "/admin/v1/resource-requests?status=open", ""))
	if len(open) != 2 || open[0] != third.ID || open[1] != second {
		t.Errorf("open = %v, want [%s %s] newest first", open, third.ID, second)
	}
	if rej := ids(e.call(e.token, "GET", "/admin/v1/resource-requests?status=rejected", "")); len(rej) != 1 || rej[0] != first {
		t.Errorf("rejected = %v", rej)
	}
	if all := ids(e.call(e.token, "GET", "/admin/v1/resource-requests", "")); len(all) != 3 {
		t.Errorf("all = %v", all)
	}
	if mine := ids(e.call(e.token, "GET", "/admin/v1/resource-requests?consumer_id="+other, "")); len(mine) != 1 || mine[0] != third.ID {
		t.Errorf("consumer filter = %v", mine)
	}
	// Paging: a page of one carries a cursor that leads to the next.
	r := e.call(e.token, "GET", "/admin/v1/resource-requests?status=open&limit=1", "")
	status(t, r, http.StatusOK)
	cur, _ := r.json()["next_cursor"].(string)
	if cur == "" {
		t.Fatalf("no next_cursor: %s", r.Body.String())
	}
	if p2 := ids(e.call(e.token, "GET", "/admin/v1/resource-requests?status=open&limit=1&cursor="+cur, "")); len(p2) != 1 || p2[0] != second {
		t.Errorf("second page = %v", p2)
	}
	status(t, e.call(e.token, "GET", "/admin/v1/resource-requests?status=bogus", ""), http.StatusBadRequest)
	r = e.call(e.token, "GET", "/admin/v1/resource-requests?cursor=x", "")
	status(t, r, http.StatusBadRequest)
	if r.errBody(t)["param"] != "cursor" {
		t.Errorf("bad cursor body: %s", r.Body.String())
	}
	// An empty answer is an empty list, not an error.
	e2 := newEnv(t)
	tok := e2.setup()
	r = e2.call(tok, "GET", "/admin/v1/resource-requests?status=open", "")
	status(t, r, http.StatusOK)
	if d, _ := r.json()["data"].([]any); d == nil || len(d) != 0 {
		t.Errorf("empty list = %s", r.Body.String())
	}
}

func TestAdminGetShowsCommentsAndAdminCommentsAreAuthored(t *testing.T) {
	e := newRequestEnv(t)
	id := e.ask("More please")
	path := "/admin/v1/resource-requests/" + id

	r := e.call(e.token, "POST", path+"/comments", `{"body":"What for?"}`)
	status(t, r, http.StatusCreated)
	comments, _ := r.json()["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("answer = %s", r.Body.String())
	}
	author := comments[0].(map[string]any)["author"].(map[string]any)
	if author["kind"] != "host_admin" || author["principal_id"] != e.adminPrincipal() {
		t.Errorf("author = %v", author)
	}
	r = e.call(e.token, "GET", path, "")
	status(t, r, http.StatusOK)
	if m := r.json(); m["status"] != "open" || m["consumer_id"] != e.consumerID || m["expires_at"] == nil || m["decided_at"] != nil {
		t.Errorf("get = %s", r.Body.String())
	}
	if _, has := r.json()["warnings"]; has {
		t.Errorf("a read has warnings: %s", r.Body.String())
	}
	status(t, e.call(e.token, "GET", "/admin/v1/resource-requests/"+noSuchID, ""), http.StatusNotFound)
	status(t, e.call(e.token, "POST", "/admin/v1/resource-requests/"+noSuchID+"/comments", `{"body":"x"}`), http.StatusNotFound)

	// The length cap is requests.max_comment_chars.
	e.set(requests.KeyMaxCommentChars, "5")
	r = e.call(e.token, "POST", path+"/comments", `{"body":"abcdef"}`)
	status(t, r, http.StatusBadRequest)
	if r.errBody(t)["param"] != "body" {
		t.Errorf("long comment body: %s", r.Body.String())
	}
	if n := e.count(`SELECT count(*) FROM req_comment`); n != 1 {
		t.Errorf("%d comments, want 1", n)
	}
}

// Approving is one unit of work: the grant version, the pointer, the status, the audit rows, the
// domain events and the sync change, with grant.changed among the events.
func TestApproveCreatesTheGrantVersionAtomically(t *testing.T) {
	e := newRequestEnv(t)
	id := e.ask("More please")
	before := e.rows()

	r := e.call(e.token, "POST", "/admin/v1/resource-requests/"+id+"/approve", `{"limits":`+grantBody+`,"comment":"Approved, enjoy"}`)
	status(t, r, http.StatusOK)
	m := r.json()
	vid, _ := m["resulting_grant_version_id"].(string)
	if m["status"] != "approved" || vid == "" || m["decided_at"] == nil || m["expires_at"] != nil {
		t.Fatalf("approved = %s", r.Body.String())
	}
	if _, has := m["warnings"]; has {
		t.Errorf("approval without an unknown field has warnings: %s", r.Body.String())
	}
	if c, _ := m["comments"].([]any); len(c) != 1 {
		t.Errorf("comments = %v", m["comments"])
	}

	// The consumer's grant: a new version, current, with the approved limits and the request as the reason.
	r = e.call(e.token, "GET", "/admin/v1/consumers/"+e.consumerID+"/grant", "")
	status(t, r, http.StatusOK)
	if r.json()["id"] != vid || r.json()["version"].(float64) != 1 {
		t.Errorf("current grant = %s, want version %s", r.Body.String(), vid)
	}
	var reason string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT reason FROM grt_version WHERE id = ?`, vid).Scan(&reason); err != nil || reason != "resource_request:"+id {
		t.Errorf("reason = %q, %v", reason, err)
	}
	got := e.rows()
	sameRows(t, "after the approval", got, map[string]int{"grants": before["grants"] + 1, "versions": before["versions"] + 1,
		"comments": before["comments"] + 1, "audit": before["audit"] + 2, "events": before["events"] + 2, "sync": before["sync"] + 1})
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = 'grant.changed' AND actor_id = ? AND target_id = ?`, e.adminPrincipal(), e.principalID); n != 1 {
		t.Errorf("%d grant.changed audit rows", n)
	}
	if rows := e.auditRows("resource_request.approved"); len(rows) != 1 || rows[0].detail["grant_version_id"] != vid || rows[0].detail["request_id"] != id {
		t.Errorf("approval audit rows = %+v", rows)
	}
	// grant.changed and resource_request.decided are both events of that unit.
	for _, typ := range []string{"grant.changed", "resource_request.decided"} {
		if n := e.count(`SELECT count(*) FROM ntf_event WHERE type = ? AND actor_principal_id = ? AND target_principal_id = ?`, typ, e.adminPrincipal(), e.principalID); n != 1 {
			t.Errorf("%d %s events, want 1", n, typ)
		}
	}
	// It is final.
	r = e.call(e.token, "POST", "/admin/v1/resource-requests/"+id+"/approve", `{"limits":`+grantBody+`}`)
	status(t, r, http.StatusConflict)
	sameRows(t, "after a second approval", e.rows(), got)
}

func TestFailedApprovalLeavesNothingBehindOverHTTP(t *testing.T) {
	e := newRequestEnv(t)
	id := e.ask("More please")
	before := e.rows()

	// Refused limits: 400 naming the field, nothing written.
	r := e.call(e.token, "POST", "/admin/v1/resource-requests/"+id+"/approve",
		`{"limits":{"models":[{"model":"no-such-model"}],"concurrency":1,"on_context_overflow":"refuse","allowed_request_properties":null}}`)
	status(t, r, http.StatusBadRequest)
	if r.errBody(t)["param"] != "limits.models.0.model" {
		t.Errorf("refused limits: %s", r.Body.String())
	}
	sameRows(t, "after refused limits", e.rows(), before)

	// The unit fails after the grant was written: everything rolls back, grant.changed included.
	if err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := tx.Exec(`CREATE TRIGGER test_no_approve BEFORE UPDATE OF status ON req_resource_request WHEN NEW.status = 'approved' BEGIN SELECT RAISE(ABORT, 'test: refused'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	r = e.call(e.token, "POST", "/admin/v1/resource-requests/"+id+"/approve", `{"limits":`+grantBody+`,"comment":"ok"}`)
	status(t, r, http.StatusInternalServerError)
	sameRows(t, "after a failed unit", e.rows(), before)
	if n := e.count(`SELECT count(*) FROM ntf_event WHERE type = 'grant.changed'`); n != 0 {
		t.Errorf("%d grant.changed events survived the rollback", n)
	}
	r = e.call(e.token, "GET", "/admin/v1/resource-requests/"+id, "")
	if r.json()["status"] != "open" || r.json()["resulting_grant_version_id"] != nil {
		t.Errorf("request = %s, want it still open", r.Body.String())
	}
}

func TestRejectAndFinalRequestsAnswer409(t *testing.T) {
	e := newRequestEnv(t)
	id := e.ask("More please")
	path := "/admin/v1/resource-requests/" + id

	r := e.call(e.token, "POST", path+"/reject", `{"comment":"Not now"}`)
	status(t, r, http.StatusOK)
	if m := r.json(); m["status"] != "rejected" || m["resulting_grant_version_id"] != nil || m["decided_at"] == nil || m["expires_at"] != nil {
		t.Errorf("rejected = %s", r.Body.String())
	}
	if rows := e.auditRows("resource_request.rejected"); len(rows) != 1 || rows[0].actor != e.adminPrincipal() || rows[0].target != e.principalID {
		t.Errorf("rejection audit rows = %+v", rows)
	}
	if n := e.count(`SELECT count(*) FROM grt_version`); n != 0 {
		t.Errorf("a rejection made %d grant versions", n)
	}
	before := e.rows()
	for name, call := range map[string][2]string{
		"reject":  {"/reject", `{}`},
		"approve": {"/approve", `{"limits":` + grantBody + `}`},
		"comment": {"/comments", `{"body":"more"}`},
		// A final request answers 409 before its approval's limits are looked at.
		"approve with bad limits": {"/approve", `{"limits":{"models":[{"model":"no-such-model"}],"concurrency":1,"on_context_overflow":"refuse","allowed_request_properties":null}}`},
	} {
		if r := e.call(e.token, "POST", path+call[0], call[1]); r.Code != http.StatusConflict {
			t.Errorf("%s on a rejected request = %d, want 409: %s", name, r.Code, r.Body.String())
		}
	}
	sameRows(t, "after refused changes", e.rows(), before)
	// A rejection body may be empty.
	id2 := e.ask("Another")
	status(t, e.call(e.token, "POST", "/admin/v1/resource-requests/"+id2+"/reject", `{}`), http.StatusOK)
}

// A request past its end time that the sweeper has not yet expired answers 409 before its
// approval's limits are looked at.
func TestApproveOfPastDueRequestAnswers409BeforeLimits(t *testing.T) {
	e := newRequestEnv(t)
	e.set("requests.ttl", "1m")
	id := e.ask("More please")
	e.clk.advance(2 * time.Minute)
	before := e.rows()
	r := e.call(e.token, "POST", "/admin/v1/resource-requests/"+id+"/approve", `{"limits":{"models":[{"model":"no-such-model"}],"concurrency":1,"on_context_overflow":"refuse","allowed_request_properties":null}}`)
	status(t, r, http.StatusConflict)
	sameRows(t, "after a past-due approval", e.rows(), before)
}

// An unknown field of the approval's limits is ignored, named in warnings by its path, and never
// quoted (req_001 D-105).
func TestApprovalNamesIgnoredLimitsFields(t *testing.T) {
	e := newRequestEnv(t)
	id := e.ask("More please")
	body := `{"limits":{"models":[{"model":"fake-chat","note":"` + hostileValue + `"}],"concurrency":2,"on_context_overflow":"refuse","allowed_request_properties":null,"bogus":"` + hostileValue + `"},"extra":1}`
	r := e.call(e.token, "POST", "/admin/v1/resource-requests/"+id+"/approve", body)
	status(t, r, http.StatusOK)
	wantWarnings(t, "approve", warningsOf(t, r.json()),
		"extra is not a known field and was ignored",
		"limits.bogus is not a known field and was ignored",
		"limits.models.0.note is not a known field and was ignored")
	if strings.Contains(r.Body.String(), hostileValue) || strings.Contains(e.tel.buf.String(), hostileValue) {
		t.Error("a request value reached the answer or the log")
	}
	// The warnings are not stored: a read shows none.
	r = e.call(e.token, "GET", "/admin/v1/resource-requests/"+id, "")
	if _, has := r.json()["warnings"]; has {
		t.Errorf("a read has warnings: %s", r.Body.String())
	}
}

// With no requests service the routes answer 501, as before the module existed.
func TestResourceRequestRoutesAre501WithoutTheService(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	e.admin.SetRequestsForTest(nil)
	for _, rt := range []struct{ method, path, body string }{
		{"GET", "/admin/v1/resource-requests", ""},
		{"POST", "/admin/v1/resource-requests/" + noSuchID + "/reject", `{}`},
	} {
		status(t, e.call(token, rt.method, rt.path, rt.body), http.StatusNotImplemented)
	}
}
