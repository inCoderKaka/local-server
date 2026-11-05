package usage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

type env struct {
	db *storage.DB
	st *settings.Store
	r  *Rollups
	n  atomic.Int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"), settings.Module(), Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{db: db, st: settings.New(db)}
	if e.r, err = NewRollups(db, e.st, telemetry.Nop()); err != nil {
		t.Fatal(err)
	}
	return e
}

const (
	consA = "aaaaaaaa-0000-4000-8000-000000000001"
	consB = "bbbbbbbb-0000-4000-8000-000000000002"
	key1  = "11111111-0000-4000-8000-000000000001"
	key2  = "22222222-0000-4000-8000-000000000002"
	key3  = "33333333-0000-4000-8000-000000000003"
)

// rec is a use_record row, reduced to what the rollups read.
type rec struct {
	cons, key, model, outcome string
	prompt, completion        int64
	finished                  time.Time
}

func (e *env) insert(t *testing.T, recs ...rec) {
	t.Helper()
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		for _, r := range recs {
			id := fmt.Sprintf("00000000-0000-4000-8000-%012d", e.n.Add(1))
			if _, err := tx.Exec(`INSERT INTO use_record
(id, consumer_id, access_key_id, grant_version_id, operation, model, outcome, prompt_tokens, completion_tokens, total_tokens,
 latency_ms, started_at, finished_at, receipt_kid, receipt_signature, created_at)
VALUES (?, ?, ?, 'gv', 'chat.completions', ?, ?, ?, ?, ?, 5, ?, ?, 'kid', x'00', ?)`,
				id, r.cons, r.key, r.model, r.outcome, r.prompt, r.completion, r.prompt+r.completion,
				r.finished.UnixMilli()-5, r.finished.UnixMilli(), r.finished.UnixMilli()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (e *env) runOnce(t *testing.T) int64 {
	t.Helper()
	n, err := e.r.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

type bucketKey struct {
	g                    Granularity
	start                int64
	consumer, key, model string
}

type sums struct{ requests, prompt, completion, total int64 }

// expected sums the records in Go, independently of the SQL the worker runs.
func expected(recs []rec) map[bucketKey]sums {
	out := map[bucketKey]sums{}
	for _, r := range recs {
		for _, g := range granularities {
			ms := g.Size().Milliseconds()
			k := bucketKey{g, r.finished.UnixMilli() / ms * ms, r.cons, r.key, r.model}
			s := out[k]
			s.requests++
			s.prompt += r.prompt
			s.completion += r.completion
			s.total += r.prompt + r.completion
			out[k] = s
		}
	}
	return out
}

func (e *env) rollupRows(t *testing.T) map[bucketKey]sums {
	t.Helper()
	rows, err := e.db.Reader().Query(context.Background(),
		`SELECT granularity, bucket_start, consumer_id, access_key_id, model, requests, prompt_tokens, completion_tokens, total_tokens FROM use_rollup`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[bucketKey]sums{}
	for rows.Next() {
		var k bucketKey
		var g string
		var s sums
		if err := rows.Scan(&g, &k.start, &k.consumer, &k.key, &k.model, &s.requests, &s.prompt, &s.completion, &s.total); err != nil {
			t.Fatal(err)
		}
		k.g = Granularity(g)
		out[k] = s
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func (e *env) checkRollups(t *testing.T, all []rec) {
	t.Helper()
	got, want := e.rollupRows(t), expected(all)
	if len(got) != len(want) {
		t.Errorf("%d rollup rows, want %d", len(got), len(want))
	}
	for k, w := range want {
		if g, ok := got[k]; !ok || g != w {
			t.Errorf("bucket %+v = %+v (present %v), want %+v", k, g, ok, w)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected bucket %+v", k)
		}
	}
	// Whatever the granularity, the rollups add up to the records.
	for _, g := range granularities {
		var n, tok int64
		if err := e.db.Reader().QueryRow(context.Background(),
			`SELECT COALESCE(SUM(requests), 0), COALESCE(SUM(total_tokens), 0) FROM use_rollup WHERE granularity = ?`, string(g)).Scan(&n, &tok); err != nil {
			t.Fatal(err)
		}
		var rn, rtok int64
		if err := e.db.Reader().QueryRow(context.Background(),
			`SELECT COUNT(*), COALESCE(SUM(total_tokens), 0) FROM use_record`).Scan(&rn, &rtok); err != nil {
			t.Fatal(err)
		}
		if n != rn || tok != rtok {
			t.Errorf("%s rollups: %d requests, %d tokens; records: %d, %d", g, n, tok, rn, rtok)
		}
	}
}

// day0 is a UTC midnight, so the records below fall either side of minute, hour and day edges.
var day0 = time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)

func sampleRecords() []rec {
	at := func(d time.Duration) time.Time { return day0.Add(d) }
	return []rec{
		{consA, key1, "m1", "completed", 10, 20, at(10*time.Hour + 5*time.Minute + 3*time.Second)},
		{consA, key1, "m1", "completed", 1, 2, at(10*time.Hour + 5*time.Minute + 59*time.Second + 999*time.Millisecond)}, // same minute
		{consA, key1, "m1", "aborted", 7, 0, at(10*time.Hour + 6*time.Minute)},                                           // next minute, same hour
		{consA, key1, "m1", "failed", 0, 0, at(11 * time.Hour)},                                                          // next hour, same day
		{consA, key2, "m1", "completed", 5, 5, at(11*time.Hour + 30*time.Second)},
		{consA, key1, "m2", "completed", 100, 200, at(11*time.Hour + 30*time.Second)},
		{consB, key3, "m1", "completed", 3, 4, at(11*time.Hour + 30*time.Second)},
		{consB, key3, "m1", "completed", 9, 9, at(23*time.Hour + 59*time.Minute + 59*time.Second + 999*time.Millisecond)}, // last ms of the day
		{consB, key3, "m1", "completed", 8, 8, at(24 * time.Hour)},                                                        // first ms of the next day
		{consA, key1, "m1", "completed", 2, 2, at(-1 * time.Millisecond)},                                                 // previous day
	}
}

func TestRollupsEqualRecordSums(t *testing.T) {
	e := newEnv(t)
	recs := sampleRecords()
	e.insert(t, recs...)
	if n := e.runOnce(t); n != int64(len(recs)) {
		t.Fatalf("RunOnce counted %d records, want %d", n, len(recs))
	}
	e.checkRollups(t, recs)
}

func TestRunOnceIsIdempotentAndCountsLaterRecordsOnce(t *testing.T) {
	e := newEnv(t)
	recs := sampleRecords()
	e.insert(t, recs[:6]...)
	e.runOnce(t)
	if n := e.runOnce(t); n != 0 {
		t.Fatalf("a second pass counted %d records, want 0", n)
	}
	e.checkRollups(t, recs[:6])

	e.insert(t, recs[6:]...) // some land in buckets that already exist
	if n := e.runOnce(t); n != int64(len(recs)-6) {
		t.Fatalf("pass after new records counted %d, want %d", n, len(recs)-6)
	}
	e.runOnce(t)
	e.checkRollups(t, recs)
}

func TestRunOnceStepsThroughASmallBatchSetting(t *testing.T) {
	e := newEnv(t)
	if err := e.st.Set(context.Background(), settings.Host, KeyRollupBatchRows, "", "3"); err != nil {
		t.Fatal(err)
	}
	recs := sampleRecords() // 10 records: steps of 3, 3, 3, 1
	e.insert(t, recs...)
	if n := e.runOnce(t); n != int64(len(recs)) {
		t.Fatalf("counted %d, want %d", n, len(recs))
	}
	e.checkRollups(t, recs)
	var last int64
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT last_rowid FROM use_rollup_state WHERE id = 1`).Scan(&last); err != nil || last != int64(len(recs)) {
		t.Fatalf("watermark = %d, %v; want %d", last, err, len(recs))
	}
}

func TestFailedStepLeavesNothingCounted(t *testing.T) {
	e := newEnv(t)
	recs := sampleRecords()
	e.insert(t, recs...)
	// A step that fails after the sums are added must roll them back with the watermark.
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		if _, err := tx.Exec(`CREATE TRIGGER use_rollup_state_boom BEFORE INSERT ON use_rollup_state BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.r.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce succeeded although the watermark could not be written")
	}
	if n := len(e.rollupRows(t)); n != 0 {
		t.Fatalf("%d rollup rows survived a failed step", n)
	}
	if err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := tx.Exec(`DROP TRIGGER use_rollup_state_boom`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e.runOnce(t)
	e.checkRollups(t, recs)
}

// countTel is a telemetry whose counters remember what they were told, by metric name.
type countTel struct {
	telemetry.Telemetry
	mu  sync.Mutex
	sum map[string]float64
}

func (c *countTel) Counter(name string, _ ...string) telemetry.Counter {
	return countCounter{c, name}
}

func (c *countTel) total(name string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sum[name]
}

type countCounter struct {
	c    *countTel
	name string
}

func (m countCounter) Inc(...string) { m.Add(1) }

func (m countCounter) Add(delta float64, _ ...string) {
	m.c.mu.Lock()
	defer m.c.mu.Unlock()
	m.c.sum[m.name] += delta
}

func TestRollupMetricsCountRecordsAndFailedPasses(t *testing.T) {
	e := newEnv(t)
	tel := &countTel{Telemetry: telemetry.Nop(), sum: map[string]float64{}}
	var err error
	if e.r, err = NewRollups(e.db, e.st, tel); err != nil {
		t.Fatal(err)
	}
	recs := sampleRecords()
	e.insert(t, recs...)
	e.runOnce(t)
	if got := tel.total("usage_rollup_records_total"); got != float64(len(recs)) {
		t.Errorf("usage_rollup_records_total = %v, want %d", got, len(recs))
	}
	if got := tel.total("usage_rollup_errors_total"); got != 0 {
		t.Errorf("usage_rollup_errors_total = %v after a good pass, want 0", got)
	}
	e.insert(t, recs[0])
	if err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := tx.Exec(`CREATE TRIGGER use_rollup_state_boom BEFORE INSERT ON use_rollup_state BEGIN SELECT RAISE(ABORT, 'boom'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.r.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce succeeded although the watermark could not be written")
	}
	if got := tel.total("usage_rollup_errors_total"); got != 1 {
		t.Errorf("usage_rollup_errors_total = %v after a failed pass, want 1", got)
	}
	if got := tel.total("usage_rollup_records_total"); got != float64(len(recs)) {
		t.Errorf("usage_rollup_records_total = %v after a failed pass, want %d", got, len(recs))
	}
}

func TestRetentionPrunesEachGranularityAtItsSetting(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for key, want := range map[string]time.Duration{KeyMinuteRetention: 7 * 24 * time.Hour, KeyHourRetention: 90 * 24 * time.Hour, KeyDayRetention: 0} {
		if d, err := e.st.Duration(ctx, key, ""); err != nil || d != want {
			t.Fatalf("%s default = %v, %v; want %v", key, d, err, want)
		}
	}
	// The settings store reads the real clock, so the records are placed relative to it.
	now := time.Now().UTC()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	day := 24 * time.Hour
	recs := []rec{
		{consA, key1, "m1", "completed", 1, 1, ago(100 * day)}, // minute and hour expired, day kept
		{consA, key1, "m1", "completed", 2, 2, ago(8 * day)},   // minute expired, hour kept
		{consA, key1, "m1", "completed", 4, 4, ago(time.Hour)}, // everything kept
	}
	e.insert(t, recs...)
	e.runOnce(t)
	if _, err := e.st.PruneOnce(ctx); err != nil {
		t.Fatal(err)
	}
	count := func(g Granularity) int {
		var n int
		if err := e.db.Reader().QueryRow(ctx, `SELECT COUNT(*) FROM use_rollup WHERE granularity = ?`, string(g)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if m, h, d := count(GranularityMinute), count(GranularityHour), count(GranularityDay); m != 1 || h != 2 || d != 3 {
		t.Fatalf("buckets left: %d minute, %d hour, %d day; want 1, 2, 3", m, h, d)
	}
	// The sums that remain are still the record sums: the hour and day totals of the pruned
	// minutes are not touched, and the records are all still there.
	got := e.rollupRows(t)
	for k, w := range expected(recs) {
		switch {
		case k.g == GranularityMinute && k.start < now.Add(-7*day).UnixMilli():
			if _, ok := got[k]; ok {
				t.Errorf("expired minute bucket %+v was kept", k)
			}
		case k.g == GranularityHour && k.start < now.Add(-90*day).UnixMilli():
			if _, ok := got[k]; ok {
				t.Errorf("expired hour bucket %+v was kept", k)
			}
		default:
			if got[k] != w {
				t.Errorf("bucket %+v = %+v, want %+v", k, got[k], w)
			}
		}
	}
	// A pass after pruning counts nothing again: the watermark, not the buckets, says what is done.
	if n := e.runOnce(t); n != 0 {
		t.Fatalf("pass after pruning counted %d records", n)
	}
	if got2 := e.rollupRows(t); len(got2) != len(got) {
		t.Fatalf("pass after pruning changed the rollups: %d rows, was %d", len(got2), len(got))
	}
	// A shorter retention prunes more at the next run.
	if err := e.st.Set(ctx, settings.Host, KeyMinuteRetention, "", "30m"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.PruneOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if m := count(GranularityMinute); m != 0 {
		t.Fatalf("minute buckets after a 30m retention = %d, want 0 (the one 1h old goes)", m)
	}
}

func TestSettingsValidators(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, key := range []string{KeyRollupInterval, KeyRollupBatchRows, KeyPageSize} {
		for _, bad := range []string{"0", "-1", "0s", "-5s", "x"} {
			err := e.st.Set(ctx, settings.Host, key, "", bad)
			if err == nil {
				t.Errorf("%s accepted %q", key, bad)
			}
		}
	}
	if err := e.st.Set(ctx, settings.Host, KeyPageSize, "", "7"); err != nil {
		t.Fatal(err)
	}
	if err := e.st.Set(ctx, settings.Host, KeyRollupInterval, "", "30s"); err != nil {
		t.Fatal(err)
	}
	if err := e.st.Set(ctx, settings.Host, KeyDayRetention, "", "-1h"); err != nil {
		t.Fatalf("a negative retention (forever) was refused: %v", err)
	}
}

func TestRunRollsUpAndStopsWithItsContext(t *testing.T) {
	e := newEnv(t)
	if err := e.st.Set(context.Background(), settings.Host, KeyRollupInterval, "", "10ms"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.r.Run(ctx); close(done) }()
	e.insert(t, sampleRecords()...)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int
		if err := e.db.Reader().QueryRow(ctx, `SELECT COUNT(*) FROM use_rollup WHERE granularity = 'day'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 && len(e.rollupRows(t)) == len(expected(sampleRecords())) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the worker did not roll up the records")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop when its context ended")
	}
	e.checkRollups(t, sampleRecords())
}

func TestMigrationThreeAppliesOverOneAndTwo(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "t.db")
	old := fstest.MapFS{}
	for _, name := range []string{"0001_record.sql", "0002_limit_indexes.sql"} {
		b, err := fs.ReadFile(ownMigrations, "migrations/"+name)
		if err != nil {
			t.Fatal(err)
		}
		old[name] = &fstest.MapFile{Data: b}
	}
	oldMod := storage.Module{Name: "usage", Prefixes: []string{"use_"}, Migrations: old}
	db, err := storage.Open(ctx, storage.DefaultConfig(path, settings.Module(), oldMod))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{db: db}
	recs := sampleRecords()
	e.insert(t, recs...)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = storage.Open(ctx, storage.DefaultConfig(path, settings.Module(), Module()))
	if err != nil {
		t.Fatalf("migration 3 on a database with 1 and 2: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	e = &env{db: db, st: settings.New(db)}
	if e.r, err = NewRollups(db, e.st, nil); err != nil {
		t.Fatal(err)
	}
	if n := len(e.rollupRows(t)); n != 0 {
		t.Fatalf("a fresh rollup table has %d rows", n)
	}
	// The records written before the migration are rolled up like any other.
	if n := e.runOnce(t); n != int64(len(recs)) {
		t.Fatalf("counted %d pre-existing records, want %d", n, len(recs))
	}
	e.checkRollups(t, recs)
}

// ---- the query ----

func (e *env) query(t *testing.T, q RollupQuery) RollupPage {
	t.Helper()
	p, err := QueryRollups(context.Background(), e.db.Reader(), e.st, q)
	if err != nil {
		t.Fatalf("query %+v: %v", q, err)
	}
	return p
}

func (e *env) seeded(t *testing.T) []rec {
	t.Helper()
	recs := sampleRecords()
	e.insert(t, recs...)
	e.runOnce(t)
	return recs
}

func TestQueryOrdersOldestFirstAndFilters(t *testing.T) {
	e := newEnv(t)
	recs := e.seeded(t)

	all := e.query(t, RollupQuery{Granularity: GranularityHour})
	if all.NextCursor != "" {
		t.Fatalf("a single page has a cursor: %q", all.NextCursor)
	}
	var want []bucketKey
	for k := range expected(recs) {
		if k.g == GranularityHour {
			want = append(want, k)
		}
	}
	sort.Slice(want, func(i, j int) bool {
		a, b := want[i], want[j]
		if a.start != b.start {
			return a.start < b.start
		}
		if a.consumer != b.consumer {
			return a.consumer < b.consumer
		}
		if a.key != b.key {
			return a.key < b.key
		}
		return a.model < b.model
	})
	if len(all.Rollups) != len(want) {
		t.Fatalf("%d rows, want %d", len(all.Rollups), len(want))
	}
	exp := expected(recs)
	for i, r := range all.Rollups {
		k := want[i]
		w := exp[k]
		if r.BucketStart.UnixMilli() != k.start || r.ConsumerID != k.consumer || r.AccessKeyID != k.key || r.Model != k.model || r.Granularity != GranularityHour ||
			r.Requests != w.requests || r.PromptTokens != w.prompt || r.CompletionTokens != w.completion || r.TotalTokens != w.total {
			t.Fatalf("row %d = %+v, want %+v %+v", i, r, k, w)
		}
		if r.BucketStart.Location() != time.UTC {
			t.Fatalf("row %d bucket is not UTC", i)
		}
	}

	count := func(q RollupQuery) int { return len(e.query(t, q).Rollups) }
	q := func(f func(*RollupQuery)) RollupQuery {
		x := RollupQuery{Granularity: GranularityHour}
		f(&x)
		return x
	}
	for _, r := range e.query(t, q(func(x *RollupQuery) { x.ConsumerID = consB })).Rollups {
		if r.ConsumerID != consB {
			t.Fatalf("consumer filter returned %s", r.ConsumerID)
		}
	}
	if n := count(q(func(x *RollupQuery) { x.ConsumerID = consB })); n != 3 {
		t.Fatalf("consumer B hour rows = %d, want 3", n)
	}
	if n := count(q(func(x *RollupQuery) { x.ConsumerID = "nobody" })); n != 0 {
		t.Fatalf("unknown consumer rows = %d", n)
	}
	if n := count(q(func(x *RollupQuery) { x.ConsumerID, x.AccessKeyID = consA, key2 })); n != 1 {
		t.Fatalf("consumer A key 2 rows = %d, want 1", n)
	}
	if n := count(q(func(x *RollupQuery) { x.ConsumerID, x.AccessKeyID = consB, key2 })); n != 0 {
		t.Fatalf("a key of another consumer returned %d rows", n)
	}
	if n := count(q(func(x *RollupQuery) { x.Model = "m2" })); n != 1 {
		t.Fatalf("model m2 rows = %d, want 1", n)
	}
	// From is inclusive, To exclusive: 11:00 to 12:00 is exactly the 11:00 bucket.
	from, to := day0.Add(11*time.Hour), day0.Add(12*time.Hour)
	for _, r := range e.query(t, q(func(x *RollupQuery) { x.From, x.To = from, to })).Rollups {
		if !r.BucketStart.Equal(from) {
			t.Fatalf("range returned bucket %v", r.BucketStart)
		}
	}
	if n := count(q(func(x *RollupQuery) { x.From, x.To = from, to })); n != 4 {
		t.Fatalf("11:00 bucket rows = %d, want 4", n)
	}
	if n := count(q(func(x *RollupQuery) { x.From = to })); n != 2 { // the 23:00 bucket and the next day's 00:00 bucket (consumer B)
		t.Fatalf("from 12:00 rows = %d, want 2", n)
	}
	if n := count(q(func(x *RollupQuery) { x.To = from })); n != 2 { // consumer A's 10:00 bucket and its previous-day 23:00 bucket
		t.Fatalf("to 11:00 rows = %d, want 2", n)
	}
	if n := count(q(func(x *RollupQuery) { x.From, x.To = to, from })); n != 0 {
		t.Fatalf("to before from returned %d rows, want an empty page", n)
	}
	// Minute and day granularity read their own buckets.
	if n := count(RollupQuery{Granularity: GranularityDay}); n != 6 {
		t.Fatalf("day rows = %d, want 6", n)
	}
	if n := count(RollupQuery{Granularity: GranularityMinute, ConsumerID: consA, AccessKeyID: key1, Model: "m1"}); n != 4 {
		t.Fatalf("minute rows for A, key 1, m1 = %d, want 4", n)
	}
}

func TestQueryPagesWithACursorAndCapsTheLimit(t *testing.T) {
	e := newEnv(t)
	recs := e.seeded(t)
	total := 0
	for k := range expected(recs) {
		if k.g == GranularityMinute {
			total++
		}
	}
	collect := func(limit int) []Rollup {
		var out []Rollup
		cursor := ""
		for pages := 0; ; pages++ {
			if pages > total+2 {
				t.Fatal("paging does not end")
			}
			p := e.query(t, RollupQuery{Granularity: GranularityMinute, Limit: limit, Cursor: cursor})
			if limit > 0 && len(p.Rollups) > limit {
				t.Fatalf("page of %d rows with limit %d", len(p.Rollups), limit)
			}
			out = append(out, p.Rollups...)
			if p.NextCursor == "" {
				return out
			}
			if len(p.Rollups) == 0 {
				t.Fatal("an empty page with a cursor")
			}
			cursor = p.NextCursor
		}
	}
	whole := collect(0)
	if len(whole) != total {
		t.Fatalf("%d rows, want %d", len(whole), total)
	}
	for _, limit := range []int{1, 2, 3, total, total + 5} {
		got := collect(limit)
		if len(got) != len(whole) {
			t.Fatalf("limit %d: %d rows, want %d", limit, len(got), len(whole))
		}
		for i := range got {
			if got[i] != whole[i] {
				t.Fatalf("limit %d: row %d = %+v, want %+v", limit, i, got[i], whole[i])
			}
		}
	}
	// A page that ends exactly on the last row has no cursor.
	if p := e.query(t, RollupQuery{Granularity: GranularityMinute, Limit: total}); p.NextCursor != "" || len(p.Rollups) != total {
		t.Fatalf("exact last page: %d rows, cursor %q", len(p.Rollups), p.NextCursor)
	}

	// The page-size setting is the default and the cap.
	if err := e.st.Set(context.Background(), settings.Host, KeyPageSize, "", "2"); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, 2, 50} {
		p := e.query(t, RollupQuery{Granularity: GranularityMinute, Limit: limit})
		if len(p.Rollups) != 2 || p.NextCursor == "" {
			t.Fatalf("limit %d with page_size 2: %d rows, cursor %q", limit, len(p.Rollups), p.NextCursor)
		}
	}
	if p := e.query(t, RollupQuery{Granularity: GranularityMinute, Limit: 1}); len(p.Rollups) != 1 {
		t.Fatalf("limit 1 returned %d rows", len(p.Rollups))
	}
	if got := collect(0); len(got) != len(whole) { // 2 per page, several pages
		t.Fatalf("paging with page_size 2 returned %d rows, want %d", len(got), len(whole))
	}
}

func TestQueryRejectsBadCursorAndGranularity(t *testing.T) {
	e := newEnv(t)
	e.seeded(t)
	ctx := context.Background()
	for _, c := range []string{"!!!", "bm90LWpzb24", "e30", "e30=", "eyJiIjoxfQ"} { // not base64url; not JSON; {} ; padded; {"b":1}
		_, err := QueryRollups(ctx, e.db.Reader(), e.st, RollupQuery{Granularity: GranularityDay, Cursor: c})
		if !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("cursor %q: err = %v, want ErrInvalidCursor", c, err)
		}
	}
	for _, g := range []Granularity{"", "week", "Minute"} {
		_, err := QueryRollups(ctx, e.db.Reader(), e.st, RollupQuery{Granularity: g})
		if !errors.Is(err, ErrInvalidGranularity) {
			t.Errorf("granularity %q: err = %v, want ErrInvalidGranularity", g, err)
		}
	}
}

func TestQueryNeverReturnsAnotherConsumersRows(t *testing.T) {
	e := newEnv(t)
	e.seeded(t)
	for _, g := range granularities {
		for _, cons := range []string{consA, consB} {
			cursor := ""
			for {
				p := e.query(t, RollupQuery{Granularity: g, ConsumerID: cons, Limit: 2, Cursor: cursor})
				for _, r := range p.Rollups {
					if r.ConsumerID != cons {
						t.Fatalf("consumer %s got a row of %s", cons, r.ConsumerID)
					}
				}
				if p.NextCursor == "" {
					break
				}
				cursor = p.NextCursor
			}
		}
	}
	// A cursor taken from another consumer's pages does not leak that consumer's rows either.
	other := e.query(t, RollupQuery{Granularity: GranularityMinute, ConsumerID: consB, Limit: 1})
	if other.NextCursor == "" {
		t.Fatal("expected a cursor")
	}
	p := e.query(t, RollupQuery{Granularity: GranularityMinute, ConsumerID: consA, Cursor: other.NextCursor})
	for _, r := range p.Rollups {
		if r.ConsumerID != consA {
			t.Fatalf("a foreign cursor returned a row of %s", r.ConsumerID)
		}
	}
}

func TestQueryDoesNotReadUseRecord(t *testing.T) {
	e := newEnv(t)
	e.seeded(t)
	// A record that is not rolled up yet is not in the answer: the query reads use_rollup only.
	e.insert(t, rec{consA, key1, "late", "completed", 1, 1, day0.Add(30 * time.Hour)})
	if n := len(e.query(t, RollupQuery{Granularity: GranularityDay, Model: "late"}).Rollups); n != 0 {
		t.Fatalf("an uncounted record showed up in the query (%d rows)", n)
	}
	e.runOnce(t)
	if n := len(e.query(t, RollupQuery{Granularity: GranularityDay, Model: "late"}).Rollups); n != 1 {
		t.Fatalf("a counted record is missing from the query (%d rows)", n)
	}
}

func TestDefaultsAreSettings(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if n, err := e.st.Int(ctx, KeyPageSize, ""); err != nil || n != 100 {
		t.Fatalf("page size default = %d, %v", n, err)
	}
	if n, err := e.st.Int(ctx, KeyRollupBatchRows, ""); err != nil || n < 1 {
		t.Fatalf("batch rows default = %d, %v", n, err)
	}
	if d, err := e.st.Duration(ctx, KeyRollupInterval, ""); err != nil || d != time.Minute {
		t.Fatalf("interval default = %v, %v", d, err)
	}
	if _, err := NewRollups(nil, e.st, nil); err == nil {
		t.Fatal("NewRollups accepted a nil DB")
	}
}
