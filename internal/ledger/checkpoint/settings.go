package checkpoint

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// Keys of the settings this package registers. All are owned by the host (req_001 D-38,
// D-54) and read at the moment of use, so a change applies to the next decision.
const (
	KeyMinLeaves        = "ledger.checkpoint_min_leaves"
	KeyMaxInterval      = "ledger.checkpoint_max_interval"
	KeyPollInterval     = "ledger.checkpoint_poll_interval"
	KeyOrigin           = "ledger.checkpoint_origin"
	KeyKeyCheckInterval = "ledger.key_check_interval"
)

// defaultPollInterval is the default of KeyPollInterval and the fallback when the setting
// cannot be read.
const defaultPollInterval = 5 * time.Second

// Defs are the checkpoint settings; New registers them with the settings store.
func Defs() []settings.Def {
	h := settings.Host
	return []settings.Def{
		{Key: KeyMinLeaves, Kind: settings.Int, Default: "100", Owner: h,
			Description: "A new ledger checkpoint is signed as soon as this many leaves have been added since the last one."},
		{Key: KeyMaxInterval, Kind: settings.Duration, Default: "1m", Owner: h,
			Description: "A new ledger checkpoint is signed when this much time has passed since the last one and at least one leaf is new. Also the Retry-After a consumer is told while a receipt's proof waits for its checkpoint."},
		{Key: KeyPollInterval, Kind: settings.Duration, Default: defaultPollInterval.String(), Owner: h,
			Description: "How often the checkpoint worker looks at the ledger to see whether a checkpoint is due."},
		{Key: KeyOrigin, Kind: settings.String, Default: "", Owner: h,
			Description: "Origin line of ledger checkpoints, also the name of the checkpoint key in the signed note: no white space and no plus sign. Empty means local-server/ledger/ followed by the host id. It can be changed only until the first checkpoint is signed; after that a change is refused so the log name never forks."},
		{Key: KeyKeyCheckInterval, Kind: settings.Duration, Default: "1h", Owner: h,
			Description: "How often the host checks whether a receipt or checkpoint signing key has reached its rotation interval."},
	}
}

// setValidators makes the store refuse values that would stall or spin the worker: fewer than
// one leaf, or an interval of zero or less. The worker's guards stay as a backstop.
func setValidators(st *settings.Store) error {
	err := st.SetValidator(KeyMinLeaves, func(_ context.Context, v string) error {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return err
		}
		if n < 1 {
			return fmt.Errorf("must be at least 1")
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, key := range []string{KeyMaxInterval, KeyPollInterval, KeyKeyCheckInterval} {
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
	return nil
}
