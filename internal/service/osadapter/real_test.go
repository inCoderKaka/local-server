package osadapter

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/service"
)

func TestExecutableIsAbsolute(t *testing.T) {
	exe, err := Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(exe) {
		t.Fatalf("Executable() = %q, want an absolute path", exe)
	}
}

// NewManager only builds the Manager; it runs no OS tool, so it is safe on any host.
func TestNewManagerBuildsForEveryTarget(t *testing.T) {
	for _, target := range []service.Target{service.TargetDarwin, service.TargetLinux, service.TargetWindows} {
		spec := service.Spec{Target: target, Mode: service.ModeUser, Name: service.DefaultName(target)}
		if _, err := NewManager(spec); err != nil {
			t.Errorf("NewManager(%s): %v", target, err)
		}
	}
	if _, err := NewManager(service.Spec{Target: "plan9", Mode: service.ModeUser, Name: "x"}); err == nil {
		t.Error("NewManager accepted an unsupported OS")
	}
}

// The ExecRunner tests run only sh and sleep, never a service manager's tool.
func skipExecRunnerOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the ExecRunner tests run POSIX sh and sleep")
	}
}

func TestExecRunnerExitCodeIsAResultNotAnError(t *testing.T) {
	skipExecRunnerOnWindows(t)
	r, err := ExecRunner{}.Run(context.Background(), "sh", "-c", "exit 3")
	if err != nil || r.Code != 3 {
		t.Errorf("Run = %+v, %v; want code 3 and no error", r, err)
	}
}

func TestExecRunnerMissingToolIsErrToolMissing(t *testing.T) {
	skipExecRunnerOnWindows(t)
	_, err := ExecRunner{}.Run(context.Background(), "local-server-no-such-tool-xyz")
	if !errors.Is(err, ErrToolMissing) {
		t.Errorf("err = %v, want ErrToolMissing", err)
	}
}

// A tool killed by its context did not finish: the Runner contract makes that an error, not a Result
// with a non-zero Code that a caller could read as "the job is not loaded".
func TestExecRunnerCancelledRunIsAnError(t *testing.T) {
	skipExecRunnerOnWindows(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	r, err := ExecRunner{}.Run(ctx, "sleep", "10")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run = %+v, %v; want context.DeadlineExceeded", r, err)
	}
}
