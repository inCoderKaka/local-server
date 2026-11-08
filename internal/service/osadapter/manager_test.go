package osadapter

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	ksvc "github.com/kardianos/service"

	"github.com/inCoderKaka/local-server/internal/service"
)

// fakeRunner records the OS tools it was asked to run and runs none of them. answers maps the
// whole command line to a Result; anything else succeeds with no output.
type fakeRunner struct {
	calls   []string
	answers map[string]Result
	missing map[string]bool
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) (Result, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, line)
	if r.missing[name] {
		return Result{}, ErrToolMissing
	}
	return r.answers[line], nil
}

// take returns the calls so far and forgets them.
func (r *fakeRunner) take() []string {
	c := r.calls
	r.calls = nil
	return c
}

// memFS is an FS in memory.
type memFS struct {
	files map[string][]byte
	// denied makes every write fail as if the caller lacked the rights.
	denied bool
	// written lists every path ever written, in order.
	written []string
	// writtenData is the last bytes written to each path, kept after Remove.
	writtenData map[string][]byte
}

func newMemFS() *memFS {
	return &memFS{files: map[string][]byte{}, writtenData: map[string][]byte{}}
}

func (m *memFS) ReadFile(p string) ([]byte, error) {
	b, ok := m.files[p]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
	}
	return b, nil
}

func (m *memFS) WriteFile(p string, data []byte, _ fs.FileMode) error {
	if m.denied {
		return &fs.PathError{Op: "open", Path: p, Err: fs.ErrPermission}
	}
	m.writtenData[p] = data
	m.files[p] = data
	m.written = append(m.written, p)
	return nil
}

func (m *memFS) Remove(p string) error {
	if _, ok := m.files[p]; !ok {
		return &fs.PathError{Op: "remove", Path: p, Err: fs.ErrNotExist}
	}
	delete(m.files, p)
	return nil
}

// fakeWinSvc is a kardianos Windows service that records what it was asked.
type fakeWinSvc struct {
	cfg       *ksvc.Config
	installed bool
	running   bool
	calls     []string
	failWith  error
}

func (f *fakeWinSvc) Install() error {
	f.calls = append(f.calls, "install")
	if f.failWith != nil {
		return f.failWith
	}
	f.installed = true
	return nil
}
func (f *fakeWinSvc) Uninstall() error {
	f.calls = append(f.calls, "uninstall")
	f.installed = false
	return nil
}
func (f *fakeWinSvc) Start() error { f.calls = append(f.calls, "start"); f.running = true; return nil }
func (f *fakeWinSvc) Stop() error  { f.calls = append(f.calls, "stop"); f.running = false; return nil }
func (f *fakeWinSvc) Status() (ksvc.Status, error) {
	f.calls = append(f.calls, "status")
	switch {
	case !f.installed:
		return ksvc.StatusUnknown, ksvc.ErrNotInstalled
	case f.running:
		return ksvc.StatusRunning, nil
	}
	return ksvc.StatusStopped, nil
}

type rig struct {
	run *fakeRunner
	fs  *memFS
	win *fakeWinSvc
	m   service.Manager
	// spec is what the Manager was built from.
	spec service.Spec
}

// newRig builds a Manager over fakes. No test may reach a real service manager.
func newRig(t *testing.T, target service.Target, mode service.Mode, tweak ...func(*service.Spec)) *rig {
	t.Helper()
	s := service.Spec{Target: target, Mode: mode, Name: service.DefaultName(target),
		RestartDelay: service.DefaultRestartDelay, RestartCount: service.DefaultRestartCount, RestartResetPeriod: service.DefaultRestartResetPeriod,
		UserName: "ana", Linger: true}
	switch target {
	case service.TargetDarwin:
		s.Binary, s.DataDir = "/usr/local/bin/local-server", "/Users/ana/Library/Application Support/local-server"
	case service.TargetLinux:
		s.Binary, s.DataDir = "/home/ana/.local/bin/local-server", "/home/ana/.config/local-server"
	case service.TargetWindows:
		s.Binary, s.DataDir, s.UserName = `C:\Program Files\local-server\local-server.exe`, `C:\ProgramData\local-server`, `PC\ana`
	}
	for _, f := range tweak {
		f(&s)
	}
	r := &rig{run: &fakeRunner{answers: map[string]Result{}}, fs: newMemFS(), win: &fakeWinSvc{}}
	env := Env{Runner: r.run, FS: r.fs, ConfigHome: "/home/ana/.config", Home: "/Users/ana", UID: 501, TempDir: `C:\Temp`,
		NewWindowsService: func(c *ksvc.Config) (WindowsService, error) { r.win.cfg = c; return r.win, nil }}
	m, err := New(s, env)
	if err != nil {
		t.Fatal(err)
	}
	r.m, r.spec = m, s
	return r
}

func wantCalls(t *testing.T, r *rig, want ...string) {
	t.Helper()
	got := r.run.take()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tool calls:\n got %q\nwant %q", got, want)
	}
}

func noErr[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

const (
	agentPlist  = "/Users/ana/Library/LaunchAgents/com.github.incoderkaka.local-server.plist"
	daemonPlist = "/Library/LaunchDaemons/com.github.incoderkaka.local-server.plist"
	label       = "com.github.incoderkaka.local-server"
)

func TestDarwinUserLifecycle(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, service.TargetDarwin, service.ModeUser)
	// Not installed yet.
	r.run.answers["launchctl print gui/501/"+label] = Result{Code: 113, Output: "Could not find service"}
	if st := noErr(r.m.Status(ctx)); st.Installed || st.Running {
		t.Errorf("status before install: %+v", st)
	}
	wantCalls(t, r, "launchctl print gui/501/"+label)
	if _, err := r.m.Start(ctx); !errors.Is(err, service.ErrNotInstalled) {
		t.Errorf("start before install: %v", err)
	}
	r.run.take()

	// Install writes the plist and runs no tool.
	out := noErr(r.m.Install(ctx))
	if !out.Changed {
		t.Error("first install should change something")
	}
	want, _ := service.Render(r.spec, service.KindService)
	if string(r.fs.files[agentPlist]) != want {
		t.Errorf("plist not written to %s", agentPlist)
	}
	wantCalls(t, r)
	// Install twice: unchanged, and says so.
	out = noErr(r.m.Install(ctx))
	if out.Changed || len(out.Notes) != 1 || !strings.Contains(out.Notes[0], "unchanged") {
		t.Errorf("second install: %+v", out)
	}

	// Start loads the job.
	if !noErr(r.m.Start(ctx)).Changed {
		t.Error("start should change something")
	}
	wantCalls(t, r, "launchctl print gui/501/"+label, "launchctl bootstrap gui/501 "+agentPlist)

	// Running: status says so, start is a no-op, stop boots it out.
	r.run.answers["launchctl print gui/501/"+label] = Result{Output: "gui/501/x = {\n\tstate = running\n\tpid = 12\n}"}
	if st := noErr(r.m.Status(ctx)); !st.Installed || !st.Running || st.Mode != service.ModeUser {
		t.Errorf("status: %+v", st)
	}
	r.run.take()
	if o := noErr(r.m.Start(ctx)); o.Changed || o.Notes[0] != "already running" {
		t.Errorf("start when running: %+v", o)
	}
	r.run.take()
	noErr(r.m.Stop(ctx))
	wantCalls(t, r, "launchctl print gui/501/"+label, "launchctl bootout gui/501/"+label)

	// Loaded but waiting to restart: start kicks it, stop boots it out.
	r.run.answers["launchctl print gui/501/"+label] = Result{Output: "state = waiting"}
	noErr(r.m.Start(ctx))
	wantCalls(t, r, "launchctl print gui/501/"+label, "launchctl kickstart gui/501/"+label)

	// Uninstall boots out a loaded job and removes the file; twice is fine.
	noErr(r.m.Uninstall(ctx))
	wantCalls(t, r, "launchctl print gui/501/"+label, "launchctl bootout gui/501/"+label)
	if _, ok := r.fs.files[agentPlist]; ok {
		t.Error("plist still there")
	}
	// The fake answers code 0 by default, which reads as "loaded": say the job is gone.
	r.run.answers["launchctl print gui/501/"+label] = Result{Code: 113, Output: "Could not find service"}
	out = noErr(r.m.Uninstall(ctx))
	if out.Changed || !strings.Contains(out.Notes[0], "not installed") {
		t.Errorf("uninstall when not installed: %+v", out)
	}
	if o := noErr(r.m.Stop(ctx)); o.Changed {
		t.Errorf("stop when not installed: %+v", o)
	}
}

// A job still loaded after its plist was removed by hand is not installed, but it is running and Stop
// must boot it out.
func TestDarwinOrphanedJobCanBeStopped(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, service.TargetDarwin, service.ModeUser)
	r.run.answers["launchctl print gui/501/"+label] = Result{Output: "state = running"}
	if st := noErr(r.m.Status(ctx)); st.Installed || !st.Running {
		t.Errorf("status of an orphaned job: %+v", st)
	}
	r.run.take()
	if o := noErr(r.m.Stop(ctx)); !o.Changed {
		t.Errorf("stop of an orphaned job: %+v", o)
	}
	wantCalls(t, r, "launchctl print gui/501/"+label, "launchctl bootout gui/501/"+label)
}

func TestDarwinSystemUsesDaemonAndNeedsRights(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, service.TargetDarwin, service.ModeSystem, func(s *service.Spec) { s.RunAs = "ana" })
	r.fs.denied = true
	if _, err := r.m.Install(ctx); !errors.Is(err, service.ErrNeedsAdmin) {
		t.Fatalf("install without rights: %v", err)
	}
	r.fs.denied = false
	noErr(r.m.Install(ctx))
	if _, ok := r.fs.files[daemonPlist]; !ok {
		t.Error("LaunchDaemon plist not written")
	}
	r.run.answers["launchctl print system/"+label] = Result{Code: 113}
	noErr(r.m.Start(ctx))
	wantCalls(t, r, "launchctl print system/"+label, "launchctl bootstrap system "+daemonPlist)
	// launchctl refusing for lack of rights.
	r.run.answers["launchctl bootstrap system "+daemonPlist] = Result{Code: 1, Output: "Operation not permitted"}
	if _, err := r.m.Start(ctx); !errors.Is(err, service.ErrNeedsAdmin) {
		t.Errorf("bootstrap without rights: %v", err)
	}
}

const unitUser = "/home/ana/.config/systemd/user/local-server.service"

func TestLinuxUserLifecycle(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, service.TargetLinux, service.ModeUser)
	r.run.answers["systemctl --user is-active local-server.service"] = Result{Code: 3, Output: "inactive"}

	noErr(r.m.Install(ctx))
	wantCalls(t, r, "loginctl enable-linger ana", "systemctl --user daemon-reload", "systemctl --user enable local-server.service")
	if !strings.Contains(string(r.fs.files[unitUser]), "WantedBy=default.target") {
		t.Errorf("user unit not written to %s", unitUser)
	}
	// Install twice: same calls, unchanged definition.
	out := noErr(r.m.Install(ctx))
	if out.Changed || !strings.Contains(out.Notes[0], "unchanged") {
		t.Errorf("second install: %+v", out)
	}
	r.run.take()

	noErr(r.m.Start(ctx))
	wantCalls(t, r, "systemctl --user is-active local-server.service", "systemctl --user start local-server.service")
	r.run.answers["systemctl --user is-active local-server.service"] = Result{Output: "active"}
	if st := noErr(r.m.Status(ctx)); !st.Running || !st.Installed {
		t.Errorf("status: %+v", st)
	}
	r.run.take()
	noErr(r.m.Stop(ctx))
	wantCalls(t, r, "systemctl --user is-active local-server.service", "systemctl --user stop local-server.service")

	out = noErr(r.m.Uninstall(ctx))
	wantCalls(t, r, "systemctl --user disable --now local-server.service", "systemctl --user daemon-reload")
	if !out.Changed {
		t.Error("uninstall should change something")
	}
	r.run.answers["systemctl --user is-active local-server.service"] = Result{Code: 3, Output: "inactive"}
	out = noErr(r.m.Uninstall(ctx))
	if out.Changed || !strings.Contains(out.Notes[0], "not installed") {
		t.Errorf("uninstall twice: %+v", out)
	}
	wantCalls(t, r, "systemctl --user is-active local-server.service")
}

// A job still loaded after its plist was removed by hand is not installed, but Uninstall must boot it
// out; otherwise it keeps running with nothing left to stop it by.
func TestDarwinUninstallStopsOrphanedJob(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, service.TargetDarwin, service.ModeUser)
	r.run.answers["launchctl print gui/501/"+label] = Result{Output: "state = running"}
	out := noErr(r.m.Uninstall(ctx))
	if !out.Changed || len(out.Notes) != 1 || !strings.Contains(out.Notes[0], "already removed") {
		t.Errorf("uninstall of an orphaned job: %+v", out)
	}
	wantCalls(t, r, "launchctl print gui/501/"+label, "launchctl bootout gui/501/"+label)
}

// The same on Linux: the unit file is gone but the service is active. `disable` would fail without the
// file, so Uninstall only stops it. With nothing running there is nothing to remove.
func TestLinuxUninstallStopsOrphanedService(t *testing.T) {
	ctx := context.Background()
	const isActive = "systemctl --user is-active local-server.service"
	r := newRig(t, service.TargetLinux, service.ModeUser)
	r.run.answers[isActive] = Result{Output: "active"}
	out := noErr(r.m.Uninstall(ctx))
	if !out.Changed || len(out.Notes) != 1 || !strings.Contains(out.Notes[0], "already removed") {
		t.Errorf("uninstall of an orphaned service: %+v", out)
	}
	wantCalls(t, r, isActive, "systemctl --user stop local-server.service")

	r.run.answers[isActive] = Result{Code: 3, Output: "inactive"}
	out = noErr(r.m.Uninstall(ctx))
	if out.Changed || len(out.Notes) != 1 || !strings.Contains(out.Notes[0], "not installed, nothing to remove") {
		t.Errorf("uninstall when nothing is left: %+v", out)
	}
	wantCalls(t, r, isActive)
}

// While systemd waits to restart a crashed unit, is-active says "activating" (exit 3): the unit is
// not running, but Stop must still cancel the pending restart.
func TestLinuxStopCancelsPendingRestart(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, service.TargetLinux, service.ModeUser)
	noErr(r.m.Install(ctx))
	r.run.take()
	r.run.answers["systemctl --user is-active local-server.service"] = Result{Code: 3, Output: "activating"}
	if st := noErr(r.m.Status(ctx)); !st.Installed || st.Running {
		t.Errorf("status while waiting to restart: %+v", st)
	}
	r.run.take()
	if o := noErr(r.m.Stop(ctx)); !o.Changed {
		t.Errorf("stop while waiting to restart: %+v", o)
	}
	wantCalls(t, r, "systemctl --user is-active local-server.service", "systemctl --user stop local-server.service")
}

// is-active exits 0 or 3 for a state it knows. Any other exit means systemctl could not ask the
// manager, so the state is unknown: Status and Stop must not report "stopped" or "not running".
func TestLinuxUnknownStateIsAnError(t *testing.T) {
	ctx := context.Background()
	const isActive = "systemctl --user is-active local-server.service"
	answer := Result{Code: 1, Output: "Failed to connect to bus: No medium found"}
	r := newRig(t, service.TargetLinux, service.ModeUser)
	noErr(r.m.Install(ctx))
	r.run.take()
	r.run.answers[isActive] = answer
	if _, err := r.m.Status(ctx); err == nil || !strings.Contains(err.Error(), "Failed to connect to bus") {
		t.Errorf("status with an unreachable manager: %v", err)
	}
	if o, err := r.m.Stop(ctx); err == nil {
		t.Errorf("stop with an unreachable manager must fail, got %+v", o)
	}
	// Without a unit file the answer does not matter: the service is not installed.
	r = newRig(t, service.TargetLinux, service.ModeUser)
	r.run.answers[isActive] = answer
	st, err := r.m.Status(ctx)
	if err != nil || st.Installed {
		t.Errorf("status when not installed: %+v, %v", st, err)
	}
}

func TestLinuxLingerFailureIsOnlyANote(t *testing.T) {
	r := newRig(t, service.TargetLinux, service.ModeUser)
	r.run.answers["loginctl enable-linger ana"] = Result{Code: 1, Output: "Access denied"}
	out, err := r.m.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Notes) != 1 || !strings.Contains(out.Notes[0], "only while you are logged in") {
		t.Errorf("notes: %q", out.Notes)
	}
	// And --linger=false makes no loginctl call.
	r = newRig(t, service.TargetLinux, service.ModeUser, func(s *service.Spec) { s.Linger = false })
	noErr(r.m.Install(context.Background()))
	wantCalls(t, r, "systemctl --user daemon-reload", "systemctl --user enable local-server.service")
}

func TestLinuxSystem(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, service.TargetLinux, service.ModeSystem)
	noErr(r.m.Install(ctx))
	wantCalls(t, r, "systemctl daemon-reload", "systemctl enable local-server.service")
	if !strings.Contains(string(r.fs.files["/etc/systemd/system/local-server.service"]), "WantedBy=multi-user.target") {
		t.Error("system unit not written")
	}
	r.run.answers["systemctl is-active local-server.service"] = Result{Output: "active"}
	noErr(r.m.Stop(ctx))
	wantCalls(t, r, "systemctl is-active local-server.service", "systemctl stop local-server.service")
	// polkit refusing.
	r.run.answers["systemctl stop local-server.service"] = Result{Code: 1, Output: "Interactive authentication required."}
	if _, err := r.m.Stop(ctx); !errors.Is(err, service.ErrNeedsAdmin) {
		t.Errorf("stop without rights: %v", err)
	}
}

// systemctl prints "Access denied" (no "is") when polkit refuses a system-mode change.
func TestLinuxSystemAccessDeniedNeedsAdmin(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, service.TargetLinux, service.ModeSystem)
	noErr(r.m.Install(ctx))
	r.run.take()
	r.run.answers["systemctl disable --now local-server.service"] = Result{Code: 1, Output: "Failed to disable unit: Access denied"}
	if _, err := r.m.Uninstall(ctx); !errors.Is(err, service.ErrNeedsAdmin) {
		t.Errorf("uninstall without rights: %v", err)
	}
}

func TestToolMissingIsClear(t *testing.T) {
	r := newRig(t, service.TargetLinux, service.ModeUser)
	r.run.missing = map[string]bool{"systemctl": true}
	_, err := r.m.Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cannot find systemctl") {
		t.Errorf("err = %v", err)
	}
}

func TestWindowsLogonTaskLifecycle(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, service.TargetWindows, service.ModeUser)
	const query = `schtasks /Query /TN local-server`
	const ps = "powershell -NoProfile -NonInteractive -Command (Get-ScheduledTask -TaskName 'local-server').State"
	r.run.answers[query] = Result{Code: 1, Output: "ERROR: The system cannot find the file specified."}

	if st := noErr(r.m.Status(ctx)); st.Installed {
		t.Errorf("status before install: %+v", st)
	}
	wantCalls(t, r, query)
	if o := noErr(r.m.Uninstall(ctx)); o.Changed || !strings.Contains(o.Notes[0], "not installed") {
		t.Errorf("uninstall when not installed: %+v", o)
	}
	r.run.take()

	// Install: schtasks reads UTF-16 XML from a temporary file, which is gone afterwards.
	r.run.answers[`schtasks /Create /TN local-server /XML C:\Temp\local-server.xml /F`] = Result{}
	noErr(r.m.Install(ctx))
	wantCalls(t, r, query, `schtasks /Create /TN local-server /XML C:\Temp\local-server.xml /F`)
	if len(r.fs.written) != 1 || r.fs.written[0] != `C:\Temp\local-server.xml` {
		t.Fatalf("written: %q", r.fs.written)
	}
	// What schtasks reads is the rendered definition as UTF-16LE with a byte order mark.
	b := r.fs.writtenData[`C:\Temp\local-server.xml`]
	if len(b) < 2 || len(b)%2 != 0 || b[0] != 0xFF || b[1] != 0xFE {
		t.Fatalf("task file is not UTF-16LE with a byte order mark: % x", b)
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 2; i < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	if want := noErr(service.Render(r.spec, service.KindService)); string(utf16.Decode(u)) != want {
		t.Errorf("task file holds a different definition:\n got %q\nwant %q", string(utf16.Decode(u)), want)
	}
	if len(r.fs.files) != 0 {
		t.Error("temporary task file was not removed")
	}

	// Installed and ready, then running.
	r.run.answers[query] = Result{}
	r.run.answers[ps] = Result{Output: "Ready\r\n"}
	if st := noErr(r.m.Status(ctx)); !st.Installed || st.Running {
		t.Errorf("status ready: %+v", st)
	}
	wantCalls(t, r, query, ps)
	noErr(r.m.Start(ctx))
	wantCalls(t, r, query, ps, `schtasks /Run /TN local-server`)
	r.run.answers[ps] = Result{Output: "Running\r\n"}
	noErr(r.m.Stop(ctx))
	wantCalls(t, r, query, ps, `schtasks /End /TN local-server`)
	// Install again: says the definition was replaced.
	if o := noErr(r.m.Install(ctx)); len(o.Notes) != 1 || !strings.Contains(o.Notes[0], "replaced") {
		t.Errorf("second install: %+v", o)
	}
	r.run.take()
	noErr(r.m.Uninstall(ctx))
	wantCalls(t, r, query, `schtasks /End /TN local-server`, `schtasks /Delete /TN local-server /F`)
}

// When the task exists but PowerShell cannot say what state it is in, the state is unknown, not
// stopped: Status and Stop must fail instead of reporting "stopped" or "not running".
func TestWindowsLogonTaskUnknownStateIsAnError(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, service.TargetWindows, service.ModeUser)
	r.run.answers[`schtasks /Query /TN local-server`] = Result{}
	r.run.answers["powershell -NoProfile -NonInteractive -Command (Get-ScheduledTask -TaskName 'local-server').State"] =
		Result{Code: 1, Output: "Get-ScheduledTask : access error"}
	if _, err := r.m.Status(ctx); err == nil || !strings.Contains(err.Error(), "access error") {
		t.Errorf("status with a failing PowerShell: %v", err)
	}
	if o, err := r.m.Stop(ctx); err == nil {
		t.Errorf("stop with a failing PowerShell must fail, got %+v", o)
	}
}

func TestUTF16LE(t *testing.T) {
	b := utf16LE("a\u00e9<")
	if b[0] != 0xFF || b[1] != 0xFE {
		t.Fatal("no byte order mark")
	}
	u := make([]uint16, 0, 3)
	for i := 2; i < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	if string(utf16.Decode(u)) != "a\u00e9<" {
		t.Error("round trip failed")
	}
}

func TestWindowsSystemService(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, service.TargetWindows, service.ModeSystem, func(s *service.Spec) { s.RestartDelay = 30 * time.Second })
	if st := noErr(r.m.Status(ctx)); st.Installed || st.Mode != service.ModeSystem {
		t.Errorf("status before install: %+v", st)
	}
	if _, err := r.m.Start(ctx); !errors.Is(err, service.ErrNotInstalled) {
		t.Errorf("start before install: %v", err)
	}

	noErr(r.m.Install(ctx))
	c := r.win.cfg
	if c.Name != "local-server" || c.Executable != `C:\Program Files\local-server\local-server.exe` ||
		!reflect.DeepEqual(c.Arguments, []string{"serve", "--data-dir", `C:\ProgramData\local-server`}) {
		t.Errorf("config: %+v", c)
	}
	// Restart on unexpected exit, after the restart delay, counting failures over the reset period.
	for k, want := range map[string]any{"StartType": "automatic", "OnFailure": "restart", "OnFailureDelayDuration": "30s", "OnFailureResetPeriod": 86400} {
		if c.Option[k] != want {
			t.Errorf("option %s = %v, want %v", k, c.Option[k], want)
		}
	}
	// Install twice: nothing happens the second time.
	r.win.calls = nil
	if o := noErr(r.m.Install(ctx)); o.Changed || !strings.Contains(o.Notes[0], "already installed") {
		t.Errorf("second install: %+v", o)
	}
	if contains(r.win.calls, "install") {
		t.Error("installed twice")
	}

	noErr(r.m.Start(ctx))
	if !r.win.running {
		t.Error("not started")
	}
	if st := noErr(r.m.Status(ctx)); !st.Running {
		t.Errorf("status: %+v", st)
	}
	noErr(r.m.Uninstall(ctx))
	if r.win.installed || r.win.running {
		t.Error("uninstall should stop and remove")
	}
	if o := noErr(r.m.Uninstall(ctx)); o.Changed {
		t.Errorf("uninstall twice: %+v", o)
	}
	// No OS tool is run for a kardianos service.
	wantCalls(t, r)

	r.win.failWith = errors.New("Access is denied.")
	if _, err := r.m.Install(ctx); !errors.Is(err, service.ErrNeedsAdmin) {
		t.Errorf("install without rights: %v", err)
	}
}

// kardianos reads a service password only from Option["Password"], which the adapter never sets, so
// a Windows system service runs as an account that needs no password.
func TestKardianosConfigRunAsHasNoPassword(t *testing.T) {
	cfg := kardianosConfig(service.WindowsServiceDef{Name: "local-server", RunAs: `NT AUTHORITY\LocalService`})
	if cfg.UserName != `NT AUTHORITY\LocalService` {
		t.Errorf("UserName = %q", cfg.UserName)
	}
	if _, ok := cfg.Option["Password"]; ok {
		t.Error("the adapter must not set a Password option")
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestInstallRejectsBadSpec(t *testing.T) {
	r := newRig(t, service.TargetLinux, service.ModeUser, func(s *service.Spec) { s.Binary = "local-server" })
	if _, err := r.m.Install(context.Background()); err == nil {
		t.Error("a relative binary path must be refused")
	}
	wantCalls(t, r)
	if len(r.fs.files) != 0 {
		t.Error("nothing may be written for a bad Spec")
	}
}
