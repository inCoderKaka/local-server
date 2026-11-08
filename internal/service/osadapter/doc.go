// Package osadapter is the adapter of the service port (internal/service.Manager): everything
// OS-dependent about running local-server as a background service lives here, and nothing
// OS-agnostic does (req_001 D-137, D-30, D-29, PLT-F-041; docs/modules/service.md names the split).
// It installs, removes, starts, stops and asks about the service on macOS, Linux and Windows, in
// user mode or system mode.
//
// github.com/kardianos/service registers the Windows system service only (its Service Control
// Manager code, with the recovery action "restart"). It cannot express the rest: its macOS
// template has a plain KeepAlive true (restarts after a clean exit too), its systemd template
// always wants multi-user.target (a user manager has none), it has no Windows logon task, and it
// runs the OS tools without a seam a test could replace. So the launchd, systemd and Windows
// logon-task definitions are rendered by internal/service and installed here by running
// launchctl, systemctl, loginctl and schtasks through the Runner and writing files through the
// FS, both injected, so every OS family is tested on any host with fakes. Only this package may
// import kardianos/service (tools/archcheck).
//
// No file carries a build constraint: the backends run against the injected Runner and FS and
// choose the OS family by the Spec's Target, and the real Runner, FS and user discovery
// (real.go) use only calls that compile on every OS.
package osadapter
