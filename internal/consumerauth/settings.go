package consumerauth

import (
	"context"
	"fmt"
	"strconv"
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

// KeyPasskeyCeremonyTTL is the host-owned setting for how long a started passkey ceremony (a
// registration or a login that has begun and not finished) can be finished. It is read when the
// ceremony begins, and the browser is told the same time as the ceremony's timeout (req_001
// D-38).
const KeyPasskeyCeremonyTTL = "consumer.passkey_ceremony_ttl"

// KeyPasskeyMaxCeremonies is the host-owned setting for how many started passkey ceremonies the
// host remembers at once. The begin routes need no login, so the count of what they leave behind
// is bounded; the oldest one is dropped to make room.
const KeyPasskeyMaxCeremonies = "consumer.passkey_max_ceremonies"

// KeyConsentUserAgentMax is the host-owned setting for how many characters of the browser user
// agent a sign-up stores as consent evidence; the rest is cut off (req_001 D-153, D-38).
const KeyConsentUserAgentMax = "consumer.consent_user_agent_max"

// Defs are the settings this module registers.
func Defs() []settings.Def {
	return []settings.Def{
		{Key: KeySessionTTL, Kind: settings.Duration, Default: "24h", Owner: settings.Host,
			Description: "How long a consumer login on the consumer page lasts before the consumer must log in again. A change applies to logins after it; a session already started keeps its end time."},
		{Key: KeyPasskeyCeremonyTTL, Kind: settings.Duration, Default: "5m", Owner: settings.Host,
			Description: "How long a started passkey sign-up, login or registration can be finished before the consumer must start it again. A ceremony already started keeps its end time."},
		{Key: KeyPasskeyMaxCeremonies, Kind: settings.Int, Default: "1000", Owner: settings.Host,
			Description: "Most started passkey ceremonies the host remembers at once; the oldest is dropped to make room."},
		{Key: KeyConsentUserAgentMax, Kind: settings.Int, Default: "256", Owner: settings.Host,
			Description: "Most characters of the browser user agent kept in the consent record of a new sign-up; the rest is cut off. A change applies to sign-ups after it."},
	}
}

func registerSettings(st *settings.Store) error {
	if err := st.Register(Defs()...); err != nil {
		return err
	}
	for _, key := range []string{KeySessionTTL, KeyPasskeyCeremonyTTL} {
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
	for _, key := range []string{KeyPasskeyMaxCeremonies, KeyConsentUserAgentMax} {
		if err := st.SetValidator(key, func(_ context.Context, v string) error {
			n, err := strconv.Atoi(v)
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
	return nil
}
