package usage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// Granularity is the size of a rollup bucket.
type Granularity string

// The bucket sizes. Buckets are aligned to the Unix epoch, so they are UTC minutes, hours and
// days.
const (
	GranularityMinute Granularity = "minute"
	GranularityHour   Granularity = "hour"
	GranularityDay    Granularity = "day"
)

var granularities = []Granularity{GranularityMinute, GranularityHour, GranularityDay}

// Size is the length of one bucket; zero for an unknown granularity.
func (g Granularity) Size() time.Duration {
	switch g {
	case GranularityMinute:
		return time.Minute
	case GranularityHour:
		return time.Hour
	case GranularityDay:
		return 24 * time.Hour
	}
	return 0
}

// Errors of QueryRollups. A caller maps them to a bad request.
var (
	ErrInvalidGranularity = errors.New("usage: granularity must be minute, hour or day")
	ErrInvalidCursor      = errors.New("usage: the cursor is not valid")
)

// RollupQuery says which rollups QueryRollups returns. It is the one usage query: the consumer
// endpoint (GET /v1/usage) sets ConsumerID to the caller's consumer and the host's admin
// endpoint (PLT-F-024, GET /admin/v1/usage) leaves it empty or sets the consumer it was asked
// about (req_001 D-104).
type RollupQuery struct {
	Granularity Granularity
	// ConsumerID narrows to one consumer; empty means every consumer.
	ConsumerID string
	// AccessKeyID narrows to one access key; empty means every key.
	AccessKeyID string
	// Model narrows to one model; empty means every model.
	Model string
	// From is the first bucket start included; the zero time means no lower bound. To is the
	// first bucket start excluded; the zero time means no upper bound. A To before From
	// matches nothing and returns an empty page.
	From, To time.Time
	// Cursor is the NextCursor of the previous page; empty starts at the first row.
	Cursor string
	// Limit is the most rows to return. Zero or less means usage.page_size, and a larger value
	// is capped to it.
	Limit int
}

// Rollup is the usage of one bucket, consumer, access key and model.
type Rollup struct {
	BucketStart      time.Time // UTC
	Granularity      Granularity
	ConsumerID       string
	AccessKeyID      string
	Model            string
	Requests         int64
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
}

// RollupPage is one page of rollups, oldest bucket first.
type RollupPage struct {
	Rollups []Rollup
	// NextCursor is empty on the last page; otherwise pass it as RollupQuery.Cursor to get the
	// next one.
	NextCursor string
}

// cursor is the key of the last row of a page. The cursor is opaque to callers.
type cursor struct {
	Bucket int64  `json:"b"`
	Cons   string `json:"c"`
	Key    string `json:"k"`
	Model  string `json:"m"`
}

func encodeCursor(c cursor) string {
	b, _ := json.Marshal(c) // fixed field types cannot fail
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (cursor, error) {
	var c cursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return c, ErrInvalidCursor
	}
	// A cursor this package made always names a consumer and an access key.
	if err := json.Unmarshal(b, &c); err != nil || c.Cons == "" || c.Key == "" {
		return cursor{}, ErrInvalidCursor
	}
	return c, nil
}

// QueryRollups returns a page of use_rollup rows, oldest bucket first, then by consumer,
// access key and model. It reads use_rollup only and never scans use_record (req_001 D-104).
// The page size is the usage.page_size setting, read now (req_001 D-38).
func QueryRollups(ctx context.Context, rd *storage.Reader, st *settings.Store, q RollupQuery) (RollupPage, error) {
	if q.Granularity.Size() == 0 {
		return RollupPage{}, ErrInvalidGranularity
	}
	var cur *cursor
	if q.Cursor != "" {
		c, err := decodeCursor(q.Cursor)
		if err != nil {
			return RollupPage{}, err
		}
		cur = &c
	}
	size, err := st.Int(ctx, KeyPageSize, "")
	if err != nil {
		return RollupPage{}, err
	}
	limit := int64(q.Limit)
	if limit < 1 || limit > size {
		limit = size
	}
	if limit < 1 { // the setting's validator forbids it; stay safe if the stored value was edited
		limit = 1
	}

	where := []string{"granularity = ?"}
	args := []any{string(q.Granularity)}
	if q.ConsumerID != "" {
		where, args = append(where, "consumer_id = ?"), append(args, q.ConsumerID)
	}
	if q.AccessKeyID != "" {
		where, args = append(where, "access_key_id = ?"), append(args, q.AccessKeyID)
	}
	if q.Model != "" {
		where, args = append(where, "model = ?"), append(args, q.Model)
	}
	if !q.From.IsZero() {
		where, args = append(where, "bucket_start >= ?"), append(args, q.From.UnixMilli())
	}
	if !q.To.IsZero() {
		where, args = append(where, "bucket_start < ?"), append(args, q.To.UnixMilli())
	}
	if cur != nil {
		where = append(where, "(bucket_start, consumer_id, access_key_id, model) > (?, ?, ?, ?)")
		args = append(args, cur.Bucket, cur.Cons, cur.Key, cur.Model)
	}
	// One extra row says whether another page follows.
	args = append(args, limit+1)
	rows, err := rd.Query(ctx, `SELECT bucket_start, consumer_id, access_key_id, model, requests, prompt_tokens, completion_tokens, total_tokens
FROM use_rollup WHERE `+strings.Join(where, " AND ")+`
ORDER BY bucket_start, consumer_id, access_key_id, model LIMIT ?`, args...)
	if err != nil {
		return RollupPage{}, err
	}
	defer rows.Close()
	var page RollupPage
	var last cursor
	for rows.Next() {
		var bucket int64
		r := Rollup{Granularity: q.Granularity}
		if err := rows.Scan(&bucket, &r.ConsumerID, &r.AccessKeyID, &r.Model, &r.Requests, &r.PromptTokens, &r.CompletionTokens, &r.TotalTokens); err != nil {
			return RollupPage{}, err
		}
		if int64(len(page.Rollups)) == limit {
			page.NextCursor = encodeCursor(last)
			break
		}
		r.BucketStart = time.UnixMilli(bucket).UTC()
		page.Rollups = append(page.Rollups, r)
		last = cursor{Bucket: bucket, Cons: r.ConsumerID, Key: r.AccessKeyID, Model: r.Model}
	}
	if err := rows.Err(); err != nil {
		return RollupPage{}, fmt.Errorf("usage: read rollups: %w", err)
	}
	return page, nil
}
