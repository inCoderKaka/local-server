package policy

import (
	"errors"
	"fmt"
	"time"
)

// Code is a typed policy error code. The gateway maps it to the API's error envelope.
type Code string

// Error codes. CodeModelNotGranted is spelled like the API code of the same meaning.
const (
	// CodeInvalidProperties: LS-Request-Properties does not parse or exceeds its limits.
	CodeInvalidProperties Code = "invalid_request_properties"
	// CodePropertyNotAllowed: the grant does not allow a property the request carries.
	CodePropertyNotAllowed Code = "property_not_allowed"
	// CodeModelNotGranted: the requested model is not in the grant.
	CodeModelNotGranted Code = "model_not_granted"
	// CodeDenied: a rule with "deny" matched the request.
	CodeDenied Code = "policy_denied"
	// CodeOutsideWindow: a rule with "windows" matched and the time is outside all of them.
	CodeOutsideWindow Code = "outside_time_window"
	// CodeInvalidRule: a rule document is malformed or breaks the rule format.
	CodeInvalidRule Code = "invalid_policy_rule"
	// CodeUnknownFormatVersion: a rule document is written in a format version this build
	// does not know.
	CodeUnknownFormatVersion Code = "unknown_rule_format_version"
	// CodeInvalidGrant: the grant passed in is malformed (for example its allowed-properties text).
	CodeInvalidGrant Code = "invalid_grant"
)

// Error is the only error type this package returns, apart from errors of the RuleSource and
// settings it reads. Its message never contains a property value, a prompt or a response
// (D-31, D-40).
type Error struct {
	Code    Code
	Message string
	// RuleID and RuleName name the rule that refused the request or is invalid, if any.
	RuleID   string
	RuleName string
	// Property is the property name concerned, if any.
	Property string
	// FormatVersion is the unknown format version (CodeUnknownFormatVersion).
	FormatVersion int64
	// Opens is when the refusing rule's windows next open (CodeOutsideWindow). It does not
	// promise that other rules allow the request then.
	Opens time.Time
}

func (e *Error) Error() string {
	s := "policy: " + string(e.Code)
	if e.RuleID != "" {
		s += fmt.Sprintf(" (rule %s)", e.RuleID)
	}
	if e.Message != "" {
		s += ": " + e.Message
	}
	return s
}

// Is matches on Code, so errors.Is(err, ErrOutsideWindow) works.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// Sentinels for errors.Is.
var (
	ErrInvalidProperties    = &Error{Code: CodeInvalidProperties}
	ErrPropertyNotAllowed   = &Error{Code: CodePropertyNotAllowed}
	ErrModelNotGranted      = &Error{Code: CodeModelNotGranted}
	ErrDenied               = &Error{Code: CodeDenied}
	ErrOutsideWindow        = &Error{Code: CodeOutsideWindow}
	ErrInvalidRule          = &Error{Code: CodeInvalidRule}
	ErrUnknownFormatVersion = &Error{Code: CodeUnknownFormatVersion}
	ErrInvalidGrant         = &Error{Code: CodeInvalidGrant}
)

// CodeOf returns the code of err, or "" if it is not an *Error.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func ruleErr(format string, args ...any) *Error {
	return &Error{Code: CodeInvalidRule, Message: fmt.Sprintf(format, args...)}
}
