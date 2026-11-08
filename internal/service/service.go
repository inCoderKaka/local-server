package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// Mode is the service mode (docs/glossary.md "Service mode"): user starts at login and needs no
// administrator rights, system starts at boot.
type Mode string

// The two service modes (req_001 D-30).
const (
	ModeUser   Mode = "user"
	ModeSystem Mode = "system"
)

// Target is the OS family a definition is rendered for. It is a value, not runtime.GOOS, so the
// same code renders and is tested for every OS on any host.
type Target string

// The three OS families local-server runs on.
const (
	TargetDarwin  Target = "darwin"
	TargetLinux   Target = "linux"
	TargetWindows Target = "windows"
)

// Defaults of the install-time values. A service definition is written once, without a database,
// so these cannot be settings: each is a flag of `local-server service install` (req_001 D-38).
const (
	// DefaultRestartDelay is how long the OS waits before it starts local-server again after an
	// unexpected exit. launchd ThrottleInterval, systemd RestartSec, Windows recovery delay.
	DefaultRestartDelay = 10 * time.Second
	// DefaultRestartCount is how many times the Windows logon task is restarted after a failed
	// run (Task Scheduler allows at most 999).
	DefaultRestartCount = 999
	// DefaultRestartResetPeriod is how long a Windows service must run without failing before its
	// failure count starts again from zero.
	DefaultRestartResetPeriod = 24 * time.Hour
	// MinRestartDelay is the shortest restart delay: every OS takes whole seconds.
	MinRestartDelay = time.Second
	// MaxRestartCount is Task Scheduler's limit on RestartOnFailure/Count.
	MaxRestartCount = 999

	// HelperCommand is the subcommand the user-session helper runs (req_001 D-30, D-29). It does
	// not exist yet: only the helper's definition is built here.
	HelperCommand = "notify-helper"

	// ServeCommand is the subcommand every definition runs.
	ServeCommand = "serve"
)

// DefaultName is the service name when --name is not given: a reverse-DNS label on macOS (where
// launchd expects one), plain "local-server" on Linux and Windows.
func DefaultName(t Target) string {
	if t == TargetDarwin {
		return "com.github.incoderkaka.local-server"
	}
	return "local-server"
}

// Spec is everything one service definition is rendered from.
type Spec struct {
	Target Target
	Mode   Mode
	// Name is the launchd label, systemd unit name or Windows task/service name.
	Name string
	// Binary is the absolute path of the local-server binary the definition runs.
	Binary string
	// DataDir is passed as --data-dir. Empty means the binary resolves its own default (user
	// mode only; system mode needs it, see Validate).
	DataDir string
	// UserName is the OS user the user-mode definition belongs to ("DOMAIN\user" on Windows).
	UserName string
	// RunAs is the account a system-mode service runs as. Empty means the service manager's
	// default (root, LocalSystem). On Windows it must be an account that needs no password, such
	// as NT AUTHORITY\LocalService, NT AUTHORITY\NetworkService or a managed service account: the
	// adapter never passes a password to the Service Control Manager.
	RunAs string
	// RestartDelay, RestartCount, RestartResetPeriod: see the Default constants.
	RestartDelay       time.Duration
	RestartCount       int
	RestartResetPeriod time.Duration
	// Linger asks Linux user mode to run `loginctl enable-linger`, so the service starts at boot
	// and keeps running after logout (req_001 D-30).
	Linger bool
}

// HelperName is the name of the user-session helper's login entry.
func (s Spec) HelperName() string {
	if s.Target == TargetDarwin {
		return s.Name + ".helper"
	}
	return s.Name + "-helper"
}

// ValidateIdentity checks what every command needs: target, mode and a name that is safe in a
// file name and in XML.
func (s Spec) ValidateIdentity() error {
	switch s.Target {
	case TargetDarwin, TargetLinux, TargetWindows:
	default:
		return fmt.Errorf("service: unsupported operating system %q", s.Target)
	}
	if s.Mode != ModeUser && s.Mode != ModeSystem {
		return fmt.Errorf("service: mode must be %q or %q, not %q", ModeUser, ModeSystem, s.Mode)
	}
	if s.Name == "" {
		return errors.New("service: the service name is empty")
	}
	const badName = "service: the service name %q must start with a letter or digit and may only hold letters, digits, '.', '-' and '_'"
	if c := s.Name[0]; !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
		return fmt.Errorf(badName, s.Name)
	}
	for _, r := range s.Name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_'
		if !ok {
			return fmt.Errorf(badName, s.Name)
		}
	}
	return nil
}

// Validate checks everything a definition is rendered from.
func (s Spec) Validate() error {
	if err := s.ValidateIdentity(); err != nil {
		return err
	}
	for _, f := range []struct{ what, value string }{
		{"binary path", s.Binary}, {"data directory", s.DataDir}, {"account", s.RunAs}, {"user name", s.UserName},
	} {
		if strings.IndexFunc(f.value, unicode.IsControl) >= 0 {
			return fmt.Errorf("service: the %s %q holds a control character", f.what, f.value)
		}
	}
	if s.Target == TargetLinux && s.RunAs != "" && strings.IndexFunc(s.RunAs, func(r rune) bool { return unicode.IsSpace(r) || r == '%' }) >= 0 {
		return fmt.Errorf("service: the account %q cannot be used in a systemd unit", s.RunAs)
	}
	if !isAbs(s.Target, s.Binary) {
		return fmt.Errorf("service: the binary path %q is not absolute", s.Binary)
	}
	if s.DataDir != "" && !isAbs(s.Target, s.DataDir) {
		return fmt.Errorf("service: the data directory %q is not absolute", s.DataDir)
	}
	if s.Mode == ModeSystem && s.DataDir == "" {
		return errors.New("service: system mode needs --data-dir: a system service has no user configuration directory of its own")
	}
	if s.Mode == ModeUser && s.RunAs != "" {
		return errors.New("service: --run-as applies to system mode only")
	}
	if s.Target == TargetWindows && s.Mode == ModeUser && s.UserName == "" {
		return errors.New("service: the Windows logon task needs the name of the current user")
	}
	if s.Target == TargetLinux && s.Mode == ModeUser && s.Linger && s.UserName == "" {
		return errors.New("service: enabling login lingering needs the name of the current user")
	}
	if s.RestartDelay < MinRestartDelay {
		return fmt.Errorf("service: the restart delay must be at least %s", MinRestartDelay)
	}
	if s.RestartCount < 1 || s.RestartCount > MaxRestartCount {
		return fmt.Errorf("service: the restart count must be between 1 and %d", MaxRestartCount)
	}
	if s.RestartResetPeriod < time.Second {
		return errors.New("service: the restart reset period must be at least 1s")
	}
	return nil
}

// isAbs says whether p is absolute on target t, whatever OS the code runs on.
func isAbs(t Target, p string) bool {
	if t == TargetWindows {
		return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') || strings.HasPrefix(p, `\\`)
	}
	return strings.HasPrefix(p, "/")
}

// Outcome is what a Manager command did, in words a host can read. It never holds prompts,
// responses or any other content (req_001 D-31, D-40).
type Outcome struct {
	// Changed is false when the command found everything as asked and did nothing.
	Changed bool
	// Notes are plain-language lines to show: why nothing changed, or what to do next.
	Notes []string
}

// Status is the state of the background service in one mode.
type Status struct {
	Mode      Mode
	Installed bool
	Running   bool
}

// String is the plain-language line `local-server service status` shows.
func (s Status) String() string {
	switch {
	case !s.Installed:
		return fmt.Sprintf("%s mode: not installed", s.Mode)
	case s.Running:
		return fmt.Sprintf("%s mode: installed, running", s.Mode)
	}
	return fmt.Sprintf("%s mode: installed, stopped", s.Mode)
}

// ErrNotInstalled is returned by Start when there is nothing to start.
var ErrNotInstalled = errors.New("the background service is not installed")

// ErrNeedsAdmin is returned when the OS refused a system-mode change for lack of rights.
var ErrNeedsAdmin = errors.New("administrator rights are needed for system mode; run the command again with sudo (macOS, Linux) or from an elevated terminal (Windows)")

// Manager is the port to the OS service manager (req_001 D-30): install, remove, start, stop and
// ask about one background service in one mode. An adapter exists per OS family. Install and
// Uninstall are idempotent: asking twice is not an error. A second Uninstall, and a second Install
// whose definition is unchanged, return Changed false with a note. A backend that cannot compare the
// registered definition with the rendered one says so in a note: the Windows logon task replaces
// its task (Changed true), and the Windows service is left as it is until it is uninstalled
// (Changed false).
type Manager interface {
	// Install registers the service so the OS starts it at login (user mode) or boot (system
	// mode), restarting it after an unexpected exit. It does not start it.
	Install(ctx context.Context) (Outcome, error)
	// Uninstall stops and removes the service. A service that is not installed is not an error.
	Uninstall(ctx context.Context) (Outcome, error)
	// Start starts the installed service. It returns ErrNotInstalled when there is none.
	Start(ctx context.Context) (Outcome, error)
	// Stop stops the service until the next login or boot. A service that is not running is not
	// an error.
	Stop(ctx context.Context) (Outcome, error)
	// Status says whether the service is installed and running.
	Status(ctx context.Context) (Status, error)
}
