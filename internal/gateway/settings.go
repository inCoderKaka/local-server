package gateway

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// Keys of the gateway settings. All are owned by the host (req_001 D-38, D-54) and read at the
// moment of use, so a change applies to the next request.
const (
	KeyDefaultMaxTokens     = "gateway.default_max_tokens"
	KeyMaxTokensCeiling     = "gateway.max_tokens_ceiling"
	KeyDefaultContextTokens = "gateway.default_context_tokens"
	KeyCharsPerToken        = "gateway.chars_per_token"
	KeyMaxEmbeddingInputs   = "gateway.max_embedding_inputs"
	KeyEngineTimeout        = "gateway.engine_timeout"
	KeyStreamFirstToken     = "gateway.stream_first_token_timeout"
	KeyStreamIdle           = "gateway.stream_idle_timeout"
	KeyStreamTotal          = "gateway.stream_total_timeout"
	KeyHealthEngineTimeout  = "gateway.health_engine_timeout"
	KeyMeteringAttempts     = "gateway.metering_attempts"
	KeyMeteringRetryDelay   = "gateway.metering_retry_delay"
	KeyMeteringTimeout      = "gateway.metering_timeout"
	KeyRuleInvalidInterval  = "gateway.rule_invalid_event_interval"
	KeySLOOverhead          = "gateway.slo_overhead"
	KeySLOAdmission         = "gateway.slo_admission"
	KeySLOReceiptSigning    = "gateway.slo_receipt_signing"
)

// The defaults of the three service-level targets (req_001 D-31, D-55), derived from
// BenchmarkGatewayOverhead, BenchmarkAdmission and BenchmarkReceiptSigning on the fake engine
// (Apple M3, real SQLite in a temporary directory): 95th percentiles of about 5 ms, 0.18 ms and
// 0.019 ms. The overhead target is ten times its figure; the two microsecond figures get 25 and
// 50 times, because at that size the scheduler and the garbage collector dominate on a shared CI
// machine. The host may tighten or loosen each one at runtime.
const (
	DefaultSLOOverhead       = "50ms"
	DefaultSLOAdmission      = "5ms"
	DefaultSLOReceiptSigning = "1ms"
)

// Defs are the gateway settings; New registers them.
func Defs() []settings.Def {
	h := settings.Host
	return []settings.Def{
		{Key: KeyDefaultMaxTokens, Kind: settings.Int, Default: "1024", Owner: h,
			Description: "Tokens a chat request may generate when it sets neither max_tokens nor max_completion_tokens. The output cap still applies: the grant's or a policy rule's (the smaller when both set one), else gateway.max_tokens_ceiling."},
		{Key: KeyMaxTokensCeiling, Kind: settings.Int, Default: "8192", Owner: h,
			Description: "Most tokens a chat request may generate when neither the grant nor a policy rule sets an output cap. A request that asks for more is capped, not refused."},
		{Key: KeyDefaultContextTokens, Kind: settings.Int, Default: "4096", Owner: h,
			Description: "Context size in tokens sent to the engine when neither the grant nor a policy rule sets a context cap."},
		{Key: KeyCharsPerToken, Kind: settings.Int, Default: "4", Owner: h,
			Description: "Bytes of prompt text counted as one token when the gateway estimates the prompt size before the engine runs. With on_context_overflow refuse, a prompt whose estimate exceeds the context cap is refused with context_too_long before the engine runs, so a higher value makes this pre-check more lenient. The engine's own count is exact."},
		{Key: KeyMaxEmbeddingInputs, Kind: settings.Int, Default: "128", Owner: h,
			Description: "Most texts one embeddings request may carry."},
		{Key: KeyEngineTimeout, Kind: settings.Duration, Default: "5m", Owner: h,
			Description: "Longest a non-streamed engine call may take in total, from the request to the engine until its last chunk. A model load the scheduler runs during admission is not included; the engine adapter's load timeout bounds it."},
		{Key: KeyStreamFirstToken, Kind: settings.Duration, Default: "60s", Owner: h,
			Description: "Longest a streamed chat call may take from the request to the engine until the engine's first chunk (the model's reading of the prompt). When it expires the engine call is cancelled and the request fails with a 504. A model load the scheduler runs during admission is not included."},
		{Key: KeyStreamIdle, Kind: settings.Duration, Default: "30s", Owner: h,
			Description: "Longest a streamed chat call may wait for the engine's next chunk. When it expires the engine call is cancelled and the stream ends with an error event; the tokens so far are metered with outcome failed."},
		{Key: KeyStreamTotal, Kind: settings.Duration, Default: "10m", Owner: h,
			Description: "Longest a streamed chat call may take in total, from the request to the engine until its last chunk. When it expires the engine call is cancelled and the stream ends with an error event; the tokens so far are metered with outcome failed."},
		{Key: KeyHealthEngineTimeout, Kind: settings.Duration, Default: "2s", Owner: h,
			Description: "Longest GET /v1/health waits for the engine to list its loaded models. When it expires the engine is reported unavailable and the response is still sent."},
		{Key: KeyMeteringAttempts, Kind: settings.Int, Default: "3", Owner: h,
			Description: "Times the gateway tries to write a call's usage record before it answers with an unmetered receipt."},
		{Key: KeyMeteringRetryDelay, Kind: settings.Duration, Default: "100ms", Owner: h,
			Description: "Wait between two attempts to write a usage record."},
		{Key: KeyMeteringTimeout, Kind: settings.Duration, Default: "10s", Owner: h,
			Description: "Longest one attempt to write a usage record may take. It also bounds the gateway's write of a metering.failed or policy.rule_invalid event. Metering ignores the client disconnecting."},
		{Key: KeyRuleInvalidInterval, Kind: settings.Duration, Default: "5m", Owner: h,
			Description: "At most one policy.rule_invalid notification is raised per broken policy rule in this time."},
		{Key: KeySLOOverhead, Kind: settings.Duration, Default: DefaultSLOOverhead, Owner: h,
			Description: "Target for the gateway's own time per chat or embeddings call at the 95th percentile: everything except the engine call and Scheduler.Admit (its queue wait and any model load included), metering write included. The gateway counts each call that takes longer in gateway_slo_breaches_total (slo overhead), and the SLO tests hold the gateway to this value."},
		{Key: KeySLOAdmission, Kind: settings.Duration, Default: DefaultSLOAdmission, Owner: h,
			Description: "Target for the time Scheduler.Admit takes when a slot is free, at the 95th percentile; the SLO tests hold the gateway to this value. gateway_admission_seconds measures every Admit call in full, waiting for a busy slot and loading the model included, so under load it runs above this target."},
		{Key: KeySLOReceiptSigning, Kind: settings.Duration, Default: DefaultSLOReceiptSigning, Owner: h,
			Description: "Target for the time to sign one usage receipt, at the 95th percentile. The SLO tests hold the gateway to this value, and the gateway counts a slower unmetered-receipt signing in gateway_slo_breaches_total (slo receipt_signing)."},
	}
}

func setValidators(st *settings.Store) error {
	ints := map[string]int64{
		KeyDefaultMaxTokens: 1, KeyMaxTokensCeiling: 1, KeyDefaultContextTokens: 1,
		KeyCharsPerToken: 1, KeyMaxEmbeddingInputs: 1, KeyMeteringAttempts: 1,
	}
	for key, min := range ints {
		min := min
		err := st.SetValidator(key, func(_ context.Context, v string) error {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return err
			}
			if n < min {
				return fmt.Errorf("must be at least %d", min)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	for _, key := range []string{KeyEngineTimeout, KeyStreamFirstToken, KeyStreamIdle, KeyStreamTotal, KeyHealthEngineTimeout, KeyMeteringTimeout, KeyRuleInvalidInterval, KeySLOOverhead, KeySLOAdmission, KeySLOReceiptSigning} {
		err := st.SetValidator(key, func(_ context.Context, v string) error {
			d, err := time.ParseDuration(v)
			if err != nil {
				return err
			}
			if d <= 0 {
				return fmt.Errorf("must be greater than 0")
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return st.SetValidator(KeyMeteringRetryDelay, func(_ context.Context, v string) error {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		if d < 0 {
			return fmt.Errorf("must be at least 0")
		}
		return nil
	})
}

// config is one read of the gateway settings.
type config struct {
	defaultMaxTokens, maxTokensCeiling, defaultContext int64
	charsPerToken, maxEmbeddingInputs                  int64
	engineTimeout                                      time.Duration
	streamFirstToken, streamIdle, streamTotal          time.Duration
	meteringAttempts                                   int64
	meteringRetryDelay, meteringTimeout                time.Duration
	ruleInvalidInterval                                time.Duration
	sloOverhead, sloReceiptSigning                     time.Duration
}

func (g *Gateway) loadConfig(ctx context.Context) (config, error) {
	var c config
	var err error
	i := func(key string) int64 {
		if err != nil {
			return 0
		}
		var v int64
		v, err = g.cfg.Settings.Int(ctx, key, "")
		return v
	}
	d := func(key string) time.Duration {
		if err != nil {
			return 0
		}
		var v time.Duration
		v, err = g.cfg.Settings.Duration(ctx, key, "")
		return v
	}
	c.defaultMaxTokens, c.maxTokensCeiling, c.defaultContext = max(i(KeyDefaultMaxTokens), 1), max(i(KeyMaxTokensCeiling), 1), max(i(KeyDefaultContextTokens), 1)
	c.charsPerToken, c.maxEmbeddingInputs = max(i(KeyCharsPerToken), 1), max(i(KeyMaxEmbeddingInputs), 1)
	c.engineTimeout = d(KeyEngineTimeout)
	c.streamFirstToken, c.streamIdle, c.streamTotal = d(KeyStreamFirstToken), d(KeyStreamIdle), d(KeyStreamTotal)
	c.meteringAttempts = max(i(KeyMeteringAttempts), 1)
	c.meteringRetryDelay, c.meteringTimeout = max(d(KeyMeteringRetryDelay), 0), d(KeyMeteringTimeout)
	c.ruleInvalidInterval = d(KeyRuleInvalidInterval)
	c.sloOverhead, c.sloReceiptSigning = d(KeySLOOverhead), d(KeySLOReceiptSigning)
	return c, err
}
