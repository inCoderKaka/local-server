package requests

import (
	"encoding/json"
	"time"
)

// Status is the state of a resource request (req_001 D-38). Every state but Open is final.
type Status string

// The states of a request.
const (
	StatusOpen      Status = "open"
	StatusApproved  Status = "approved"
	StatusRejected  Status = "rejected"
	StatusWithdrawn Status = "withdrawn"
	StatusExpired   Status = "expired"
)

// Valid reports whether s is one of the five states.
func (s Status) Valid() bool {
	switch s {
	case StatusOpen, StatusApproved, StatusRejected, StatusWithdrawn, StatusExpired:
		return true
	}
	return false
}

// Request is a resource request as the API shows it.
type Request struct {
	ID         string
	ConsumerID string
	Status     Status
	// Message is the consumer's own text. It is never logged.
	Message string
	// Ask is the JSON object of what the consumer asks for, as the API's ResourceRequestAsk
	// writes it; nil when it asked for nothing in particular. This package keeps it as it came
	// and reads no field of it.
	Ask       json.RawMessage
	CreatedAt time.Time
	// ExpiresAt is when an open request expires; nil once the request is final.
	ExpiresAt *time.Time
	// DecidedAt is when the request became final; nil while open.
	DecidedAt *time.Time
	// ResultingVersionID is the grant version an approval made.
	ResultingVersionID *string
	// Comments are oldest first. They are filled by Get and by the calls that change a request,
	// and left nil in a list.
	Comments []Comment
}

// Comment is one comment on a request.
type Comment struct {
	ID        string
	Author    Author
	Body      string
	CreatedAt time.Time
}

// Author is who wrote a comment.
type Author struct {
	PrincipalID string
	// Kind is the principal's kind: consumer, host_admin or platform_admin.
	Kind string
	// DisplayName is nil when the principal has none, or when a deleted consumer's was erased.
	DisplayName *string
}

// NewRequest is what Create stores.
type NewRequest struct {
	Message string
	// Ask is a JSON object or empty.
	Ask json.RawMessage
}

// ListQuery asks for one page of a list.
type ListQuery struct {
	// Status keeps the requests in this state; empty keeps every state.
	Status Status
	// ConsumerID keeps one consumer's requests. The consumer-side calls set it from the caller;
	// the admin list takes it from the query.
	ConsumerID string
	// Cursor is the NextCursor of the previous page; empty starts at the newest.
	Cursor string
	// Limit is the page size asked for; nil asks for the setting's size.
	Limit *int
}

// Page is one page of requests, newest first, without comments.
type Page struct {
	Items      []Request
	NextCursor string // empty on the last page
}
