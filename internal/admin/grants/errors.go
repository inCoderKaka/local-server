package grants

// ErrorKind says what kind of refusal an Error is.
type ErrorKind int

// The kinds of refusal.
const (
	// KindInvalid is input the host refuses: the request is wrong (HTTP 400).
	KindInvalid ErrorKind = iota + 1
	// KindNotFound is a consumer, grant, model or rule that does not exist (HTTP 404).
	KindNotFound
	// KindConflict is a request the current state of the resource does not allow (HTTP 409).
	KindConflict
)

// Error is a refusal with fixed text. Param names the request field at fault, or is empty.
// Neither field ever holds request bytes.
type Error struct {
	Kind    ErrorKind
	Param   string
	Message string
}

func (e *Error) Error() string { return "admin grants: " + e.Message }

func invalid(param, message string) *Error {
	return &Error{Kind: KindInvalid, Param: param, Message: message}
}

var (
	errNoConsumer = &Error{Kind: KindNotFound, Message: "No such consumer."}
	errNoGrant    = &Error{Kind: KindNotFound, Message: "This consumer has no grant yet."}
	errNoModel    = &Error{Kind: KindNotFound, Message: "No such model."}
	errNoRule     = &Error{Kind: KindNotFound, Message: "No such policy rule."}
	errDeleted    = &Error{Kind: KindConflict, Message: "The consumer is deleted."}
)
