package osadapter

import (
	"context"
	"errors"
	"io/fs"
	"strings"

	ksvc "github.com/kardianos/service"
)

// This file holds the seams of the adapter: the Runner, the FS and the Env that carries them.
// Nothing here touches the OS, so every OS family's backend runs against fakes on any host. The
// real implementations are in real.go (req_001 D-137, D-30).

// ErrToolMissing is returned by a Runner when the program it was asked to run is not installed.
var ErrToolMissing = errors.New("service: tool not installed")

// Result is what a finished tool printed and returned.
type Result struct {
	// Code is the exit code. Zero means success.
	Code int
	// Output is stdout and stderr together. It is the tool's own text about the service, never
	// content of the host's users (req_001 D-31).
	Output string
}

// Runner runs one OS tool and waits for it. A tool that ran and failed is a Result with a
// non-zero Code and a nil error; the error is for a tool that could not run at all
// (ErrToolMissing, a cancelled context). Tests pass a fake: they must never call launchctl,
// systemctl, loginctl, schtasks or sc.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
}

// FS is the few file operations the adapter needs, so tests keep to memory.
type FS interface {
	// ReadFile returns fs.ErrNotExist when there is no such file.
	ReadFile(path string) ([]byte, error)
	// WriteFile creates the parent folders and replaces the file.
	WriteFile(path string, data []byte, perm fs.FileMode) error
	// Remove returns fs.ErrNotExist when there is no such file.
	Remove(path string) error
}

// WindowsService is the part of a kardianos/service Service this adapter uses.
type WindowsService interface {
	Install() error
	Uninstall() error
	Start() error
	Stop() error
	Status() (ksvc.Status, error)
}

// Env is everything the adapter touches outside the Spec, so tests replace it.
type Env struct {
	Runner Runner
	FS     FS
	// ConfigHome is the user's configuration folder on Linux: an absolute $XDG_CONFIG_HOME, else ~/.config.
	ConfigHome string
	// Home is the user's home folder (macOS LaunchAgents live under it).
	Home string
	// UID is the user's numeric id: macOS user-mode jobs live in the domain gui/<UID>.
	UID int
	// TempDir is where the Windows logon task's XML is written for schtasks to read.
	TempDir string
	// NewWindowsService opens the Windows system service for a kardianos config.
	NewWindowsService func(*ksvc.Config) (WindowsService, error)
}

// adminHints are phrases the OS tools print when the caller lacks the rights for system mode.
// The adapter detects missing privilege from what the tool or the file system said, not by asking
// the OS who the user is, so the check compiles and is tested everywhere.
var adminHints = []string{"permission denied", "access is denied", "access denied", "authentication required",
	"not privileged", "operation not permitted", "must be root", "requires elevation"}

func looksLikeNoRights(text string) bool {
	text = strings.ToLower(text)
	for _, h := range adminHints {
		if strings.Contains(text, h) {
			return true
		}
	}
	return false
}
