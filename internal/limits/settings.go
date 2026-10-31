package limits

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// Keys of the limits settings. All are owned by the host (req_001 D-38, D-54) and read at the
// moment of use, so a change applies to the next decision.
const (
	KeyQuotaWindow       = "limits.quota_window"
	KeyNearLimitPercent  = "limits.near_limit_percent"
	KeyRateEnabled       = "limits.rate_enabled"
	KeyRateCapacity      = "limits.rate_capacity"
	KeyRateWindow        = "limits.rate_window"
	KeyBodyLimitEnabled  = "limits.body_limit_enabled"
	KeyMaxBodyBytes      = "limits.max_body_bytes"
	KeyLockoutEnabled    = "limits.lockout_enabled"
	KeyLockoutFailures   = "limits.lockout_failures"
	KeyLockoutWindow     = "limits.lockout_window"
	KeyLockoutDuration   = "limits.lockout_duration"
	KeyTrackerIdleTTL    = "limits.tracker_idle_ttl"
	KeyTrackerMaxEntries = "limits.tracker_max_entries"
)

// Defs are the limits settings; New registers them.
func Defs() []settings.Def {
	h := settings.Host
	return []settings.Def{
		{Key: KeyQuotaWindow, Kind: settings.Duration, Default: "24h", Owner: h,
			Description: "Length of the window a token or request quota counts over. Windows are fixed: each starts at a whole multiple of this length counted from 0001-01-01 00:00 UTC, so a length that divides 24h starts a window at every midnight UTC, and 168h starts one every Monday at midnight UTC. The quotas of keys and grants are stored per day; changing this changes what they mean."},
		{Key: KeyNearLimitPercent, Kind: settings.Int, Default: "80", Owner: h,
			Description: "Percent of a quota used at which one quota.near_limit notification is raised for that window. 0 turns the notification off."},
		{Key: KeyRateEnabled, Kind: settings.Bool, Default: "true", Owner: h,
			Description: "Whether requests per access key are rate limited."},
		{Key: KeyRateCapacity, Kind: settings.Int, Default: "60", Owner: h,
			Description: "Requests one access key may make in a burst, and per rate window once the burst is used."},
		{Key: KeyRateWindow, Kind: settings.Duration, Default: "1m", Owner: h,
			Description: "Time an empty request allowance takes to refill completely."},
		{Key: KeyBodyLimitEnabled, Kind: settings.Bool, Default: "true", Owner: h,
			Description: "Whether the size of a request body is limited."},
		{Key: KeyMaxBodyBytes, Kind: settings.Int, Default: "8388608", Owner: h,
			Description: "Largest request body in bytes; a larger one is refused."},
		{Key: KeyLockoutEnabled, Kind: settings.Bool, Default: "true", Owner: h,
			Description: "Whether repeated failed access key checks lock out the address and the key prefix."},
		{Key: KeyLockoutFailures, Kind: settings.Int, Default: "10", Owner: h,
			Description: "Failed access key checks from one address, or against one key prefix, within the lockout window that cause a lockout."},
		{Key: KeyLockoutWindow, Kind: settings.Duration, Default: "10m", Owner: h,
			Description: "Time over which failed access key checks are counted."},
		{Key: KeyLockoutDuration, Kind: settings.Duration, Default: "15m", Owner: h,
			Description: "How long an address or key prefix stays locked out."},
		{Key: KeyTrackerIdleTTL, Kind: settings.Duration, Default: "1h", Owner: h,
			Description: "How long an idle address or key prefix is remembered by the failure counters before it is dropped from memory."},
		{Key: KeyTrackerMaxEntries, Kind: settings.Int, Default: "10000", Owner: h,
			Description: "Most addresses and key prefixes the failure counters remember at once; the least recently seen is dropped to make room."},
	}
}

// setValidators makes the store refuse values that would break the limits: a zero threshold, a
// zero window, a body limit below one byte. The runtime clamps stay as a guard.
func setValidators(st *settings.Store) error {
	ints := []struct {
		key string
		min int64
	}{
		{KeyRateCapacity, 1}, {KeyMaxBodyBytes, 1}, {KeyLockoutFailures, 1}, {KeyTrackerMaxEntries, 1},
		{KeyNearLimitPercent, 0},
	}
	for _, k := range ints {
		min := k.min
		err := st.SetValidator(k.key, func(_ context.Context, v string) error {
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
	durations := []struct {
		key string
		min time.Duration // the value must be at least min; 0 means greater than 0
	}{
		{KeyRateWindow, 0}, {KeyLockoutWindow, 0}, {KeyLockoutDuration, 0}, {KeyTrackerIdleTTL, 0},
		{KeyQuotaWindow, time.Second},
	}
	for _, k := range durations {
		min := k.min
		err := st.SetValidator(k.key, func(_ context.Context, v string) error {
			d, err := time.ParseDuration(v)
			if err != nil {
				return err
			}
			if min == 0 && d <= 0 {
				return fmt.Errorf("must be greater than 0")
			}
			if d < min {
				return fmt.Errorf("must be at least %s", min)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
