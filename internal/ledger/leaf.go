package ledger

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// LeafVersion is the version "v" of the canonical leaf encoding and of the receipt payload.
const LeafVersion = 1

// The values of Usage.Operation and Usage.Outcome, as in the UsageReceipt schema.
const (
	OpChatCompletions = "chat.completions"
	OpEmbeddings      = "embeddings"

	OutcomeCompleted = "completed"
	OutcomeAborted   = "aborted"
	OutcomeFailed    = "failed"
)

// timeLayout is the text form of every timestamp in a leaf and a receipt.
const timeLayout = "2006-01-02T15:04:05.000Z"

// Usage is what the gateway knows about one finished call. It holds numbers and ids only:
// never a prompt or a response (D-31, D-40).
type Usage struct {
	// UsageID is the record's id (a UUID). Use NewUsageID. For an unmetered receipt it is the
	// id the record would have had.
	UsageID          string
	ConsumerID       string
	AccessKeyID      string
	GrantVersionID   string
	JobID            string // empty when the call did not run as a job
	Operation        string // OpChatCompletions or OpEmbeddings
	Model            string
	Outcome          string // OutcomeCompleted, OutcomeAborted or OutcomeFailed
	PromptTokens     int64
	CompletionTokens int64
	// PromptTokensEstimated is true when PromptTokens is an estimate from the prompt's length
	// and not the engine's count (an aborted call, req_001 D-108). It goes on the signed receipt
	// only: it is not in the canonical leaf, so LeafVersion and every leaf hash stay as they are.
	PromptTokensEstimated bool
	StartedAt             time.Time // when the call was admitted
	FinishedAt            time.Time // when it ended
}

// NewUsageID returns a fresh usage id.
func NewUsageID() string { return uuid.NewString() }

// TotalTokens is the sum of prompt and completion tokens.
func (u Usage) TotalTokens() int64 { return u.PromptTokens + u.CompletionTokens }

// normalized checks the usage and returns it with ids in lower-case hyphenated form and times
// cut to milliseconds, so that the record, the leaf and the receipt agree exactly.
func (u Usage) normalized() (Usage, error) {
	for _, f := range []struct {
		name string
		p    *string
		opt  bool
	}{{"usage_id", &u.UsageID, false}, {"consumer_id", &u.ConsumerID, false}, {"access_key_id", &u.AccessKeyID, false},
		{"grant_version_id", &u.GrantVersionID, false}, {"job_id", &u.JobID, true}} {
		if f.opt && *f.p == "" {
			continue
		}
		id, err := uuid.Parse(*f.p)
		if err != nil {
			return u, fmt.Errorf("ledger: %s is not a UUID: %w", f.name, err)
		}
		*f.p = id.String()
	}
	switch u.Operation {
	case OpChatCompletions, OpEmbeddings:
	default:
		return u, fmt.Errorf("ledger: unknown operation %q", u.Operation)
	}
	switch u.Outcome {
	case OutcomeCompleted, OutcomeAborted, OutcomeFailed:
	default:
		return u, fmt.Errorf("ledger: unknown outcome %q", u.Outcome)
	}
	if u.Model == "" || !utf8.ValidString(u.Model) {
		return u, fmt.Errorf("ledger: model must be non-empty UTF-8")
	}
	if u.PromptTokens < 0 || u.CompletionTokens < 0 {
		return u, fmt.Errorf("ledger: token counts must not be negative")
	}
	if u.PromptTokens > math.MaxInt64-u.CompletionTokens {
		return u, fmt.Errorf("ledger: token counts overflow total_tokens")
	}
	u.StartedAt, u.FinishedAt = u.StartedAt.UTC().Truncate(time.Millisecond), u.FinishedAt.UTC().Truncate(time.Millisecond)
	if u.StartedAt.IsZero() || u.FinishedAt.Before(u.StartedAt) {
		return u, fmt.Errorf("ledger: started_at must be set and not after finished_at")
	}
	return u, nil
}

// CanonicalLeaf returns the leaf bytes of u: the external contract described in the package
// comment. Its tlog record hash is the ldg_leaf hash.
func CanonicalLeaf(u Usage) ([]byte, error) {
	u, err := u.normalized()
	if err != nil {
		return nil, err
	}
	return canonicalLeaf(u), nil
}

// canonicalLeaf encodes an already normalized Usage. Keys are written in ascending order.
func canonicalLeaf(u Usage) []byte {
	var b strings.Builder
	str := func(key, val string) {
		b.WriteString(jcsString(key))
		b.WriteByte(':')
		b.WriteString(jcsString(val))
		b.WriteByte(',')
	}
	num := func(key string, val int64) {
		b.WriteString(jcsString(key))
		b.WriteByte(':')
		b.WriteString(strconv.FormatInt(val, 10))
		b.WriteByte(',')
	}
	b.WriteByte('{')
	str("access_key_id", u.AccessKeyID)
	num("completion_tokens", u.CompletionTokens)
	str("consumer_id", u.ConsumerID)
	str("finished_at", u.FinishedAt.Format(timeLayout))
	str("grant_version_id", u.GrantVersionID)
	if u.JobID == "" {
		b.WriteString(`"job_id":null,`)
	} else {
		str("job_id", u.JobID)
	}
	str("model", u.Model)
	str("operation", u.Operation)
	str("outcome", u.Outcome)
	num("prompt_tokens", u.PromptTokens)
	str("started_at", u.StartedAt.Format(timeLayout))
	num("total_tokens", u.TotalTokens())
	str("usage_id", u.UsageID)
	b.WriteString(`"v":` + strconv.Itoa(LeafVersion) + `}`)
	return []byte(b.String())
}

// jcsString quotes s the way RFC 8785 section 3.2.2.2 does: only the quote, the backslash and
// the control characters are escaped; everything else is written as UTF-8.
func jcsString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
