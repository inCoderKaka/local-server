package scheduler

import (
	"fmt"
	"time"
)

// Code is a typed scheduler error code. QueueFull and HostPaused are the API error
// codes of lld section 9 (spelled the same).
type Code string

// Error codes.
const (
	// CodeQueueFull means the admission queue is full or the queue-wait limit passed.
	CodeQueueFull Code = "queue_full"
	// CodeHostPaused means the host is paused or in quiet hours.
	CodeHostPaused Code = "host_paused"
	// CodeOverBudget means the model plus the estimated context can never fit the
	// memory budget.
	CodeOverBudget Code = "over_budget"
)

// Error is the typed error Admit returns for a refusal. Its message never contains
// request content (req_001 D-31, D-40).
type Error struct {
	Code Code
	// RetryAfter is how long the caller should wait before retrying; it becomes the
	// Retry-After header. Zero when retrying will not help (CodeOverBudget) or when the end
	// is not known (CodeHostPaused).
	RetryAfter time.Duration
	Message    string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return "scheduler: " + string(e.Code)
	}
	return fmt.Sprintf("scheduler: %s: %s", e.Code, e.Message)
}

// Is matches on Code, so errors.Is(err, ErrQueueFull) works.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// Sentinels for errors.Is; use errors.As to read RetryAfter.
var (
	ErrQueueFull  = &Error{Code: CodeQueueFull}
	ErrHostPaused = &Error{Code: CodeHostPaused}
	ErrOverBudget = &Error{Code: CodeOverBudget}
)
