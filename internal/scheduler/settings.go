package scheduler

import (
	"context"
	"strings"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// Keys of the share-budget and lifecycle settings. All are owned by the host
// (req_001 D-38, D-54) and read at the moment of use, so a change applies to the next
// decision.
const (
	KeyMemoryBudgetBytes    = "scheduler.memory_budget_bytes"
	KeyTotalConcurrency     = "scheduler.total_concurrency"
	KeyOllamaNumParallel    = "scheduler.ollama_num_parallel"
	KeyGrantConcurrency     = "scheduler.default_grant_concurrency"
	KeyQueueLength          = "scheduler.queue_length"
	KeyQueueWait            = "scheduler.queue_wait"
	KeyRetryAfter           = "scheduler.retry_after"
	KeyBackgroundAging      = "scheduler.background_aging"
	KeyContextBytesPerToken = "scheduler.context_bytes_per_token"
	KeyKeepAlive            = "scheduler.keep_alive"
	KeyPreloadModels        = "scheduler.preload_models"
	KeyMaintenanceInterval  = "scheduler.maintenance_interval"
	KeyRecheckInterval      = "scheduler.recheck_interval"
)

// Defs are the scheduler's settings; New registers them with the settings store.
func Defs() []settings.Def {
	h := settings.Host
	return []settings.Def{
		{Key: KeyMemoryBudgetBytes, Kind: settings.Int, Default: "0", Owner: h,
			Description: "Memory in bytes the host sets aside for loaded models and request contexts together. 0 means no limit."},
		{Key: KeyTotalConcurrency, Kind: settings.Int, Default: "4", Owner: h,
			Description: "Most requests the host runs at once for all consumers together. The effective limit is also capped by the Ollama parallel setting."},
		{Key: KeyOllamaNumParallel, Kind: settings.Int, Default: "1", Owner: h,
			Description: "The value of OLLAMA_NUM_PARALLEL that Ollama runs with. local-server never runs more requests than this at once, so Ollama's own queue stays empty."},
		{Key: KeyGrantConcurrency, Kind: settings.Int, Default: "1", Owner: h,
			Description: "Most requests one grant runs at once when the grant sets no limit of its own."},
		{Key: KeyQueueLength, Kind: settings.Int, Default: "16", Owner: h,
			Description: "Most requests that may wait for a slot. A request that finds the queue full is refused at once with a Retry-After."},
		{Key: KeyQueueWait, Kind: settings.Duration, Default: "30s", Owner: h,
			Description: "Longest an interactive request waits in the queue before it is refused with a Retry-After. Background work waits without a limit."},
		{Key: KeyRetryAfter, Kind: settings.Duration, Default: "5s", Owner: h,
			Description: "The Retry-After a refused request is told to wait."},
		{Key: KeyBackgroundAging, Kind: settings.Duration, Default: "30s", Owner: h,
			Description: "How much later than an interactive request a background request is treated as arriving. After waiting this long it goes ahead of newer interactive requests, so it never starves."},
		{Key: KeyContextBytesPerToken, Kind: settings.Int, Default: "131072", Owner: h,
			Description: "Estimated memory in bytes one context token takes, used to check a request against the memory budget."},
		{Key: KeyKeepAlive, Kind: settings.Duration, Default: "5m", Owner: h,
			Description: "How long an idle model stays loaded. A negative value keeps it until it is unloaded by hand or to make room."},
		{Key: KeyPreloadModels, Kind: settings.String, Default: "", Owner: h,
			Description: "Comma-separated models to load at start and keep loaded."},
		{Key: KeyMaintenanceInterval, Kind: settings.Duration, Default: "15s", Owner: h,
			Description: "How often the model lifecycle manager applies preload changes and unloads idle models."},
		{Key: KeyRecheckInterval, Kind: settings.Duration, Default: "1s", Owner: h,
			Description: "How often a waiting request re-reads the settings and the sharing state."},
	}
}

// config is one read of the settings.
type config struct {
	budget      int64
	total       int // effective: the smaller of total concurrency and OLLAMA_NUM_PARALLEL, at least 1
	grantCap    int
	queueLen    int
	queueWait   time.Duration
	retryAfter  time.Duration
	aging       time.Duration
	ctxBytes    int64
	keepAlive   time.Duration
	preload     []string
	maintenance time.Duration
	recheck     time.Duration
}

func (s *Scheduler) loadConfig(ctx context.Context) (config, error) {
	var c config
	var err error
	i := func(key string) int64 {
		if err != nil {
			return 0
		}
		var v int64
		v, err = s.st.Int(ctx, key, "")
		return v
	}
	d := func(key string) time.Duration {
		if err != nil {
			return 0
		}
		var v time.Duration
		v, err = s.st.Duration(ctx, key, "")
		return v
	}
	c.budget = max(i(KeyMemoryBudgetBytes), 0)
	total, par := i(KeyTotalConcurrency), i(KeyOllamaNumParallel)
	c.total = int(max(min(total, par), 1))
	c.grantCap = int(max(i(KeyGrantConcurrency), 1))
	c.queueLen = int(max(i(KeyQueueLength), 0))
	c.queueWait = d(KeyQueueWait)
	c.retryAfter = d(KeyRetryAfter)
	c.aging = max(d(KeyBackgroundAging), 0)
	c.ctxBytes = max(i(KeyContextBytesPerToken), 0)
	c.keepAlive = d(KeyKeepAlive)
	c.maintenance = max(d(KeyMaintenanceInterval), time.Millisecond)
	c.recheck = max(d(KeyRecheckInterval), time.Millisecond)
	if err != nil {
		return config{}, err
	}
	raw, perr := s.st.String(ctx, KeyPreloadModels, "")
	if perr != nil {
		return config{}, perr
	}
	for _, m := range strings.Split(raw, ",") {
		if m = strings.TrimSpace(m); m != "" {
			c.preload = append(c.preload, m)
		}
	}
	return c, nil
}
