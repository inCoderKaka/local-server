package admin

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/jx"
	ht "github.com/ogen-go/ogen/http"
	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/ogen-go/ogen/validate"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// API error types and codes (api/openapi.yaml, "Errors"). An admin failure other than a lockout
// has no code.
const (
	typeInvalidRequest = "invalid_request_error"
	typeServer         = "server_error"
	typeRequests       = "requests"
	codeRateLimit      = "rate_limit_exceeded"
)

// apiError is one error answer. Its fields are fixed text, numbers and the names of registered
// settings; no request bytes are ever copied into it. param is a request field name, and only
// when it passed safeName.
type apiError struct {
	status     int
	typ        string
	code       string // "" is a null code
	param      string // "" is a null param
	message    string
	retryAfter time.Duration
}

func (e *apiError) Error() string {
	return "admin: " + strconv.Itoa(e.status) + " " + e.code + ": " + e.message
}

func newErr(status int, typ, code, param, message string) *apiError {
	return &apiError{status: status, typ: typ, code: code, param: param, message: message}
}

func badRequest(param, message string) *apiError {
	return newErr(http.StatusBadRequest, typeInvalidRequest, "", param, message)
}

// notFoundErr is a 404 for a resource that does not exist; notFound is the one for a path.
func notFoundErr(message string) *apiError {
	return newErr(http.StatusNotFound, typeInvalidRequest, "", "", message)
}

// conflict is a 409: the resource is in a state that does not allow the request.
func conflict(message string) *apiError {
	return newErr(http.StatusConflict, typeInvalidRequest, "", "", message)
}

func internalError() *apiError {
	return newErr(http.StatusInternalServerError, typeServer, "", "", "The host could not process the request. Quote the X-Request-Id when reporting it.")
}

func unauthenticated() *apiError {
	return newErr(http.StatusUnauthorized, typeInvalidRequest, "", "", "Log in first: there is no valid admin session.")
}

func forbidden(message string) *apiError {
	return newErr(http.StatusForbidden, typeInvalidRequest, "", "", message)
}

func tooLarge() *apiError {
	return newErr(http.StatusRequestEntityTooLarge, typeInvalidRequest, "", "", "The request body is larger than this host allows.")
}

func notFound() *apiError {
	return newErr(http.StatusNotFound, typeInvalidRequest, "", "", "No such endpoint.")
}

// retryAfterSeconds rounds a wait up to whole seconds, at least 1, and returns 0 for none.
func retryAfterSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
}

func (e *apiError) response() *api.ErrorStatusCodeWithHeaders {
	d := api.ErrorDetail{Message: e.message, Type: e.typ}
	if e.param != "" {
		d.Param = api.NewNilString(e.param)
	} else {
		d.Param.SetToNull()
	}
	if e.code != "" {
		d.Code = api.NewNilString(e.code)
	} else {
		d.Code.SetToNull()
	}
	out := &api.ErrorStatusCodeWithHeaders{StatusCode: e.status, Response: api.Error{Error: d}}
	if s := retryAfterSeconds(e.retryAfter); s > 0 {
		out.RetryAfter = api.NewOptInt(s)
	}
	return out
}

// writeError writes e as the error envelope.
func writeError(w http.ResponseWriter, e *apiError) {
	resp := e.response()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if resp.RetryAfter.Set {
		w.Header().Set("Retry-After", strconv.Itoa(resp.RetryAfter.Value))
	}
	w.WriteHeader(e.status)
	enc := new(jx.Encoder)
	resp.Response.Encode(enc)
	_, _ = enc.WriteTo(w)
}

// classify maps any error to an apiError and reports whether it is an internal failure whose
// text the caller should log. The text of err is never copied into the answer.
func classify(err error) (e *apiError, internal bool) {
	var ae *apiError
	var le *LockedOutError
	var mb *http.MaxBytesError
	var ce *consumers.Error
	switch {
	case errors.As(err, &ae):
		return ae, false
	case errors.As(err, &ce):
		return consumersError(ce), false
	case errors.As(err, &le):
		return &apiError{status: http.StatusTooManyRequests, typ: typeRequests, code: codeRateLimit,
			message: "Too many failed login attempts. Try again later.", retryAfter: le.RetryAfter}, false
	case errors.Is(err, ErrInvalidLogin):
		return newErr(http.StatusUnauthorized, typeInvalidRequest, "", "", "The login name or password is wrong."), false
	case errors.Is(err, ogenerrors.ErrSecurityRequirementIsNotSatisfied):
		return unauthenticated(), false
	case errors.As(err, &mb):
		return tooLarge(), false
	case errors.Is(err, usage.ErrInvalidCursor), errors.Is(err, audit.ErrInvalidCursor):
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

// consumersError maps a refusal of the consumers service to the envelope. Its text is fixed and
// holds no request bytes; its param is a constant field name, and is checked anyway.
func consumersError(e *consumers.Error) *apiError {
	switch e.Kind {
	case consumers.KindNotFound:
		return notFoundErr(e.Message)
	case consumers.KindConflict:
		return conflict(e.Message)
	}
	param := e.Param
	if !safeName(param) {
		param = ""
	}
	return badRequest(param, e.Message)
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

// decodeError maps an ogen request decoding error to an answer without quoting the request.
// ogen's messages can contain request bytes, so only fixed text and checked field names are
// used. The text is read for classification and dropped.
func decodeError(err error) *apiError {
	var mb *http.MaxBytesError
	if errors.As(err, &mb) {
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
			// A missing required field: ogen reports "invalid: <name> (field required)" without the "validate:" prefix.
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

// errorHandler answers what ogen sends it directly: request decoding errors, the
// not-implemented error of an operation no ticket has built yet, and a response it could not
// write. A refusal is logged with its status only, never the error text.
func (a *Admin) errorHandler(ctx context.Context, w http.ResponseWriter, _ *http.Request, err error) {
	var de *ogenerrors.DecodeRequestError
	var dp *ogenerrors.DecodeParamsError
	var mb *http.MaxBytesError
	var e *apiError
	switch {
	case errors.Is(err, ht.ErrNotImplemented):
		e = newErr(http.StatusNotImplemented, typeServer, "", "", "This endpoint is not available on this host yet.")
	case errors.As(err, &de), errors.As(err, &dp), errors.As(err, &mb):
		e = decodeError(err)
		a.log.InfoContext(ctx, "admin: request refused at decoding", "status", e.status)
	default:
		a.log.WarnContext(ctx, "admin: response could not be written")
		e = internalError()
	}
	writeError(w, e)
}

func (a *Admin) notFound(w http.ResponseWriter, _ *http.Request) { writeError(w, notFound()) }

func (a *Admin) methodNotAllowed(w http.ResponseWriter, _ *http.Request, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, newErr(http.StatusMethodNotAllowed, typeInvalidRequest, "", "", "This endpoint does not accept this method."))
}

// NewError implements api.Handler: it maps every error a handler or the security handler
// returns to the envelope. Expected refusals are not logged; an internal failure is, with the
// error text of the modules (which never hold content, req_001 D-31).
func (a *Admin) NewError(ctx context.Context, err error) *api.ErrorStatusCodeWithHeaders {
	e, internal := classify(err)
	if internal {
		a.log.ErrorContext(ctx, "admin: request failed", "err", err)
	}
	return e.response()
}
