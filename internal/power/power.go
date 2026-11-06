package power

import (
	"context"
	"errors"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// KeyCheckInterval is the setting that says how often the Keeper reads the sharing state. The
// host owns it (D-38, D-54).
const KeyCheckInterval = "power.check_interval"

// defaultCheckInterval is KeyCheckInterval's default, in the text form settings accepts. It is
// also how long the Keeper waits while the check is off, so a later change is noticed.
const defaultCheckInterval = "15s"

// Defs are the power settings; register them with settings.Store.Register.
func Defs() []settings.Def {
	return []settings.Def{
		{Key: KeyCheckInterval, Kind: settings.Duration, Default: defaultCheckInterval, Owner: settings.Host,
			Description: "How often the host's sharing state is read to take or release the sleep lock. Zero or less turns the sleep lock off."},
	}
}

// ErrToolMissing is returned by a Runner when the program it was asked to start is not installed.
// The OS adapters treat it as "no sleep lock on this host": they log it once and carry on.
var ErrToolMissing = errors.New("power: tool not installed")

// Power is the port to the OS sleep lock (req_001 D-30). One adapter exists per OS, chosen at
// build time by New.
type Power interface {
	// Acquire takes the sleep lock and returns the handle that releases it. Acquire never
	// returns a nil Lock without an error. An adapter whose OS tool is missing returns a Lock
	// that holds nothing, after logging the fact once, and no error.
	Acquire(ctx context.Context) (Lock, error)
}

// Lock is one held sleep lock.
type Lock interface {
	// Release gives the lock back and stops anything started for it. It is safe to call more
	// than once and from several goroutines; only the first call does anything.
	Release() error
	// Alive reports whether the lock is still held: false once it was released or the OS lock
	// vanished (the helper process died). A Lock that holds nothing by design reports true.
	Alive() bool
}

// noopLock is the Lock of an adapter that has nothing to hold.
type noopLock struct{}

func (noopLock) Release() error { return nil }
func (noopLock) Alive() bool    { return true }
