// Package kardianos is the adapter of the service port (internal/service.Manager): it installs,
// removes, starts, stops and asks about local-server as a background service on macOS, Linux and
// Windows, in user mode or system mode (req_001 D-30, D-29, PLT-F-041).
//
// github.com/kardianos/service registers the Windows system service (its Service Control Manager
// code, with the recovery action "restart"). It cannot express the rest: its macOS template has a
// plain KeepAlive true (restarts after a clean exit too), its systemd template always wants
// multi-user.target (a user manager has none), it has no Windows logon task, and it runs the OS
// tools without a seam a test could replace. Those definitions are rendered by internal/service
// and installed here by running launchctl, systemctl, loginctl and schtasks through the Runner
// and writing files through the FS, both injected, so every OS family is tested on any host with
// fakes. Only this package may import kardianos/service (tools/archcheck).
package kardianos
