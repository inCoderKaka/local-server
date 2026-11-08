package osadapter

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

	ksvc "github.com/kardianos/service"

	"github.com/inCoderKaka/local-server/internal/service"
)

// This file holds the real, OS-touching implementations of the seams in env.go: the real Runner
// and FS, the discovery of the running user, home folder and binary, and the kardianos/service
// constructor. It compiles on every OS (os.Getuid returns -1 on Windows, where UID is unused), so
// it carries no build constraint; the OS an Env serves is chosen by the Spec's Target, not by the
// build (req_001 D-137, D-30).

// NewManager is the Manager of the real machine this process runs on for the Spec: New over
// NewEnv. It is the one constructor the CLI calls.
func NewManager(spec service.Spec) (service.Manager, error) {
	env, err := NewEnv()
	if err != nil {
		return nil, err
	}
	return New(spec, env)
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

// RunByRootThroughSudo says whether this process is root because someone ran it with sudo. A
// user-mode service belongs to the signed-in user, so under sudo it would be set up for root.
func RunByRootThroughSudo() bool { return os.Getuid() == 0 && os.Getenv("SUDO_USER") != "" }

// Executable is the absolute path of the running local-server binary, which a service definition
// runs.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(exe)
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
