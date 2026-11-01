package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func retentionEnv(t *testing.T) (*storage.DB, *settings.Store) {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.DefaultConfig(t.TempDir()+"/t.db", settings.Module(), events.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := settings.New(db)
	if err := st.RegisterStorage(); err != nil {
		t.Fatal(err)
	}
	if err := events.RegisterRetention(st); err != nil {
		t.Fatal(err)
	}
	return db, st
}

func seed(t *testing.T, db *storage.DB) {
	t.Helper()
	now := time.Now()
	err := db.Write(context.Background(), func(tx *storage.Tx) error {
		for id, at := range map[string]time.Time{"old": now.Add(-48 * time.Hour), "new": now.Add(-time.Hour)} {
			if _, err := tx.Exec(`INSERT INTO ntf_event (id, type, occurred_at) VALUES (?, 'host.paused', ?)`, id, at.UnixMilli()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRetentionDefaultIs30Days(t *testing.T) {
	_, st := retentionEnv(t)
	d, err := st.Duration(context.Background(), events.KeyRetention, "")
	if err != nil || d != 720*time.Hour {
		t.Fatalf("got %v, %v; want 720h", d, err)
	}
	if def, _ := st.Definition(events.KeyRetention); def.Owner != settings.Host {
		t.Fatalf("owner %q, want host", def.Owner)
	}
}

func TestPruneDeletesOnlyOlderThanRetention(t *testing.T) {
	db, st := retentionEnv(t)
	seed(t, db)
	ctx := context.Background()
	if err := st.Set(ctx, settings.Host, events.KeyRetention, "", "24h"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PruneOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db); n != 1 {
		t.Fatalf("rows=%d, want 1", n)
	}
	var id string
	if err := db.Reader().QueryRow(ctx, `SELECT id FROM ntf_event`).Scan(&id); err != nil || id != "new" {
		t.Fatalf("kept %q, %v; want new", id, err)
	}
}

func TestPruneKeepsEverythingAtDefaultAndAtZero(t *testing.T) {
	db, st := retentionEnv(t)
	seed(t, db)
	ctx := context.Background()
	if _, err := st.PruneOnce(ctx); err != nil { // default 30 d keeps both
		t.Fatal(err)
	}
	if n := count(t, db); n != 2 {
		t.Fatalf("default: rows=%d, want 2", n)
	}
	if err := st.Set(ctx, settings.Host, events.KeyRetention, "", "0s"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PruneOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db); n != 2 {
		t.Fatalf("zero: rows=%d, want 2", n)
	}
}
