package policy_test

import (
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/policy"
)

func TestParseProperties(t *testing.T) {
	lim := policy.PropertyLimits{MaxBytes: 128, MaxCount: 5}
	ok := []struct {
		name  string
		lines []string
		want  policy.Properties
	}{
		{"absent", nil, policy.Properties{}},
		{"empty lines", []string{"", "  \t"}, policy.Properties{}},
		{"spec example", []string{"background, purpose=summarize"}, policy.Properties{
			"background": {Kind: policy.KindBool, Bool: true}, "purpose": {Kind: policy.KindToken, Text: "summarize"}}},
		{"all value types", []string{`a=?0, b=-42, c="x \"y\" \\z", d=tok/en:1, *e`}, policy.Properties{
			"a": {Kind: policy.KindBool}, "b": {Kind: policy.KindInteger, Int: -42},
			"c": {Kind: policy.KindString, Text: `x "y" \z`}, "d": {Kind: policy.KindToken, Text: "tok/en:1"},
			"*e": {Kind: policy.KindBool, Bool: true}}},
		{"two field lines combine", []string{"a", "b=1"}, policy.Properties{
			"a": {Kind: policy.KindBool, Bool: true}, "b": {Kind: policy.KindInteger, Int: 1}}},
		{"whitespace", []string{"  a ,\tb  "}, policy.Properties{
			"a": {Kind: policy.KindBool, Bool: true}, "b": {Kind: policy.KindBool, Bool: true}}},
		{"15-digit integer", []string{"n=999999999999999"}, policy.Properties{"n": {Kind: policy.KindInteger, Int: 999999999999999}}},
	}
	for _, c := range ok {
		t.Run(c.name, func(t *testing.T) {
			got, err := policy.ParseProperties(c.lines, lim)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for k, v := range c.want {
				if got[k] != v {
					t.Fatalf("%s: got %+v, want %+v", k, got[k], v)
				}
			}
		})
	}
	bad := []struct{ name, in string }{
		{"upper-case key", "Background"},
		{"leading digit key", "1a"},
		{"decimal", "a=1.5"},
		{"16-digit integer", "a=1000000000000000"},
		{"byte sequence", "a=:aGk=:"},
		{"date", "a=@1659578233"},
		{"display string", `a=%"x"`},
		{"inner list", "a=(b c)"},
		{"parameters", "a;p=1"},
		{"value parameters", "a=b;p=1"},
		{"duplicate key", "a, a=?0"},
		{"trailing comma", "a,"},
		{"leading comma", ",a"},
		{"missing comma", "a b"},
		{"bad boolean", "a=?2"},
		{"bad escape", `a="\n"`},
		{"unterminated string", `a="x`},
		{"non-ASCII in string", "a=\"\xc3\xa9\""},
		{"control in string", "a=\"\x01\""},
		{"empty value", "a="},
		{"leading tab", "\ta"},
		{"minus alone", "a=-"},
		{"too many", "a,b,c,d,e,f"},
		{"too long", "a=\"" + strings.Repeat("x", 200) + "\""},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			got, err := policy.ParseProperties([]string{c.in}, lim)
			if policy.CodeOf(err) != policy.CodeInvalidProperties {
				t.Fatalf("got %v, %v; want invalid_request_properties", got, err)
			}
			if strings.Contains(err.Error(), "summ") {
				t.Fatalf("error quotes a value: %v", err)
			}
		})
	}
}

func TestParsePropertiesLimitsZero(t *testing.T) {
	if _, err := policy.ParseProperties([]string{"a"}, policy.PropertyLimits{MaxBytes: 0, MaxCount: 4}); policy.CodeOf(err) != policy.CodeInvalidProperties {
		t.Fatalf("max_bytes 0: %v", err)
	}
	if _, err := policy.ParseProperties([]string{"a"}, policy.PropertyLimits{MaxBytes: 64, MaxCount: 0}); policy.CodeOf(err) != policy.CodeInvalidProperties {
		t.Fatalf("max_count 0: %v", err)
	}
	if p, err := policy.ParseProperties(nil, policy.PropertyLimits{}); err != nil || len(p) != 0 {
		t.Fatalf("absent header with zero limits: %v %v", p, err)
	}
}

func TestErrorNeverQuotesAValue(t *testing.T) {
	_, err := policy.ParseProperties([]string{`purpose="secret-value", purpose=x`}, policy.PropertyLimits{MaxBytes: 128, MaxCount: 4})
	if err == nil || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("err = %v", err)
	}
}
