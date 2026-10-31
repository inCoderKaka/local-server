package policy

import (
	"fmt"
	"strconv"
	"strings"
)

// HeaderName is the request header that carries request properties (D-33).
const HeaderName = "LS-Request-Properties"

// ValueKind is the type of a request property's value.
type ValueKind int

// The value types LS-Request-Properties accepts (api/openapi.yaml, LSRequestProperties).
const (
	KindBool ValueKind = iota + 1
	KindInteger
	KindToken
	KindString
)

// Value is one request property's value. A bare key is the boolean true.
type Value struct {
	Kind ValueKind
	Bool bool
	Int  int64
	// Text holds a token or a string.
	Text string
}

// Properties are the parsed request properties, keyed by property name. Values come from the
// consumer and are never logged or stored by this package (D-31, D-40).
type Properties map[string]Value

// PropertyLimits bound the parsing of LS-Request-Properties. Both come from settings (D-54).
type PropertyLimits struct {
	// MaxBytes is the most bytes the field may hold, all field lines together.
	MaxBytes int
	// MaxCount is the most properties the field may hold.
	MaxCount int
}

// The RFC 9651 integer range.
const sfIntMax = 999_999_999_999_999

// ParseProperties parses the LS-Request-Properties field lines as received. The value is an
// RFC 9651 Structured Field Dictionary (api/openapi.yaml): keys are property names, a bare key
// means true, and values are tokens, strings, integers or booleans. Everything else the RFC
// allows (decimals, byte sequences, dates, display strings, inner lists, parameters) is refused,
// and so is a key given twice. Field lines that are empty count as absent; no lines, or only
// empty ones, give no properties. Errors are *Error with CodeInvalidProperties and never quote
// a value. MaxBytes counts every byte received, empty lines included.
func ParseProperties(lines []string, lim PropertyLimits) (Properties, error) {
	size := 0
	for _, l := range lines {
		if size += len(l); size > lim.MaxBytes {
			return nil, propErr("longer than %d bytes", max(lim.MaxBytes, 0))
		}
	}
	var parts []string
	for _, l := range lines {
		if strings.Trim(l, " \t") != "" {
			parts = append(parts, l)
		}
	}
	if len(parts) == 0 {
		return Properties{}, nil
	}
	p := &sfParser{s: strings.Join(parts, ", ")}
	return p.dictionary(lim.MaxCount)
}

func propErr(format string, args ...any) *Error {
	return &Error{Code: CodeInvalidProperties, Message: fmt.Sprintf(format, args...)}
}

type sfParser struct {
	s   string
	pos int
}

func (p *sfParser) done() bool { return p.pos >= len(p.s) }

func (p *sfParser) peek() byte { return p.s[p.pos] }

func (p *sfParser) skipSP() {
	for !p.done() && p.peek() == ' ' {
		p.pos++
	}
}

func (p *sfParser) skipOWS() {
	for !p.done() && (p.peek() == ' ' || p.peek() == '\t') {
		p.pos++
	}
}

// dictionary follows RFC 9651 section 4.2 (leading SP, members, trailing OWS).
func (p *sfParser) dictionary(maxCount int) (Properties, error) {
	out := Properties{}
	p.skipSP()
	for !p.done() {
		key, err := p.key()
		if err != nil {
			return nil, err
		}
		v := Value{Kind: KindBool, Bool: true}
		if !p.done() && p.peek() == '=' {
			p.pos++
			if v, err = p.bareItem(); err != nil {
				return nil, err
			}
		}
		if !p.done() && p.peek() == ';' {
			return nil, propErr("property %q: parameters are not accepted", key)
		}
		if _, dup := out[key]; dup {
			return nil, propErr("property %q is given more than once", key)
		}
		if len(out) >= maxCount {
			return nil, propErr("more than %d properties", max(maxCount, 0))
		}
		out[key] = v
		p.skipOWS()
		if p.done() {
			return out, nil
		}
		if p.peek() != ',' {
			return nil, propErr("expected a comma at byte %d", p.pos)
		}
		p.pos++
		p.skipOWS()
		if p.done() {
			return nil, propErr("trailing comma")
		}
	}
	return out, nil
}

func isLCAlpha(c byte) bool { return c >= 'a' && c <= 'z' }
func isDigit(c byte) bool   { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool   { return isLCAlpha(c) || (c >= 'A' && c <= 'Z') }

// isKeyChar reports whether c may follow the first character of a key (RFC 9651 3.1.2).
func isKeyChar(c byte) bool {
	return isLCAlpha(c) || isDigit(c) || c == '_' || c == '-' || c == '.' || c == '*'
}

// isTChar is RFC 9110 tchar.
func isTChar(c byte) bool {
	if isAlpha(c) || isDigit(c) {
		return true
	}
	return strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
}

// ValidPropertyName reports whether name is a valid property name (an RFC 9651 key).
func ValidPropertyName(name string) bool {
	if name == "" || (!isLCAlpha(name[0]) && name[0] != '*') {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !isKeyChar(name[i]) {
			return false
		}
	}
	return true
}

func (p *sfParser) key() (string, error) {
	if p.done() || (!isLCAlpha(p.peek()) && p.peek() != '*') {
		return "", propErr("expected a property name at byte %d", p.pos)
	}
	start := p.pos
	for !p.done() && isKeyChar(p.peek()) {
		p.pos++
	}
	return p.s[start:p.pos], nil
}

func (p *sfParser) bareItem() (Value, error) {
	if p.done() {
		return Value{}, propErr("missing value at byte %d", p.pos)
	}
	switch c := p.peek(); {
	case c == '-' || isDigit(c):
		return p.integer()
	case c == '"':
		return p.str()
	case isAlpha(c) || c == '*':
		return p.token(), nil
	case c == '?':
		return p.boolean()
	case c == '(':
		return Value{}, propErr("lists are not accepted (byte %d)", p.pos)
	default:
		return Value{}, propErr("unsupported value type at byte %d", p.pos)
	}
}

func (p *sfParser) integer() (Value, error) {
	start := p.pos
	if p.peek() == '-' {
		p.pos++
	}
	digits := p.pos
	for !p.done() && isDigit(p.peek()) {
		p.pos++
		if p.pos-digits > 15 {
			return Value{}, propErr("integer longer than 15 digits at byte %d", start)
		}
	}
	if p.pos == digits {
		return Value{}, propErr("expected a digit at byte %d", p.pos)
	}
	if !p.done() && p.peek() == '.' {
		return Value{}, propErr("decimals are not accepted (byte %d)", start)
	}
	n, err := strconv.ParseInt(p.s[start:p.pos], 10, 64)
	if err != nil || n > sfIntMax || n < -sfIntMax {
		return Value{}, propErr("integer out of range at byte %d", start)
	}
	return Value{Kind: KindInteger, Int: n}, nil
}

func (p *sfParser) str() (Value, error) {
	start := p.pos
	p.pos++ // opening quote
	var b strings.Builder
	for !p.done() {
		c := p.peek()
		p.pos++
		switch {
		case c == '\\':
			if p.done() {
				return Value{}, propErr("unterminated string at byte %d", start)
			}
			n := p.peek()
			if n != '"' && n != '\\' {
				return Value{}, propErr("invalid escape in string at byte %d", p.pos-1)
			}
			p.pos++
			b.WriteByte(n)
		case c == '"':
			return Value{Kind: KindString, Text: b.String()}, nil
		case c < 0x20 || c > 0x7e:
			return Value{}, propErr("invalid character in string at byte %d", p.pos-1)
		default:
			b.WriteByte(c)
		}
	}
	return Value{}, propErr("unterminated string at byte %d", start)
}

func (p *sfParser) token() Value {
	start := p.pos
	p.pos++
	for !p.done() && (isTChar(p.peek()) || p.peek() == ':' || p.peek() == '/') {
		p.pos++
	}
	return Value{Kind: KindToken, Text: p.s[start:p.pos]}
}

func (p *sfParser) boolean() (Value, error) {
	p.pos++ // '?'
	if p.done() {
		return Value{}, propErr("missing boolean at byte %d", p.pos)
	}
	c := p.peek()
	p.pos++
	switch c {
	case '0':
		return Value{Kind: KindBool, Bool: false}, nil
	case '1':
		return Value{Kind: KindBool, Bool: true}, nil
	}
	return Value{}, propErr("invalid boolean at byte %d", p.pos-1)
}
