package admin_test

import (
	"net/http"
	"net/netip"
	"testing"

	"github.com/inCoderKaka/local-server/internal/transport"
)

const tailnetName = "host.example-tailnet.ts.net"

// The remote admin listener answers to the host's own *.ts.net name, besides localhost and
// admin.tailnet_name (req_001 D-86, D-148). Any other name is still refused.
func TestGuardAcceptsTheHostsTailnetNames(t *testing.T) {
	e := newEnv(t, func(o *envOpts) { o.tailnetHosts = []string{tailnetName} })
	for host, want := range map[string]int{
		tailnetName + ":8486":          http.StatusOK,
		"HOST.example-tailnet.ts.net":  http.StatusOK,
		"other.example-tailnet.ts.net": http.StatusForbidden,
		"100.64.0.1:8486":              http.StatusForbidden,
	} {
		if r := e.do(req{method: "GET", path: "/admin/v1/setup", host: host}); r.Code != want {
			t.Errorf("Host %q = %d, want %d", host, r.Code, want)
		}
	}
}

// A login that names the host's own tailnet name as its Host is accepted, and the session cookie it
// issues is Secure and HttpOnly (req_001 D-86). The TLS listener itself is tested in cmd/local-server.
func TestLoginUnderTailnetHostIssuesSecureCookie(t *testing.T) {
	e := newEnv(t, func(o *envOpts) { o.tailnetHosts = []string{tailnetName} })
	e.setup()
	r := e.do(req{method: "POST", path: "/admin/v1/session", body: creds(testUser, testPass), host: tailnetName + ":8486",
		origin: "http://" + tailnetName + ":8486", remote: "100.64.0.2:4444"})
	status(t, r, http.StatusOK)
	var secure bool
	for _, c := range r.Result().Cookies() {
		if c.Name == "ls_admin_session" {
			secure = c.Secure && c.HttpOnly
		}
	}
	if !secure {
		t.Errorf("the session cookie is not Secure and HttpOnly: %v", r.Header().Values("Set-Cookie"))
	}
}

// GET /admin/v1/host says how the host is reachable over the tailnet, and names the fix when it
// is not (req_001 D-148).
func TestHostInfoReportsTheTailnet(t *testing.T) {
	ready := transport.Report{
		Status: transport.Status{State: transport.StateReady, Address: netip.MustParseAddr("100.64.0.1"), DNSName: tailnetName},
		Listeners: []transport.Spec{{Role: transport.RoleConsumerAPI, Addr: "100.64.0.1:8484"},
			{Role: transport.RoleConsumerPage, Addr: "100.64.0.1:8485"}},
	}
	e := newEnv(t, func(o *envOpts) { o.tailnet = ready })
	token := e.setup()
	r := e.call(token, "GET", "/admin/v1/host", "")
	status(t, r, http.StatusOK)
	tn := r.obj("tailnet")
	ls, _ := tn["listeners"].([]any)
	if tn["state"] != "ready" || tn["message"] != nil || tn["fix"] != nil || tn["dns_name"] != tailnetName || len(ls) != 2 ||
		ls[0].(map[string]any)["role"] != "consumer_api" || ls[0].(map[string]any)["address"] != "100.64.0.1:8484" {
		t.Fatalf("tailnet = %s", r.Body.String())
	}

	e = newEnv(t, func(o *envOpts) {
		o.tailnet = transport.Report{Status: transport.Status{State: transport.StateLoggedOut}}
	})
	token = e.setup()
	tn = e.call(token, "GET", "/admin/v1/host", "").obj("tailnet")
	ls, _ = tn["listeners"].([]any)
	if tn["state"] != "logged_out" || tn["message"] == nil || tn["fix"] == nil || tn["dns_name"] != nil || len(ls) != 0 {
		t.Fatalf("logged out: tailnet = %v", tn)
	}
	// A host built with no report at all says Tailscale is unavailable.
	e = newEnv(t)
	token = e.setup()
	if tn = e.call(token, "GET", "/admin/v1/host", "").obj("tailnet"); tn["state"] != "unavailable" || tn["fix"] == nil {
		t.Fatalf("no report: tailnet = %v", tn)
	}
}

// A key is bound to a node by a reference the host types; the answer carries the node id and the
// machine name, and a node that is not in the tailnet is refused (req_001 D-154, D-159).
func TestBindKeyToTailnetNode(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	cid := e.createConsumer(token, `{"kind":"application","display_name":"Example App"}`)["id"].(string)
	e.giveGrant(cid, 1000, "example-model")
	r := e.call(token, "POST", "/admin/v1/consumers/"+cid+"/access-keys", `{"name":"laptop key","bound_node":"laptop"}`)
	status(t, r, http.StatusCreated)
	k := r.obj("access_key")
	if k["bound_node"] != "node-id-1" || k["bound_node_name"] != "laptop" {
		t.Fatalf("issued = %s", r.Body.String())
	}
	r = e.call(token, "POST", "/admin/v1/consumers/"+cid+"/access-keys", `{"bound_node":"nobody"}`)
	status(t, r, http.StatusBadRequest)
	if r.errBody(t)["param"] != "bound_node" {
		t.Errorf("error = %s", r.Body.String())
	}
	// Null unbinds, and clears both fields.
	r = e.call(token, "PATCH", "/admin/v1/access-keys/"+k["id"].(string), `{"bound_node":null}`)
	status(t, r, http.StatusOK)
	if got := r.json(); got["bound_node"] != nil || got["bound_node_name"] != nil {
		t.Fatalf("unbound = %s", r.Body.String())
	}
}
