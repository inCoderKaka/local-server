package policy_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/policy"
)

func TestValidateRule(t *testing.T) {
	good := []string{
		`{"format_version":1,"deny":true}`,
		`{"format_version":1,"priority":"interactive"}`,
		`{"format_version":1,"caps":{"max_output_tokens":1}}`,
		backgroundHours,
		` {"format_version":1,"time_zone":"Local","windows":[{"start":"22:00","end":"00:00"},{"start":"00:00","end":"07:00"}]} ` + "\n",
		`{"format_version":1,"time_zone":"America/Argentina/Buenos_Aires","windows":[{"start":"09:00","end":"17:00"}]}`,
		`{"format_version":1,"when":{"models":["llama3.1:8b"],"properties":{"tier":-3,"purpose":"a b","*x":false}},"priority":"background"}`,
		`{"format_version":1,"deny":false,"priority":"background"}`,
	}
	for _, b := range good {
		v, err := policy.ValidateRule([]byte(b), 8192)
		if err != nil || v != policy.FormatV1 {
			t.Errorf("%s: v=%d err=%v", b, v, err)
		}
	}
	bad := []struct{ name, body string }{
		{"not JSON", `nope`},
		{"empty", ``},
		{"array", `[1]`},
		{"missing format_version", `{"deny":true}`},
		{"string format_version", `{"format_version":"1","deny":true}`},
		{"decimal format_version", `{"format_version":1.0,"deny":true}`},
		{"unknown field", `{"format_version":1,"deny":true,"extra":1}`},
		{"unknown nested field", `{"format_version":1,"caps":{"context_cap":1,"other":2}}`},
		{"case-folded field", `{"format_version":1,"Deny":true}`},
		{"unicode-folded field", `{"format_version":1,"cap\u017f":{"context_cap":1}}`},
		{"unicode-folded field, raw", "{\"format_version\":1,\"cap\u017f\":{\"context_cap\":1}}"},
		{"Cyrillic field", "{\"format_version\":1,\"den\u0443\":true}"},
		{"duplicate key", `{"format_version":1,"deny":true,"deny":false}`},
		{"escaped duplicate key", `{"format_version":1,"deny":true,"deny":true}`},
		{"null", `{"format_version":1,"deny":null,"priority":"background"}`},
		{"trailing data", `{"format_version":1,"deny":true} {}`},
		{"trailing garbage", `{"format_version":1,"deny":true}x`},
		{"invalid UTF-8", "{\"format_version\":1,\"when\":{\"models\":[\"\xff\"]},\"deny\":true}"},
		{"no effect", `{"format_version":1,"when":{"models":["m"]}}`},
		{"deny false only", `{"format_version":1,"deny":false}`},
		{"deny with priority", `{"format_version":1,"deny":true,"priority":"background"}`},
		{"empty when", `{"format_version":1,"when":{},"deny":true}`},
		{"empty properties", `{"format_version":1,"when":{"properties":{}},"deny":true}`},
		{"empty models", `{"format_version":1,"when":{"models":[]},"deny":true}`},
		{"empty model name", `{"format_version":1,"when":{"models":[""]},"deny":true}`},
		{"bad property name", `{"format_version":1,"when":{"properties":{"Bad":true}},"deny":true}`},
		{"decimal property value", `{"format_version":1,"when":{"properties":{"a":1.5}},"deny":true}`},
		{"object property value", `{"format_version":1,"when":{"properties":{"a":{"b":1}}},"deny":true}`},
		{"out of range property value", `{"format_version":1,"when":{"properties":{"a":1000000000000000}},"deny":true}`},
		{"non-ASCII property value", `{"format_version":1,"when":{"properties":{"a":"é"}},"deny":true}`},
		{"windows without zone", `{"format_version":1,"windows":[{"start":"00:00","end":"07:00"}]}`},
		{"zone without windows", `{"format_version":1,"time_zone":"UTC","deny":true}`},
		{"unknown zone", `{"format_version":1,"time_zone":"Mars/Olympus","windows":[{"start":"00:00","end":"07:00"}]}`},
		{"path zone", `{"format_version":1,"time_zone":"../../etc/passwd","windows":[{"start":"00:00","end":"07:00"}]}`},
		{"empty windows", `{"format_version":1,"time_zone":"UTC","windows":[]}`},
		{"window missing end", `{"format_version":1,"time_zone":"UTC","windows":[{"start":"00:00"}]}`},
		{"one-digit hour", `{"format_version":1,"time_zone":"UTC","windows":[{"start":"7:00","end":"08:00"}]}`},
		{"24:00", `{"format_version":1,"time_zone":"UTC","windows":[{"start":"22:00","end":"24:00"}]}`},
		{"equal start and end", `{"format_version":1,"time_zone":"UTC","windows":[{"start":"07:00","end":"07:00"}]}`},
		{"unknown priority", `{"format_version":1,"priority":"urgent"}`},
		{"empty caps", `{"format_version":1,"caps":{}}`},
		{"zero cap", `{"format_version":1,"caps":{"context_cap":0}}`},
		{"negative cap", `{"format_version":1,"caps":{"max_concurrency":-1}}`},
		{"too large", `{"format_version":1,"deny":true,"when":{"models":["` + strings.Repeat("m", 9000) + `"]}}`},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			_, err := policy.ValidateRule([]byte(c.body), 8192)
			if !errors.Is(err, policy.ErrInvalidRule) {
				t.Fatalf("err = %v, want invalid_policy_rule", err)
			}
		})
	}
}

func TestValidateRuleUnknownVersion(t *testing.T) {
	for _, b := range []string{
		`{"format_version":2,"deny":true}`,
		`{"format_version":0}`,
		`{"format_version":-1,"whatever":[1,2]}`,
	} {
		_, err := policy.ValidateRule([]byte(b), 8192)
		if !errors.Is(err, policy.ErrUnknownFormatVersion) {
			t.Errorf("%s: err = %v, want unknown_rule_format_version", b, err)
		}
	}
}
