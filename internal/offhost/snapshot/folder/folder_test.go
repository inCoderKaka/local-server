package folder_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/offhost/snapshot"
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot/folder"
)

func TestFolderTarget(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "dir")
	tg, err := folder.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := tg.Put(ctx, "a", strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	if err := tg.Put(ctx, "a", strings.NewReader("hello again"), 11); err != nil {
		t.Fatal(err)
	}
	if err := tg.Put(ctx, "b", strings.NewReader("short"), 99); err == nil {
		t.Fatal("a short write was accepted")
	}
	if _, err := tg.Stat(ctx, "b"); !errors.Is(err, snapshot.ErrNotFound) {
		t.Fatalf("a failed Put left an object: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("%d entries, want only a (no temporary file)", len(entries))
	}
	fi, _ := os.Stat(filepath.Join(dir, "a"))
	if fi.Mode().Perm()&0o077 != 0 && filepath.Separator == '/' {
		t.Fatalf("snapshot file mode %v is readable by others", fi.Mode().Perm())
	}
	o, err := tg.Stat(ctx, "a")
	if err != nil || o.Size != 11 {
		t.Fatalf("stat %+v %v", o, err)
	}
	rc, err := tg.Open(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(b, []byte("hello again")) {
		t.Fatalf("read %q", b)
	}
	objs, _ := tg.List(ctx)
	if len(objs) != 1 || objs[0].Name != "a" {
		t.Fatalf("list %+v", objs)
	}
	if err := tg.Delete(ctx, "a"); err != nil || tg.Delete(ctx, "a") != nil {
		t.Fatal("delete")
	}
	for _, bad := range []string{"", "../x", "a/b", ".", ".."} {
		if err := tg.Put(ctx, bad, strings.NewReader("x"), 1); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	if _, err := folder.New("  "); err == nil {
		t.Fatal("an empty folder was accepted")
	}
}
