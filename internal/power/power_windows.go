package power

import (
	"fmt"
	"log/slog"
	"syscall"
)

var setThreadExecutionState = syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadExecutionState")

// callSetThreadExecutionState calls kernel32's SetThreadExecutionState; it returns 0 on failure.
func callSetThreadExecutionState(flags uintptr) error {
	if r, _, err := setThreadExecutionState.Call(flags); r == 0 {
		return fmt.Errorf("call failed: %v", err)
	}
	return nil
}

// New returns the Windows Power: SetThreadExecutionState holds the sleep lock. log is unused: the
// call is always present.
func New(*slog.Logger) Power { return newThreadPower(callSetThreadExecutionState) }
