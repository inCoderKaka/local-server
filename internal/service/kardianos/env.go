package kardianos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	ksvc "github.com/kardianos/service"
)

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
	// ConfigHome is the user's configuration folder on Linux: $XDG_CONFIG_HOME, else ~/.config.
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

// NewEnv is the real Env of the machine this process runs on.
func NewEnv() (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, fmt.Errorf("find the home folder: %w", err)
	}
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	return Env{Runner: ExecRunner{}, FS: OSFS{}, ConfigHome: cfg, Home: home, UID: os.Getuid(), TempDir: os.TempDir(),
		NewWindowsService: newKardianosService}, nil
}

// CurrentUserName is the name of the user this process runs as: DOMAIN\user on Windows.
func CurrentUserName() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

// ExecRunner is the real Runner.
type ExecRunner struct{}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return Result{Output: out.String()}, nil
	case errors.As(err, &ee):
		return Result{Code: ee.ExitCode(), Output: out.String()}, nil
	case errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist):
		return Result{}, fmt.Errorf("%w: %s", ErrToolMissing, name)
	}
	return Result{}, err
}

// OSFS is the real FS.
type OSFS struct{}

// ReadFile implements FS.
func (OSFS) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// WriteFile implements FS.
func (OSFS) WriteFile(path string, data []byte, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}

// Remove implements FS.
func (OSFS) Remove(path string) error { return os.Remove(path) }

// idle is the kardianos program of a service this process only installs and controls: it never
// calls Run, so Start and Stop do nothing.
type idle struct{}

func (idle) Start(ksvc.Service) error { return nil }
func (idle) Stop(ksvc.Service) error  { return nil }

func newKardianosService(cfg *ksvc.Config) (WindowsService, error) {
	return ksvc.New(idle{}, cfg)
}

// adminHints are phrases the OS tools print when the caller lacks the rights for system mode.
var adminHints = []string{"permission denied", "access is denied", "authentication required",
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
