package settings

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

const testSQL = `CREATE TABLE tst_event (id INTEGER PRIMARY KEY, at INTEGER, kind TEXT NOT NULL) STRICT;
--@table tst_event classification=internal billable=false | Test events.
--@column tst_event.id classification=internal billable=false | Id.
--@column tst_event.at classification=internal billable=false | Time.
--@column tst_event.kind classification=internal billable=false | Kind.
`

func newStore(t *testing.T) *Store {
	t.Helper()
	tst := storage.Module{Name: "tst", Prefixes: []string{"tst_"},
		Migrations: fstest.MapFS{"0001_event.sql": &fstest.MapFile{Data: []byte(testSQL)}}}
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"), Module(), tst))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db)
}

func TestDefaultThenOwnerOverride(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if err := s.Register(
		Def{Key: "queue.limit", Kind: Int, Default: "8", Owner: Host, Description: "Queue size."},
		Def{Key: "job.deadline", Kind: Duration, Default: "10m", Owner: Consumer, Scoped: true, Description: "Deadline."},
	); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Int(ctx, "queue.limit", ""); err != nil || n != 8 {
		t.Fatalf("default = %d, %v; want 8", n, err)
	}
	if err := s.Set(ctx, Consumer, "queue.limit", "", "2"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("non-owner Set = %v; want ErrNotOwner", err)
	}
	if n, _ := s.Int(ctx, "queue.limit", ""); n != 8 {
		t.Fatalf("rejected Set changed the value to %d", n)
	}
	if err := s.Set(ctx, Host, "queue.limit", "", "1000000"); err != nil { // no bounds (D-54)
		t.Fatal(err)
	}
	if n, _ := s.Int(ctx, "queue.limit", ""); n != 1000000 {
		t.Fatalf("override = %d", n)
	}
	if err := s.Set(ctx, Host, "queue.limit", "", "many"); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("bad value = %v; want ErrInvalidValue", err)
	}
	if err := s.Reset(ctx, Host, "queue.limit", ""); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Int(ctx, "queue.limit", ""); n != 8 {
		t.Fatalf("after Reset = %d; want 8", n)
	}

	// Scoped: one value per consumer.
	if err := s.Set(ctx, Consumer, "job.deadline", "c1", "1h"); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Duration(ctx, "job.deadline", "c1"); d != time.Hour {
		t.Fatalf("c1 = %v", d)
	}
	if d, _ := s.Duration(ctx, "job.deadline", "c2"); d != 10*time.Minute {
		t.Fatalf("c2 = %v; want default", d)
	}
	if _, err := s.Get(ctx, "job.deadline", ""); !errors.Is(err, ErrScope) {
		t.Fatalf("missing scope = %v", err)
	}
	if _, err := s.Bool(ctx, "queue.limit", ""); !errors.Is(err, ErrKind) {
		t.Fatalf("wrong type = %v", err)
	}
	if _, err := s.Get(ctx, "nope", ""); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("unknown = %v", err)
	}
}

func TestRegisterConflict(t *testing.T) {
	s := newStore(t)
	d := Def{Key: "a.b", Kind: Bool, Default: "true", Owner: Host}
	if err := s.Register(d, d); err != nil {
		t.Fatal(err)
	}
	d.Default = "false"
	if err := s.Register(d); !errors.Is(err, ErrDefinition) {
		t.Fatalf("conflict = %v", err)
	}
	if err := s.Register(Def{Key: "x", Kind: Int, Default: "z", Owner: Host}); !errors.Is(err, ErrDefinition) {
		t.Fatalf("bad default = %v", err)
	}
}

func TestRetentionPrunesExpiredUsingCurrentSetting(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	if err := s.Register(Def{Key: "event.retention", Kind: Duration, Default: "24h", Owner: Host}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterRetention(Rule{Table: "tst_event", TimeColumn: "at", Setting: "event.retention",
		Where: "kind = ?", Args: []any{"old"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(ctx, Host, KeyRetentionBatchRows, "", "2"); err != nil { // forces several batches
		t.Fatal(err)
	}
	ms := func(ago time.Duration) int64 { return now.Add(-ago).UnixMilli() }
	err := s.db.Write(ctx, func(tx *storage.Tx) error {
		for i, row := range []struct {
			at   int64
			kind string
		}{
			{ms(48 * time.Hour), "old"}, {ms(30 * time.Hour), "old"}, {ms(25 * time.Hour), "old"}, // expired
			{ms(48 * time.Hour), "other"}, // expired but the extra condition excludes it
			{ms(time.Hour), "old"},        // unexpired
		} {
			if _, err := tx.Exec(`INSERT INTO tst_event (id, at, kind) VALUES (?, ?, ?)`, i+1, row.at, row.kind); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`INSERT INTO tst_event (id, at, kind) VALUES (9, NULL, 'old')`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		var n int
		if err := s.db.Reader().QueryRow(ctx, `SELECT count(*) FROM tst_event`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	res, err := s.PruneOnce(ctx)
	if err != nil || len(res) != 1 || res[0].Rows != 3 {
		t.Fatalf("PruneOnce = %v, %v; want 3 rows", res, err)
	}
	if n := count(); n != 3 {
		t.Fatalf("rows left = %d; want 3", n)
	}
	// Shorten the retention at run time: the unexpired row now expires.
	if err := s.Set(ctx, Host, "event.retention", "", "30m"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PruneOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 2 {
		t.Fatalf("rows left = %d; want 2 (other + NULL time)", n)
	}
	// Zero keeps forever.
	if err := s.Set(ctx, Host, "event.retention", "", "0s"); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return now.Add(1000 * time.Hour) }
	if _, err := s.PruneOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 2 {
		t.Fatalf("rows left = %d; want 2", n)
	}
}

func TestRegisterRetentionValidates(t *testing.T) {
	s := newStore(t)
	_ = s.Register(Def{Key: "n", Kind: Int, Default: "1", Owner: Host})
	if err := s.RegisterRetention(Rule{Table: "tst_event; DROP", TimeColumn: "at", Setting: "retention.interval"}); !errors.Is(err, ErrDefinition) {
		t.Fatalf("ident = %v", err)
	}
	if err := s.RegisterRetention(Rule{Table: "tst_event", TimeColumn: "at", Setting: "n"}); !errors.Is(err, ErrDefinition) {
		t.Fatalf("kind = %v", err)
	}
	if err := s.RegisterRetention(Rule{Table: "tst_event", TimeColumn: "at", Setting: "zzz"}); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("unknown = %v", err)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	s := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx, nil); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}
