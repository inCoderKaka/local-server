package tailscale

import (
	"context"
	"crypto/tls"
	"errors"
	"net/netip"
	"testing"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"

	"github.com/inCoderKaka/local-server/internal/transport"
)

// fakeAPI is a Tailscale local API with canned answers. No test here talks to a real Tailscale.
type fakeAPI struct {
	status    *ipnstate.Status
	statusErr error
	whois     map[string]*apitype.WhoIsResponse
	whoisErr  error
	cert      *tls.Certificate
	asked     []string
}

func (f *fakeAPI) StatusWithoutPeers(context.Context) (*ipnstate.Status, error) {
	return f.status, f.statusErr
}
func (f *fakeAPI) Status(context.Context) (*ipnstate.Status, error) { return f.status, f.statusErr }
func (f *fakeAPI) WhoIs(_ context.Context, addr string) (*apitype.WhoIsResponse, error) {
	f.asked = append(f.asked, addr)
	if f.whoisErr != nil {
		return nil, f.whoisErr
	}
	if r, ok := f.whois[addr]; ok {
		return r, nil
	}
	return nil, local.ErrPeerNotFound
}
func (f *fakeAPI) GetCertificate(hi *tls.ClientHelloInfo) (*tls.Certificate, error) {
	f.asked = append(f.asked, "cert:"+hi.ServerName)
	return f.cert, nil
}

func ip(s string) netip.Addr { return netip.MustParseAddr(s) }

func running() *ipnstate.Status {
	return &ipnstate.Status{
		Version:      "1.99.0",
		BackendState: "Running",
		TailscaleIPs: []netip.Addr{ip("fd7a:115c:a1e0::1"), ip("100.64.0.1")},
		CertDomains:  []string{"host.example-tailnet.ts.net"},
		Self: &ipnstate.PeerStatus{ID: "nSELF", HostName: "host", DNSName: "host.example-tailnet.ts.net.",
			TailscaleIPs: []netip.Addr{ip("100.64.0.1")}},
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			key.NewNode().Public(): {ID: "nLAPTOP", HostName: "Laptop", DNSName: "laptop.example-tailnet.ts.net.", TailscaleIPs: []netip.Addr{ip("100.64.0.2")}},
			key.NewNode().Public(): {ID: "nPHONE", HostName: "phone", DNSName: "phone.example-tailnet.ts.net.", TailscaleIPs: []netip.Addr{ip("100.64.0.3")}},
		},
	}
}

func TestStatusStates(t *testing.T) {
	cases := map[string]struct {
		api  *fakeAPI
		want transport.State
	}{
		"ready":            {&fakeAPI{status: running()}, transport.StateReady},
		"daemon not there": {&fakeAPI{statusErr: errors.New("dial unix /var/run/tailscaled.socket: connect: no such file or directory")}, transport.StateUnavailable},
		"access denied":    {&fakeAPI{statusErr: &local.AccessDeniedError{}}, transport.StateUnavailable},
		"switched off":     {&fakeAPI{status: &ipnstate.Status{BackendState: "Stopped"}}, transport.StateStopped},
		"needs login":      {&fakeAPI{status: &ipnstate.Status{BackendState: "NeedsLogin"}}, transport.StateLoggedOut},
		"needs approval":   {&fakeAPI{status: &ipnstate.Status{BackendState: "NeedsMachineAuth"}}, transport.StateLoggedOut},
		"starting":         {&fakeAPI{status: &ipnstate.Status{BackendState: "Starting"}}, transport.StateStarting},
		"no state":         {&fakeAPI{status: &ipnstate.Status{BackendState: "NoState"}}, transport.StateStarting},
		"running, no address": {&fakeAPI{status: &ipnstate.Status{BackendState: "Running",
			CertDomains: []string{"x.ts.net"}}}, transport.StateStarting},
		"no https certs": {&fakeAPI{status: func() *ipnstate.Status { s := running(); s.CertDomains = nil; return s }()}, transport.StateNoHTTPS},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := NewWith(tc.api).Status(context.Background())
			if got.State != tc.want {
				t.Fatalf("state = %q, want %q (detail %q)", got.State, tc.want, got.Detail)
			}
			if got.Ready() != (tc.want == transport.StateReady) {
				t.Errorf("Ready() = %v", got.Ready())
			}
			if tc.want != transport.StateReady && (got.Message() == "" || got.Fix() == "") {
				t.Errorf("a state that keeps the tailnet listeners off must name the problem and the fix: %q / %q", got.Message(), got.Fix())
			}
		})
	}
}

func TestStatusReadyDetails(t *testing.T) {
	got := NewWith(&fakeAPI{status: running()}).Status(context.Background())
	if got.Address != ip("100.64.0.1") {
		t.Errorf("address = %v, want the IPv4 address", got.Address)
	}
	if got.DNSName != "host.example-tailnet.ts.net" {
		t.Errorf("dns name = %q, want it without the trailing dot", got.DNSName)
	}
	if got.Version != "1.99.0" {
		t.Errorf("version = %q", got.Version)
	}
}

func whoisNode(stable, computed, name string) *apitype.WhoIsResponse {
	return &apitype.WhoIsResponse{Node: &tailcfg.Node{StableID: tailcfg.StableNodeID(stable), ComputedName: computed, Name: name}}
}

func TestWhoIsReturnsIDAndName(t *testing.T) {
	api := &fakeAPI{whois: map[string]*apitype.WhoIsResponse{
		"100.64.0.2:5555": whoisNode("nLAPTOP", "laptop", "laptop.example-tailnet.ts.net."),
		"100.64.0.3:1":    whoisNode("nPHONE", "", "phone.example-tailnet.ts.net."),
	}}
	a := NewWith(api)
	n, err := a.WhoIs(context.Background(), "100.64.0.2:5555")
	if err != nil || n != (transport.Node{ID: "nLAPTOP", Name: "laptop"}) {
		t.Fatalf("WhoIs = %+v, %v", n, err)
	}
	n, err = a.WhoIs(context.Background(), "100.64.0.3:1")
	if err != nil || n != (transport.Node{ID: "nPHONE", Name: "phone"}) {
		t.Fatalf("WhoIs without a computed name = %+v, %v", n, err)
	}
	if _, err := a.WhoIs(context.Background(), "203.0.113.9:1"); !errors.Is(err, transport.ErrNodeNotFound) {
		t.Errorf("a stranger: err = %v, want ErrNodeNotFound", err)
	}
	api.whoisErr = errors.New("connection refused")
	if _, err := a.WhoIs(context.Background(), "100.64.0.2:5555"); !errors.Is(err, transport.ErrUnavailable) {
		t.Errorf("local API down: err = %v, want ErrUnavailable", err)
	}
}

func TestResolve(t *testing.T) {
	api := &fakeAPI{status: running(), whois: map[string]*apitype.WhoIsResponse{
		"100.64.0.2": whoisNode("nLAPTOP", "laptop", "laptop.example-tailnet.ts.net."),
	}}
	a := NewWith(api)
	want := transport.Node{ID: "nLAPTOP", Name: "laptop"}
	for _, ref := range []string{"100.64.0.2", "nLAPTOP", "laptop", "LAPTOP", "laptop.example-tailnet.ts.net", " laptop.example-tailnet.ts.net. "} {
		n, err := a.Resolve(context.Background(), ref)
		if err != nil || n != want {
			t.Errorf("Resolve(%q) = %+v, %v; want %+v", ref, n, err, want)
		}
	}
	if n, err := a.Resolve(context.Background(), "host"); err != nil || n.ID != "nSELF" {
		t.Errorf("this machine: %+v, %v", n, err)
	}
	for _, ref := range []string{"", "nobody", "100.64.0.99"} {
		if _, err := a.Resolve(context.Background(), ref); !errors.Is(err, transport.ErrNodeNotFound) {
			t.Errorf("Resolve(%q) err = %v, want ErrNodeNotFound", ref, err)
		}
	}
	api.statusErr = errors.New("down")
	if _, err := a.Resolve(context.Background(), "laptop"); !errors.Is(err, transport.ErrUnavailable) {
		t.Errorf("local API down: err = %v, want ErrUnavailable", err)
	}
}

func TestResolveAmbiguousName(t *testing.T) {
	st := running()
	st.Peer[key.NewNode().Public()] = &ipnstate.PeerStatus{ID: "nLAPTOP2", HostName: "laptop", DNSName: "laptop-1.example-tailnet.ts.net."}
	a := NewWith(&fakeAPI{status: st})
	if _, err := a.Resolve(context.Background(), "laptop"); !errors.Is(err, transport.ErrAmbiguousNode) {
		t.Fatalf("err = %v, want transport.ErrAmbiguousNode", err)
	}
	if n, err := a.Resolve(context.Background(), "nLAPTOP2"); err != nil || n.ID != "nLAPTOP2" {
		t.Errorf("by node ID: %+v, %v", n, err)
	}
}

func TestTLSConfigAsksTailscaleForTheCertificate(t *testing.T) {
	want := &tls.Certificate{}
	api := &fakeAPI{cert: want}
	cfg := NewWith(api).TLSConfig()
	if cfg.MinVersion < tls.VersionTLS12 {
		t.Errorf("min version = %x", cfg.MinVersion)
	}
	got, err := cfg.GetCertificate(&tls.ClientHelloInfo{ServerName: "host.example-tailnet.ts.net"})
	if err != nil || got != want {
		t.Fatalf("GetCertificate = %v, %v", got, err)
	}
	if len(api.asked) != 1 || api.asked[0] != "cert:host.example-tailnet.ts.net" {
		t.Errorf("asked %v", api.asked)
	}
}
