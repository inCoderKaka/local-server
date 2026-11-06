package usage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

var t0 = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

// TestTokensStartedInChargesByAdmission: a record that started in a window and finished in the next
// is summed in the window it started in, and not in the one it finished in (req_001 D-107).
func TestTokensStartedInChargesByAdmission(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	boundary := t0.Add(time.Hour)
	if err := e.db.Write(ctx, func(tx *storage.Tx) error {
		for i, r := range []struct {
			started, finished time.Time
			total             int64
		}{
			{boundary.Add(-time.Second), boundary.Add(time.Second), 100},  // spans the boundary
			{boundary.Add(time.Second), boundary.Add(2 * time.Second), 7}, // wholly in the next window
			{boundary, boundary.Add(time.Second), 3},                      // starts exactly at the boundary
		} {
			if _, err := tx.Exec(`INSERT INTO use_record
(id, consumer_id, access_key_id, grant_version_id, operation, model, outcome, prompt_tokens, completion_tokens, total_tokens,
 latency_ms, started_at, finished_at, receipt_kid, receipt_signature, created_at)
VALUES (?, ?, ?, 'gv', 'chat.completions', 'm', 'completed', 0, ?, ?, 5, ?, ?, 'kid', x'00', 1)`,
				"00000000-0000-4000-8000-00000000000"+string(rune('1'+i)), consA, key1, r.total, r.total, r.started.UnixMilli(), r.finished.UnixMilli()); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rd := e.db.Reader()
	for _, tc := range []struct {
		name     string
		by       By
		id       string
		from, to time.Time
		want     int64
	}{
		{"window before the boundary, by consumer", ByConsumer, consA, t0, boundary, 100},
		{"window after the boundary, by consumer", ByConsumer, consA, boundary, boundary.Add(time.Hour), 10},
		{"window before, by key", ByAccessKey, key1, t0, boundary, 100},
		{"another subject", ByConsumer, consB, t0, boundary.Add(time.Hour), 0},
	} {
		got, err := TokensStartedIn(ctx, rd, tc.by, tc.id, tc.from, tc.to)
		if err != nil || got != tc.want {
			t.Errorf("%s: TokensStartedIn = %d, %v; want %d", tc.name, got, err, tc.want)
		}
	}
	if _, err := TokensStartedIn(ctx, rd, By(99), consA, t0, boundary); err == nil {
		t.Error("an unknown subject kind was accepted")
	}
}

func TestRequestCounter(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	rd := e.db.Reader()
	hour := time.Hour
	add := func(by By, id string, w time.Duration, start time.Time, times int) {
		t.Helper()
		if err := e.db.Write(ctx, func(tx *storage.Tx) error {
			for range times {
				if err := AddRequest(tx, by, id, w, start); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	count := func(by By, id string, w time.Duration, start time.Time) int64 {
		t.Helper()
		n, err := RequestCount(ctx, rd, by, id, w, start)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := count(ByConsumer, consA, hour, t0); got != 0 {
		t.Fatalf("empty counter = %d, want 0", got)
	}
	add(ByConsumer, consA, hour, t0, 3)
	add(ByConsumer, consA, hour, t0, 1)
	add(ByAccessKey, key1, hour, t0, 2)           // same id space, other kind
	add(ByConsumer, consA, 2*hour, t0, 5)         // other window length
	add(ByConsumer, consA, hour, t0.Add(hour), 7) // next window
	for _, tc := range []struct {
		name  string
		by    By
		id    string
		w     time.Duration
		start time.Time
		want  int64
	}{
		{"consumer window", ByConsumer, consA, hour, t0, 4},
		{"key window", ByAccessKey, key1, hour, t0, 2},
		{"other length", ByConsumer, consA, 2 * hour, t0, 5},
		{"next window", ByConsumer, consA, hour, t0.Add(hour), 7},
		{"other subject", ByConsumer, consB, hour, t0, 0},
	} {
		if got := count(tc.by, tc.id, tc.w, tc.start); got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, got, tc.want)
		}
	}

	// Prune: a window is removed only once it ended before the cutoff.
	cutoff := t0.Add(hour) // the first hour window ended exactly at the cutoff: kept (end < cutoff is false)
	prune := func(c time.Time) int64 {
		var n int64
		if err := e.db.Write(ctx, func(tx *storage.Tx) (err error) { n, err = PruneRequestCounters(tx, c); return err }); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := prune(cutoff); n != 0 {
		t.Fatalf("prune at the window end removed %d rows, want 0", n)
	}
	if n := prune(cutoff.Add(time.Millisecond)); n != 2 { // consumer and key hour windows of t0
		t.Fatalf("prune removed %d rows, want 2", n)
	}
	if got := count(ByConsumer, consA, hour, t0); got != 0 {
		t.Errorf("pruned counter = %d, want 0", got)
	}
	if got := count(ByConsumer, consA, 2*hour, t0); got != 5 { // its window runs to t0+2h: not ended
		t.Errorf("a running window was pruned: %d", got)
	}
	if got := count(ByConsumer, consA, hour, t0.Add(hour)); got != 7 {
		t.Errorf("a later window was pruned: %d", got)
	}
}

// TestRequestCounterTable: the table refuses what it must not hold.
func TestRequestCounterTable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for name, q := range map[string]string{
		"bad kind":        `INSERT INTO use_request_counter VALUES ('node', 'x', 1000, 0, 1)`,
		"zero window":     `INSERT INTO use_request_counter VALUES ('consumer', 'x', 0, 0, 1)`,
		"negative count":  `INSERT INTO use_request_counter VALUES ('consumer', 'x', 1000, 0, -1)`,
		"null subject id": `INSERT INTO use_request_counter VALUES ('consumer', NULL, 1000, 0, 1)`,
	} {
		err := e.db.Write(ctx, func(tx *storage.Tx) error { _, err := tx.Exec(q); return err })
		if err == nil || !(strings.Contains(err.Error(), "CHECK") || strings.Contains(err.Error(), "NOT NULL")) {
			t.Errorf("%s: want CHECK or NOT NULL error, got %v", name, err)
		}
	}
}
