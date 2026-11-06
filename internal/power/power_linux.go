package power

import (
	"log/slog"
	"os"
)

// New returns the Linux Power: a systemd-inhibit logind inhibitor holds the sleep lock, best
// effort. A host without systemd-inhibit gets no lock, logged once.
func New(log *slog.Logger) Power {
	return newProcessPower(ExecRunner{}, linuxCommand, os.Getpid(), log)
}
