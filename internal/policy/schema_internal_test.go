package policy

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestSchemaMatchesDecoder keeps rule_v1.schema.json and the v1 decoder in step: every object
// in the schema lists exactly the fields the decoder reads.
func TestSchemaMatchesDecoder(t *testing.T) {
	raw, err := os.ReadFile("rule_v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Properties map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Items      struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	keys := func(m any) []string {
		var out []string
		for _, k := range reflect.ValueOf(m).MapKeys() {
			out = append(out, k.String())
		}
		slices.Sort(out)
		return out
	}
	tags := func(v any) []string {
		var out []string
		rt := reflect.TypeOf(v)
		for i := range rt.NumField() {
			out = append(out, strings.Split(rt.Field(i).Tag.Get("json"), ",")[0])
		}
		slices.Sort(out)
		return out
	}
	check := func(what string, schema, decoder []string) {
		if !slices.Equal(schema, decoder) {
			t.Errorf("%s: schema %v, decoder %v", what, schema, decoder)
		}
	}
	check("document", keys(s.Properties), tags(docV1{}))
	check("when", keys(s.Properties["when"].Properties), tags(whenV1{}))
	check("caps", keys(s.Properties["caps"].Properties), tags(capsV1{}))
	check("windows[]", keys(s.Properties["windows"].Items.Properties), tags(windowV1{}))
}
