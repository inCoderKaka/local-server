package policy_test

import (
	"errors"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/policy"
)

func i64(v int64) *int64   { return &v }
func str(v string) *string { return &v }

var lim = policy.Limits{Properties: policy.PropertyLimits{MaxBytes: 1024, MaxCount: 16}, RuleMaxBytes: 8192}

func grant() policy.Grant {
	return policy.Grant{
		ID:             "g1",
		Models:         []policy.GrantModel{{Name: "llama3.1:8b", ContextCap: i64(8192), MaxOutputTokens: i64(1024)}, {Name: "nomic-embed-text"}},
		MaxConcurrency: i64(4),
	}
}

func global(id string, pos int64, body string) policy.StoredRule {
	return policy.StoredRule{ID: id, Name: id, Position: pos, FormatVersion: 1, Body: body, Enabled: true}
}

func scoped(id, grantID string, pos int64, body string) policy.StoredRule {
	r := global(id, pos, body)
	r.GrantID = &grantID
	return r
}

// at returns a UTC time on 2026-10-31 at hh:mm.
func at(hh, mm int) time.Time { return time.Date(2026, 10, 31, hh, mm, 0, 0, time.UTC) }

const backgroundHours = `{"format_version":1,"when":{"properties":{"background":true}},"time_zone":"UTC","windows":[{"start":"00:00","end":"07:00"}],"priority":"background"}`

func TestDecide(t *testing.T) {
	type tc struct {
		name     string
		rules    []policy.StoredRule
		props    []string
		model    string
		time     time.Time
		mutate   func(*policy.Grant)
		wantCode policy.Code
		want     policy.Decision
	}
	interactive := policy.Decision{Priority: policy.Interactive, Caps: policy.Caps{ContextCap: i64(8192), MaxOutputTokens: i64(1024), MaxConcurrency: i64(4)}}
	background := interactive
	background.Priority = policy.Background
	cases := []tc{
		{name: "no rules, no properties", model: "llama3.1:8b", time: at(12, 0), want: interactive},
		{name: "background request outside its window is denied", rules: []policy.StoredRule{global("bg", 0, backgroundHours)},
			props: []string{"background"}, model: "llama3.1:8b", time: at(12, 0), wantCode: policy.CodeOutsideWindow},
		{name: "background request inside its window is background", rules: []policy.StoredRule{global("bg", 0, backgroundHours)},
			props: []string{"background"}, model: "llama3.1:8b", time: at(3, 0), want: background},
		{name: "window end is exclusive", rules: []policy.StoredRule{global("bg", 0, backgroundHours)},
			props: []string{"background"}, model: "llama3.1:8b", time: at(7, 0), wantCode: policy.CodeOutsideWindow},
		{name: "explicit background=?0 does not match the background rule", rules: []policy.StoredRule{global("bg", 0, backgroundHours)},
			props: []string{"background=?0"}, model: "llama3.1:8b", time: at(12, 0), want: interactive},
		{name: "interactive request is not restricted by the background window", rules: []policy.StoredRule{global("bg", 0, backgroundHours)},
			model: "llama3.1:8b", time: at(12, 0), want: interactive},
		{name: "window crossing midnight, before midnight", props: []string{"background"}, model: "llama3.1:8b", time: at(23, 30), want: background,
			rules: []policy.StoredRule{global("night", 0, `{"format_version":1,"when":{"properties":{"background":true}},"time_zone":"UTC","windows":[{"start":"22:00","end":"06:00"}],"priority":"background"}`)}},
		{name: "window crossing midnight, after midnight", props: []string{"background"}, model: "llama3.1:8b", time: at(5, 59), want: background,
			rules: []policy.StoredRule{global("night", 0, `{"format_version":1,"when":{"properties":{"background":true}},"time_zone":"UTC","windows":[{"start":"22:00","end":"06:00"}],"priority":"background"}`)}},
		{name: "window crossing midnight, outside", props: []string{"background"}, model: "llama3.1:8b", time: at(12, 0), wantCode: policy.CodeOutsideWindow,
			rules: []policy.StoredRule{global("night", 0, `{"format_version":1,"when":{"properties":{"background":true}},"time_zone":"UTC","windows":[{"start":"22:00","end":"06:00"}],"priority":"background"}`)}},
		{name: "window read in its own time zone", props: []string{"background"}, model: "llama3.1:8b",
			// 2026-10-31 20:00 UTC is 05:00 on 2026-11-01 in Tokyo (UTC+9).
			time: at(20, 0), want: background,
			rules: []policy.StoredRule{global("tokyo", 0, `{"format_version":1,"when":{"properties":{"background":true}},"time_zone":"Asia/Tokyo","windows":[{"start":"00:00","end":"07:00"}],"priority":"background"}`)}},
		{name: "unknown rule version is rejected", rules: []policy.StoredRule{{ID: "v2", FormatVersion: 2, Enabled: true, Body: `{"format_version":2,"priority":"background"}`}},
			model: "llama3.1:8b", time: at(12, 0), wantCode: policy.CodeUnknownFormatVersion},
		{name: "stored and document versions disagree", rules: []policy.StoredRule{{ID: "x", FormatVersion: 2, Enabled: true, Body: `{"format_version":1,"priority":"background"}`}},
			model: "llama3.1:8b", time: at(12, 0), wantCode: policy.CodeInvalidRule},
		{name: "disabled broken rule is skipped", rules: []policy.StoredRule{{ID: "off", FormatVersion: 9, Enabled: false, Body: `nonsense`}},
			model: "llama3.1:8b", time: at(12, 0), want: interactive},
		{name: "enabled broken rule fails closed", rules: []policy.StoredRule{global("bad", 0, `{"format_version":1}`)},
			model: "llama3.1:8b", time: at(12, 0), wantCode: policy.CodeInvalidRule},
		{name: "rule of another grant does not apply", rules: []policy.StoredRule{scoped("deny", "g2", 0, `{"format_version":1,"deny":true}`)},
			model: "llama3.1:8b", time: at(12, 0), want: interactive},
		{name: "rule of this grant applies", rules: []policy.StoredRule{scoped("deny", "g1", 0, `{"format_version":1,"deny":true}`)},
			model: "llama3.1:8b", time: at(12, 0), wantCode: policy.CodeDenied},
		{name: "deny by model", rules: []policy.StoredRule{global("deny", 0, `{"format_version":1,"when":{"models":["nomic-embed-text"]},"deny":true}`)},
			model: "nomic-embed-text", time: at(12, 0), wantCode: policy.CodeDenied},
		{name: "model not granted", model: "other", time: at(12, 0), wantCode: policy.CodeModelNotGranted},
		{name: "empty model not granted", model: "", time: at(12, 0), wantCode: policy.CodeModelNotGranted},
		{name: "property not allowed by the grant", props: []string{"background, purpose=summarize"}, model: "llama3.1:8b", time: at(12, 0),
			mutate: func(g *policy.Grant) { g.AllowedProperties = str(`["background"]`) }, wantCode: policy.CodePropertyNotAllowed},
		{name: "allowed properties", props: []string{"background, purpose=summarize"}, model: "llama3.1:8b", time: at(12, 0),
			mutate: func(g *policy.Grant) { g.AllowedProperties = str(`["background","purpose"]`) }, want: interactive},
		{name: "empty allowed list refuses any property", props: []string{"background"}, model: "llama3.1:8b", time: at(12, 0),
			mutate: func(g *policy.Grant) { g.AllowedProperties = str(`[]`) }, wantCode: policy.CodePropertyNotAllowed},
		{name: "malformed allowed list", model: "llama3.1:8b", time: at(12, 0),
			mutate: func(g *policy.Grant) { g.AllowedProperties = str(`{"a":1}`) }, wantCode: policy.CodeInvalidGrant},
		{name: "invalid header", props: []string{"Background"}, model: "llama3.1:8b", time: at(12, 0), wantCode: policy.CodeInvalidProperties},
		{name: "caps only narrow", model: "llama3.1:8b", time: at(12, 0),
			rules: []policy.StoredRule{
				global("a", 0, `{"format_version":1,"caps":{"context_cap":4096,"max_output_tokens":4096}}`),
				global("b", 1, `{"format_version":1,"caps":{"max_concurrency":1}}`),
			},
			want: policy.Decision{Priority: policy.Interactive, Caps: policy.Caps{ContextCap: i64(4096), MaxOutputTokens: i64(1024), MaxConcurrency: i64(1)}}},
		{name: "rule caps apply where the grant has none", model: "nomic-embed-text", time: at(12, 0),
			mutate:   func(g *policy.Grant) { g.MaxConcurrency = nil },
			rules:    []policy.StoredRule{global("a", 0, `{"format_version":1,"caps":{"context_cap":2048}}`)},
			want:     policy.Decision{Priority: policy.Interactive, Caps: policy.Caps{ContextCap: i64(2048)}},
			wantCode: ""},
		{name: "first matching rule by position sets the priority", model: "llama3.1:8b", time: at(12, 0), props: []string{"purpose=summarize"},
			rules: []policy.StoredRule{
				global("late", 5, `{"format_version":1,"priority":"interactive"}`),
				global("early", 1, `{"format_version":1,"when":{"properties":{"purpose":"summarize"}},"priority":"background"}`),
			}, want: background},
		{name: "string rule value matches a string header value", model: "llama3.1:8b", time: at(12, 0), props: []string{`purpose="summarize"`},
			rules: []policy.StoredRule{global("p", 0, `{"format_version":1,"when":{"properties":{"purpose":"summarize"}},"priority":"background"}`)}, want: background},
		{name: "integer rule value", model: "llama3.1:8b", time: at(12, 0), props: []string{"tier=2"},
			rules: []policy.StoredRule{global("p", 0, `{"format_version":1,"when":{"properties":{"tier":2}},"deny":true}`)}, wantCode: policy.CodeDenied},
		{name: "integer rule value does not match a token", model: "llama3.1:8b", time: at(12, 0), props: []string{"tier=two"},
			rules: []policy.StoredRule{global("p", 0, `{"format_version":1,"when":{"properties":{"tier":2}},"deny":true}`)}, want: interactive},
		{name: "every condition of when must hold", model: "llama3.1:8b", time: at(12, 0), props: []string{"background"},
			rules: []policy.StoredRule{global("p", 0, `{"format_version":1,"when":{"properties":{"background":true},"models":["nomic-embed-text"]},"deny":true}`)}, want: interactive},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := grant()
			if c.mutate != nil {
				c.mutate(&g)
			}
			got, err := policy.Decide(c.rules, policy.Input{Properties: c.props, Model: c.model, Time: c.time, Grant: g}, lim)
			if c.wantCode != "" {
				if policy.CodeOf(err) != c.wantCode {
					t.Fatalf("err = %v, want code %s", err, c.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Priority != c.want.Priority || !sameCaps(got.Caps, c.want.Caps) {
				t.Fatalf("got %+v, want %+v", show(got), show(c.want))
			}
		})
	}
}

func sameCaps(a, b policy.Caps) bool {
	eq := func(x, y *int64) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return eq(a.ContextCap, b.ContextCap) && eq(a.MaxOutputTokens, b.MaxOutputTokens) && eq(a.MaxConcurrency, b.MaxConcurrency)
}

func show(d policy.Decision) map[string]any {
	v := func(p *int64) any {
		if p == nil {
			return nil
		}
		return *p
	}
	return map[string]any{"priority": d.Priority, "ctx": v(d.Caps.ContextCap), "out": v(d.Caps.MaxOutputTokens), "conc": v(d.Caps.MaxConcurrency)}
}

func TestOutsideWindowError(t *testing.T) {
	_, err := policy.Decide([]policy.StoredRule{global("bg", 0, backgroundHours)},
		policy.Input{Properties: []string{"background"}, Model: "llama3.1:8b", Time: at(12, 0), Grant: grant()}, lim)
	if !errors.Is(err, policy.ErrOutsideWindow) {
		t.Fatalf("err = %v", err)
	}
	var pe *policy.Error
	if !errors.As(err, &pe) || pe.RuleID != "bg" {
		t.Fatalf("rule id missing: %#v", err)
	}
	if want := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC); !pe.Opens.Equal(want) {
		t.Fatalf("opens = %v, want %v", pe.Opens, want)
	}
}

func TestUnknownFormatVersionError(t *testing.T) {
	_, err := policy.Decide([]policy.StoredRule{{ID: "r9", Name: "future", FormatVersion: 7, Enabled: true, Body: `{"format_version":7,"anything":{"new":1}}`}},
		policy.Input{Model: "llama3.1:8b", Time: at(12, 0), Grant: grant()}, lim)
	var pe *policy.Error
	if !errors.As(err, &pe) || !errors.Is(err, policy.ErrUnknownFormatVersion) || pe.FormatVersion != 7 || pe.RuleID != "r9" {
		t.Fatalf("err = %#v", err)
	}
}

func TestDecideIsDeterministicAcrossRefusals(t *testing.T) {
	rules := []policy.StoredRule{
		global("deny-b", 2, `{"format_version":1,"deny":true}`),
		global("deny-a", 1, `{"format_version":1,"deny":true}`),
	}
	for range 20 {
		_, err := policy.Decide(rules, policy.Input{Model: "llama3.1:8b", Time: at(12, 0), Grant: grant()}, lim)
		var pe *policy.Error
		if !errors.As(err, &pe) || pe.RuleID != "deny-a" {
			t.Fatalf("err = %v, want the lowest-position rule", err)
		}
	}
}
