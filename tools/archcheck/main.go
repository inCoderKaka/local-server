// Command archcheck enforces local-server's module boundaries (D-21, D-31).
// Run it from anywhere inside the repository: go run ./tools/archcheck
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "archcheck:", err)
		os.Exit(2)
	}
	mod, err := modulePath(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "archcheck:", err)
		os.Exit(2)
	}
	violations := Check(os.DirFS(root), mod, DefaultConfig())
	for _, v := range violations {
		fmt.Fprintln(os.Stderr, v)
	}
	if len(violations) > 0 {
		fmt.Fprintf(os.Stderr, "archcheck: %d violation(s)\n", len(violations))
		os.Exit(1)
	}
	fmt.Println("archcheck: ok")
}

// repoRoot walks up from the working directory to the directory with go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above the working directory")
		}
		dir = parent
	}
}
