package events_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func open(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"), events.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func count(t *testing.T, db *storage.DB) int {
	t.Helper()
	var n int
	if err := db.Reader().QueryRow(context.Background(), `SELECT count(*) FROM ntf_event`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRolledBackUnitLeavesNoRowAndCallsNoSubscriber(t *testing.T) {
	db, bus := open(t), events.New()
	called := 0
	bus.Subscribe("s", func(context.Context, events.Event) error { called++; return nil })
	boom := errors.New("boom")
	err := bus.Write(context.Background(), db, func(tx *storage.Tx) error {
		if _, err := bus.Emit(tx, events.Event{Type: events.HostPaused}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	if n := count(t, db); n != 0 || called != 0 {
		t.Fatalf("rows=%d calls=%d, want 0 and 0", n, called)
	}
}

func TestSubscriberRunsAfterCommitAndSeesRow(t *testing.T) {
	db, bus := open(t), events.New()
	var got events.Event
	var seen int
	var inUnit bool
	bus.Subscribe("s", func(ctx context.Context, ev events.Event) error {
		got = ev
		return db.Reader().QueryRow(ctx, `SELECT count(*) FROM ntf_event WHERE id = ?`, ev.ID).Scan(&seen)
	}, events.RequestCreated)
	other := 0
	bus.Subscribe("other", func(context.Context, events.Event) error { other++; return nil }, events.JobDone)

	err := bus.Write(context.Background(), db, func(tx *storage.Tx) error {
		inUnit = true
		_, err := bus.Emit(tx, events.Event{
			Type: events.RequestCreated, ActorPrincipalID: "p1", TargetPrincipalID: "p2",
			Payload: json.RawMessage(`{"request_id":"r1"}`),
		})
		if got.ID != "" {
			t.Error("subscriber called before commit")
		}
		return err
	})
	if err != nil || !inUnit {
		t.Fatal(err)
	}
	if got.ID == "" || seen != 1 || other != 0 {
		t.Fatalf("got=%+v seen=%d other=%d", got, seen, other)
	}
	if got.ActorPrincipalID != "p1" || string(got.Payload) != `{"request_id":"r1"}` || got.OccurredAt.IsZero() {
		t.Fatalf("event fields lost: %+v", got)
	}
}

func TestFailingSubscriberDoesNotAffectCommit(t *testing.T) {
	var logs bytes.Buffer
	db := open(t)
	bus := events.New(events.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	bus.Subscribe("fails", func(context.Context, events.Event) error { return errors.New("nope") })
	bus.Subscribe("panics", func(context.Context, events.Event) error { panic("x") })
	later := 0
	bus.Subscribe("later", func(context.Context, events.Event) error { later++; return nil })

	err := bus.Write(context.Background(), db, func(tx *storage.Tx) error {
		_, err := bus.Emit(tx, events.Event{Type: events.JobDone, Payload: json.RawMessage(`{"job_id":"SECRETISH-1"}`)})
		return err
	})
	if err != nil {
		t.Fatalf("Write failed because of a subscriber: %v", err)
	}
	if count(t, db) != 1 || later != 1 {
		t.Fatalf("rows=%d later=%d", count(t, db), later)
	}
	out := logs.String()
	if !strings.Contains(out, "fails") || !strings.Contains(out, "panics") {
		t.Fatalf("failures not logged: %s", out)
	}
	if strings.Contains(out, "SECRETISH") {
		t.Fatalf("payload leaked into the log: %s", out)
	}
}

func TestEmitValidation(t *testing.T) {
	db, bus := open(t), events.New()
	for name, ev := range map[string]events.Event{
		"unknown type": {Type: "made.up"},
		"not object":   {Type: events.HostPaused, Payload: json.RawMessage(`[1]`)},
		"prompt key":   {Type: events.HostPaused, Payload: json.RawMessage(`{"Prompt":"hi"}`)},
		"secret key":   {Type: events.HostPaused, Payload: json.RawMessage(`{"secret":"x"}`)},
		"invalid json": {Type: events.HostPaused, Payload: json.RawMessage(`{`)},
	} {
		err := bus.Write(context.Background(), db, func(tx *storage.Tx) error {
			_, err := bus.Emit(tx, ev)
			return err
		})
		if err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if count(t, db) != 0 {
		t.Fatal("a refused event was stored")
	}
	if err := bus.Subscribe("s", func(context.Context, events.Event) error { return nil }, "made.up"); err == nil {
		t.Fatal("subscribe to unknown type accepted")
	}
}

func TestEmitOutsideWriteStoresRowWithoutDispatch(t *testing.T) {
	db, bus := open(t), events.New()
	called := 0
	bus.Subscribe("s", func(context.Context, events.Event) error { called++; return nil })
	if err := db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := bus.Emit(tx, events.Event{Type: events.HostPaused})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if count(t, db) != 1 || called != 0 {
		t.Fatalf("rows=%d called=%d", count(t, db), called)
	}
}

func TestTypesAreKnownAndNamed(t *testing.T) {
	for _, ty := range events.Types() {
		if !ty.Known() || !strings.Contains(string(ty), ".") {
			t.Errorf("bad type %q", ty)
		}
	}
}
