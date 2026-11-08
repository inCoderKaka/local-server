package consumers

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// Keys of the settings this package registers. All are owned by the host (req_001 D-38) and read
// at the moment of use, so a change applies to the next request.
const (
	KeyListPageSize      = "admin.list_page_size"
	KeyInviteTTL         = "invites.ttl"
	KeyInviteLinkBaseURL = "invites.link_base_url"
	// KeyDeletedRetention is how long a deleted consumer's consent records, sessions and access
	// keys are kept before the purge removes them (req_001 D-106, D-134, D-136).
	KeyDeletedRetention = "consumers.deleted_retention"
)

// Defs are the settings of the consumer, access key and invite operations; New registers them.
func Defs() []settings.Def {
	h := settings.Host
	return []settings.Def{
		{Key: KeyListPageSize, Kind: settings.Int, Default: "50", Owner: h,
			Description: "The most consumers or invites one list page returns, and the page size when the request does not ask for a number."},
		{Key: KeyInviteTTL, Kind: settings.Duration, Default: "24h", Owner: h,
			Description: "How long a new invite stays valid when the request gives no expiry. A change applies to invites created after it."},
		{Key: KeyInviteLinkBaseURL, Kind: settings.String, Default: "", Owner: h,
			Description: "The address of the consumer web page, for example https://my-host.example.ts.net, put in front of /invite#<token> to make an invite link. Empty gives the relative link /invite#<token>."},
		{Key: KeyDeletedRetention, Kind: settings.Duration, Default: "2160h", Owner: h,
			Description: "How long after a consumer was deleted its consent records, sessions and access keys are kept before they are removed for good. The default is 90 days. The consumer's id and its usage records stay. Zero or less keeps them forever. A change applies to the next purge run."},
	}
}

func registerSettings(st *settings.Store) error {
	if err := st.Register(Defs()...); err != nil {
		return err
	}
	if err := st.SetValidator(KeyListPageSize, func(_ context.Context, v string) error {
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
	if err := st.SetValidator(KeyInviteTTL, func(_ context.Context, v string) error {
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
	return st.SetValidator(KeyInviteLinkBaseURL, func(_ context.Context, v string) error { return checkLinkBase(v) })
}

// checkLinkBase accepts an empty value (a relative link) or an absolute http or https URL with a
// host and without user information, query or fragment, since the invite path and the token are
// appended to it.
func checkLinkBase(v string) error {
	if v == "" {
		return nil
	}
	const msg = "must be empty or an absolute http or https address with a host, without user information, query or fragment"
	if strings.ContainsAny(v, "?#") || strings.IndexFunc(v, func(r rune) bool { return r <= ' ' || r == 0x7f }) >= 0 {
		return fmt.Errorf("%s", msg)
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return fmt.Errorf("%s", msg)
	}
	return nil
}
