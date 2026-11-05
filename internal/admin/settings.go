package admin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// Keys of the admin settings. All are owned by the host (req_001 D-38, D-54) and read at the
// moment of use, so a change applies to the next request.
const (
	KeyMaxBodyBytes         = "admin.max_body_bytes"
	KeySessionTTL           = "admin.session_ttl"
	KeyEngineProbeTimeout   = "admin.engine_probe_timeout"
	KeyMetricsHistoryRange  = "admin.metrics_history_default_range"
	KeyMetricsHistoryPoints = "admin.metrics_history_max_points"
	KeyTailnetName          = "admin.tailnet_name"
	defaultMaxBodyBytes     = 1 << 20
	maxTailnetNameLen       = 253
	maxTailnetLabelLen      = 63
)

// Defs are the admin settings; New registers them.
func Defs() []settings.Def {
	h := settings.Host
	return []settings.Def{
		{Key: KeyMaxBodyBytes, Kind: settings.Int, Default: strconv.Itoa(defaultMaxBodyBytes), Owner: h,
			Description: "Largest request body, in bytes, the admin API reads; a larger one is refused with 413. It also bounds the longest password."},
		{Key: KeySessionTTL, Kind: settings.Duration, Default: "12h", Owner: h,
			Description: "How long an admin login lasts before the admin must log in again. A change applies to logins after it; a session already started keeps its end time."},
		{Key: KeyEngineProbeTimeout, Kind: settings.Duration, Default: "2s", Owner: h,
			Description: "Longest the admin API waits for the model engine when it reports whether the engine answers."},
		{Key: KeyMetricsHistoryRange, Kind: settings.Duration, Default: "1h", Owner: h,
			Description: "How far back the host-metrics history reaches when the request gives no start time."},
		{Key: KeyMetricsHistoryPoints, Kind: settings.Int, Default: "1440", Owner: h,
			Description: "Most per-minute samples one host-metrics history answer holds. A longer range returns only the newest samples."},
		{Key: KeyTailnetName, Kind: settings.String, Default: "", Owner: h,
			Description: "The tailnet name of this host (for example my-host.example.ts.net) that the admin listener accepts in the Host header besides localhost and loopback addresses. Empty means none. Applies to the next request."},
	}
}

// registerSettings registers the admin settings and their validators.
func registerSettings(st *settings.Store) error {
	if err := st.Register(Defs()...); err != nil {
		return err
	}
	for _, key := range []string{KeyMaxBodyBytes, KeyMetricsHistoryPoints} {
		if err := st.SetValidator(key, func(_ context.Context, v string) error {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return err
			}
			if n < 1 {
				return fmt.Errorf("must be at least 1")
			}
			return nil
		}); err != nil {
			return err
		}
	}
	for _, key := range []string{KeySessionTTL, KeyEngineProbeTimeout, KeyMetricsHistoryRange} {
		if err := st.SetValidator(key, func(_ context.Context, v string) error {
			d, err := time.ParseDuration(v)
			if err != nil {
				return err
			}
			if d <= 0 {
				return fmt.Errorf("must be greater than 0")
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return st.SetValidator(KeyTailnetName, func(_ context.Context, v string) error { return checkTailnetName(v) })
}

// checkTailnetName accepts a bare host name: dot-separated labels of 1 to 63 letters, digits or
// hyphens, none starting or ending with a hyphen, with one optional trailing dot (the guard
// removes it before comparing). Empty is allowed (no extra name). A scheme, a port or a path is
// refused, because the guard compares names without them.
func checkTailnetName(v string) error {
	if v == "" {
		return nil
	}
	if len(v) > maxTailnetNameLen {
		return fmt.Errorf("must be at most %d characters", maxTailnetNameLen)
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
			return fmt.Errorf("must be a host name: letters, digits, dots and hyphens, without a scheme, port or path")
		}
	}
	for _, label := range strings.Split(strings.TrimSuffix(v, "."), ".") {
		if len(label) < 1 || len(label) > maxTailnetLabelLen || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("must be a host name: dot-separated labels of 1 to %d letters, digits or hyphens, none starting or ending with a hyphen", maxTailnetLabelLen)
		}
	}
	return nil
}

func (a *Admin) intSetting(ctx context.Context, key string) (int64, error) {
	return a.cfg.Settings.Int(ctx, key, "")
}

func (a *Admin) durationSetting(ctx context.Context, key string) (time.Duration, error) {
	return a.cfg.Settings.Duration(ctx, key, "")
}
