package gateway

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

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/gateway/stream"
	"github.com/inCoderKaka/local-server/internal/ledger/checkpoint"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/policy"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// API error codes (api/openapi.yaml, lld section 9).
const (
	codeQuotaExceeded      = "quota_exceeded"
	codeQueueFull          = "queue_full"
	codeHostPaused         = "host_paused"
	codeModelNotGranted    = "model_not_granted"
	codeContextTooLong     = "context_too_long"
	codeEngineUnavailable  = "engine_unavailable"
	codeOverBudget         = "over_budget"
	codePolicyDenied       = "policy_denied"
	codeOutsideTimeWindow  = "outside_time_window"
	codeInvalidProperties  = "invalid_request_properties"
	codeUnsupportedParam   = "unsupported_parameter"
	codeInvalidAPIKey      = "invalid_api_key"
	codeRateLimitExceeded  = "rate_limit_exceeded"
	typeInvalidRequest     = "invalid_request_error"
	typeServer             = "server_error"
	typeInsufficientQuota  = "insufficient_quota"
	typeRequests           = "requests"
	headerRequestPropsName = policy.HeaderName
)

// apiError is one error answer. Its fields are fixed text, numbers and stored rule ids and
// names. Only two come from the request: Param, when it passed safeName, and properties, which
// are property names the policy parser already checked (RFC 9651 key characters, at most the
// host's property limits), never values.
type apiError struct {
	status     int
	typ        string
	code       string // "" is a null code
	param      string // "" is a null param
	message    string
	retryAfter time.Duration
	ruleID     string
	ruleName   string
	properties []string
}

func (e *apiError) Error() string {
	return "gateway: " + strconv.Itoa(e.status) + " " + e.code + ": " + e.message
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
	if e.ruleID != "" {
		d.RuleID = api.NewOptString(e.ruleID)
	}
	if e.ruleName != "" {
		d.RuleName = api.NewOptString(e.ruleName)
	}
	d.Properties = e.properties
	out := &api.ErrorStatusCodeWithHeaders{StatusCode: e.status, Response: api.Error{Error: d}}
	if s := retryAfterSeconds(e.retryAfter); s > 0 {
		out.RetryAfter = api.NewOptInt(s)
	}
	return out
}

func newErr(status int, typ, code, param, message string) *apiError {
	return &apiError{status: status, typ: typ, code: code, param: param, message: message}
}

func internalError() *apiError {
	return newErr(http.StatusInternalServerError, typeServer, "", "", "The host could not process the request. Quote the X-Request-Id when reporting it.")
}

// Fixed answers shared by several paths.
func invalidKey() *apiError {
	return newErr(http.StatusUnauthorized, typeInvalidRequest, codeInvalidAPIKey, "", "The access key is missing, unknown, expired or revoked.")
}

func unsupportedParam(field, message string) *apiError {
	return newErr(http.StatusBadRequest, typeInvalidRequest, codeUnsupportedParam, field, message)
}

func invalidParam(field, message string) *apiError {
	return newErr(http.StatusBadRequest, typeInvalidRequest, "", field, message)
}

// classify maps any error to an apiError, and reports whether it is an internal failure whose
// text the caller should log (every other answer is expected and carries no new information).
// The text of err is never copied into the answer.
func classify(err error, now time.Time) (e *apiError, internal bool) {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae, false
	}
	// Security errors from the security handler arrive wrapped by ogen.
	var qe *limits.QuotaError
	var re *limits.RateLimitError
	var se *scheduler.Error
	var pe *policy.Error
	var ee *engine.Error
	var nc *checkpoint.NotYetCoveredError
	var te *stream.TimeoutError
	switch {
	case errors.As(err, &qe):
		msg := "The token quota for this window is used up."
		if qe.Unit == limits.UnitRequests {
			msg = "The request quota for this window is used up."
		}
		return &apiError{status: http.StatusTooManyRequests, typ: typeInsufficientQuota, code: codeQuotaExceeded, message: msg, retryAfter: qe.RetryAfter}, false
	case errors.As(err, &re):
		msg := "Too many requests. Slow down."
		if re.LockedOut {
			msg = "Too many failed attempts. Try again later."
		}
		return &apiError{status: http.StatusTooManyRequests, typ: typeRequests, code: codeRateLimitExceeded, message: msg, retryAfter: re.RetryAfter}, false
	case errors.Is(err, limits.ErrTooLarge):
		return newErr(http.StatusRequestEntityTooLarge, typeInvalidRequest, "", "", "The request body is larger than this host allows."), false
	case errors.Is(err, auth.ErrNoGrant):
		return newErr(http.StatusForbidden, typeInvalidRequest, codeModelNotGranted, "model", "The host has not granted this account any model yet."), false
	case errors.Is(err, auth.ErrInvalidKey), errors.Is(err, auth.ErrRevoked), errors.Is(err, auth.ErrExpired),
		errors.Is(err, auth.ErrGrantExpired), errors.Is(err, auth.ErrNodeMismatch), errors.Is(err, auth.ErrConsumerNotActive),
		errors.Is(err, ogenerrors.ErrSecurityRequirementIsNotSatisfied):
		return invalidKey(), false
	case errors.As(err, &se):
		e := schedulerError(se)
		return e, e.status == http.StatusInternalServerError
	case errors.As(err, &pe):
		return policyError(pe, now)
	case errors.As(err, &ee):
		e := engineError(ee)
		return e, e.status == http.StatusBadGateway
	case errors.As(err, &nc):
		return &apiError{status: http.StatusConflict, typ: typeInvalidRequest, message: "No checkpoint covers this record yet. Retry later.", retryAfter: max(nc.RetryAfter, time.Second)}, false
	case errors.Is(err, checkpoint.ErrUnknownUsage), errors.Is(err, usage.ErrNotFound):
		return newErr(http.StatusNotFound, typeInvalidRequest, "", "usage_id", "No metered usage record has this id."), false
	case errors.Is(err, checkpoint.ErrNoCheckpoint):
		return newErr(http.StatusNotFound, typeInvalidRequest, "", "", "No checkpoint has been signed yet."), false
	case errors.Is(err, checkpoint.ErrBadTreeSize):
		return invalidParam("from_tree_size", "from_tree_size must be between 1 and the latest checkpoint's tree size."), false
	case errors.As(err, &te):
		return newErr(http.StatusGatewayTimeout, typeServer, "", "", "The model engine did not answer in time."), false
	case errors.Is(err, context.Canceled):
		// The client went away; nobody reads this answer.
		return newErr(499, typeServer, "", "", "The client closed the request."), false
	case errors.Is(err, context.DeadlineExceeded):
		return newErr(http.StatusGatewayTimeout, typeServer, "", "", "The host did not finish in time."), true
	}
	return internalError(), true
}

func schedulerError(e *scheduler.Error) *apiError {
	switch e.Code {
	case scheduler.CodeQueueFull:
		return &apiError{status: http.StatusTooManyRequests, typ: typeServer, code: codeQueueFull, message: "The host is busy: its queue is full or the request waited too long for a slot. Retry after the time in Retry-After.", retryAfter: e.RetryAfter}
	case scheduler.CodeHostPaused:
		return &apiError{status: http.StatusServiceUnavailable, typ: typeServer, code: codeHostPaused, message: "The host has paused sharing.", retryAfter: e.RetryAfter}
	case scheduler.CodeOverBudget:
		return newErr(http.StatusUnprocessableEntity, typeInvalidRequest, codeOverBudget, "", "The model and the request's context can never fit the host's memory budget.")
	}
	return internalError()
}

func engineError(e *engine.Error) *apiError {
	switch e.Code {
	case engine.CodeContextTooLong:
		return newErr(http.StatusBadRequest, typeInvalidRequest, codeContextTooLong, "messages", "The prompt is longer than the context limit.")
	case engine.CodeEngineUnavailable, engine.CodeUnsupportedVersion:
		return newErr(http.StatusServiceUnavailable, typeServer, codeEngineUnavailable, "", "The host's model engine is not reachable.")
	case engine.CodeModelNotFound:
		return newErr(http.StatusNotFound, typeInvalidRequest, "", "model", "The model is not installed on the host.")
	case engine.CodeInvalidRequest:
		return newErr(http.StatusBadRequest, typeInvalidRequest, "", "", "The model engine refused the request.")
	case engine.CodeTimeout:
		return newErr(http.StatusGatewayTimeout, typeServer, "", "", "The model engine did not answer in time.")
	}
	return newErr(http.StatusBadGateway, typeServer, "", "", "The model engine failed.")
}

// policyError maps a policy refusal as req_001 D-79 says. A rule that does not parse is the
// host's fault: the consumer gets a plain 500 and the rule id stays on the host.
func policyError(e *policy.Error, now time.Time) (*apiError, bool) {
	switch e.Code {
	case policy.CodeModelNotGranted:
		return newErr(http.StatusForbidden, typeInvalidRequest, codeModelNotGranted, "model", "The model is not available to this access key."), false
	case policy.CodeDenied:
		return &apiError{status: http.StatusForbidden, typ: typeInvalidRequest, code: codePolicyDenied, message: "A policy rule on this host refuses the request.",
			ruleID: e.RuleID, ruleName: e.RuleName}, false
	case policy.CodePropertyNotAllowed:
		out := &apiError{status: http.StatusForbidden, typ: typeInvalidRequest, code: codePolicyDenied, message: "The request carries a property this access key may not use."}
		if out.properties = e.Properties; len(out.properties) == 0 && e.Property != "" {
			out.properties = []string{e.Property}
		}
		return out, false
	case policy.CodeOutsideWindow:
		out := &apiError{status: http.StatusTooManyRequests, typ: typeRequests, code: codeOutsideTimeWindow, message: "A policy rule allows this request only inside its time windows, and none is open now.",
			ruleID: e.RuleID, ruleName: e.RuleName}
		if !e.Opens.IsZero() {
			out.retryAfter = max(e.Opens.Sub(now), time.Second)
		}
		return out, false
	case policy.CodeInvalidProperties:
		return newErr(http.StatusBadRequest, typeInvalidRequest, codeInvalidProperties, headerRequestPropsName, "The "+headerRequestPropsName+" header is malformed or over the host's limits."), false
	}
	// invalid_policy_rule, unknown_rule_format_version, invalid_grant.
	return internalError(), true
}

// ---- request decoding errors (ogen) ----

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
// ogen's messages can contain request bytes (an unknown role, a bad value), so only fixed
// text and checked field names are used. The text is read for classification and dropped.
func decodeError(err error) *apiError {
	text := err.Error()
	var de *ogenerrors.DecodeRequestError
	var dp *ogenerrors.DecodeParamsError
	switch {
	case errors.Is(err, limits.ErrTooLarge):
		e, _ := classify(err, time.Time{})
		return e
	case errors.As(err, &de):
		if strings.Contains(text, "unexpected Content-Type") || strings.Contains(text, "unsupported Content-Type") {
			return newErr(http.StatusUnsupportedMediaType, typeInvalidRequest, "", "", "The Content-Type must be application/json.")
		}
		if m := unexpectedField.FindStringSubmatch(text); m != nil {
			if safeName(m[1]) {
				return unsupportedParam(m[1], "This field is not supported by this host.")
			}
			return unsupportedParam("", "A field in the request is not supported by this host.")
		}
		field := ""
		if m := decodeField.FindStringSubmatch(text); m != nil {
			field = m[1]
		} else if m := validateField.FindStringSubmatch(text); m != nil {
			field = m[1]
		}
		return invalidParam(field, "The request body is not valid.")
	case errors.As(err, &dp):
		var pe *ogenerrors.DecodeParamError
		name := ""
		if errors.As(err, &pe) && safeName(pe.Name) {
			name = pe.Name
		}
		return invalidParam(name, "A request parameter is not valid.")
	}
	return invalidParam("", "The request is not valid.")
}

// writeError writes e as the OpenAI error envelope.
func writeError(w http.ResponseWriter, e *apiError) {
	resp := e.response()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Expose-Headers", "Retry-After")
	if resp.RetryAfter.Set {
		w.Header().Set("Retry-After", strconv.Itoa(resp.RetryAfter.Value))
	}
	w.WriteHeader(e.status)
	enc := new(jx.Encoder)
	resp.Response.Encode(enc)
	_, _ = enc.WriteTo(w)
}
