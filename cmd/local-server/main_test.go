package main

import (
	"os"
	"testing"
)

func TestRunVersion(t *testing.T) {
	if got := run([]string{"--version"}, os.Stdout, os.Stderr); got != 0 {
		t.Fatalf("run(--version) = %d, want 0", got)
	}
	if got := run([]string{"--nope"}, os.Stdout, os.Stderr); got != 2 {
		t.Fatalf("run(--nope) = %d, want 2", got)
	}
}
