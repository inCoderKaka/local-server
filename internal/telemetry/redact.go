package telemetry

import (
	"fmt"
	"log/slog"
	"strings"
)

// RedactedText is what a Redacted value shows everywhere.
const RedactedText = "[redacted]"

// Redacted holds text that must never reach a log, a metric label or an error
// message: prompt and response content (D-31, D-40). It always renders as
// "[redacted]" in slog, fmt and JSON, so redaction is a property of the type,
// not a habit. Read the real text with Reveal, only where the content is
// legitimately used (for example to send it to the engine). The text is held
// behind a pointer so that fmt prints only an address even when a Redacted is
// an unexported field of another struct, where fmt cannot call its methods.
type Redacted struct{ text *string }

// Redact wraps content so it cannot be logged by accident.
func Redact(text string) Redacted { return Redacted{text: &text} }

// Reveal returns the wrapped text, or "" for the zero value.
func (r Redacted) Reveal() string {
	if r.text == nil {
		return ""
	}
	return *r.text
}

// LogValue implements slog.LogValuer.
func (Redacted) LogValue() slog.Value { return slog.StringValue(RedactedText) }

// String implements fmt.Stringer.
func (Redacted) String() string { return RedactedText }

// GoString implements fmt.GoStringer, so %#v does not leak.
func (Redacted) GoString() string { return RedactedText }

// Format implements fmt.Formatter, so every verb prints the placeholder.
func (Redacted) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(RedactedText)) }

// MarshalText implements encoding.TextMarshaler (used by JSON and others).
func (Redacted) MarshalText() ([]byte, error) { return []byte(RedactedText), nil }

// MarshalJSON implements json.Marshaler.
func (Redacted) MarshalJSON() ([]byte, error) { return []byte(`"` + RedactedText + `"`), nil }

// contentKeys are normalised JSON object keys (see ContentKey) that name content or secrets.
var contentKeys = map[string]bool{
	"prompt": true, "response": true, "completion": true, "messages": true, "content": true,
	"secret": true, "password": true, "token": true, "apikey": true, "accesskey": true,
	"privatekey": true, "authorization": true, "accesstoken": true, "refreshtoken": true,
}

// ContentKey reports whether a JSON object key names content or a secret (D-31, D-40). The key is
// lower-cased and stripped of "_" and "-" before an exact lookup, so api_key, apiKey and Api-Key
// all match while token_count and request_id do not.
func ContentKey(k string) bool {
	k = strings.ToLower(k)
	k = strings.NewReplacer("_", "", "-", "").Replace(k)
	return contentKeys[k]
}

// FindContentKey walks a value decoded by encoding/json (map[string]any, []any, scalars) and
// returns the first key, at any depth, for which ContentKey is true.
func FindContentKey(v any) (string, bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if ContentKey(k) {
				return k, true
			}
			if found, ok := FindContentKey(e); ok {
				return found, true
			}
		}
	case []any:
		for _, e := range x {
			if found, ok := FindContentKey(e); ok {
				return found, true
			}
		}
	}
	return "", false
}
