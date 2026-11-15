package requests

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// Keys of the settings this package registers. All are owned by the host (req_001 D-38, D-157)
// and read at the moment of use, so a change applies to the next call.
const (
	KeyTTL             = "requests.ttl"
	KeyMaxOpen         = "requests.max_open_per_consumer"
	KeyMaxCommentChars = "requests.max_comment_chars"
	KeySweepInterval   = "requests.sweep_interval"
	KeyPageSize        = "requests.page_size"
)

// defaultSweepInterval is the default of KeySweepInterval and the wait the sweeper uses when the
// setting cannot be read.
const defaultSweepInterval = time.Minute

// Defs are the settings of the requests module; New registers them. The defaults of the first
// three are req_001 D-157's.
func Defs() []settings.Def {
	h := settings.Host
	return []settings.Def{
		{Key: KeyTTL, Kind: settings.Duration, Default: "336h", Owner: h,
			Description: "How long a resource request stays open before it expires (336h is 14 days). A change applies to requests opened after it; a request already open keeps its end time."},
		{Key: KeyMaxOpen, Kind: settings.Int, Default: "5", Owner: h,
			Description: "The most open resource requests one consumer may have at once. A consumer at the limit must wait for a decision or withdraw one."},
		{Key: KeyMaxCommentChars, Kind: settings.Int, Default: "4000", Owner: h,
			Description: "The most characters in a resource request's message or in one comment."},
		{Key: KeySweepInterval, Kind: settings.Duration, Default: "1m", Owner: h,
			Description: "How often the host looks for open resource requests older than requests.ttl and expires them."},
		{Key: KeyPageSize, Kind: settings.Int, Default: "50", Owner: h,
			Description: "The most resource requests one list page returns, and the page size when the caller does not ask for a number."},
	}
}

func registerSettings(st *settings.Store) error {
	if err := st.Register(Defs()...); err != nil {
		return err
	}
	for _, key := range []string{KeyMaxOpen, KeyMaxCommentChars, KeyPageSize} {
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
	for _, key := range []string{KeyTTL, KeySweepInterval} {
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
	return nil
}
