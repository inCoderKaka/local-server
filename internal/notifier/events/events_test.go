package events_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
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
	}, events.ResourceRequestCreated)
	other := 0
	bus.Subscribe("other", func(context.Context, events.Event) error { other++; return nil }, events.JobDone)

	err := bus.Write(context.Background(), db, func(tx *storage.Tx) error {
		inUnit = true
		_, err := bus.Emit(tx, events.Event{
			Type: events.ResourceRequestCreated, ActorPrincipalID: "p1", TargetPrincipalID: "p2",
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
		"unknown type":  {Type: "made.up"},
		"not object":    {Type: events.HostPaused, Payload: json.RawMessage(`[1]`)},
		"prompt key":    {Type: events.HostPaused, Payload: json.RawMessage(`{"Prompt":"hi"}`)},
		"secret key":    {Type: events.HostPaused, Payload: json.RawMessage(`{"secret":"x"}`)},
		"nested prompt": {Type: events.HostPaused, Payload: json.RawMessage(`{"meta":{"prompt":"x"}}`)},
		"authorization": {Type: events.HostPaused, Payload: json.RawMessage(`{"authorization":"x"}`)},
		"apiKey":        {Type: events.HostPaused, Payload: json.RawMessage(`{"apiKey":"x"}`)},
		"in array":      {Type: events.HostPaused, Payload: json.RawMessage(`{"a":[{"secret":1}]}`)},
		"invalid json":  {Type: events.HostPaused, Payload: json.RawMessage(`{`)},
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
	noop := func(context.Context, events.Event) error { return nil }
	if err := bus.Subscribe("s", noop); err != nil {
		t.Fatal(err)
	}
	if err := bus.Subscribe("s", noop); err == nil {
		t.Fatal("duplicate subscriber name accepted")
	}
}

func TestEmitOutsideWriteIsRefused(t *testing.T) {
	db, bus := open(t), events.New()
	called := 0
	bus.Subscribe("s", func(context.Context, events.Event) error { called++; return nil })
	err := db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := bus.Emit(tx, events.Event{Type: events.HostPaused})
		return err
	})
	if !errors.Is(err, events.ErrOutsideWrite) {
		t.Fatalf("want ErrOutsideWrite, got %v", err)
	}
	if n := count(t, db); n != 0 || called != 0 {
		t.Fatalf("rows=%d called=%d, want 0 and 0", n, called)
	}
}

func TestPayloadIsCopiedByEmit(t *testing.T) {
	db, bus := open(t), events.New()
	var got events.Event
	bus.Subscribe("s", func(_ context.Context, ev events.Event) error { got = ev; return nil })
	buf := []byte(`{"request_id":"r1"}`)
	err := bus.Write(context.Background(), db, func(tx *storage.Tx) error {
		if _, err := bus.Emit(tx, events.Event{Type: events.HostPaused, Payload: buf}); err != nil {
			return err
		}
		copy(buf, strings.Repeat("x", len(buf)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Payload) != `{"request_id":"r1"}` {
		t.Fatalf("payload changed after Emit: %s", got.Payload)
	}
}

func TestConcurrentWritesDispatchOnlyCommittedUnits(t *testing.T) {
	db, bus := open(t), events.New()
	var mu sync.Mutex
	seen := map[int]int{}
	bus.Subscribe("s", func(_ context.Context, ev events.Event) error {
		var p struct{ N int }
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return err
		}
		mu.Lock()
		seen[p.N]++
		mu.Unlock()
		return nil
	})
	boom := errors.New("boom")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = bus.Write(context.Background(), db, func(tx *storage.Tx) error {
				if _, err := bus.Emit(tx, events.Event{Type: events.HostPaused, Payload: json.RawMessage(fmt.Sprintf(`{"n":%d}`, i))}); err != nil {
					return err
				}
				if i%2 == 1 {
					return boom
				}
				return nil
			})
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 10 {
		t.Fatalf("received %d distinct values, want 10: %v", len(seen), seen)
	}
	for i := 0; i < 20; i += 2 {
		if seen[i] != 1 {
			t.Errorf("n=%d received %d times, want 1", i, seen[i])
		}
	}
	if n := count(t, db); n != 10 {
		t.Fatalf("rows=%d, want 10", n)
	}
}

func TestTypesAreKnownAndNamed(t *testing.T) {
	for _, ty := range events.Types() {
		if !ty.Known() || !strings.Contains(string(ty), ".") {
			t.Errorf("bad type %q", ty)
		}
	}
}
