package snapshot

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// TestSweepTempRemovesOnlyOldBackupFolders: a leftover private folder older than the run timeout
// goes; a fresh one (a run in progress) and a folder with another name stay.
func TestSweepTempRemovesOnlyOldBackupFolders(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(dir, "live.db"), settings.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := settings.New(db)
	if err := RegisterSettings(ctx, db, st); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, "tmp")
	if err := os.Mkdir(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := st.Set(ctx, settings.Host, KeyTempDir, "", tmp); err != nil {
		t.Fatal(err)
	}
	mk := func(name string, age time.Duration) string {
		p := filepath.Join(tmp, name)
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "database.sqlite"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := mk("ls-backup-old", 2*time.Hour)
	fresh := mk("ls-backup-fresh", time.Minute)
	other := mk("other-old", 2*time.Hour)

	r := &Runner{cfg: RunnerConfig{DB: db, Settings: st, Log: slog.New(slog.DiscardHandler)}, now: time.Now}
	r.sweepTemp(ctx)

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("the old backup folder stayed: %v", err)
	}
	for _, p := range []string{fresh, other} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed: %v", filepath.Base(p), err)
		}
	}
}
