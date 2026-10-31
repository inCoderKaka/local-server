package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// RequestIDHeader carries the request id on HTTP requests and responses.
const RequestIDHeader = "X-Request-Id"

const maxRequestIDLen = 64

type requestIDKey struct{}

// WithRequestID returns a context carrying the request id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the request id in ctx, or "" if there is none.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// NewRequestID returns a new random request id (32 hex characters).
func NewRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("telemetry: no randomness: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// Middleware gives every request an id: it keeps a well-formed inbound
// X-Request-Id (letters, digits, '-', '_', at most 64 characters), otherwise
// generates one. The id goes into the request context and the response header.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID(id) {
			id = NewRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), id)))
	})
}

func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}
