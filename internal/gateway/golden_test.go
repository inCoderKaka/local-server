package gateway_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

// golden compares got with testdata/golden/<name>. got is JSON; it is re-indented so the file
// is readable and the comparison does not depend on key order.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	var v any
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatalf("%s: not JSON: %v", name, err)
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	pretty = append(pretty, '\n')
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, pretty, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(pretty, want) {
		t.Errorf("%s differs from the golden file:\n got:\n%s\nwant:\n%s", name, pretty, want)
	}
}

var (
	uuidRe    = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	createdRe = regexp.MustCompile(`"created":\s*\d+`)
)

// stable replaces the parts of a body that change on every run.
func stable(body []byte) []byte {
	body = uuidRe.ReplaceAll(body, []byte("00000000-0000-0000-0000-000000000000"))
	return createdRe.ReplaceAll(body, []byte(`"created":1`))
}

func statusIs(t *testing.T, r resp, want int) {
	t.Helper()
	if r.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", r.Code, want, r.Body.String())
	}
}

var _ = http.StatusOK
