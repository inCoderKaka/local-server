package main

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

const testMod = "example.com/ls"

func smallConfig() Config {
	return Config{
		Modules: []Module{
			{Name: "storage", Packages: []string{"internal/storage"}, Tables: []string{"ops_"}},
			{Name: "gateway", Packages: []string{"internal/gateway"}},
			{Name: "transport", Packages: []string{"internal/transport"}, Adapters: []string{"internal/transport/tsnet"}},
		},
		TablePrefixes: []string{"ops_"},
		Restricted:    map[string][]string{"database/sql": {"storage"}},
		NoteDir:       "docs/modules",
		NoteHeadings:  []string{"## Purpose"},
	}
}

func cleanTree() fstest.MapFS {
	note := &fstest.MapFile{Data: []byte("# x\n\n## Purpose\nx\n")}
	return fstest.MapFS{
		"internal/storage/doc.go":           {Data: []byte("package storage\n")},
		"internal/storage/migrations/.keep": {},
		"internal/gateway/doc.go":           {Data: []byte("package gateway\n")},
		"internal/transport/doc.go":         {Data: []byte("package transport\n")},
		"docs/modules/storage.md":           note,
		"docs/modules/gateway.md":           note,
		"docs/modules/transport.md":         note,
	}
}

func run(t *testing.T, mutate func(fstest.MapFS)) string {
	t.Helper()
	tree := cleanTree()
	if mutate != nil {
		mutate(tree)
	}
	return strings.Join(Check(tree, testMod, smallConfig()), "\n")
}

func TestCleanTreePasses(t *testing.T) {
	if got := run(t, nil); got != "" {
		t.Fatalf("clean tree reported violations:\n%s", got)
	}
}

// TestPlantedForbiddenImport is the ticket's proof: a planted forbidden import
// makes the check fail.
func TestPlantedForbiddenImport(t *testing.T) {
	cases := map[string]struct {
		file, src, want string
	}{
		"driver outside storage": {"internal/gateway/x.go", "package gateway\nimport _ \"database/sql\"\n", `allowed only in [storage]`},
		"core imports adapter":   {"internal/gateway/x.go", "package gateway\nimport _ \"" + testMod + "/internal/transport/tsnet\"\n", "only cmd/ may"},
		"internal imports cmd":   {"internal/gateway/x.go", "package gateway\nimport _ \"" + testMod + "/cmd/local-server\"\n", "never imports cmd/"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := run(t, func(m fstest.MapFS) {
				m["internal/transport/tsnet/doc.go"] = &fstest.MapFile{Data: []byte("package tsnet\n")}
				m[tc.file] = &fstest.MapFile{Data: []byte(tc.src)}
			})
			if !strings.Contains(got, tc.want) {
				t.Fatalf("want violation containing %q, got:\n%s", tc.want, got)
			}
		})
	}
}

func TestModuleCycle(t *testing.T) {
	got := run(t, func(m fstest.MapFS) {
		m["internal/gateway/x.go"] = &fstest.MapFile{Data: []byte("package gateway\nimport _ \"" + testMod + "/internal/storage\"\n")}
		m["internal/storage/x.go"] = &fstest.MapFile{Data: []byte("package storage\nimport _ \"" + testMod + "/internal/gateway\"\n")}
	})
	if !strings.Contains(got, "module cycle") {
		t.Fatalf("want a cycle violation, got:\n%s", got)
	}
}

func TestLayoutAndNoteRules(t *testing.T) {
	got := run(t, func(m fstest.MapFS) {
		delete(m, "docs/modules/gateway.md")
		delete(m, "internal/storage/migrations/.keep")
		m["internal/stray/doc.go"] = &fstest.MapFile{Data: []byte("package stray\n")}
		m["docs/modules/transport.md"] = &fstest.MapFile{Data: []byte("# no headings\n")}
	})
	for _, want := range []string{
		`module "gateway" has no architecture note`,
		"has no internal/storage/migrations folder",
		"internal/stray belongs to no module",
		`missing the heading "## Purpose"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing violation %q in:\n%s", want, got)
		}
	}
}

func TestConfigRules(t *testing.T) {
	cfg := smallConfig()
	cfg.TablePrefixes = append(cfg.TablePrefixes, "zzz_")
	cfg.Modules = append(cfg.Modules, Module{Name: "gateway", Packages: []string{"internal/gateway"}})
	got := strings.Join(Check(cleanTree(), testMod, cfg), "\n")
	for _, want := range []string{`duplicate module "gateway"`, `table prefix "zzz_" is owned by no module`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing violation %q in:\n%s", want, got)
		}
	}
}

// TestRepoPassesDefaultConfig checks the real tree against the real config,
// so a new package or a missing note fails go test, not only CI's archcheck step.
func TestRepoPassesDefaultConfig(t *testing.T) {
	root := "../.."
	mod, err := modulePath(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := Check(os.DirFS(root), mod, DefaultConfig()); len(got) > 0 {
		t.Fatalf("repository violates the module map:\n%s", strings.Join(got, "\n"))
	}
}
