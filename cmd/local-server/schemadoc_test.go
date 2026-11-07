package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/inCoderKaka/local-server/internal/storage"
)

var update = flag.Bool("update", false, "regenerate docs/schema-reference.md")

// TestSchemaDoc regenerates the schema reference for the storage module and every registered
// module, and compares it with the committed file. Run `go generate ./cmd/local-server` (or
// go test -run TestSchemaDoc -update ./cmd/local-server) to rewrite it (req_001 D-37, D-44).
func TestSchemaDoc(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(t.TempDir(), "d.db"), registryModules()...))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.SchemaDoc(ctx)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join("..", "..", "docs", "schema-reference.md")
	if *update {
		if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%v (run go generate ./cmd/local-server)", err)
	}
	if string(want) != got {
		t.Fatal("docs/schema-reference.md is out of date; run go generate ./cmd/local-server")
	}
}
