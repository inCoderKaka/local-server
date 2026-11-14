package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"net/netip"
)

// Node is a tailnet node as the transport names it. ID is the stable node ID, which survives a
// rename; Name is the machine name, which a person recognises (req_001 D-159). Neither is a
// secret.
type Node struct {
	ID   string
	Name string
}

// Errors of the port.
var (
	// ErrNodeNotFound means no node matches: an address that is not a peer, or a name the tailnet
	// does not know.
	ErrNodeNotFound = errors.New("transport: no such tailnet node")
	// ErrAmbiguousNode means a machine name matches more than one node; the host uses the node ID
	// or the tailnet address instead.
	ErrAmbiguousNode = errors.New("transport: more than one tailnet node has that name; use its node ID or tailnet address")
	// ErrUnavailable means the transport could not be asked (Tailscale is not running, or this
	// user may not use it).
	ErrUnavailable = errors.New("transport: the transport is not available")
)

// State is how ready the host's Tailscale is for the tailnet listeners.
type State string

// The states. Only StateReady brings the tailnet listeners up; every other one leaves local-server
// serving on localhost and names a fix (req_001 D-148).
const (
	// StateReady: running, logged in, with an address and a *.ts.net name that can have a
	// certificate.
	StateReady State = "ready"
	// StateUnavailable: Tailscale did not answer: not installed, not running, or not reachable
	// by this user.
	StateUnavailable State = "unavailable"
	// StateStopped: the Tailscale app is installed and switched off.
	StateStopped State = "stopped"
	// StateLoggedOut: Tailscale needs a login or the tailnet admin's approval of this machine.
	StateLoggedOut State = "logged_out"
	// StateStarting: Tailscale is still connecting.
	StateStarting State = "starting"
	// StateNoHTTPS: the tailnet has no HTTPS certificates (MagicDNS or HTTPS is off).
	StateNoHTTPS State = "no_https"
)

// Status is what detection found. It is never an error: a host without a usable Tailscale is a
// normal state that local-server reports and runs without.
type Status struct {
	State State
	// Address is the host's tailnet IPv4 address (IPv6 when it has none); zero unless Tailscale
	// is running.
	Address netip.Addr
	// DNSName is the host's MagicDNS name that the certificate is for, without the trailing
	// dot, for example host.tail1234.ts.net. Empty when the tailnet has no HTTPS certificates.
	DNSName string
	// Version is Tailscale's own version string, when it answered.
	Version string
	// Detail is the technical cause of an unavailable state. It never holds a secret.
	Detail string
}

// Ready reports whether the tailnet listeners can be brought up.
func (s Status) Ready() bool { return s.State == StateReady }

// Message says in one sentence why the tailnet listeners are off; empty when Ready.
func (s Status) Message() string {
	switch s.State {
	case StateReady:
		return ""
	case StateStopped:
		return "Tailscale is installed but switched off"
	case StateLoggedOut:
		return "Tailscale is not logged in on this machine"
	case StateStarting:
		return "Tailscale is still connecting"
	case StateNoHTTPS:
		return "this tailnet has no HTTPS certificates"
	}
	return "Tailscale is not installed or not running"
}

// Fix names what the host does about it; empty when Ready.
func (s Status) Fix() string {
	switch s.State {
	case StateReady:
		return ""
	case StateStopped:
		return "turn Tailscale on, then restart local-server"
	case StateLoggedOut:
		return "log in to Tailscale (and have the tailnet admin approve this machine if it asks), then restart local-server"
	case StateStarting:
		return "wait for Tailscale to connect, then restart local-server"
	case StateNoHTTPS:
		return "in the tailnet's admin console turn on MagicDNS and HTTPS Certificates (DNS page), then restart local-server"
	}
	return "install and start the standalone Tailscale app from tailscale.com, log in, then restart local-server"
}

// Transport is the host's encrypted channel. An adapter implements it; nothing outside cmd/
// imports an adapter.
type Transport interface {
	// Status detects the state of the transport now. It never fails: a transport that does not
	// answer is StateUnavailable.
	Status(ctx context.Context) Status
	// WhoIs names the node behind a connection's remote address ("ip:port"). It returns
	// ErrNodeNotFound for an address that is not a tailnet peer.
	WhoIs(ctx context.Context, remoteAddr string) (Node, error)
	// Resolve finds a node of this tailnet by a reference the host types: a tailnet IP address,
	// a stable node ID, a machine name or a MagicDNS name. It returns ErrNodeNotFound when none
	// matches, ErrAmbiguousNode when a name matches several, and ErrUnavailable when the
	// transport cannot be asked.
	Resolve(ctx context.Context, ref string) (Node, error)
	// TLSConfig is the server TLS configuration of the tailnet listeners: it serves the
	// *.ts.net certificate the transport issues for the SNI name.
	TLSConfig() *tls.Config
}
