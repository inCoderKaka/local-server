package usage

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// Keys of the usage settings. All are owned by the host and read at the moment of use
// (req_001 D-38).
const (
	KeyRollupInterval     = "usage.rollup.interval"
	KeyRollupBatchRows    = "usage.rollup.batch_rows"
	KeyMinuteRetention    = "usage.rollup.minute_retention"
	KeyHourRetention      = "usage.rollup.hour_retention"
	KeyDayRetention       = "usage.rollup.day_retention"
	KeyPageSize           = "usage.page_size"
	defaultRollupInterval = time.Minute
)

// Defs are the usage settings; NewRollups registers them. The retention defaults are 7 days
// of minute buckets, 90 days of hour buckets and day buckets forever (req_001 D-40).
func Defs() []settings.Def {
	h := settings.Host
	return []settings.Def{
		{Key: KeyRollupInterval, Kind: settings.Duration, Default: defaultRollupInterval.String(), Owner: h,
			Description: "How often the usage rollup worker adds new usage records to the minute, hour and day totals. Must be greater than zero. A change applies after the next pass."},
		{Key: KeyRollupBatchRows, Kind: settings.Int, Default: "5000", Owner: h,
			Description: "Most usage records one rollup step reads at a time, so the database is never locked for long while the worker catches up. Must be at least 1."},
		{Key: KeyMinuteRetention, Kind: settings.Duration, Default: "168h", Owner: h,
			Description: "How long the per-minute usage totals are kept. Zero or less keeps them forever. The usage records themselves are always kept."},
		{Key: KeyHourRetention, Kind: settings.Duration, Default: "2160h", Owner: h,
			Description: "How long the per-hour usage totals are kept. Zero or less keeps them forever. The usage records themselves are always kept."},
		{Key: KeyDayRetention, Kind: settings.Duration, Default: "0s", Owner: h,
			Description: "How long the per-day usage totals are kept. Zero or less keeps them forever. The usage records themselves are always kept."},
		{Key: KeyPageSize, Kind: settings.Int, Default: "100", Owner: h,
			Description: "Most usage rows one page of the usage endpoint returns. A request may ask for fewer, never more. Must be at least 1."},
	}
}

// Rollups adds use_record rows to use_rollup in the background (req_001 D-27, D-40, D-104).
// Build it with NewRollups and run it with Run. It is safe for concurrent use: every step runs
// in the storage layer's single writer.
type Rollups struct {
	db  *storage.DB
	st  *settings.Store
	tel telemetry.Telemetry
}

// NewRollups registers the usage settings, their validators and the three retention rules of
// use_rollup with st, and returns the worker. A nil tel means telemetry.Nop().
func NewRollups(db *storage.DB, st *settings.Store, tel telemetry.Telemetry) (*Rollups, error) {
	if db == nil || st == nil {
		return nil, errors.New("usage: rollups need a DB and a settings store")
	}
	if tel == nil {
		tel = telemetry.Nop()
	}
	if err := st.Register(Defs()...); err != nil {
		return nil, err
	}
	for _, key := range []string{KeyRollupInterval, KeyRollupBatchRows, KeyPageSize} {
		isDuration := key == KeyRollupInterval
		if err := st.SetValidator(key, func(_ context.Context, v string) error { return checkPositive(v, isDuration) }); err != nil {
			return nil, err
		}
	}
	// One rollup table, three rules: each granularity has its own retention setting (D-40).
	var rules []settings.Rule
	for _, r := range []struct {
		g   Granularity
		key string
	}{
		{GranularityMinute, KeyMinuteRetention}, {GranularityHour, KeyHourRetention}, {GranularityDay, KeyDayRetention},
	} {
		rules = append(rules, settings.Rule{Table: "use_rollup", TimeColumn: "bucket_start", Setting: r.key,
			Where: "granularity = '" + string(r.g) + "'"})
	}
	if err := st.RegisterRetention(rules...); err != nil {
		return nil, err
	}
	return &Rollups{db: db, st: st, tel: tel}, nil
}

func checkPositive(v string, isDuration bool) error {
	if isDuration {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		if d <= 0 {
			return errors.New("must be greater than 0")
		}
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return err
	}
	if n < 1 {
		return errors.New("must be at least 1")
	}
	return nil
}

// RunOnce adds every use_record row not yet counted to its minute, hour and day buckets and
// returns how many rows it added. It works in steps of usage.rollup.batch_rows rows; each step
// is one unit of work that adds the sums and moves the watermark together, so a record is
// counted exactly once even if the process stops between steps.
func (r *Rollups) RunOnce(ctx context.Context) (int64, error) {
	size, err := r.st.Int(ctx, KeyRollupBatchRows, "")
	if err != nil {
		return 0, err
	}
	if size < 1 {
		size = 1
	}
	var total int64
	for {
		n, err := r.step(ctx, size)
		total += n
		if err != nil {
			return total, err
		}
		if n < size {
			return total, nil
		}
		if err := ctx.Err(); err != nil {
			return total, err
		}
	}
}

// step rolls up at most limit uncounted rows and returns how many it counted.
func (r *Rollups) step(ctx context.Context, limit int64) (int64, error) {
	var n int64
	err := r.db.Write(ctx, func(tx *storage.Tx) error {
		n = 0
		var last, upTo int64
		// Aggregates always return a row: 0 before the first pass.
		if err := tx.QueryRow(`SELECT COALESCE(MAX(last_rowid), 0) FROM use_rollup_state WHERE id = 1`).Scan(&last); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT COALESCE(MAX(rowid), 0), COUNT(*) FROM (SELECT rowid FROM use_record WHERE rowid > ? ORDER BY rowid LIMIT ?)`,
			last, limit).Scan(&upTo, &n); err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		for _, g := range granularities {
			ms := g.Size().Milliseconds()
			// The sums come straight from use_record for every granularity, not from the minute
			// buckets, which are pruned long before the day buckets are. Buckets are aligned to
			// the Unix epoch, so they are UTC minutes, hours and days.
			if _, err := tx.Exec(`INSERT INTO use_rollup
  (granularity, bucket_start, consumer_id, access_key_id, model, requests, prompt_tokens, completion_tokens, total_tokens)
SELECT ?, bucket, consumer_id, access_key_id, model, count(*), sum(prompt_tokens), sum(completion_tokens), sum(total_tokens)
FROM (SELECT (finished_at / ?) * ? AS bucket, consumer_id, access_key_id, model, prompt_tokens, completion_tokens, total_tokens
      FROM use_record WHERE rowid > ? AND rowid <= ?)
WHERE true
GROUP BY bucket, consumer_id, access_key_id, model
ON CONFLICT (granularity, bucket_start, consumer_id, access_key_id, model) DO UPDATE SET
  requests = requests + excluded.requests,
  prompt_tokens = prompt_tokens + excluded.prompt_tokens,
  completion_tokens = completion_tokens + excluded.completion_tokens,
  total_tokens = total_tokens + excluded.total_tokens`,
				string(g), ms, ms, last, upTo); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`INSERT INTO use_rollup_state (id, last_rowid) VALUES (1, ?)
ON CONFLICT (id) DO UPDATE SET last_rowid = excluded.last_rowid`, upTo)
		return err
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// Run does a pass now and then every usage.rollup.interval (read after each pass, so a change
// applies to the next wait) until ctx ends. A failed pass is logged (an error, never record
// content) and the loop continues.
func (r *Rollups) Run(ctx context.Context) {
	for {
		if _, err := r.RunOnce(ctx); err != nil && ctx.Err() == nil {
			r.tel.Logger().ErrorContext(ctx, "usage rollup: pass failed", "error", err)
		}
		wait, err := r.st.Duration(ctx, KeyRollupInterval, "")
		if err != nil || wait <= 0 {
			if err != nil && ctx.Err() == nil {
				r.tel.Logger().ErrorContext(ctx, "usage rollup: read interval", "error", err)
			}
			wait = defaultRollupInterval
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
