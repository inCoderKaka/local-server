package engine

import (
	"errors"
	"fmt"
)

// Code is a typed engine error code. ContextTooLong and EngineUnavailable are
// the API error codes of lld section 9 (they are spelled the same).
type Code string

// Error codes.
const (
	CodeContextTooLong     Code = "context_too_long"
	CodeEngineUnavailable  Code = "engine_unavailable"
	CodeModelNotFound      Code = "model_not_found"
	CodeInvalidRequest     Code = "invalid_request"
	CodeTimeout            Code = "engine_timeout"
	CodeUnsupportedVersion Code = "unsupported_engine_version"
	CodeEngineError        Code = "engine_error"
)

// Error is the only error type engines return (besides context errors). Its
// message never contains prompt or response text (req_001 D-31, D-40).
type Error struct {
	Code    Code
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Message == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the cause (a transport error, never content).
func (e *Error) Unwrap() error { return e.Cause }

// Is matches on Code, so errors.Is(err, ErrContextTooLong) works.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code
}

// Sentinels for errors.Is.
var (
	ErrContextTooLong     = &Error{Code: CodeContextTooLong}
	ErrEngineUnavailable  = &Error{Code: CodeEngineUnavailable}
	ErrModelNotFound      = &Error{Code: CodeModelNotFound}
	ErrInvalidRequest     = &Error{Code: CodeInvalidRequest}
	ErrTimeout            = &Error{Code: CodeTimeout}
	ErrUnsupportedVersion = &Error{Code: CodeUnsupportedVersion}
	ErrEngineError        = &Error{Code: CodeEngineError}
)

// Errorf builds an *Error.
func Errorf(code Code, cause error, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Cause: cause}
}

// CodeOf returns the code of err, or "" if it is not an *Error.
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
