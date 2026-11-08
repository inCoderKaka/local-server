package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/service"
)

// fakeManager records what the command asked of it. Nothing here reaches a real service manager.
type fakeManager struct {
	spec    service.Spec
	calls   []string
	outcome service.Outcome
	status  service.Status
	err     error
}

func (f *fakeManager) rec(c string) { f.calls = append(f.calls, c) }
func (f *fakeManager) Install(context.Context) (service.Outcome, error) {
	f.rec("install")
	return f.outcome, f.err
}
func (f *fakeManager) Uninstall(context.Context) (service.Outcome, error) {
	f.rec("uninstall")
	return f.outcome, f.err
}
func (f *fakeManager) Start(context.Context) (service.Outcome, error) {
	f.rec("start")
	return f.outcome, f.err
}
func (f *fakeManager) Stop(context.Context) (service.Outcome, error) {
	f.rec("stop")
	return f.outcome, f.err
}
func (f *fakeManager) Status(context.Context) (service.Status, error) {
	f.rec("status")
	st := f.status
	st.Mode = f.spec.Mode
	return st, f.err
}

// serviceRig runs service commands against fake managers, one per mode.
type serviceRig struct {
	out, errb bytes.Buffer
	mgrs      map[service.Mode]*fakeManager
	app       *app
}

func newServiceRig(target service.Target) *serviceRig {
	r := &serviceRig{mgrs: map[service.Mode]*fakeManager{
		service.ModeUser: {}, service.ModeSystem: {}}}
	r.app = &app{stdout: &r.out, stderr: &r.errb, getenv: func(string) string { return "" },
		userConfigDir: func() (string, error) { return "/home/ana/.config", nil },
		svc: serviceHooks{
			executable: func() (string, error) { return "/opt/local-server/local-server", nil },
			target:     target,
			userName:   func() (string, error) { return "ana", nil },
			newManager: func(s service.Spec) (service.Manager, error) {
				m := r.mgrs[s.Mode]
				m.spec = s
				return m, nil
			},
		}}
	return r
}

func (r *serviceRig) run(args ...string) int {
	r.out.Reset()
	r.errb.Reset()
	return r.app.run(context.Background(), append([]string{"service"}, args...))
}

func TestServiceInstallDefaultsToUserMode(t *testing.T) {
	r := newServiceRig(service.TargetLinux)
	r.mgrs[service.ModeUser].outcome = service.Outcome{Changed: true}
	if code := r.run("install"); code != 0 {
		t.Fatalf("exit %d: %s", code, r.errb.String())
	}
	m := r.mgrs[service.ModeUser]
	s := m.spec
	if s.Mode != service.ModeUser || s.Name != "local-server" || s.Binary != "/opt/local-server/local-server" ||
		s.DataDir != "/home/ana/.config/local-server" || s.RestartDelay != service.DefaultRestartDelay ||
		s.RestartCount != service.DefaultRestartCount || !s.Linger || s.UserName != "ana" {
		t.Errorf("spec: %+v", s)
	}
	if strings.Join(m.calls, ",") != "install" {
		t.Errorf("calls %v", m.calls)
	}
	if !strings.Contains(r.out.String(), "installed (user mode)") || !strings.Contains(r.out.String(), "local-server service start") {
		t.Errorf("output: %q", r.out.String())
	}
	if len(r.mgrs[service.ModeSystem].calls) != 0 {
		t.Error("system mode must not be touched")
	}
}

func TestServiceInstallFlags(t *testing.T) {
	r := newServiceRig(service.TargetDarwin)
	r.mgrs[service.ModeSystem].outcome = service.Outcome{Changed: true}
	code := r.run("install", "--mode", "system", "--name", "my.label", "--data-dir", "/var/ls", "--run-as", "ana",
		"--restart-delay", "30s", "--restart-count", "5", "--restart-reset-period", "2h", "--linger=false")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, r.errb.String())
	}
	s := r.mgrs[service.ModeSystem].spec
	if s.Mode != service.ModeSystem || s.Name != "my.label" || s.DataDir != "/var/ls" || s.RunAs != "ana" ||
		s.RestartDelay != 30*time.Second || s.RestartCount != 5 || s.RestartResetPeriod != 2*time.Hour || s.Linger {
		t.Errorf("spec: %+v", s)
	}
	if !strings.Contains(r.out.String(), "--mode system") {
		t.Errorf("the next step must name the mode: %q", r.out.String())
	}
	// macOS default name is a label.
	r = newServiceRig(service.TargetDarwin)
	r.run("install")
	if r.mgrs[service.ModeUser].spec.Name != "com.github.incoderkaka.local-server" {
		t.Errorf("name %q", r.mgrs[service.ModeUser].spec.Name)
	}
}

func TestServiceInstallSystemNeedsDataDir(t *testing.T) {
	r := newServiceRig(service.TargetLinux)
	if code := r.run("install", "--mode", "system"); code != 2 || !strings.Contains(r.errb.String(), "--data-dir") {
		t.Errorf("exit %d: %s", code, r.errb.String())
	}
	// LS_DATA_DIR counts as given.
	r.app.getenv = func(k string) string {
		if k == "LS_DATA_DIR" {
			return "/srv/ls"
		}
		return ""
	}
	if code := r.run("install", "--mode", "system"); code != 0 || r.mgrs[service.ModeSystem].spec.DataDir != "/srv/ls" {
		t.Errorf("exit %d: %s", code, r.errb.String())
	}
}

func TestServiceCommandLineErrorsExit2(t *testing.T) {
	r := newServiceRig(service.TargetLinux)
	for name, args := range map[string][]string{
		"no action":            {},
		"unknown action":       {"restart"},
		"bad mode":             {"install", "--mode", "both"},
		"unknown flag":         {"install", "--nope"},
		"install flag on stop": {"stop", "--data-dir", "/x"},
		"extra argument":       {"start", "now"},
		"restart delay zero":   {"install", "--restart-delay", "0s"},
		"restart count 0":      {"install", "--restart-count", "0"},
		"run-as in user mode":  {"install", "--run-as", "root"},
		"bad name":             {"install", "--name", "a b"},
	} {
		if code := r.run(args...); code != 2 {
			t.Errorf("%s: exit %d, want 2 (stderr %q)", name, code, r.errb.String())
		}
	}
	if code := r.app.run(context.Background(), []string{"service", "install", "-h"}); code != 0 {
		t.Errorf("-h: exit %d", code)
	}
}

func TestServiceActions(t *testing.T) {
	for _, action := range []string{"uninstall", "start", "stop"} {
		r := newServiceRig(service.TargetLinux)
		r.mgrs[service.ModeUser].outcome = service.Outcome{Changed: true}
		if code := r.run(action); code != 0 {
			t.Fatalf("%s: exit %d: %s", action, code, r.errb.String())
		}
		if got := strings.Join(r.mgrs[service.ModeUser].calls, ","); got != action {
			t.Errorf("%s: calls %q", action, got)
		}
		if len(r.mgrs[service.ModeSystem].calls) != 0 {
			t.Errorf("%s: touched system mode by default", action)
		}
		r = newServiceRig(service.TargetLinux)
		r.run(action, "--mode", "system")
		if len(r.mgrs[service.ModeUser].calls) != 0 || len(r.mgrs[service.ModeSystem].calls) != 1 {
			t.Errorf("%s --mode system: wrong manager", action)
		}
	}
}

func TestServiceNothingToDoIsNotAnError(t *testing.T) {
	r := newServiceRig(service.TargetLinux)
	r.mgrs[service.ModeUser].outcome = service.Outcome{Notes: []string{"not installed, nothing to remove"}}
	if code := r.run("uninstall"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(r.out.String(), "not installed, nothing to remove") {
		t.Errorf("output: %q", r.out.String())
	}
	r.mgrs[service.ModeUser].outcome = service.Outcome{}
	if code := r.run("stop"); code != 0 || !strings.Contains(r.out.String(), "nothing to do") {
		t.Errorf("exit %d output %q", code, r.out.String())
	}
}

func TestServiceFailuresExit1WithoutStackTrace(t *testing.T) {
	r := newServiceRig(service.TargetLinux)
	r.mgrs[service.ModeUser].err = service.ErrNotInstalled
	if code := r.run("start"); code != 1 || !strings.Contains(r.errb.String(), "service install") {
		t.Errorf("exit %d: %q", code, r.errb.String())
	}
	r.mgrs[service.ModeSystem].err = service.ErrNeedsAdmin
	if code := r.run("install", "--mode", "system", "--data-dir", "/srv/ls"); code != 1 || !strings.Contains(r.errb.String(), "sudo") {
		t.Errorf("exit %d: %q", code, r.errb.String())
	}
	r.mgrs[service.ModeUser].err = errors.New("systemctl enable failed (exit code 1): boom")
	if code := r.run("install"); code != 1 || strings.Contains(r.errb.String(), "goroutine") {
		t.Errorf("exit %d: %q", code, r.errb.String())
	}
	r.app.svc.executable = func() (string, error) { return "", errors.New("no path") }
	if code := r.run("install"); code != 1 {
		t.Errorf("exit %d", code)
	}
}

func TestServiceStatus(t *testing.T) {
	r := newServiceRig(service.TargetLinux)
	// Nothing installed.
	if code := r.run("status"); code != 0 || !strings.Contains(r.out.String(), "not installed") {
		t.Errorf("exit %d: %q", code, r.out.String())
	}
	// Installed and running in system mode only: one line, saying which mode.
	r.mgrs[service.ModeSystem].status = service.Status{Installed: true, Running: true}
	if code := r.run("status"); code != 0 || r.out.String() != "local-server background service, system mode: installed, running\n" {
		t.Errorf("exit %d: %q", code, r.out.String())
	}
	// Asking for one mode names it even when not installed.
	if code := r.run("status", "--mode", "user"); code != 0 || !strings.Contains(r.out.String(), "user mode: not installed") {
		t.Errorf("exit %d: %q", code, r.out.String())
	}
	r.mgrs[service.ModeUser].status = service.Status{Installed: true}
	if code := r.run("status", "--mode", "user"); code != 0 || !strings.Contains(r.out.String(), "user mode: installed, stopped") {
		t.Errorf("exit %d: %q", code, r.out.String())
	}
	r.mgrs[service.ModeUser].err = errors.New("cannot find systemctl")
	if code := r.run("status"); code != 1 {
		t.Errorf("a failed check must exit 1, got %d", code)
	}
}

func TestServiceUnsupportedOS(t *testing.T) {
	r := newServiceRig("plan9")
	if code := r.run("status"); code != 1 || !strings.Contains(r.errb.String(), "not supported on plan9") {
		t.Errorf("exit %d: %q", code, r.errb.String())
	}
}
