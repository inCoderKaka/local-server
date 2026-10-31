package host_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

type env struct {
	db  *storage.DB
	st  *settings.Store
	bus *events.Bus
	now time.Time
	sh  *host.SharingState
}

func newEnv(t *testing.T, withHost bool) *env {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"),
		settings.Module(), host.Module(), identity.Module(), audit.Module(), events.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{db: db, st: settings.New(db), bus: events.New(), now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}
	e.sh, err = host.NewSharing(db, e.st, e.bus, func() time.Time { return e.now })
	if err != nil {
		t.Fatal(err)
	}
	if withHost {
		err = db.Write(context.Background(), func(tx *storage.Tx) error {
			_, err := tx.Exec(`INSERT INTO hst_host (id, created_at, data_key_sealed) VALUES ('h1', 1, x'00')`)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e *env) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.Reader().QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) set(t *testing.T, key, v string) {
	t.Helper()
	if err := e.st.Set(context.Background(), settings.Host, key, "", v); err != nil {
		t.Fatal(err)
	}
}

func TestPauseAuditsAndEmitsOneEvent(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	var got []events.Event
	if err := e.bus.Subscribe("t", func(_ context.Context, ev events.Event) error { got = append(got, ev); return nil }, events.HostPaused); err != nil {
		t.Fatal(err)
	}
	changed, err := e.sh.Pause(ctx, "")
	if err != nil || !changed {
		t.Fatalf("pause: %v %v", changed, err)
	}
	if n := e.count(t, `SELECT count(*) FROM aud_event WHERE action = 'host.paused'`); n != 1 {
		t.Fatalf("audit rows = %d", n)
	}
	if n := e.count(t, `SELECT count(*) FROM ntf_event WHERE type = 'host.paused'`); n != 1 || len(got) != 1 {
		t.Fatalf("events = %d, delivered = %d", n, len(got))
	}
	var p map[string]string
	if err := json.Unmarshal(got[0].Payload, &p); err != nil || p["host_id"] != "h1" || p["sharing_state"] != "paused" {
		t.Fatalf("payload = %s (%v)", got[0].Payload, err)
	}
	if s, err := e.sh.Effective(ctx); err != nil || s != host.Paused {
		t.Fatalf("effective = %v %v", s, err)
	}

	// Pausing again changes nothing.
	if changed, err := e.sh.Pause(ctx, ""); err != nil || changed {
		t.Fatalf("second pause: %v %v", changed, err)
	}
	if n := e.count(t, `SELECT count(*) FROM aud_event`); n != 1 {
		t.Fatalf("audit rows after repeat = %d", n)
	}
	if n := e.count(t, `SELECT count(*) FROM ntf_event`); n != 1 {
		t.Fatalf("events after repeat = %d", n)
	}

	if changed, err := e.sh.Resume(ctx, ""); err != nil || !changed {
		t.Fatalf("resume: %v %v", changed, err)
	}
	if n := e.count(t, `SELECT count(*) FROM aud_event WHERE action = 'host.resumed'`); n != 1 {
		t.Fatalf("resume audit rows = %d", n)
	}
	if n := e.count(t, `SELECT count(*) FROM ntf_event`); n != 1 {
		t.Fatalf("resume must not emit; events = %d", n)
	}
	if s, _ := e.sh.Effective(ctx); s != host.Sharing {
		t.Fatalf("effective after resume = %v", s)
	}
}

func TestPauseRecordsActor(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	var actor string
	if err := e.db.Write(ctx, func(tx *storage.Tx) error {
		p, err := identity.NewRepos(nil).Principals.Create(tx, "admin", nil, nil)
		actor = p.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.sh.Pause(ctx, actor); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM aud_event WHERE actor_id = ?`, actor); n != 1 {
		t.Fatalf("audit rows for actor = %d", n)
	}
	if n := e.count(t, `SELECT count(*) FROM ntf_event WHERE actor_principal_id = ?`, actor); n != 1 {
		t.Fatalf("event rows for actor = %d", n)
	}
}

func TestPauseRollsBackWithAuditFailure(t *testing.T) {
	e := newEnv(t, true)
	// An actor that is not a principal violates the audit foreign key: nothing may persist.
	if _, err := e.sh.Pause(context.Background(), "no-such-principal"); err == nil {
		t.Fatal("want error")
	}
	if paused, _ := e.sh.StoredPaused(context.Background()); paused {
		t.Fatal("state changed although the audit write failed")
	}
	if n := e.count(t, `SELECT count(*) FROM ntf_event`); n != 0 {
		t.Fatalf("events = %d", n)
	}
}

func TestNoHost(t *testing.T) {
	e := newEnv(t, false)
	if _, err := e.sh.Pause(context.Background(), ""); !errors.Is(err, host.ErrNoHost) {
		t.Fatalf("err = %v", err)
	}
}

func TestQuietHoursEnterAndLeave(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	at := func(h, m int) time.Time { return time.Date(2026, 3, 1, h, m, 0, 0, time.UTC) }
	state := func(tm time.Time) host.State {
		t.Helper()
		s, err := e.sh.EffectiveAt(ctx, tm)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	// Off by default.
	if state(at(23, 0)) != host.Sharing {
		t.Fatal("quiet hours must be off by default")
	}

	e.set(t, host.KeyQuietEnabled, "true")
	e.set(t, host.KeyQuietTimeZone, "UTC")
	e.set(t, host.KeyQuietStart, "01:00")
	e.set(t, host.KeyQuietEnd, "05:30")
	for _, c := range []struct {
		h, m int
		want host.State
	}{{0, 59, host.Sharing}, {1, 0, host.Quiet}, {5, 29, host.Quiet}, {5, 30, host.Sharing}, {12, 0, host.Sharing}} {
		if got := state(at(c.h, c.m)); got != c.want {
			t.Errorf("%02d:%02d = %v, want %v", c.h, c.m, got, c.want)
		}
	}

	// Crossing midnight (the default window).
	e.set(t, host.KeyQuietStart, "22:00")
	e.set(t, host.KeyQuietEnd, "07:00")
	for _, c := range []struct {
		h, m int
		want host.State
	}{{21, 59, host.Sharing}, {22, 0, host.Quiet}, {23, 59, host.Quiet}, {0, 0, host.Quiet}, {6, 59, host.Quiet}, {7, 0, host.Sharing}} {
		if got := state(at(c.h, c.m)); got != c.want {
			t.Errorf("%02d:%02d = %v, want %v", c.h, c.m, got, c.want)
		}
	}

	// The clock port drives Effective, and a time zone shifts the window.
	e.now = at(23, 0)
	if s, _ := e.sh.Effective(ctx); s != host.Quiet {
		t.Fatalf("effective at 23:00 UTC = %v", s)
	}
	e.set(t, host.KeyQuietTimeZone, "Asia/Tokyo") // 23:00 UTC is 08:00 there
	if s, _ := e.sh.Effective(ctx); s != host.Sharing {
		t.Fatalf("effective at 08:00 Tokyo = %v", s)
	}

	// A pause beats quiet hours, and no write happens on a quiet transition.
	e.set(t, host.KeyQuietTimeZone, "UTC")
	if _, err := e.sh.Pause(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if s, _ := e.sh.Effective(ctx); s != host.Paused {
		t.Fatalf("pause inside quiet hours = %v", s)
	}
	if n := e.count(t, `SELECT count(*) FROM aud_event`); n != 1 {
		t.Fatalf("audit rows = %d", n)
	}
}

func TestEmptyWindowIsNeverQuiet(t *testing.T) {
	s := host.Schedule{Enabled: true, Start: time.Hour, End: time.Hour}
	for h := 0; h < 24; h++ {
		if s.Quiet(time.Date(2026, 1, 1, h, 0, 0, 0, time.UTC)) {
			t.Fatalf("quiet at %d", h)
		}
	}
}

func TestInvalidScheduleKeepsPause(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	e.set(t, host.KeyQuietEnabled, "true")
	e.set(t, host.KeyQuietStart, "25:99")
	if _, err := e.sh.Effective(ctx); !errors.Is(err, host.ErrInvalidSchedule) {
		t.Fatalf("err = %v", err)
	}
	e.set(t, host.KeyQuietStart, "22:00")
	e.set(t, host.KeyQuietTimeZone, "Not/AZone")
	if _, err := e.sh.Effective(ctx); !errors.Is(err, host.ErrInvalidSchedule) {
		t.Fatalf("zone err = %v", err)
	}
	e.set(t, host.KeyQuietStart, "22:00")
	e.set(t, host.KeyQuietTimeZone, "")
	if _, err := e.sh.Effective(ctx); !errors.Is(err, host.ErrInvalidSchedule) {
		t.Fatalf("empty zone err = %v", err)
	}
	if _, err := e.sh.Pause(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if s, err := e.sh.Effective(ctx); err != nil || s != host.Paused {
		t.Fatalf("paused with bad schedule = %v %v", s, err)
	}
}
