package telemetry

import (
	"fmt"
	"log/slog"
)

// RedactedText is what a Redacted value shows everywhere.
const RedactedText = "[redacted]"

// Redacted holds text that must never reach a log, a metric label or an error
// message: prompt and response content (D-31, D-40). It always renders as
// "[redacted]" in slog, fmt and JSON, so redaction is a property of the type,
// not a habit. Read the real text with Reveal, only where the content is
// legitimately used (for example to send it to the engine).
type Redacted struct{ text string }

// Redact wraps content so it cannot be logged by accident.
func Redact(text string) Redacted { return Redacted{text: text} }

// Reveal returns the wrapped text.
func (r Redacted) Reveal() string { return r.text }

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
