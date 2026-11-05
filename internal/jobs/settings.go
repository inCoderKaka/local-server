package jobs

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// Keys of the job settings (req_001 D-38, D-54). Every value is read at the moment of use, so a
// change applies to the next decision.
const (
	// KeyDeadline is consumer-owned, one value per consumer id: how long a job may wait queued.
	KeyDeadline = "jobs.deadline"
	// KeyResultTTL is consumer-owned, one value per consumer id: how long an unfetched result is kept.
	KeyResultTTL = "jobs.result_ttl"
	// KeyPollInterval is host-owned: how often the worker looks for work and sweeps.
	KeyPollInterval = "jobs.poll_interval"
	// KeyRetryInterval is host-owned: how long a job waits before the worker tries it again.
	KeyRetryInterval = "jobs.retry_interval"
	// KeyMaxActive is host-owned: how many jobs the worker hands to the scheduler at once.
	KeyMaxActive = "jobs.max_active"
	// KeyRetention is host-owned: how long a finished job's record is kept (req_001 D-40, D-54).
	KeyRetention = "jobs.retention"
)

// Defs are the job settings; New registers them.
func Defs() []settings.Def {
	c, h := settings.Consumer, settings.Host
	return []settings.Def{
		{Key: KeyDeadline, Kind: settings.Duration, Default: "24h", Owner: c, Scoped: true,
			Description: "How long a job may wait queued before it expires, counted from when it was submitted. A job that has started is not expired. 0s means a job never expires. The value in force when the job is submitted applies to it."},
		{Key: KeyResultTTL, Kind: settings.Duration, Default: "24h", Owner: c, Scoped: true,
			Description: "How long a job's result is kept for fetching, counted from when the job finished. A result is also deleted once a fetch has delivered it whole. The value in force when the job finishes applies to its result."},
		{Key: KeyPollInterval, Kind: settings.Duration, Default: "2s", Owner: h,
			Description: "How often the job worker looks for queued jobs to start, expires jobs past their deadline and deletes results past their time-to-live."},
		{Key: KeyRetryInterval, Kind: settings.Duration, Default: "30s", Owner: h,
			Description: "How long a queued job waits before the worker tries it again after a failure that may pass (the model engine is down, the key's request allowance is empty, a policy time window is closed with no known opening time). A full admission queue uses the scheduler's own Retry-After instead."},
		{Key: KeyMaxActive, Kind: settings.Int, Default: "4", Owner: h,
			Description: "Most jobs the worker hands to the scheduler at once, running or waiting for a slot. Jobs beyond it wait in the job queue, oldest first. Raise it together with scheduler.total_concurrency."},
		{Key: KeyRetention, Kind: settings.Duration, Default: "0s", Owner: h,
			Description: "How long a finished job's record (its state and times, never content) is kept after it reached a terminal state. A job whose result can still be fetched is kept until the result is fetched whole or passes its time-to-live (jobs.result_ttl). Deleting a record frees its Idempotency-Key, so a resubmission with that key starts a new job. Zero or less keeps records forever."},
	}
}

// defaultDuration is the registered default of a Duration setting, used only when the store cannot be read.
func defaultDuration(key string) time.Duration {
	for _, d := range Defs() {
		if d.Key == key {
			if v, err := time.ParseDuration(d.Default); err == nil {
				return v
			}
		}
	}
	return 0
}

func setValidators(st *settings.Store) error {
	positive := func(_ context.Context, v string) error {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		if d <= 0 {
			return fmt.Errorf("must be greater than 0")
		}
		return nil
	}
	notNegative := func(_ context.Context, v string) error {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		if d < 0 {
			return fmt.Errorf("must be at least 0")
		}
		return nil
	}
	for key, fn := range map[string]func(context.Context, string) error{
		KeyResultTTL: positive, KeyPollInterval: positive, KeyRetryInterval: positive, KeyDeadline: notNegative,
	} {
		if err := st.SetValidator(key, fn); err != nil {
			return err
		}
	}
	return st.SetValidator(KeyMaxActive, func(_ context.Context, v string) error {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return err
		}
		if n < 1 {
			return fmt.Errorf("must be at least 1")
		}
		return nil
	})
}
