package consumerauth

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/jx"
	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/ogen-go/ogen/validate"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// Error types of the API's error envelope (api/openapi.yaml, "Errors").
const (
	typeInvalidRequest = "invalid_request_error"
	typeServer         = "server_error"
	typeRequests       = "requests"
	codeRateLimit      = "rate_limit_exceeded"
)

// Error is a refusal with its API answer. Every field is fixed text, a number or the name of a
// request field that passed safeName: no request byte, password, token or hash is ever copied
// into one. The consumer API (internal/gateway) maps it to its envelope the same way, so a
// refusal reads the same on both listeners.
type Error struct {
	Status     int
	Type       string
	Code       string // "" is a null code
	Param      string // "" is a null param
	Message    string
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	return "consumerauth: " + strconv.Itoa(e.Status) + " " + e.Code + ": " + e.Message
}

func newErr(status int, typ, code, param, message string) *Error {
	return &Error{Status: status, Type: typ, Code: code, Param: param, Message: message}
}

func badRequest(param, message string) *Error {
	return newErr(http.StatusBadRequest, typeInvalidRequest, "", param, message)
}

func notFoundErr(message string) *Error {
	return newErr(http.StatusNotFound, typeInvalidRequest, "", "", message)
}

func conflict(message string) *Error {
	return newErr(http.StatusConflict, typeInvalidRequest, "", "", message)
}

func forbidden(param, message string) *Error {
	return newErr(http.StatusForbidden, typeInvalidRequest, "", param, message)
}

func internalError() *Error {
	return newErr(http.StatusInternalServerError, typeServer, "", "", "The host could not process the request. Quote the X-Request-Id when reporting it.")
}

func unauthenticated() *Error {
	return newErr(http.StatusUnauthorized, typeInvalidRequest, "", "", "Log in first: there is no valid session.")
}

func wrongLogin() *Error {
	return newErr(http.StatusUnauthorized, typeInvalidRequest, "", "", "The login name or password is wrong.")
}

func tooLarge() *Error {
	return newErr(http.StatusRequestEntityTooLarge, typeInvalidRequest, "", "", "The request body is larger than this host allows.")
}

func lockedOut(wait time.Duration) *Error {
	return &Error{Status: http.StatusTooManyRequests, Type: typeRequests, Code: codeRateLimit,
		Message: "Too many failed login attempts. Try again later.", RetryAfter: wait}
}

// LockedOutError means a login or a password check is locked out: too many failures from this
// address or for this login name. RetryAfter is how long the longest lockout still runs.
type LockedOutError struct{ RetryAfter time.Duration }

func (e *LockedOutError) Error() string { return "consumerauth: login is locked out" }

// ErrInvalidLogin means the login name or the password is wrong. It never says which.
var ErrInvalidLogin = errors.New("consumerauth: login name or password is wrong")

func retryAfterSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
}

func (e *Error) response() *api.ErrorStatusCodeWithHeaders {
	d := api.ErrorDetail{Message: e.Message, Type: e.Type}
	if e.Param != "" {
		d.Param = api.NewNilString(e.Param)
	} else {
		d.Param.SetToNull()
	}
	if e.Code != "" {
		d.Code = api.NewNilString(e.Code)
	} else {
		d.Code.SetToNull()
	}
	out := &api.ErrorStatusCodeWithHeaders{StatusCode: e.Status, Response: api.Error{Error: d}}
	if s := retryAfterSeconds(e.RetryAfter); s > 0 {
		out.RetryAfter = api.NewOptInt(s)
	}
	return out
}

// writeError writes e as the error envelope.
func writeError(w http.ResponseWriter, e *Error) {
	resp := e.response()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if resp.RetryAfter.Set {
		w.Header().Set("Retry-After", strconv.Itoa(resp.RetryAfter.Value))
	}
	w.WriteHeader(e.Status)
	enc := new(jx.Encoder)
	resp.Response.Encode(enc)
	_, _ = enc.WriteTo(w)
}

// classify maps any error to an *Error and reports whether it is an internal failure whose text
// the caller should log. The text of err is never copied into the answer.
func classify(err error) (e *Error, internal bool) {
	var ce *Error
	var le *LockedOutError
	var mb *http.MaxBytesError
	switch {
	case errors.As(err, &ce):
		return ce, false
	case errors.As(err, &le):
		return lockedOut(le.RetryAfter), false
	case errors.Is(err, ErrInvalidLogin):
		return wrongLogin(), false
	case errors.Is(err, ogenerrors.ErrSecurityRequirementIsNotSatisfied):
		return unauthenticated(), false
	case errors.As(err, &mb), errors.Is(err, limits.ErrTooLarge):
		return tooLarge(), false
	case errors.Is(err, usage.ErrInvalidCursor):
		return badRequest("cursor", "The cursor is not valid. Use the next_cursor of the previous page."), false
	case errors.Is(err, usage.ErrInvalidGranularity):
		return badRequest("granularity", "The granularity must be minute, hour or day."), false
	case errors.Is(err, context.Canceled):
		// The client went away; nobody reads this answer.
		return newErr(499, typeServer, "", "", "The client closed the request."), false
	case errors.Is(err, context.DeadlineExceeded):
		return newErr(http.StatusGatewayTimeout, typeServer, "", "", "The host did not finish in time."), true
	}
	return internalError(), true
}

var (
	unexpectedField = regexp.MustCompile(`unexpected field "(.*)"$`)
	decodeField     = regexp.MustCompile(`decode field "([A-Za-z_][A-Za-z0-9_]*)"`)
	validateField   = regexp.MustCompile(`validate: invalid: ([A-Za-z_][A-Za-z0-9_]*)`)
)

// maxFieldName bounds the field name echoed in param.
const maxFieldName = 64

// safeName reports whether s may be echoed as a param: a short identifier. Anything else is
// dropped, so a hostile key cannot reflect arbitrary bytes into the answer.
func safeName(s string) bool {
	if s == "" || len(s) > maxFieldName {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

// decodeError maps an ogen request decoding error to an answer without quoting the request:
// ogen's messages can contain request bytes, so only fixed text and checked field names are
// used. The text is read for classification and dropped.
func decodeError(err error) *Error {
	var mb *http.MaxBytesError
	if errors.As(err, &mb) || errors.Is(err, limits.ErrTooLarge) {
		return tooLarge()
	}
	text := err.Error()
	var de *ogenerrors.DecodeRequestError
	var dp *ogenerrors.DecodeParamsError
	switch {
	case errors.As(err, &de):
		if strings.Contains(text, "unexpected Content-Type") || strings.Contains(text, "unsupported Content-Type") {
			return newErr(http.StatusUnsupportedMediaType, typeInvalidRequest, "", "", "The Content-Type must be application/json.")
		}
		if m := unexpectedField.FindStringSubmatch(text); m != nil {
			if safeName(m[1]) {
				return badRequest(m[1], "This field is not supported by this host.")
			}
			return badRequest("", "A field in the request is not supported by this host.")
		}
		field := ""
		if m := decodeField.FindStringSubmatch(text); m != nil {
			field = m[1]
		} else if m := validateField.FindStringSubmatch(text); m != nil {
			field = m[1]
		} else if ve := (*validate.Error)(nil); errors.As(err, &ve) && len(ve.Fields) > 0 {
			field = ve.Fields[0].Name
		}
		if !safeName(field) {
			field = ""
		}
		return badRequest(field, "The request body is not valid.")
	case errors.As(err, &dp):
		var pe *ogenerrors.DecodeParamError
		name := ""
		if errors.As(err, &pe) && safeName(pe.Name) {
			name = pe.Name
		}
		return badRequest(name, "A request parameter is not valid.")
	}
	return badRequest("", "The request is not valid.")
}
