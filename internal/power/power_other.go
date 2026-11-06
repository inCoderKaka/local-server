//go:build !darwin && !linux && !windows

package power

import (
	"context"
	"log/slog"
	"sync"
)

// New returns a Power for an OS with no sleep-lock adapter: it holds nothing, and says so once.
func New(log *slog.Logger) Power {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &unsupported{log: log}
}

type unsupported struct {
	log  *slog.Logger
	once sync.Once
}

func (u *unsupported) Acquire(context.Context) (Lock, error) {
	u.once.Do(func() { u.log.Warn("sleep lock unavailable: no adapter for this OS") })
	return noopLock{}, nil
}
