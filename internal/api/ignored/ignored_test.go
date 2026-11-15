package ignored_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/api/ignored"
)

func TestIgnoredFieldsWalker(t *testing.T) {
	entry := `{"metric":"tokens","limit":5,"window_seconds":60`
	for _, tc := range []struct {
		name string
		raw  string
		req  any
		want []string
	}{
		{"no unknown field", `{"models":[{"model":"m","context_limit":1,"max_output_tokens":1}],"quotas":[` + entry + `}],"concurrency":1,"on_context_overflow":"refuse","allowed_request_properties":[],"expires_at":null}`,
			&api.GrantLimits{}, nil},
		{"nested arrays", `{"models":[{"model":"m","a":1},{"model":"m","b":{"c":1}}],"quotas":[` + entry + `,"d":1},` + entry + `}],"e":[1]}`,
			&api.GrantLimits{}, []string{"e is not a known field and was ignored", "models.0.a is not a known field and was ignored",
				"models.1.b is not a known field and was ignored", "quotas.0.d is not a known field and was ignored"}},
		{"opt wrappers", `{"narrowing":{"quota":{"tokens":1,"z":1},"context_limit":3,"y":1},"expires_at":"2027-01-01T00:00:00Z"}`,
			&api.AccessKeyCreate{}, []string{"narrowing.quota.z is not a known field and was ignored", "narrowing.y is not a known field and was ignored"}},
		{"null wrappers", `{"narrowing":null,"name":null,"expires_at":null}`, &api.AccessKeyUpdate{}, nil},
		{"update with a narrowing", `{"narrowing":{"quotas":[` + entry + `,"q":1}]}}`, &api.AccessKeyUpdate{},
			[]string{"narrowing.quotas.0.q is not a known field and was ignored"}},
		{"case sensitive", `{"Display_Name":"x"}`, &api.ConsumerUpdate{}, []string{"Display_Name is not a known field and was ignored"}},
		{"free-form map", `{"rule":{"anything":{"goes":1}},"name":"n"}`, &api.PolicyRuleCreate{}, nil},
		{"unsafe name", "{\"bad\\u0001\":1,\"narrowing\":{\"" + strings.Repeat("x", 65) + "\":1}}", &api.AccessKeyCreate{},
			[]string{"a field with a name that is empty, unprintable or too long was ignored",
				"a field of narrowing with a name that is empty, unprintable or too long was ignored"}},
		{"empty name", `{"":1}`, &api.InviteCreate{}, []string{"a field with a name that is empty, unprintable or too long was ignored"}},
		{"not an object", `[1,2]`, &api.InviteCreate{}, nil},
		{"not json", `{"a":`, &api.InviteCreate{}, nil},
		{"empty body", ``, &api.InviteCreate{}, nil},
		{"no request", `{"a":1}`, nil, nil},
		{"a type with no body", `{"a":1}`, "text", nil},
	} {
		got := ignored.Fields([]byte(tc.raw), tc.req)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
