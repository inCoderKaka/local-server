package admin

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ogen-go/ogen/middleware"
)

// Unknown request fields (req_001 D-105). The /admin/v1 contract is additive only, so a newer
// client may send a field this host does not know yet. The generated decoders skip such a field
// without a trace; this file finds it again so the answer can name it in its warnings list.
//
// Only a field's name is ever kept, never its value, and the request bytes are held no longer than
// the call that reads them (req_001 D-31, D-40).

type ignoredKey struct{}

// ignoredMiddleware runs after ogen decoded a request body and before the handler. It finds the
// fields of the body that the decoded request type has no place for and hands their warnings to
// the handler through the context (see withIgnored).
func ignoredMiddleware(req middleware.Request, next middleware.Next) (middleware.Response, error) {
	if w := ignoredFields(req.RawBody, req.Body); len(w) > 0 {
		req.SetContext(context.WithValue(req.Context, ignoredKey{}, w))
	}
	return next(req)
}

// withIgnored returns the warnings of a handler followed by one warning per request field the host
// ignored. It returns warnings itself when no field was ignored, so an answer without any warning
// carries no warnings list.
func withIgnored(ctx context.Context, warnings []string) []string {
	w, _ := ctx.Value(ignoredKey{}).([]string)
	if len(w) == 0 {
		return warnings
	}
	out := make([]string, 0, len(warnings)+len(w))
	out = append(out, warnings...)
	return append(out, w...)
}

// ignoredFields returns one warning for each field of the JSON body raw that the decoded request
// req has no field for: nil when there is none. Names are exact and case sensitive, the same as the
// generated decoder reads them. Paths are dotted, and an array element is its index
// (`narrowing.quotas.0.bogus`). The order is the order of the walk: object keys sorted, array
// elements by index. A free-form value (a map, raw JSON, an interface or a one-of type) is not
// walked. A name that is not safe to echo is not echoed (see safeIgnoredName).
func ignoredFields(raw []byte, req any) []string {
	if len(raw) == 0 || req == nil {
		return nil
	}
	var top json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil
	}
	var out []string
	walkIgnored(top, reflect.TypeOf(req), "", &out)
	return out
}

func walkIgnored(raw json.RawMessage, t reflect.Type, path string, out *[]string) {
	t = unwrapIgnored(t)
	switch t.Kind() {
	case reflect.Struct:
		known, ok := jsonFields(t)
		if !ok {
			return // no json names: a one-of type or another free-form value
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil || obj == nil {
			return
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			ft, isKnown := known[k]
			switch {
			case isKnown:
				walkIgnored(obj[k], ft, joinPath(path, k), out)
			case safeIgnoredName(k):
				*out = append(*out, joinPath(path, k)+" is not a known field and was ignored")
			case path == "":
				*out = append(*out, "a field with a name that is empty, unprintable or too long was ignored")
			default:
				*out = append(*out, "a field of "+path+" with a name that is empty, unprintable or too long was ignored")
			}
		}
	case reflect.Slice:
		if k := unwrapIgnored(t.Elem()).Kind(); k != reflect.Struct && k != reflect.Slice {
			return
		}
		var elems []json.RawMessage
		if json.Unmarshal(raw, &elems) != nil {
			return
		}
		for i, e := range elems {
			walkIgnored(e, t.Elem(), joinPath(path, strconv.Itoa(i)), out)
		}
	}
}

// unwrapIgnored strips pointers and the Opt, OptNil and Nil wrappers of the generated types
// (a struct of Value plus Set and Null) down to the type the JSON value is read as.
func unwrapIgnored(t reflect.Type) reflect.Type {
	for {
		switch {
		case t.Kind() == reflect.Pointer:
			t = t.Elem()
		case isOptWrapper(t):
			f, _ := t.FieldByName("Value")
			t = f.Type
		default:
			return t
		}
	}
}

func isOptWrapper(t reflect.Type) bool {
	if t.Kind() != reflect.Struct || t.NumField() < 2 || t.NumField() > 3 {
		return false
	}
	hasValue := false
	for i := 0; i < t.NumField(); i++ {
		switch t.Field(i).Name {
		case "Value":
			hasValue = true
		case "Set", "Null":
		default:
			return false
		}
	}
	return hasValue
}

// jsonFields maps the JSON names of a generated struct to its field types. ok is false when the
// struct has no JSON names at all, which is how the one-of types look.
func jsonFields(t reflect.Type) (fields map[string]reflect.Type, ok bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if !f.IsExported() || name == "" || name == "-" {
			continue
		}
		if fields == nil {
			fields = map[string]reflect.Type{}
		}
		fields[name] = f.Type
	}
	return fields, fields != nil
}

func joinPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "." + name
}

// safeIgnoredName reports whether an unknown field's name may be echoed in a warning: valid text
// of printable characters, no longer than maxFieldName bytes. Unlike safeName it allows a space or
// any letter, since a client's new field is named by a person and JSON encoding escapes the rest.
// The JSON decoder turns a byte that is not UTF-8 into U+FFFD, so that character is refused too.
func safeIgnoredName(s string) bool {
	if s == "" || len(s) > maxFieldName || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == utf8.RuneError || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}
