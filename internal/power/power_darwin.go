package power

import (
	"log/slog"
	"os"
)

// New returns the macOS Power: caffeinate -i holds the sleep lock. A Mac without caffeinate gets
// no lock, logged once.
func New(log *slog.Logger) Power {
	return newProcessPower(ExecRunner{}, darwinCommand, os.Getpid(), log)
}
