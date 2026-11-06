package grants

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// Quota metrics (req_001 D-107): what a quota entry counts.
const (
	MetricTokens   = "tokens"
	MetricRequests = "requests"
)

// QuotaEntry is one windowed quota: at most Limit of Metric in one fixed window of length Window.
// A grant version, and an access key's narrowing, hold a list of them with at most one entry per
// (Metric, Window). Limit 0 allows none. Window is a whole number of seconds, at least one.
type QuotaEntry struct {
	Metric string
	Limit  int64
	Window time.Duration
}

// quotaJSON is the stored and wire form of a QuotaEntry: the window is whole seconds.
type quotaJSON struct {
	Metric        string `json:"metric"`
	Limit         int64  `json:"limit"`
	WindowSeconds int64  `json:"window_seconds"`
}

// MarshalJSON writes {"metric", "limit", "window_seconds"}, the form an access key's narrowing
// stores. A window that is not a whole number of seconds is rounded down.
func (q QuotaEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(quotaJSON{Metric: q.Metric, Limit: q.Limit, WindowSeconds: int64(q.Window / time.Second)})
}

// UnmarshalJSON reads the form MarshalJSON writes and refuses unknown fields. It checks only the
// shape; ValidateQuotas checks the values.
func (q *QuotaEntry) UnmarshalJSON(b []byte) error {
	var j quotaJSON
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&j); err != nil {
		return err
	}
	*q = QuotaEntry{Metric: j.Metric, Limit: j.Limit, Window: time.Duration(j.WindowSeconds) * time.Second}
	return nil
}

// QuotaProblem names the first thing wrong with a quota list: the index of the entry, the
// field of it ("metric", "limit" or "window_seconds") and a fixed-text reason.
type QuotaProblem struct {
	Index  int
	Field  string
	Reason string
}

// ValidateQuotas checks a list of quota entries for what storage cannot hold: a metric other than
// tokens or requests, a negative limit, a window that is not a positive whole number of seconds,
// or two entries with the same (metric, window). The bounds of the window (limits.quota_window_min
// and limits.quota_window_max) are host settings and are checked by the caller. It returns nil
// when the list is fine.
func ValidateQuotas(qs []QuotaEntry) *QuotaProblem {
	type key struct {
		metric string
		window time.Duration
	}
	seen := map[key]bool{}
	for i, q := range qs {
		switch {
		case q.Metric != MetricTokens && q.Metric != MetricRequests:
			return &QuotaProblem{i, "metric", "The metric must be tokens or requests."}
		case q.Limit < 0:
			return &QuotaProblem{i, "limit", "The limit must not be negative."}
		case q.Window < time.Second || q.Window%time.Second != 0:
			return &QuotaProblem{i, "window_seconds", "The window must be a whole number of seconds, at least 1."}
		case seen[key{q.Metric, q.Window}]:
			return &QuotaProblem{i, "window_seconds", "Two entries have the same metric and window."}
		}
		seen[key{q.Metric, q.Window}] = true
	}
	return nil
}

// Error makes a problem printable; it never holds request bytes.
func (p *QuotaProblem) Error() string {
	return fmt.Sprintf("quotas[%d].%s: %s", p.Index, p.Field, p.Reason)
}

// WithLegacy returns list plus, for each deprecated single figure that is set and whose metric
// has no entry in list, one entry with the given window (the host's limits.quota_window, whole
// seconds). The list wins over the figure of the same metric (req_001 D-107). The result is a
// new slice.
func WithLegacy(list []QuotaEntry, tokens, requests *int64, window time.Duration) []QuotaEntry {
	out := append([]QuotaEntry(nil), list...)
	for _, l := range []struct {
		metric string
		limit  *int64
	}{{MetricTokens, tokens}, {MetricRequests, requests}} {
		if l.limit == nil || slices.ContainsFunc(list, func(q QuotaEntry) bool { return q.Metric == l.metric }) {
			continue
		}
		out = append(out, QuotaEntry{Metric: l.metric, Limit: *l.limit, Window: window})
	}
	return out
}
