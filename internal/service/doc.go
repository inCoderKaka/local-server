// Package service is part of the service module; see docs/modules/service.md. It is the port to
// the OS service manager and the pure definitions behind it (req_001 D-30, D-29, PLT-F-041): the
// Manager interface, the Spec of one background service, and the functions that render the
// LaunchAgent or LaunchDaemon plist, the systemd unit, the Windows logon task and the Windows
// service definition, plus the definition of the user-session helper that system mode comes
// with. Nothing here starts a process or touches the OS, so the golden-file tests run the same on
// every OS. The adapter that installs the definitions, and holds everything OS-dependent, is
// internal/service/osadapter (req_001 D-137).
package service
