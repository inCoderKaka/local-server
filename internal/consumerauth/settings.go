package consumerauth

import (
	"context"
	"fmt"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// KeySessionTTL is the host-owned setting for how long a consumer login lasts. It is named like
// the admin's admin.session_ttl and read at the moment a session starts (req_001 D-38, D-54).
//
// The login lockout is not a new setting: a consumer's failed logins count with the same
// host-owned auth.login_lockout.* settings as the admin's (credential.RegisterLockout), but in
// the consumer page's own tracker, so a locked-out consumer never locks an admin out or the
// other way round. The password cost and minimum length are the shared auth.password.* settings.
const KeySessionTTL = "consumer.session_ttl"

// Defs are the settings this module registers.
func Defs() []settings.Def {
	return []settings.Def{
		{Key: KeySessionTTL, Kind: settings.Duration, Default: "24h", Owner: settings.Host,
			Description: "How long a consumer login on the consumer page lasts before the consumer must log in again. A change applies to logins after it; a session already started keeps its end time."},
	}
}

func registerSettings(st *settings.Store) error {
	if err := st.Register(Defs()...); err != nil {
		return err
	}
	return st.SetValidator(KeySessionTTL, func(_ context.Context, v string) error {
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
