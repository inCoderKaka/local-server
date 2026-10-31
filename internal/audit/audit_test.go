package audit_test

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func open(t *testing.T) (*storage.DB, *audit.Log, *settings.Store) {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"),
		settings.Module(), identity.Module(), audit.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := settings.New(db)
	l, err := audit.NewLog(st)
	if err != nil {
		t.Fatal(err)
	}
	return db, l, st
}

func principal(t *testing.T, db *storage.DB, kind string) string {
	t.Helper()
	var id string
	err := db.Write(context.Background(), func(tx *storage.Tx) error {
		p, err := identity.NewRepos(nil).Principals.Create(tx, kind, nil, nil)
		id = p.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func count(t *testing.T, db *storage.DB) int {
	t.Helper()
	var n int
	if err := db.Reader().QueryRow(context.Background(), `SELECT count(*) FROM aud_event`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAppendOnly(t *testing.T) {
	db, _, _ := open(t)
	ctx := context.Background()
	if err := db.Write(ctx, func(tx *storage.Tx) error {
		_, err := audit.Record(tx, audit.Event{Action: "x.done"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE aud_event SET action = 'y'`, `DELETE FROM aud_event`} {
		err := db.Write(ctx, func(tx *storage.Tx) error { _, err := tx.Exec(q); return err })
		if err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("%s: want append-only error, got %v", q, err)
		}
	}
	if n := count(t, db); n != 1 {
		t.Fatalf("events = %d, want 1", n)
	}
}

func TestEventCommitsOnlyWithItsAction(t *testing.T) {
	db, _, _ := open(t)
	ctx := context.Background()
	actor := principal(t, db, "host_admin")
	boom := errors.New("boom")

	err := db.Write(ctx, func(tx *storage.Tx) error {
		if _, err := audit.Record(tx, audit.Event{Action: "grant.changed", ActorID: actor}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	if n := count(t, db); n != 0 {
		t.Fatalf("after rollback: %d events, want 0", n)
	}

	if err := db.Write(ctx, func(tx *storage.Tx) error {
		_, err := audit.Record(tx, audit.Event{Action: "grant.changed", ActorID: actor})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db); n != 1 {
		t.Fatalf("after commit: %d events, want 1", n)
	}
}

func TestRecordValidation(t *testing.T) {
	db, _, _ := open(t)
	ctx := context.Background()
	bad := []audit.Event{
		{},
		{Action: "a", ActorID: "no-such-principal"},
		{Action: "a", Detail: "not an object"},
		{Action: "a", Detail: map[string]any{"prompt": "hi"}},
		{Action: "a", Detail: map[string]any{"x": []any{map[string]any{"Password": "p"}}}},
	}
	for i, ev := range bad {
		err := db.Write(ctx, func(tx *storage.Tx) error { _, err := audit.Record(tx, ev); return err })
		if err == nil {
			t.Fatalf("event %d: want error", i)
		}
	}
	if n := count(t, db); n != 0 {
		t.Fatalf("%d events written, want 0", n)
	}
	err := db.Write(ctx, func(tx *storage.Tx) error {
		_, err := audit.Record(tx, audit.Event{Action: "a", Detail: map[string]any{"model": "m", "n": 2}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestListFiltersAndCursor(t *testing.T) {
	db, l, st := open(t)
	ctx := context.Background()
	admin := principal(t, db, "host_admin")
	c1 := principal(t, db, "consumer")
	c2 := principal(t, db, "consumer")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	n := 0
	clock := func() time.Time { n++; return base.Add(time.Duration(n) * time.Minute) }

	rec := func(ev audit.Event) {
		t.Helper()
		if err := db.Write(ctx, func(tx *storage.Tx) error { _, err := audit.RecordAt(tx, clock, ev); return err }); err != nil {
			t.Fatal(err)
		}
	}
	rec(audit.Event{Action: "key.issued", ActorID: admin, TargetID: c1})                  // t+1
	rec(audit.Event{Action: "grant.changed", ActorID: admin, TargetID: c1})               // t+2
	rec(audit.Event{Action: "key.issued", ActorID: admin, TargetID: c2})                  // t+3
	rec(audit.Event{Action: "key.revoked", TargetID: c2, Detail: map[string]any{"k": 1}}) // t+4, system actor

	list := func(f audit.Filter) audit.Page {
		t.Helper()
		p, err := l.List(ctx, db.Reader(), f)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	actions := func(p audit.Page) string {
		var s []string
		for _, e := range p.Events {
			s = append(s, e.Action)
		}
		return strings.Join(s, ",")
	}

	if got := actions(list(audit.Filter{})); got != "key.revoked,key.issued,grant.changed,key.issued" {
		t.Fatalf("all: %s", got)
	}
	if got := actions(list(audit.Filter{ActorID: admin})); got != "key.issued,grant.changed,key.issued" {
		t.Fatalf("actor: %s", got)
	}
	if got := actions(list(audit.Filter{TargetID: c2})); got != "key.revoked,key.issued" {
		t.Fatalf("target: %s", got)
	}
	if got := actions(list(audit.Filter{Action: "key.issued"})); got != "key.issued,key.issued" {
		t.Fatalf("action: %s", got)
	}
	if got := actions(list(audit.Filter{Since: base.Add(2 * time.Minute), Until: base.Add(4 * time.Minute)})); got != "key.issued,grant.changed" {
		t.Fatalf("range: %s", got)
	}
	last := list(audit.Filter{Action: "key.revoked"}).Events[0]
	if last.ActorID != "" || last.TargetID != c2 || string(last.Detail) != `{"k":1}` || !last.CreatedAt.Equal(base.Add(4*time.Minute)) {
		t.Fatalf("stored row: %+v", last)
	}

	// Paging: 3 per page over 4 events.
	p1 := list(audit.Filter{Limit: 3})
	if len(p1.Events) != 3 || p1.NextCursor == "" {
		t.Fatalf("page 1: %d events, cursor %q", len(p1.Events), p1.NextCursor)
	}
	p2 := list(audit.Filter{Limit: 3, Cursor: p1.NextCursor})
	if actions(p2) != "key.issued" || p2.NextCursor != "" {
		t.Fatalf("page 2: %s cursor %q", actions(p2), p2.NextCursor)
	}

	// The default page size is a setting.
	if err := st.Set(ctx, settings.Host, audit.KeyListPageSize, "", "2"); err != nil {
		t.Fatal(err)
	}
	if p := list(audit.Filter{}); len(p.Events) != 2 || p.NextCursor == "" {
		t.Fatalf("setting page size: %d events", len(p.Events))
	}
	// The setting also caps an explicit limit.
	if p := list(audit.Filter{Limit: 10}); len(p.Events) != 2 || p.NextCursor == "" {
		t.Fatalf("capped limit: %d events, cursor %q", len(p.Events), p.NextCursor)
	}

	if _, err := l.List(ctx, db.Reader(), audit.Filter{Cursor: "junk"}); !errors.Is(err, audit.ErrInvalidCursor) {
		t.Fatalf("junk cursor: %v", err)
	}
}

func TestCursorSameMillisecond(t *testing.T) {
	db, l, _ := open(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return base }
	var ids []string
	for i := 0; i < 3; i++ {
		if err := db.Write(ctx, func(tx *storage.Tx) error {
			id, err := audit.RecordAt(tx, clock, audit.Event{Action: "x.done"})
			ids = append(ids, id)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	want := append([]string(nil), ids...)
	sort.Sort(sort.Reverse(sort.StringSlice(want)))

	var got []string
	pages, cursor := 0, ""
	for {
		p, err := l.List(ctx, db.Reader(), audit.Filter{Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, e := range p.Events {
			got = append(got, e.ID)
		}
		if p.NextCursor == "" || pages > 10 {
			break
		}
		cursor = p.NextCursor
	}
	if pages != 3 || strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("pages=%d got=%v want=%v", pages, got, want)
	}
}
