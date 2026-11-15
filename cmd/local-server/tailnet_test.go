package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/transport"
)

const (
	testTailnetName = "host.example-tailnet.ts.net"
	testTailnetIP   = "100.64.0.1"
)

// fakeTailscale is the host's Tailscale with canned answers and a certificate made for the test.
// No test here reaches a real Tailscale.
type fakeTailscale struct {
	mu     sync.Mutex
	status transport.Status
	cert   *tls.Certificate
	// peers maps the host part of a remote address to the node behind it.
	peers map[string]transport.Node
	// refs maps what a host types to the node it names.
	refs map[string]transport.Node
	// whoisCalls counts WhoIs lookups.
	whoisCalls int
}

func (f *fakeTailscale) Status(context.Context) transport.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *fakeTailscale) WhoIs(_ context.Context, remoteAddr string) (transport.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.whoisCalls++
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	if n, ok := f.peers[host]; ok {
		return n, nil
	}
	return transport.Node{}, transport.ErrNodeNotFound
}

func (f *fakeTailscale) Resolve(_ context.Context, ref string) (transport.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n, ok := f.refs[ref]; ok {
		return n, nil
	}
	return transport.Node{}, transport.ErrNodeNotFound
}

func (f *fakeTailscale) TLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(hi *tls.ClientHelloInfo) (*tls.Certificate, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.cert == nil || hi.ServerName != testTailnetName {
			return nil, errors.New("no certificate for " + hi.ServerName)
		}
		return f.cert, nil
	}}
}

func (f *fakeTailscale) setPeer(ip string, n transport.Node) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.peers[ip] = n
}

// newReadyTailscale is a Tailscale that is running with HTTPS certificates; pool trusts its
// certificate.
func newReadyTailscale(t *testing.T) (*fakeTailscale, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: testTailnetName},
		DNSNames: []string{testTailnetName}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return &fakeTailscale{
		status: transport.Status{State: transport.StateReady, Address: netip.MustParseAddr(testTailnetIP), DNSName: testTailnetName, Version: "1.99.0"},
		cert:   &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf},
		peers:  map[string]transport.Node{"127.0.0.1": {ID: "nLAPTOP", Name: "laptop"}},
		refs:   map[string]transport.Node{"laptop": {ID: "nLAPTOP", Name: "laptop"}, "phone": {ID: "nPHONE", Name: "phone"}},
	}, pool
}

// redirectListen stands in for the tailnet interface, which a test machine has not: it records
// the address serve asked for and listens on a free loopback port instead.
type redirectListen struct {
	mu    sync.Mutex
	asked []string
}

func (r *redirectListen) listen(_, addr string) (net.Listener, error) {
	r.mu.Lock()
	r.asked = append(r.asked, addr)
	r.mu.Unlock()
	return net.Listen("tcp", "127.0.0.1:0")
}

func (r *redirectListen) addrs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.asked...)
}

// tailnetClient talks TLS to a tailnet listener, trusting the test certificate.
func tailnetClient(pool *x509.CertPool) *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: testTailnetName, MinVersion: tls.VersionTLS12}}}
}

func upByRole(l listening, role transport.Role) string {
	for _, sp := range l.Tailnet {
		if sp.Role == role {
			return sp.Addr
		}
	}
	return ""
}

// startTailnetServe starts serve with a ready Tailscale. settings are applied on top of the
// loopback ones.
func startTailnetServe(t *testing.T, settings map[string]string) (*testApp, *running, *fakeTailscale, *x509.CertPool, *redirectListen) {
	t.Helper()
	ts, pool := newReadyTailscale(t)
	ta := newTestApp(t, true)
	ta.transport = ts
	rl := &redirectListen{}
	ta.tailnetListen = rl.listen
	seed(t, ta.dir, merge(loopbackAny, settings))
	return ta, startServe(t, ta, "--engine", "fake"), ts, pool, rl
}

// The tailnet listeners bind only the tailnet address; the remote admin listener exists only when
// its setting is on (req_001 D-10, D-148).
func TestServeBindsTheAdminListenerOnTheTailnetOnlyWhenEnabled(t *testing.T) {
	for _, on := range []bool{false, true} {
		name := "off"
		kv := map[string]string{}
		if on {
			name, kv[keyTailnetAdminEnabled] = "on", "true"
		}
		t.Run(name, func(t *testing.T) {
			_, r, _, _, rl := startTailnetServe(t, kv)
			asked := rl.addrs()
			want := []string{testTailnetIP + ":8484", testTailnetIP + ":8485"}
			if on {
				want = append(want, testTailnetIP+":8486")
			}
			if strings.Join(asked, " ") != strings.Join(want, " ") {
				t.Errorf("tailnet listeners asked for %v, want %v", asked, want)
			}
			for _, a := range asked {
				host, _, _ := net.SplitHostPort(a)
				if ip := net.ParseIP(host); ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
					t.Errorf("a tailnet listener binds %s, which is not the tailnet address", a)
				}
			}
			if got := upByRole(r.addrs, transport.RoleAdmin) != ""; got != on {
				t.Errorf("remote admin listener up = %v, want %v", got, on)
			}
			if code := r.stop(); code != 0 {
				t.Errorf("exit = %d", code)
			}
		})
	}
}

// Without a usable Tailscale serve still starts, serves on localhost only, binds nothing on a
// tailnet address and says what to do on stderr and in the log (req_001 D-148).
func TestServeWithoutTailscaleServesLocalOnlyAndNamesTheFix(t *testing.T) {
	for state, fix := range map[transport.State]string{
		transport.StateUnavailable: "install and start the standalone Tailscale app",
		transport.StateStopped:     "turn Tailscale on",
		transport.StateLoggedOut:   "log in to Tailscale",
		transport.StateStarting:    "wait for Tailscale",
		transport.StateNoHTTPS:     "HTTPS Certificates",
	} {
		t.Run(string(state), func(t *testing.T) {
			ta := newTestApp(t, true)
			ta.transport = &fakeTailscale{status: transport.Status{State: state, Address: netip.MustParseAddr(testTailnetIP)}}
			rl := &redirectListen{}
			ta.tailnetListen = rl.listen
			seed(t, ta.dir, merge(loopbackAny, map[string]string{keyTailnetAdminEnabled: "true"}))
			r := startServe(t, ta, "--engine", "fake")
			if len(rl.addrs()) != 0 || len(r.addrs.Tailnet) != 0 {
				t.Errorf("tailnet listeners were opened: %v", rl.addrs())
			}
			if status, _ := get(t, "http://"+r.addrs.Consumer+"/v1/health"); status != http.StatusUnauthorized {
				t.Errorf("the localhost consumer API answers %d, want 401 from the gateway", status)
			}
			contains(t, "stderr", ta.err.String(), "warning", "this machine only", fix)
			if code := r.stop(); code != 0 {
				t.Fatalf("exit = %d; stderr:\n%s", code, ta.err.String())
			}
			logs, err := os.ReadFile(filepath.Join(ta.logDir, logFileName))
			if err != nil {
				t.Fatal(err)
			}
			contains(t, "log", string(logs), `"level":"WARN"`, "tailnet listeners are off", fix)
		})
	}
}

// The consumer API and the consumer page answer over TLS with the host's certificate, and the page
// listener holds the seam for the account routes. No listener sends CORS headers (req_001 D-148).
func TestTailnetListenersServeHTTPSWithoutCORS(t *testing.T) {
	_, r, _, pool, _ := startTailnetServe(t, nil)
	c := tailnetClient(pool)
	apiAddr, pageAddr := upByRole(r.addrs, transport.RoleConsumerAPI), upByRole(r.addrs, transport.RoleConsumerPage)

	resp, err := c.Get("https://" + apiAddr + "/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || resp.TLS == nil || resp.TLS.PeerCertificates[0].DNSNames[0] != testTailnetName {
		t.Errorf("consumer API: status %d, tls %v", resp.StatusCode, resp.TLS != nil)
	}
	// A plain-HTTP client cannot talk to a tailnet listener.
	if resp, err := testClient.Get("http://" + apiAddr + "/v1/health"); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Errorf("plain HTTP on the tailnet consumer API answered %d", resp.StatusCode)
		}
	}
	for _, addr := range []string{apiAddr, pageAddr} {
		req, _ := http.NewRequest(http.MethodOptions, "https://"+addr+"/v1/chat/completions", nil)
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Access-Control-Request-Method", "POST")
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Errorf("%s answered a CORS preflight with %d", addr, resp.StatusCode)
		}
		for _, k := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials"} {
			if resp.Header.Get(k) != "" {
				t.Errorf("%s answered a preflight with %s", addr, k)
			}
		}
	}
	resp, err = c.Get("https://" + pageAddr + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("consumer page listener: %d, want 404 until the page is built", resp.StatusCode)
	}
	r.stop()
}

// The consumer API takes no cookie: whatever a browser sends never reaches the gateway.
func TestAccessKeysOnlyDropsTheCookie(t *testing.T) {
	var seen string
	h := accessKeysOnly(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = r.Header.Get("Cookie") }))
	req := httptest.NewRequest("GET", "/v1/health", nil)
	req.Header.Set("Cookie", "ls_consumer_session=abc")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "" {
		t.Errorf("the handler saw the cookie %q", seen)
	}
}

// The admin client of a test: a request to a listener with the Host and Origin a browser would send.
func adminCall(t *testing.T, c *http.Client, base, host, origin, method, path, cookie, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "ls_admin_session", Value: cookie})
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func cookieOf(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == "ls_admin_session" {
			return c
		}
	}
	return nil
}

// PLT-B-008: a login on the tailnet admin listener works over TLS, and the session cookie it issues
// stays Secure (req_001 D-86).
func TestRemoteAdminLoginOverTLSIssuesSecureCookie(t *testing.T) {
	_, r, _, pool, _ := startTailnetServe(t, map[string]string{keyTailnetAdminEnabled: "true"})
	local := "http://" + r.addrs.Admin
	resp, _ := adminCall(t, testClient, local, "", "", "POST", "/admin/v1/setup", "", `{"username":"remote-admin","password":"a long enough password"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup = %d", resp.StatusCode)
	}

	adminAddr := upByRole(r.addrs, transport.RoleAdmin)
	if adminAddr == "" {
		t.Fatal("the remote admin listener is not up")
	}
	c := tailnetClient(pool)
	base, host, origin := "https://"+adminAddr, testTailnetName+":8486", "https://"+testTailnetName+":8486"

	// Not logged in yet: refused, and the Host is accepted (the guard knows the host's own name).
	if resp, _ := adminCall(t, c, base, host, "", "GET", "/admin/v1/host", "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET /admin/v1/host without a session = %d, want 401", resp.StatusCode)
	}
	// A wrong name in Host is refused, as on localhost.
	if resp, _ := adminCall(t, c, base, "evil.example", "", "GET", "/admin/v1/host", "", ""); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a foreign Host = %d, want 403", resp.StatusCode)
	}
	resp, body := adminCall(t, c, base, host, origin, "POST", "/admin/v1/session", "", `{"username":"remote-admin","password":"a long enough password"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login over TLS = %d %s", resp.StatusCode, body)
	}
	ck := cookieOf(resp)
	if ck == nil || !ck.Secure || !ck.HttpOnly || ck.Value == "" {
		t.Fatalf("session cookie = %+v, want a Secure, HttpOnly cookie", ck)
	}
	// A browser sends the Secure cookie back over HTTPS, and the session works; the host reports
	// the three tailnet listeners.
	resp, body = adminCall(t, c, base, host, "", "GET", "/admin/v1/host", ck.Value, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/v1/host with the session = %d %s", resp.StatusCode, body)
	}
	var info struct {
		Tailnet struct {
			State     string `json:"state"`
			DNSName   string `json:"dns_name"`
			Listeners []struct {
				Role string `json:"role"`
			} `json:"listeners"`
		} `json:"tailnet"`
	}
	if err := json.Unmarshal([]byte(body), &info); err != nil || info.Tailnet.State != "ready" || info.Tailnet.DNSName != testTailnetName || len(info.Tailnet.Listeners) != 3 {
		t.Errorf("tailnet info = %+v (%v) in %s", info.Tailnet, err, body)
	}
	// A state-changing request over the tailnet needs the page's own https origin.
	if resp, _ := adminCall(t, c, base, host, "http://"+host, "POST", "/admin/v1/host/pause", ck.Value, ""); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a plain-http Origin on the TLS listener = %d, want 403", resp.StatusCode)
	}
	if resp, _ := adminCall(t, c, base, host, origin, "POST", "/admin/v1/host/pause", ck.Value, ""); resp.StatusCode != http.StatusOK {
		t.Errorf("the https Origin = %d, want 200", resp.StatusCode)
	}
	r.stop()
}

// A key bound to a tailnet node works only from that node: the consumer API listener asks Tailscale
// who the peer is and refuses any other with the ordinary invalid_api_key answer (req_001 D-154,
// D-159). The log names the calling node for every tailnet request (req_001 D-146).
func TestKeyBoundToANodeWorksOnlyFromThatNode(t *testing.T) {
	ta, r, ts, pool, _ := startTailnetServe(t, map[string]string{keyTailnetAdminEnabled: "false"})
	local := "http://" + r.addrs.Admin
	origin := "http://" + r.addrs.Admin
	resp, _ := adminCall(t, testClient, local, "", "", "POST", "/admin/v1/setup", "", `{"username":"bind-admin","password":"a long enough password"}`)
	ck := cookieOf(resp)
	if resp.StatusCode != http.StatusCreated || ck == nil {
		t.Fatalf("setup = %d", resp.StatusCode)
	}
	call := func(method, path, body string) (int, map[string]any) {
		resp, b := adminCall(t, testClient, local, "", origin, method, path, ck.Value, body)
		var m map[string]any
		json.Unmarshal([]byte(b), &m)
		return resp.StatusCode, m
	}
	_, models := call("GET", "/admin/v1/models", "")
	for _, m := range models["data"].([]any) {
		if code, _ := call("PATCH", "/admin/v1/models/"+m.(map[string]any)["id"].(string), `{"allowed":true}`); code != http.StatusOK {
			t.Fatalf("allow model = %d", code)
		}
	}
	code, cons := call("POST", "/admin/v1/consumers", `{"kind":"application","display_name":"Bound App"}`)
	if code != http.StatusCreated {
		t.Fatalf("create consumer = %d", code)
	}
	cid := cons["id"].(string)
	if code, _ := call("PUT", "/admin/v1/consumers/"+cid+"/grant", `{"models":[{"model":"fake-chat","context_limit":4096,"max_output_tokens":1024}],
"quotas":[{"metric":"tokens","limit":100000,"window_seconds":86400}],"concurrency":2,"on_context_overflow":"refuse","allowed_request_properties":null,"expires_at":null}`); code != http.StatusOK {
		t.Fatalf("set grant = %d", code)
	}
	code, issued := call("POST", "/admin/v1/consumers/"+cid+"/access-keys", `{"name":"bound","bound_node":"laptop"}`)
	if code != http.StatusCreated {
		t.Fatalf("issue key = %d %v", code, issued)
	}
	if k := issued["access_key"].(map[string]any); k["bound_node"] != "nLAPTOP" || k["bound_node_name"] != "laptop" {
		t.Fatalf("issued key = %v, want the node id and the machine name", k)
	}
	secret := issued["secret"].(string)

	c := tailnetClient(pool)
	health := func() int {
		req, _ := http.NewRequest("GET", "https://"+upByRole(r.addrs, transport.RoleConsumerAPI)+"/v1/health", nil)
		req.Header.Set("Authorization", "Bearer "+secret)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := health(); got != http.StatusOK {
		t.Fatalf("the bound node: GET /v1/health = %d, want 200", got)
	}
	ts.setPeer("127.0.0.1", transport.Node{ID: "nPHONE", Name: "phone"})
	if got := health(); got != http.StatusUnauthorized {
		t.Fatalf("another node: GET /v1/health = %d, want 401", got)
	}
	delete(ts.peers, "127.0.0.1") // a connection Tailscale cannot name
	if got := health(); got != http.StatusUnauthorized {
		t.Fatalf("an unknown node: GET /v1/health = %d, want 401", got)
	}
	r.stop()

	logs, err := os.ReadFile(filepath.Join(ta.logDir, logFileName))
	if err != nil {
		t.Fatal(err)
	}
	contains(t, "log", string(logs), `"msg":"tailnet request"`, `"node_id":"nLAPTOP"`, `"node_name":"laptop"`, `"node_id":"nPHONE"`, "could not name")
	if strings.Contains(string(logs), secret) {
		t.Error("the access key reached the log")
	}
}

// doctor reports Tailscale: a warning that names the fix when it cannot carry the tailnet listeners,
// and what serve would bind when it can.
func TestDoctorChecksTailscale(t *testing.T) {
	ta := newTestApp(t, false)
	rep := ta.runDoctor(context.Background(), ta.dir)
	c := rep.byName(t, "tailscale")
	if c.Status != statusWarn || !strings.Contains(c.Advice, "Tailscale") || !strings.Contains(c.Detail, "this machine only") {
		t.Errorf("without Tailscale: %+v", c)
	}
	if !rep.OK {
		t.Error("a host without Tailscale must not fail doctor")
	}
	for _, c := range rep.Checks {
		if strings.HasPrefix(c.Name, "tailnet_") {
			t.Errorf("a tailnet port check ran without a tailnet: %+v", c)
		}
	}

	ts, _ := newReadyTailscale(t)
	ta.transport = ts
	seed(t, ta.dir, map[string]string{keyTailnetAdminEnabled: "true", keyTailnetConsumerPort: "18484", keyTailnetPagePort: "18485", keyTailnetAdminPort: "18486"})
	rep = ta.runDoctor(context.Background(), ta.dir)
	c = rep.byName(t, "tailscale")
	contains(t, "tailscale check", c.Detail, "1.99.0", testTailnetName, testTailnetIP, "consumer_api "+testTailnetIP+":18484", "admin "+testTailnetIP+":18486")
	if c.Status != statusOK {
		t.Errorf("ready Tailscale: %+v", c)
	}
	// The ports are checked on the tailnet address, which this machine does not have, so the bind
	// is a warning at worst; the checks must exist for each listener.
	for _, name := range []string{"tailnet_consumer_api_port", "tailnet_consumer_page_port", "tailnet_admin_port"} {
		rep.byName(t, name)
	}
}

// A setting that makes two tailnet listeners share a port stops the start with the settings named.
func TestTailnetPortsMustDiffer(t *testing.T) {
	ta := newTestApp(t, true)
	seed(t, ta.dir, merge(loopbackAny, map[string]string{keyTailnetPagePort: "8484"}))
	code := ta.run(context.Background(), []string{"serve", "--engine", "fake", "--data-dir", ta.dir})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	contains(t, "stderr", ta.err.String(), keyTailnetConsumerPort, keyTailnetPagePort, "different ports")
}

func TestNodeOfReturnsTheStableNodeID(t *testing.T) {
	ts, _ := newReadyTailscale(t)
	n := nodeOf{tr: ts, timeout: time.Second}
	if id, err := n.NodeOf(context.Background(), "127.0.0.1:5555"); err != nil || id != "nLAPTOP" {
		t.Errorf("NodeOf = %q, %v", id, err)
	}
	if _, err := n.NodeOf(context.Background(), "203.0.113.9:1"); err == nil {
		t.Error("a stranger was named")
	}
}

// hangingResolver is a transport whose Resolve waits until its context is done.
type hangingResolver struct{ transport.Transport }

func (hangingResolver) Resolve(ctx context.Context, _ string) (transport.Node, error) {
	<-ctx.Done()
	return transport.Node{}, ctx.Err()
}

func TestBoundedResolverStopsAHangingLookup(t *testing.T) {
	r := boundedResolver{tr: hangingResolver{}, timeout: 50 * time.Millisecond}
	start := time.Now()
	_, err := r.Resolve(context.Background(), "laptop")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Resolve error = %v, want a deadline error", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Resolve took %v, want it to stop at the timeout", d)
	}
}

func TestBoundedResolverWithZeroTimeoutPassesTheContextThrough(t *testing.T) {
	ts, _ := newReadyTailscale(t)
	ts.refs = map[string]transport.Node{"laptop": {ID: "nLAPTOP"}}
	n, err := boundedResolver{tr: ts}.Resolve(context.Background(), "laptop")
	if err != nil || n.ID != "nLAPTOP" {
		t.Errorf("Resolve = %+v, %v", n, err)
	}
}

// PLT-F-027: statement 4 of the finish line, over the tailnet's TLS listeners. A consumer signs up
// from an invite on the page listener, sees its limits, keys and usage with the session cookie,
// and logs out. The same cookie never works on the consumer API listener, and a key never works on
// the page listener (req_001 D-148).
func TestConsumerSignsUpAndSeesItsAccountOnThePageListener(t *testing.T) {
	_, r, _, pool, _ := startTailnetServe(t, nil)
	local := "http://" + r.addrs.Admin
	resp, _ := adminCall(t, testClient, local, "", "", "POST", "/admin/v1/setup", "", `{"username":"page-admin","password":"a long enough password"}`)
	ck := cookieOf(resp)
	if resp.StatusCode != http.StatusCreated || ck == nil {
		t.Fatalf("setup = %d", resp.StatusCode)
	}
	admin := func(method, path, body string) (int, map[string]any) {
		resp, b := adminCall(t, testClient, local, "", local, method, path, ck.Value, body)
		var m map[string]any
		json.Unmarshal([]byte(b), &m)
		return resp.StatusCode, m
	}
	_, models := admin("GET", "/admin/v1/models", "")
	for _, m := range models["data"].([]any) {
		if code, _ := admin("PATCH", "/admin/v1/models/"+m.(map[string]any)["id"].(string), `{"allowed":true}`); code != http.StatusOK {
			t.Fatalf("allow model = %d", code)
		}
	}
	code, inv := admin("POST", "/admin/v1/invites", `{}`)
	if code != http.StatusCreated {
		t.Fatalf("create invite = %d %v", code, inv)
	}
	token := inv["token"].(string)

	c := tailnetClient(pool)
	pageAddr := upByRole(r.addrs, transport.RoleConsumerPage)
	apiAddr := upByRole(r.addrs, transport.RoleConsumerAPI)
	pageHost := testTailnetName + ":8485"
	page := func(method, path, cookie, body string) (*http.Response, string) {
		req, err := http.NewRequest(method, "https://"+pageAddr+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = pageHost
		if method != http.MethodGet {
			req.Header.Set("Origin", "https://"+pageHost)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "ls_session", Value: cookie})
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}
	sessionOf := func(resp *http.Response) *http.Cookie {
		for _, ck := range resp.Cookies() {
			if ck.Name == "ls_session" {
				return ck
			}
		}
		return nil
	}

	// Sign up from the invite.
	signup := `{"invite_token":"` + token + `","display_name":"Page Consumer","username":"page-consumer","password":"a long enough password","consent":{"terms_version":"terms-2026-10-v1","privacy_version":"privacy-2026-10-v1"}}`
	resp, body := page("POST", "/v1/invite-redemptions/password", "", signup)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("sign-up = %d %s", resp.StatusCode, body)
	}
	sess := sessionOf(resp)
	if sess == nil || !sess.Secure || !sess.HttpOnly || sess.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie = %+v", sess)
	}
	var me map[string]any
	if err := json.Unmarshal([]byte(body), &me); err != nil || me["username"] != "page-consumer" {
		t.Fatalf("consumer = %s", body)
	}
	cid := me["id"].(string)
	// The invite is spent.
	if resp, _ := page("POST", "/v1/invite-redemptions/password", "", signup); resp.StatusCode != http.StatusGone {
		t.Fatalf("second sign-up = %d, want 410", resp.StatusCode)
	}

	// No grant yet: no limits; no keys; no usage.
	if resp, _ := page("GET", "/v1/me", sess.Value, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/me = %d", resp.StatusCode)
	}
	if resp, _ := page("GET", "/v1/me/limits", sess.Value, ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("limits before a grant = %d, want 404", resp.StatusCode)
	}
	// The host gives a grant and a key.
	if code, _ := admin("PUT", "/admin/v1/consumers/"+cid+"/grant", `{"models":[{"model":"fake-chat","context_limit":4096,"max_output_tokens":1024}],
"quotas":[{"metric":"tokens","limit":100000,"window_seconds":86400}],"concurrency":2,"on_context_overflow":"refuse","allowed_request_properties":null,"expires_at":null}`); code != http.StatusOK {
		t.Fatalf("set grant = %d", code)
	}
	code, issued := admin("POST", "/admin/v1/consumers/"+cid+"/access-keys", `{"name":"page key"}`)
	if code != http.StatusCreated {
		t.Fatalf("issue key = %d %v", code, issued)
	}
	secret := issued["secret"].(string)
	prefix := issued["access_key"].(map[string]any)["prefix"].(string)

	resp, body = page("GET", "/v1/me/limits", sess.Value, "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "fake-chat") || !strings.Contains(body, `"version":1`) {
		t.Fatalf("limits = %d %s", resp.StatusCode, body)
	}
	resp, body = page("GET", "/v1/me/access-keys", sess.Value, "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, prefix) || strings.Contains(body, secret) {
		t.Fatalf("access keys = %d %s", resp.StatusCode, body)
	}
	resp, body = page("GET", "/v1/usage?granularity=day", sess.Value, "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"data":[]`) {
		t.Fatalf("usage = %d %s", resp.StatusCode, body)
	}

	// A wrong Host is refused by the guard; a key is not accepted on the page listener.
	req, _ := http.NewRequest("GET", "https://"+pageAddr+"/v1/me", nil)
	req.Host = "evil.example"
	req.AddCookie(&http.Cookie{Name: "ls_session", Value: sess.Value})
	if resp, err := c.Do(req); err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a foreign Host on the page listener: %v %v", resp, err)
	}
	req, _ = http.NewRequest("GET", "https://"+pageAddr+"/v1/me", nil)
	req.Host = pageHost
	req.Header.Set("Authorization", "Bearer "+secret)
	if resp, err := c.Do(req); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a key on the page listener: %v %v", resp, err)
	}

	// The consumer API takes the key for /v1/me and never the cookie.
	apiGet := func(path string, hdr map[string]string) (int, string) {
		req, _ := http.NewRequest("GET", "https://"+apiAddr+path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, b := apiGet("/v1/me", map[string]string{"Authorization": "Bearer " + secret}); code != http.StatusOK || !strings.Contains(b, cid) {
		t.Fatalf("GET /v1/me with a key = %d %s", code, b)
	}
	if code, b := apiGet("/v1/me/access-keys", map[string]string{"Authorization": "Bearer " + secret}); code != http.StatusOK || !strings.Contains(b, prefix) {
		t.Fatalf("GET /v1/me/access-keys with a key = %d %s", code, b)
	}
	if code, _ := apiGet("/v1/me", map[string]string{"Cookie": "ls_session=" + sess.Value}); code != http.StatusUnauthorized {
		t.Fatalf("GET /v1/me with a cookie on the consumer API = %d, want 401", code)
	}

	// Log out: the cookie is cleared and the session is gone.
	resp, _ = page("DELETE", "/v1/session", sess.Value, "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout = %d", resp.StatusCode)
	}
	if resp, _ := page("GET", "/v1/me", sess.Value, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout GET /v1/me = %d, want 401", resp.StatusCode)
	}
	// Login again with the password.
	resp, body = page("POST", "/v1/session/password", "", `{"username":"page-consumer","password":"a long enough password"}`)
	if resp.StatusCode != http.StatusOK || sessionOf(resp) == nil {
		t.Fatalf("login = %d %s", resp.StatusCode, body)
	}
	r.stop()
}
