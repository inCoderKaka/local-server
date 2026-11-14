package transport

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

// Role is which service a tailnet listener carries (req_001 D-148).
type Role string

// The three tailnet listeners, each on its own port.
const (
	// RoleConsumerAPI serves /v1 to client apps, with access keys only.
	RoleConsumerAPI Role = "consumer_api"
	// RoleConsumerPage serves the consumer web page and the /v1 account routes it needs, same
	// origin.
	RoleConsumerPage Role = "consumer_page"
	// RoleAdmin serves the admin API and UI behind the admin login. It exists only when the host
	// switched it on.
	RoleAdmin Role = "admin"
)

// Ports is the host-owned configuration of the tailnet listeners (req_001 D-38, D-148).
type Ports struct {
	ConsumerAPI  int
	ConsumerPage int
	Admin        int
	// AdminEnabled adds the remote admin listener. It is off by default: the admin listener on
	// localhost is the default and nothing admin-related is on the tailnet unless the host
	// asks (req_001 D-10).
	AdminEnabled bool
}

// Spec is one listener to bring up: its role and the host:port it binds. The address is always
// the host's tailnet address, never a wildcard and never loopback.
type Spec struct {
	Role Role
	Addr string
}

// Plan returns the tailnet listeners for a status: none unless the transport is ready; then the
// consumer API and consumer page, and the admin listener only when ports.AdminEnabled. Every
// listener binds only to the tailnet address (req_001 D-148).
func Plan(st Status, ports Ports) []Spec {
	if !st.Ready() || !st.Address.IsValid() || st.Address.IsUnspecified() || st.Address.IsLoopback() {
		return nil
	}
	at := func(port int) string { return net.JoinHostPort(st.Address.String(), strconv.Itoa(port)) }
	specs := []Spec{
		{RoleConsumerAPI, at(ports.ConsumerAPI)},
		{RoleConsumerPage, at(ports.ConsumerPage)},
	}
	if ports.AdminEnabled {
		specs = append(specs, Spec{RoleAdmin, at(ports.Admin)})
	}
	return specs
}

// Report is how the host is reachable over the tailnet: what detection found and the listeners
// that were brought up. The admin API and doctor show it (req_001 D-148).
type Report struct {
	Status    Status
	Listeners []Spec
}

// Listener is a bound tailnet listener.
type Listener struct {
	Spec
	net.Listener
}

// ListenFunc opens a TCP listener; net.Listen("tcp", addr) is the real one. Tests pass a fake
// to see which addresses would be bound.
type ListenFunc func(network, addr string) (net.Listener, error)

// Bind opens every spec, all or none: if one cannot be bound, those already bound are closed
// and the error names the role and the address.
func Bind(specs []Spec, listen ListenFunc) ([]Listener, error) {
	if listen == nil {
		listen = net.Listen
	}
	var out []Listener
	for _, sp := range specs {
		if err := checkTailnetAddr(sp.Addr); err != nil {
			closeAll(out)
			return nil, fmt.Errorf("transport: %s listener: %w", sp.Role, err)
		}
		ln, err := listen("tcp", sp.Addr)
		if err != nil {
			closeAll(out)
			return nil, fmt.Errorf("transport: %s listener on %s: %w", sp.Role, sp.Addr, err)
		}
		out = append(out, Listener{Spec: sp, Listener: ln})
	}
	return out, nil
}

// checkTailnetAddr refuses a wildcard or loopback address: a tailnet listener must never be
// reachable from other networks, and must never be mistaken for the localhost one.
func checkTailnetAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.IsUnspecified() || ip.IsLoopback() {
		return errors.New("must bind to the tailnet address only, not a wildcard or loopback address")
	}
	return nil
}

func closeAll(ls []Listener) {
	for _, l := range ls {
		l.Close()
	}
}
