package grants

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/policy"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/settings"
)

// Keys of the settings this package registers. All are owned by the host (req_001 D-38) and read
// at the moment of use.
const (
	KeyRuleMaxFailures     = "policy.rule.max_failures"
	KeyRuleCheckInterval   = "policy.rule.check_interval"
	KeyModelDefaultAllowed = "admin.model_default_allowed"
)

// defaultRuleCheckInterval is the default of KeyRuleCheckInterval and the wait the sweeper uses
// when the setting cannot be read.
const defaultRuleCheckInterval = 30 * time.Second

// Defs are the settings of the grant, model and policy rule operations; New registers them.
func Defs() []settings.Def {
	h := settings.Host
	return []settings.Def{
		{Key: KeyRuleMaxFailures, Kind: settings.Int, Default: "5", Owner: h,
			Description: "How many failed evaluations a policy rule without `deny` may have before the host switches it off. A rule with `deny` is never switched off."},
		{Key: KeyRuleCheckInterval, Kind: settings.Duration, Default: defaultRuleCheckInterval.String(), Owner: h,
			Description: "How often the host looks for policy rules that failed too many times."},
		{Key: KeyModelDefaultAllowed, Kind: settings.Bool, Default: "false", Owner: h,
			Description: "Whether a model the host sees for the first time may be put in grants at once. Off means the host opens each model for sharing itself."},
	}
}

// borrowed returns the definitions of other modules' settings that this package reads or
// writes. Registering an identical definition again is allowed, so the order of building the
// modules does not matter.
func borrowed() []settings.Def {
	var out []settings.Def
	out = append(out, limits.Defs()...)
	out = append(out, policy.Defs()...)
	for _, d := range consumers.Defs() {
		if d.Key == consumers.KeyListPageSize {
			out = append(out, d)
		}
	}
	for _, d := range scheduler.Defs() {
		if d.Key == scheduler.KeyGrantConcurrency || d.Key == scheduler.KeyPreloadModels {
			out = append(out, d)
		}
	}
	return out
}

func registerSettings(st *settings.Store) error {
	if err := st.Register(borrowed()...); err != nil {
		return err
	}
	if err := st.Register(Defs()...); err != nil {
		return err
	}
	if err := st.SetValidator(KeyRuleMaxFailures, func(_ context.Context, v string) error {
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
	return st.SetValidator(KeyRuleCheckInterval, func(_ context.Context, v string) error {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		if d <= 0 {
			return fmt.Errorf("must be greater than 0")
		}
		return nil
	})
}
