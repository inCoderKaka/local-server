package api

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/pb33f/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

const examplesRefPrefix = "#/components/examples/"

// TestSpecExamplesMatchSchemas checks the examples in api/openapi.yaml against
// the schemas they illustrate: every media type that has a schema carries an
// example, each example validates against that schema, each schema-level and
// property-level example (in component schemas, and in the schemas of
// parameters, headers and media types) validates against the schema it sits
// in, and no component example is unused.
func TestSpecExamplesMatchSchemas(t *testing.T) {
	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var parsed any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	asJSON, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("convert spec to JSON: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(asJSON))
	dec.UseNumber()
	var docAny any
	if err := dec.Decode(&docAny); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	doc, ok := docAny.(map[string]any)
	if !ok {
		t.Fatalf("spec root is %T, want object", docAny)
	}

	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	if err := c.AddResource("spec.json", docAny); err != nil {
		t.Fatalf("add spec resource: %v", err)
	}

	esc := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
	}
	obj := func(v any, key string) map[string]any {
		m, _ := v.(map[string]any)
		out, _ := m[key].(map[string]any)
		return out
	}
	sortedKeys := func(m map[string]any) []string {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	}
	isRef := func(m map[string]any) bool {
		_, ok := m["$ref"].(string)
		return ok
	}

	components := obj(doc, "components")
	compExamples := obj(components, "examples")

	validate := func(where, schemaPtr string, value any) {
		sch, err := c.Compile("spec.json#" + schemaPtr)
		if err != nil {
			t.Errorf("%s: compile schema %s: %v", where, schemaPtr, err)
			return
		}
		if err := sch.Validate(value); err != nil {
			t.Errorf("%s: example does not match %s: %v", where, schemaPtr, err)
		}
	}

	// walkSchema validates the example and examples of the schema at ptr and of
	// every schema nested in it. It follows the JSON Schema keywords that hold
	// schemas, not every key, because a property can be named "properties" and
	// example values are data, not schemas.
	visited := map[string]bool{}
	var walkSchema func(where, ptr string, s map[string]any)
	walkSchema = func(where, ptr string, s map[string]any) {
		visited[ptr] = true
		if ex, ok := s["example"]; ok {
			validate(where+" "+ptr+" example", ptr, ex)
		}
		if exs, ok := s["examples"].([]any); ok {
			for i, ex := range exs {
				validate(where+" "+ptr+" examples["+strconv.Itoa(i)+"]", ptr, ex)
			}
		}
		for _, kw := range []string{"properties", "patternProperties", "$defs", "dependentSchemas"} {
			children := obj(s, kw)
			for _, name := range sortedKeys(children) {
				if child, _ := children[name].(map[string]any); child != nil {
					walkSchema(where, ptr+"/"+kw+"/"+esc(name), child)
				}
			}
		}
		for _, kw := range []string{
			"items", "additionalProperties", "not", "if", "then", "else",
			"contains", "propertyNames", "unevaluatedItems", "unevaluatedProperties",
		} {
			if child, _ := s[kw].(map[string]any); child != nil {
				walkSchema(where, ptr+"/"+kw, child)
			}
		}
		for _, kw := range []string{"allOf", "anyOf", "oneOf", "prefixItems"} {
			children, _ := s[kw].([]any)
			for i, v := range children {
				if child, _ := v.(map[string]any); child != nil {
					walkSchema(where, ptr+"/"+kw+"/"+strconv.Itoa(i), child)
				}
			}
		}
	}

	// walkParams walks the schema of each inline parameter in a parameters array.
	walkParams := func(where, ptr string, params any) {
		list, _ := params.([]any)
		for i, v := range list {
			param, _ := v.(map[string]any)
			if param == nil || isRef(param) {
				continue
			}
			if sch, _ := param["schema"].(map[string]any); sch != nil {
				walkSchema(where+" parameter", ptr+"/"+strconv.Itoa(i)+"/schema", sch)
			}
		}
	}

	checkMedia := func(where, ptr string, media map[string]any) {
		if _, ok := media["schema"]; !ok {
			return
		}
		schemaPtr := ptr + "/schema"
		if sch, _ := media["schema"].(map[string]any); sch != nil {
			walkSchema(where, schemaPtr, sch)
		}
		example, hasExample := media["example"]
		examples, _ := media["examples"].(map[string]any)
		if !hasExample && len(examples) == 0 {
			t.Errorf("%s: media type has a schema but no example", where)
			return
		}
		if hasExample {
			validate(where+" example", schemaPtr, example)
		}
		for _, key := range sortedKeys(examples) {
			entry, _ := examples[key].(map[string]any)
			at := where + " examples." + key
			if ref, ok := entry["$ref"].(string); ok {
				name, found := strings.CutPrefix(ref, examplesRefPrefix)
				if !found {
					t.Errorf("%s: unexpected $ref %q", at, ref)
					continue
				}
				entry, _ = compExamples[name].(map[string]any)
				if entry == nil {
					t.Errorf("%s: $ref %q has no component example", at, ref)
					continue
				}
			}
			value, ok := entry["value"]
			if !ok {
				t.Errorf("%s: example has no value", at)
				continue
			}
			validate(at, schemaPtr, value)
		}
	}

	walkContent := func(where, ptr string, holder map[string]any) {
		content := obj(holder, "content")
		for _, ct := range sortedKeys(content) {
			media, _ := content[ct].(map[string]any)
			if media == nil {
				continue
			}
			checkMedia(where+" "+ct, ptr+"/content/"+esc(ct), media)
		}
	}

	walkItems := func(where, ptr string, items map[string]any) {
		for _, name := range sortedKeys(items) {
			item, _ := items[name].(map[string]any)
			walkParams(where+" "+name, ptr+"/"+esc(name)+"/parameters", item["parameters"])
			for _, method := range sortedKeys(item) {
				if method == "parameters" {
					continue
				}
				op, _ := item[method].(map[string]any)
				if op == nil {
					continue
				}
				opPtr := ptr + "/" + esc(name) + "/" + method
				opWhere := where + " " + name + " " + method
				walkParams(opWhere, opPtr+"/parameters", op["parameters"])
				if rb, _ := op["requestBody"].(map[string]any); rb != nil && !isRef(rb) {
					walkContent(opWhere+" request", opPtr+"/requestBody", rb)
				}
				responses := obj(op, "responses")
				for _, code := range sortedKeys(responses) {
					resp, _ := responses[code].(map[string]any)
					if resp == nil || isRef(resp) {
						continue
					}
					walkContent(opWhere+" response "+code, opPtr+"/responses/"+code, resp)
				}
			}
		}
	}

	walkItems("path", "/paths", obj(doc, "paths"))
	walkItems("webhook", "/webhooks", obj(doc, "webhooks"))

	compResponses := obj(components, "responses")
	for _, name := range sortedKeys(compResponses) {
		resp, _ := compResponses[name].(map[string]any)
		if resp == nil {
			continue
		}
		walkContent("response "+name, "/components/responses/"+esc(name), resp)
	}

	schemas := obj(components, "schemas")
	for _, name := range sortedKeys(schemas) {
		sch, _ := schemas[name].(map[string]any)
		if sch == nil {
			continue
		}
		walkSchema("schema "+name, "/components/schemas/"+esc(name), sch)
	}

	for _, kind := range []string{"parameters", "headers"} {
		holders := obj(components, kind)
		for _, name := range sortedKeys(holders) {
			holder, _ := holders[name].(map[string]any)
			if sch, _ := holder["schema"].(map[string]any); sch != nil {
				walkSchema(kind+" "+name, "/components/"+kind+"/"+esc(name)+"/schema", sch)
			}
		}
	}

	// A walker that stops early would pass without checking; make sure it
	// reached a nested property and a parameter schema.
	for _, ptr := range []string{
		"/components/schemas/UsageProof/properties/tree_size",
		"/components/parameters/ResourceRequestStatusFilter/schema",
	} {
		if !visited[ptr] {
			t.Errorf("schema walker never visited %s", ptr)
		}
	}

	// Every component example is used somewhere.
	used := map[string]bool{}
	var collect func(v any)
	collect = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if ref, ok := x["$ref"].(string); ok {
				if name, found := strings.CutPrefix(ref, examplesRefPrefix); found {
					used[name] = true
				}
			}
			for _, child := range x {
				collect(child)
			}
		case []any:
			for _, child := range x {
				collect(child)
			}
		}
	}
	collect(doc)
	for _, name := range sortedKeys(compExamples) {
		if !used[name] {
			t.Errorf("component example %s is never referenced", name)
		}
	}

	t.Run("validator rejects an invalid value", func(t *testing.T) {
		sch, err := c.Compile("spec.json#/components/schemas/Error")
		if err != nil {
			t.Fatalf("compile Error schema: %v", err)
		}
		if err := sch.Validate(map[string]any{}); err == nil {
			t.Fatal("empty object validated against Error; the checker is not checking")
		}
	})
}
