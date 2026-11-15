package requests

// ErrorKind says what kind of refusal an Error is.
type ErrorKind int

// The kinds of refusal.
const (
	// KindInvalid is input the host refuses: the request is wrong (HTTP 400).
	KindInvalid ErrorKind = iota + 1
	// KindNotFound is a request that does not exist, or that belongs to another consumer
	// (HTTP 404; the answers are the same, so an id of someone else's request tells nothing).
	KindNotFound
	// KindConflict is a call the request's state does not allow: it is final, or the consumer has
	// too many open requests (HTTP 409).
	KindConflict
)

// Error is a refusal with fixed text. Param names the request field at fault, or is empty.
// Neither field ever holds request bytes (req_001 D-31).
type Error struct {
	Kind    ErrorKind
	Param   string
	Message string
}

func (e *Error) Error() string { return "requests: " + e.Message }

func invalid(param, message string) *Error {
	return &Error{Kind: KindInvalid, Param: param, Message: message}
}

var (
	errNotFound = &Error{Kind: KindNotFound, Message: "No such resource request."}
	// ErrNotOpen is the refusal of a comment or a decision on a request that is final. The admin API
	// tests for it before it checks the limits of an approval, so a final request answers 409 and
	// not 400.
	ErrNotOpen    = &Error{Kind: KindConflict, Message: "The resource request is no longer open."}
	errTooMany    = &Error{Kind: KindConflict, Message: "You have as many open resource requests as the host allows. Wait for a decision or withdraw one."}
	errNoConsumer = &Error{Kind: KindNotFound, Message: "No such consumer."}
	errDeleted    = &Error{Kind: KindConflict, Message: "The consumer is deleted."}
	errBadCursor  = invalid("cursor", "The cursor is not valid. Use the next_cursor of the previous page.")
)
