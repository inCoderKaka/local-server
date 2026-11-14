package transport

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
)

var ports = Ports{ConsumerAPI: 8484, ConsumerPage: 8485, Admin: 8486}

func readyStatus() Status {
	return Status{State: StateReady, Address: netip.MustParseAddr("100.64.0.1"), DNSName: "host.example-tailnet.ts.net"}
}

func TestPlanBindsOnlyTheTailnetAddress(t *testing.T) {
	got := Plan(readyStatus(), ports)
	want := []Spec{{RoleConsumerAPI, "100.64.0.1:8484"}, {RoleConsumerPage, "100.64.0.1:8485"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Plan = %v, want %v", got, want)
	}
	for _, sp := range got {
		if sp.Role == RoleAdmin {
			t.Fatal("the admin listener is planned while its setting is off")
		}
	}
}

func TestPlanAddsTheAdminListenerOnlyWhenEnabled(t *testing.T) {
	p := ports
	p.AdminEnabled = true
	got := Plan(readyStatus(), p)
	if len(got) != 3 || got[2] != (Spec{RoleAdmin, "100.64.0.1:8486"}) {
		t.Fatalf("Plan = %v", got)
	}
	host, _, _ := net.SplitHostPort(got[2].Addr)
	if ip := net.ParseIP(host); ip.IsLoopback() || ip.IsUnspecified() {
		t.Errorf("the admin listener binds %s, which is not the tailnet address", got[2].Addr)
	}
}

func TestPlanIsEmptyUnlessReady(t *testing.T) {
	p := ports
	p.AdminEnabled = true
	for _, st := range []State{StateUnavailable, StateStopped, StateLoggedOut, StateStarting, StateNoHTTPS} {
		s := readyStatus()
		s.State = st
		if got := Plan(s, p); got != nil {
			t.Errorf("state %s: Plan = %v, want none", st, got)
		}
	}
	for _, a := range []string{"", "127.0.0.1", "0.0.0.0", "::"} {
		s := readyStatus()
		s.Address = netip.Addr{}
		if a != "" {
			s.Address = netip.MustParseAddr(a)
		}
		if got := Plan(s, p); got != nil {
			t.Errorf("address %q: Plan = %v, want none", a, got)
		}
	}
}

func TestBindIsAllOrNone(t *testing.T) {
	var opened []net.Listener
	failOn := "100.64.0.1:8485"
	listen := func(_, addr string) (net.Listener, error) {
		if addr == failOn {
			return nil, errors.New("address in use")
		}
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		opened = append(opened, ln)
		return ln, err
	}
	specs := Plan(readyStatus(), ports)
	if _, err := Bind(specs, listen); err == nil || !strings.Contains(err.Error(), "consumer_page") || !strings.Contains(err.Error(), failOn) {
		t.Fatalf("err = %v, want it to name the role and the address", err)
	}
	if len(opened) != 1 {
		t.Fatalf("opened %d", len(opened))
	}
	if _, err := opened[0].Accept(); err == nil {
		t.Error("the first listener was left open after the second failed")
	}
	failOn = ""
	ls, err := Bind(specs, listen)
	if err != nil || len(ls) != 2 || ls[0].Role != RoleConsumerAPI {
		t.Fatalf("Bind = %v, %v", ls, err)
	}
	for _, l := range ls {
		l.Close()
	}
}

func TestBindRefusesWildcardAndLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8484", "127.0.0.1:8484", "[::]:8484", ":8484", "localhost:8484"} {
		called := false
		_, err := Bind([]Spec{{RoleConsumerAPI, addr}}, func(_, _ string) (net.Listener, error) {
			called = true
			return nil, errors.New("must not be called")
		})
		if err == nil || called {
			t.Errorf("%s: err = %v, listen called = %v", addr, err, called)
		}
	}
}
