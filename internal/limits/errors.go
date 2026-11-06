package limits

import (
	"errors"
	"fmt"
	"time"

	"github.com/inCoderKaka/local-server/internal/grants"
)

// Sentinels that the typed errors match with errors.Is.
var (
	ErrQuotaExceeded = errors.New("limits: quota exceeded")
	ErrRateLimited   = errors.New("limits: rate limit exceeded")
	ErrTooLarge      = errors.New("limits: request body too large")
)

// API error codes (api/openapi.yaml). A too-large body has no typed code: it answers 413 with a
// null code.
const (
	CodeQuotaExceeded     = "quota_exceeded"
	CodeRateLimitExceeded = "rate_limit_exceeded"
)

// Quota scopes, as in the QuotaStatus schema.
const (
	ScopeGrant     = "grant"
	ScopeAccessKey = "access_key"
)

// Units a quota is counted in: the metrics of a quota entry (req_001 D-107).
const (
	UnitTokens   = grants.MetricTokens
	UnitRequests = grants.MetricRequests
)

// QuotaError means a token or request quota is used up. The gateway answers 429 quota_exceeded with
// RetryAfter, rounded up to whole seconds, in the Retry-After header.
type QuotaError struct {
	Scope      string // ScopeGrant or ScopeAccessKey
	Unit       string // UnitTokens or UnitRequests (req_001 D-107)
	Limit      int64
	Used       int64
	Window     time.Duration
	ResetsAt   time.Time
	RetryAfter time.Duration // always positive
}

func (e *QuotaError) Error() string {
	return fmt.Sprintf("limits: %s %s quota used up (%d of %d), retry in %s", e.Scope, e.Unit, e.Used, e.Limit, e.RetryAfter.Round(time.Second))
}

// Is matches ErrQuotaExceeded.
func (e *QuotaError) Is(target error) bool { return target == ErrQuotaExceeded }

// Code returns CodeQuotaExceeded.
func (e *QuotaError) Code() string { return CodeQuotaExceeded }

// RateLimitError means the request rate is too high or the caller is locked out after failed
// key checks. The gateway answers 429 rate_limit_exceeded with RetryAfter, rounded up to whole seconds, in the Retry-After header.
type RateLimitError struct {
	LockedOut  bool          // true for a lockout, false for the request rate
	RetryAfter time.Duration // always positive
}

func (e *RateLimitError) Error() string {
	if e.LockedOut {
		return fmt.Sprintf("limits: locked out after failed key checks, retry in %s", e.RetryAfter.Round(time.Second))
	}
	return fmt.Sprintf("limits: rate limit exceeded, retry in %s", e.RetryAfter.Round(time.Millisecond))
}

// Is matches ErrRateLimited.
func (e *RateLimitError) Is(target error) bool { return target == ErrRateLimited }

// Code returns CodeRateLimitExceeded.
func (e *RateLimitError) Code() string { return CodeRateLimitExceeded }

// TooLargeError means a request body is larger than the limit. The gateway answers 413.
type TooLargeError struct{ Limit int64 }

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("limits: request body larger than %d bytes", e.Limit)
}

// Is matches ErrTooLarge.
func (e *TooLargeError) Is(target error) bool { return target == ErrTooLarge }

// retryAfterAtLeast makes a wait positive; with the gateway rounding up to whole seconds, a
// Retry-After header is never 0.
func retryAfterAtLeast(d, floor time.Duration) time.Duration {
	if d < floor {
		return floor
	}
	return d
}
