package tailscale

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn/ipnstate"

	"github.com/inCoderKaka/local-server/internal/transport"
)

// LocalAPI is the part of Tailscale's local API the adapter uses; *local.Client implements it.
// Tests pass a fake: no unit test talks to a real Tailscale.
type LocalAPI interface {
	// StatusWithoutPeers is the node's own state, quickly.
	StatusWithoutPeers(ctx context.Context) (*ipnstate.Status, error)
	// Status is the node's state with every peer.
	Status(ctx context.Context) (*ipnstate.Status, error)
	// WhoIs names the node behind an "ip" or "ip:port".
	WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error)
	// GetCertificate issues or loads the *.ts.net certificate for hi.ServerName.
	GetCertificate(hi *tls.ClientHelloInfo) (*tls.Certificate, error)
}

// Backend states of Tailscale (ipn.State), as ipnstate.Status.BackendState spells them.
const (
	backendRunning          = "Running"
	backendStopped          = "Stopped"
	backendStarting         = "Starting"
	backendNeedsLogin       = "NeedsLogin"
	backendNeedsMachineAuth = "NeedsMachineAuth"
)

// Adapter implements transport.Transport over Tailscale's local API.
type Adapter struct{ api LocalAPI }

var _ transport.Transport = (*Adapter)(nil)

// New returns the adapter on the default local client, which finds the daemon's socket, or on
// macOS the standalone and App Store apps' port and token, by itself (req_001 D-147).
func New() *Adapter { return NewWith(&local.Client{}) }

// NewWith returns the adapter on api.
func NewWith(api LocalAPI) *Adapter { return &Adapter{api: api} }

// Status detects the Tailscale state now (req_001 D-148).
func (a *Adapter) Status(ctx context.Context) transport.Status {
	st, err := a.api.StatusWithoutPeers(ctx)
	if err != nil {
		return transport.Status{State: transport.StateUnavailable, Detail: describe(err)}
	}
	out := transport.Status{Version: st.Version}
	switch st.BackendState {
	case backendRunning:
	case backendStopped:
		out.State = transport.StateStopped
		return out
	case backendNeedsLogin, backendNeedsMachineAuth:
		out.State = transport.StateLoggedOut
		return out
	default: // Starting, NoState and any state a newer Tailscale adds
		out.State = transport.StateStarting
		out.Detail = "backend state " + st.BackendState
		return out
	}
	out.Address = pickAddress(st.TailscaleIPs)
	if !out.Address.IsValid() {
		out.State = transport.StateStarting
		out.Detail = "no tailnet address assigned yet"
		return out
	}
	out.DNSName = certName(st)
	if out.DNSName == "" {
		out.State = transport.StateNoHTTPS
		return out
	}
	out.State = transport.StateReady
	return out
}

// pickAddress prefers the IPv4 address: it is the one every client can reach.
func pickAddress(ips []netip.Addr) netip.Addr {
	for _, ip := range ips {
		if ip.Is4() {
			return ip
		}
	}
	if len(ips) > 0 {
		return ips[0]
	}
	return netip.Addr{}
}

// certName is the name the node's certificate is for: its own MagicDNS name when the tailnet
// issues certificates for it, else the first certificate domain; empty when there are none.
func certName(st *ipnstate.Status) string {
	if len(st.CertDomains) == 0 {
		return ""
	}
	if st.Self != nil {
		self := strings.TrimSuffix(st.Self.DNSName, ".")
		for _, d := range st.CertDomains {
			if strings.EqualFold(d, self) {
				return d
			}
		}
	}
	return st.CertDomains[0]
}

// describe is the cause of a failed local API call, for a log line. The text of an error from
// the local API holds no secret.
func describe(err error) string {
	if local.IsAccessDeniedError(err) {
		return "this user may not use Tailscale's local API: " + err.Error()
	}
	return err.Error()
}

// WhoIs names the tailnet node behind a connection.
func (a *Adapter) WhoIs(ctx context.Context, remoteAddr string) (transport.Node, error) {
	resp, err := a.api.WhoIs(ctx, remoteAddr)
	switch {
	case errors.Is(err, local.ErrPeerNotFound):
		return transport.Node{}, transport.ErrNodeNotFound
	case err != nil:
		return transport.Node{}, fmt.Errorf("%w: %s", transport.ErrUnavailable, describe(err))
	case resp == nil || resp.Node == nil || resp.Node.StableID == "":
		return transport.Node{}, transport.ErrNodeNotFound
	}
	n := resp.Node
	name := n.ComputedName
	if name == "" {
		name = firstLabel(n.Name)
	}
	return transport.Node{ID: string(n.StableID), Name: name}, nil
}

// Resolve finds a node by tailnet IP address (through WhoIs), or by stable node ID, machine name
// or MagicDNS name among this node and its peers.
func (a *Adapter) Resolve(ctx context.Context, ref string) (transport.Node, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return transport.Node{}, transport.ErrNodeNotFound
	}
	if ip, err := netip.ParseAddr(ref); err == nil {
		return a.WhoIs(ctx, ip.String())
	}
	st, err := a.api.Status(ctx)
	if err != nil {
		return transport.Node{}, fmt.Errorf("%w: %s", transport.ErrUnavailable, describe(err))
	}
	var found []transport.Node
	consider := func(p *ipnstate.PeerStatus) {
		if p == nil || p.ID == "" || !matches(p, ref) {
			return
		}
		found = append(found, transport.Node{ID: string(p.ID), Name: peerName(p)})
	}
	consider(st.Self)
	for _, p := range st.Peer {
		consider(p)
	}
	switch len(found) {
	case 0:
		return transport.Node{}, transport.ErrNodeNotFound
	case 1:
		return found[0], nil
	}
	// A stable ID is unique, so several hits are several nodes with one machine name.
	return transport.Node{}, transport.ErrAmbiguousNode
}

func matches(p *ipnstate.PeerStatus, ref string) bool {
	ref = strings.TrimSuffix(ref, ".")
	dns := strings.TrimSuffix(p.DNSName, ".")
	return string(p.ID) == ref || strings.EqualFold(dns, ref) || strings.EqualFold(firstLabel(dns), ref) || strings.EqualFold(p.HostName, ref)
}

func peerName(p *ipnstate.PeerStatus) string {
	if l := firstLabel(p.DNSName); l != "" {
		return l
	}
	return p.HostName
}

func firstLabel(dns string) string {
	dns = strings.TrimSuffix(dns, ".")
	if i := strings.IndexByte(dns, '.'); i >= 0 {
		return dns[:i]
	}
	return dns
}

// TLSConfig serves the *.ts.net certificate Tailscale issues for the name a client asks for.
// Tailscale caches the certificate and renews it, so every handshake asks. TLS 1.2 is the
// floor.
func (a *Adapter) TLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: a.api.GetCertificate}
}
