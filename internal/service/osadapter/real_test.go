package osadapter

import (
	"path/filepath"
	"testing"

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
