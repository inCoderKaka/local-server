package policy_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/policy"
)

// The seed corpora live in testdata/fuzz/<target>/ (D-60); plain go test runs them.

// FuzzParseProperties: LS-Request-Properties is consumer input. The parser must never panic,
// must keep its limits, and what it accepts must survive a serialize-and-parse round trip.
func FuzzParseProperties(f *testing.F) {
	lim := policy.PropertyLimits{MaxBytes: 512, MaxCount: 8}
	f.Fuzz(func(t *testing.T, header string) {
		p, err := policy.ParseProperties([]string{header}, lim)
		if err != nil {
			var pe *policy.Error
			if !errors.As(err, &pe) || pe.Code != policy.CodeInvalidProperties {
				t.Fatalf("untyped error %v", err)
			}
			return
		}
		if len(p) > lim.MaxCount || len(header) > lim.MaxBytes {
			t.Fatalf("limits not kept: %d properties from %d bytes", len(p), len(header))
		}
		for k := range p {
			if !policy.ValidPropertyName(k) {
				t.Fatalf("invalid name %q accepted", k)
			}
		}
		again, err := policy.ParseProperties([]string{serialize(p)}, policy.PropertyLimits{MaxBytes: 1 << 20, MaxCount: lim.MaxCount})
		if err != nil {
			t.Fatalf("round trip of %q: %v", serialize(p), err)
		}
		if len(again) != len(p) {
			t.Fatalf("round trip changed the count")
		}
		for k, v := range p {
			if again[k] != v {
				t.Fatalf("round trip changed %q: %+v -> %+v", k, v, again[k])
			}
		}
	})
}

// serialize writes properties back in RFC 9651 form, in name order.
func serialize(p policy.Properties) string {
	var parts []string
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range p {
			if !yield(k) {
				return
			}
		}
	}) {
		v := p[k]
		switch v.Kind {
		case policy.KindBool:
			if v.Bool {
				parts = append(parts, k)
			} else {
				parts = append(parts, k+"=?0")
			}
		case policy.KindInteger:
			parts = append(parts, fmt.Sprintf("%s=%d", k, v.Int))
		case policy.KindToken:
			parts = append(parts, k+"="+v.Text)
		case policy.KindString:
			s := strings.ReplaceAll(strings.ReplaceAll(v.Text, `\`, `\\`), `"`, `\"`)
			parts = append(parts, k+`="`+s+`"`)
		}
	}
	return strings.Join(parts, ", ")
}

// FuzzRuleDocument: rule documents can arrive through the admin API or an import bundle. The
// decoder must never panic, must return only typed errors, and an accepted rule must be
// evaluable at any time against any request.
func FuzzRuleDocument(f *testing.F) {
	headers := []string{"", "background", "background, purpose=summarize", "tier=2", `purpose="summarize"`}
	times := []time.Time{at(0, 0), at(6, 59), at(7, 0), at(23, 59)}
	f.Fuzz(func(t *testing.T, body []byte) {
		v, err := policy.ValidateRule(body, 4096)
		if err != nil {
			var pe *policy.Error
			if !errors.As(err, &pe) || (pe.Code != policy.CodeInvalidRule && pe.Code != policy.CodeUnknownFormatVersion) {
				t.Fatalf("untyped error %v", err)
			}
			return
		}
		if v != policy.FormatV1 {
			t.Fatalf("accepted format version %d", v)
		}
		rules := []policy.StoredRule{{ID: "f", FormatVersion: v, Body: string(body), Enabled: true}}
		for _, h := range headers {
			for _, tm := range times {
				d, err := policy.Decide(rules, policy.Input{Properties: []string{h}, Model: "llama3.1:8b", Time: tm, Grant: grant()}, lim)
				if err != nil {
					switch policy.CodeOf(err) {
					case policy.CodeDenied:
					case policy.CodeOutsideWindow:
						var pe *policy.Error
						if errors.As(err, &pe) && !pe.Opens.After(tm) {
							t.Fatalf("window opens %v, not after %v", pe.Opens, tm)
						}
					default:
						t.Fatalf("accepted rule failed evaluation: %v", err)
					}
					continue
				}
				if d.Priority != policy.Interactive && d.Priority != policy.Background {
					t.Fatalf("priority %q", d.Priority)
				}
				for _, c := range []*int64{d.Caps.ContextCap, d.Caps.MaxOutputTokens, d.Caps.MaxConcurrency} {
					if c != nil && *c < 1 {
						t.Fatalf("cap %d", *c)
					}
				}
			}
		}
	})
}
