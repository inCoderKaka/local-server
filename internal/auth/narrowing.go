package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/inCoderKaka/local-server/internal/grants"
)

// ErrInvalidNarrowing is returned for narrowing text that is not a valid narrowing.
var ErrInvalidNarrowing = errors.New("auth: invalid narrowing")

// Narrowing is the stored form of the limits an access key adds on top of its consumer's grant.
// A field left out adds no limit. A narrowing can only reduce what the grant allows, never widen
// it (D-25). The text is a JSON object with exactly these fields.
type Narrowing struct {
	ModelIDs            []string `json:"model_ids,omitempty"`
	QuotaTokensPerDay   *int64   `json:"quota_tokens_per_day,omitempty"`
	QuotaRequestsPerDay *int64   `json:"quota_requests_per_day,omitempty"`
	MaxConcurrency      *int64   `json:"max_concurrency,omitempty"`
	ContextCap          *int64   `json:"context_cap,omitempty"`
	MaxOutputTokens     *int64   `json:"max_output_tokens,omitempty"`
}

// ParseNarrowing reads and validates narrowing text. Unknown fields and negative numbers are refused.
func ParseNarrowing(text string) (Narrowing, error) {
	var n Narrowing
	dec := json.NewDecoder(bytes.NewReader([]byte(text)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&n); err != nil {
		return Narrowing{}, fmt.Errorf("%w: %v", ErrInvalidNarrowing, err)
	}
	var rest json.RawMessage
	if err := dec.Decode(&rest); err != io.EOF {
		return Narrowing{}, fmt.Errorf("%w: trailing data", ErrInvalidNarrowing)
	}
	for name, v := range map[string]*int64{"quota_tokens_per_day": n.QuotaTokensPerDay, "quota_requests_per_day": n.QuotaRequestsPerDay,
		"max_concurrency": n.MaxConcurrency, "context_cap": n.ContextCap, "max_output_tokens": n.MaxOutputTokens} {
		if v != nil && *v < 0 {
			return Narrowing{}, fmt.Errorf("%w: %s is negative", ErrInvalidNarrowing, name)
		}
	}
	return n, nil
}

// Text returns the JSON text to store, or nil when the narrowing adds nothing.
func (n Narrowing) Text() (*string, error) {
	b, err := json.Marshal(n)
	if err != nil {
		return nil, err
	}
	if string(b) == "{}" {
		return nil, nil
	}
	s := string(b)
	return &s, nil
}

// ModelLimit is one allowed model with its caps.
type ModelLimit struct {
	ModelID         string
	ContextCap      *int64
	MaxOutputTokens *int64
}

// Limits are the effective limits of a key: its consumer's current grant version narrowed by
// the key. A nil number means no limit.
type Limits struct {
	Models              []ModelLimit
	QuotaTokensPerDay   *int64
	QuotaRequestsPerDay *int64
	MaxConcurrency      *int64
	AllowedProperties   *string
	OnContextOverflow   string
}

// narrow applies n to a grant version. For every number the smaller one wins; a model is kept
// only if the narrowing lists it (when it lists any); the key's context cap and output cap lower
// the cap of each kept model.
func narrow(v grants.Version, n Narrowing) Limits {
	l := Limits{
		QuotaTokensPerDay:   minLimit(v.QuotaTokensPerDay, n.QuotaTokensPerDay),
		QuotaRequestsPerDay: minLimit(v.QuotaRequestsPerDay, n.QuotaRequestsPerDay),
		MaxConcurrency:      minLimit(v.MaxConcurrency, n.MaxConcurrency),
		OnContextOverflow:   v.OnContextOverflow,
	}
	if v.AllowedProperties != nil {
		ap := *v.AllowedProperties
		l.AllowedProperties = &ap
	}
	allow := map[string]bool{}
	for _, id := range n.ModelIDs {
		allow[id] = true
	}
	for _, m := range v.Models {
		if len(allow) > 0 && !allow[m.ModelID] {
			continue
		}
		l.Models = append(l.Models, ModelLimit{ModelID: m.ModelID,
			ContextCap: minLimit(m.ContextCap, n.ContextCap), MaxOutputTokens: minLimit(m.MaxOutputTokens, n.MaxOutputTokens)})
	}
	return l
}

// minLimit returns the smaller of two limits, where nil means no limit. The result is a copy.
func minLimit(a, b *int64) *int64 {
	switch {
	case a == nil && b == nil:
		return nil
	case a == nil:
		v := *b
		return &v
	case b == nil || *a <= *b:
		v := *a
		return &v
	default:
		v := *b
		return &v
	}
}
