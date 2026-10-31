package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"
)

// FormatV1 is the only rule format version this build reads. The format is documented in
// docs/modules/policy.md and rule_v1.schema.json. A new version gets a new number and its own
// decoder; a stored document is never reinterpreted.
const FormatV1 int64 = 1

// rule is a parsed, valid rule document.
type rule struct {
	id, name string
	position int64

	props  map[string]Value // nil: any properties
	models []string         // nil: any model

	deny     bool
	loc      *time.Location
	windows  []window // nil: no time restriction
	priority PriorityClass
	caps     Caps
}

// docV1 is format version 1. Every field is optional apart from format_version; nulls are
// refused before decoding, so a nil pointer means the field is absent.
type docV1 struct {
	FormatVersion int64       `json:"format_version"`
	When          *whenV1     `json:"when"`
	Deny          *bool       `json:"deny"`
	TimeZone      *string     `json:"time_zone"`
	Windows       *[]windowV1 `json:"windows"`
	Priority      *string     `json:"priority"`
	Caps          *capsV1     `json:"caps"`
}

type whenV1 struct {
	Properties *map[string]json.RawMessage `json:"properties"`
	Models     *[]string                   `json:"models"`
}

type windowV1 struct {
	Start *string `json:"start"`
	End   *string `json:"end"`
}

type capsV1 struct {
	ContextCap      *int64 `json:"context_cap"`
	MaxOutputTokens *int64 `json:"max_output_tokens"`
	MaxConcurrency  *int64 `json:"max_concurrency"`
}

// ValidateRule checks a rule document that is about to be stored or imported and returns the
// format version written in it, which the caller stores next to the body. maxBytes is the
// policy.rule.max_bytes setting (LoadLimits). Errors are *Error: CodeUnknownFormatVersion for
// a version this build does not know, CodeInvalidRule for anything else.
func ValidateRule(body []byte, maxBytes int) (int64, error) {
	v, err := docVersion(body, maxBytes)
	if err != nil {
		return 0, err
	}
	if _, err := parseRule(StoredRule{FormatVersion: v, Body: string(body), Enabled: true}, maxBytes); err != nil {
		return 0, err
	}
	return v, nil
}

// parseRule parses a stored rule. The document's format_version must equal the stored one.
func parseRule(sr StoredRule, maxBytes int) (*rule, error) {
	body := []byte(sr.Body)
	v, err := docVersion(body, maxBytes)
	if err != nil {
		return nil, err
	}
	if v != sr.FormatVersion {
		return nil, ruleErr("document format_version %d does not match the stored format version %d", v, sr.FormatVersion)
	}
	var d docV1
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return nil, ruleErr("%v", err)
	}
	r := &rule{id: sr.ID, name: sr.Name, position: sr.Position}
	if err := r.fromV1(d); err != nil {
		return nil, err
	}
	return r, nil
}

// docVersion checks the document's size and strict JSON shape and reads its format_version.
// An unknown version is reported before anything else about the document is checked.
func docVersion(body []byte, maxBytes int) (int64, error) {
	if len(body) > maxBytes {
		return 0, ruleErr("longer than %d bytes", max(maxBytes, 0))
	}
	if err := strictJSON(body); err != nil {
		return 0, err
	}
	var head struct {
		FormatVersion *json.RawMessage `json:"format_version"`
	}
	if err := json.Unmarshal(body, &head); err != nil {
		return 0, ruleErr("%v", err)
	}
	if head.FormatVersion == nil {
		return 0, ruleErr("format_version is missing")
	}
	v, err := strconv.ParseInt(string(*head.FormatVersion), 10, 64)
	if err != nil {
		return 0, ruleErr("format_version is not an integer")
	}
	if v != FormatV1 {
		return 0, &Error{Code: CodeUnknownFormatVersion, FormatVersion: v,
			Message: "format_version " + strconv.FormatInt(v, 10) + " is not supported by this build"}
	}
	return v, nil
}

// jsonKeyRE is the only shape an object key may have anywhere in a rule document: the format's
// field names and property names (RFC 9651 keys) are all lower case. This also stops
// encoding/json's case-insensitive field matching from accepting a differently spelled field.
var jsonKeyRE = regexp.MustCompile(`^[a-z*][a-z0-9_.*-]*$`)

// strictJSON refuses what encoding/json would quietly accept: invalid UTF-8, duplicate keys,
// keys outside jsonKeyRE, null values and trailing data after the document. The document must
// be one JSON object.
func strictJSON(body []byte) error {
	if !utf8.Valid(body) {
		return ruleErr("not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	type frame struct {
		object  bool
		keys    map[string]struct{}
		wantKey bool
	}
	var stack []*frame
	first := true
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			if first {
				return ruleErr("empty document")
			}
			return nil
		}
		if err != nil {
			return ruleErr("%v", err)
		}
		if len(stack) == 0 && !first {
			return ruleErr("trailing data after the document")
		}
		if first {
			if d, ok := tok.(json.Delim); !ok || d != '{' {
				return ruleErr("the document must be a JSON object")
			}
		}
		first = false
		var top *frame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		if top != nil && top.object && top.wantKey {
			if d, ok := tok.(json.Delim); ok && d == '}' {
				stack = stack[:len(stack)-1]
				continue
			}
			k, ok := tok.(string)
			if !ok {
				return ruleErr("object key is not a string")
			}
			if !jsonKeyRE.MatchString(k) {
				return ruleErr("invalid key %q", k)
			}
			if _, dup := top.keys[k]; dup {
				return ruleErr("key %q is given more than once", k)
			}
			top.keys[k] = struct{}{}
			top.wantKey = false
			continue
		}
		// A value (or the end of an array).
		if top != nil && top.object {
			top.wantKey = true
		}
		switch t := tok.(type) {
		case nil:
			return ruleErr("null is not accepted")
		case json.Delim:
			switch t {
			case '{':
				stack = append(stack, &frame{object: true, keys: map[string]struct{}{}, wantKey: true})
			case '[':
				stack = append(stack, &frame{})
			case ']':
				stack = stack[:len(stack)-1]
				// The array was the value of its parent key; wantKey was set when it opened.
			}
		}
	}
}

func (r *rule) fromV1(d docV1) error {
	effects := 0
	if d.When != nil {
		if d.When.Properties == nil && d.When.Models == nil {
			return ruleErr("when is empty; leave it out to match every request")
		}
		if d.When.Properties != nil {
			if len(*d.When.Properties) == 0 {
				return ruleErr("when.properties is empty")
			}
			r.props = map[string]Value{}
			for _, k := range slices.Sorted(maps.Keys(*d.When.Properties)) {
				raw := (*d.When.Properties)[k]
				if !ValidPropertyName(k) {
					return &Error{Code: CodeInvalidRule, Property: k, Message: "when.properties: invalid property name"}
				}
				v, err := matchValue(raw)
				if err != nil {
					return &Error{Code: CodeInvalidRule, Property: k, Message: "when.properties: " + err.Error()}
				}
				r.props[k] = v
			}
		}
		if d.When.Models != nil {
			if len(*d.When.Models) == 0 {
				return ruleErr("when.models is empty")
			}
			for _, m := range *d.When.Models {
				if m == "" {
					return ruleErr("when.models holds an empty name")
				}
			}
			r.models = *d.When.Models
		}
	}
	if d.Deny != nil {
		r.deny = *d.Deny
		if r.deny {
			effects++
		}
	}
	if d.Windows != nil {
		if len(*d.Windows) == 0 {
			return ruleErr("windows is empty")
		}
		if d.TimeZone == nil {
			return ruleErr("windows needs time_zone")
		}
		loc, err := loadZone(*d.TimeZone)
		if err != nil {
			return err
		}
		r.loc = loc
		for i, w := range *d.Windows {
			pw, err := parseWindow(w)
			if err != nil {
				return ruleErr("windows[%d]: %v", i, err)
			}
			r.windows = append(r.windows, pw)
		}
		effects++
	} else if d.TimeZone != nil {
		return ruleErr("time_zone is set without windows")
	}
	if d.Priority != nil {
		switch p := PriorityClass(*d.Priority); p {
		case Interactive, Background:
			r.priority = p
		default:
			return ruleErr("priority must be %q or %q", Interactive, Background)
		}
		effects++
	}
	if d.Caps != nil {
		c := d.Caps
		if c.ContextCap == nil && c.MaxOutputTokens == nil && c.MaxConcurrency == nil {
			return ruleErr("caps is empty")
		}
		for _, f := range []struct {
			name string
			v    *int64
		}{{"context_cap", c.ContextCap}, {"max_output_tokens", c.MaxOutputTokens}, {"max_concurrency", c.MaxConcurrency}} {
			if f.v != nil && *f.v < 1 {
				return ruleErr("caps.%s must be at least 1", f.name)
			}
		}
		r.caps = Caps{ContextCap: c.ContextCap, MaxOutputTokens: c.MaxOutputTokens, MaxConcurrency: c.MaxConcurrency}
		effects++
	}
	if effects == 0 {
		return ruleErr("the rule has no effect (deny, windows, priority or caps)")
	}
	if r.deny && effects > 1 {
		return ruleErr("deny cannot be combined with windows, priority or caps")
	}
	return nil
}

// matchValue reads a when.properties value: true/false, an integer in the RFC 9651 range, or
// a string (which matches a token or a string property value of the same text).
func matchValue(raw json.RawMessage) (Value, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return Value{Kind: KindBool, Bool: b}, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		for i := 0; i < len(s); i++ {
			if s[i] < 0x20 || s[i] > 0x7e {
				return Value{}, errors.New("string values may hold only printable ASCII, as in the header")
			}
		}
		return Value{Kind: KindString, Text: s}, nil
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return Value{}, errors.New("value must be a boolean, an integer or a string")
	}
	if n > sfIntMax || n < -sfIntMax {
		return Value{}, errors.New("integer out of range")
	}
	return Value{Kind: KindInteger, Int: n}, nil
}

// matches reports whether the rule's when applies to the request.
func (r *rule) matches(props Properties, model string) bool {
	if r.models != nil {
		found := false
		for _, m := range r.models {
			if m == model {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for k, want := range r.props {
		got, ok := props[k]
		if !ok || !sameValue(want, got) {
			return false
		}
	}
	return true
}

// sameValue compares a rule's value with a request's. A rule string matches a token or a
// string with the same text.
func sameValue(want, got Value) bool {
	switch want.Kind {
	case KindBool:
		return got.Kind == KindBool && got.Bool == want.Bool
	case KindInteger:
		return got.Kind == KindInteger && got.Int == want.Int
	default:
		return (got.Kind == KindToken || got.Kind == KindString) && got.Text == want.Text
	}
}
