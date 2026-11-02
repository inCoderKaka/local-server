package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// The registry opens cleanly on a fresh database: every migration applies, every table starts
// with its module's prefix and has catalog entries, and no table references a module registered
// after it (storage.Open enforces all three). Adding a module to the registry is covered by this.
func TestRegistryOpensOnFreshDatabase(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"), registryModules()...))
	if err != nil {
		t.Fatalf("storage.Open with the registry: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.Reader().QueryRow(ctx, `SELECT count(DISTINCT module) FROM ops_schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if want := len(registry) + 1; n != want { // the registry plus storage's own
		t.Errorf("modules with applied migrations = %d, want %d", n, want)
	}
}

func TestRegistryModulesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range registryModules() {
		if m.Name == "" || len(m.Prefixes) == 0 || m.Migrations == nil {
			t.Errorf("module %+v needs a name, prefixes and migrations", m)
		}
		if seen[m.Name] {
			t.Errorf("module %q is registered twice", m.Name)
		}
		seen[m.Name] = true
		for _, p := range m.Prefixes {
			if !strings.HasSuffix(p, "_") {
				t.Errorf("module %s: prefix %q must end in an underscore", m.Name, p)
			}
		}
	}
	if len(registry) == 0 || registryModules()[0].Name != "settings" {
		t.Error("settings must be the first registry module")
	}
}
