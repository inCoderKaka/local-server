package usage

import (
	"context"
	"fmt"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// By says whose use_record rows a read counts.
type By int

// The subjects use can be counted by. Each maps to a fixed column of use_record.
const (
	ByConsumer  By = iota + 1 // consumer_id
	ByAccessKey               // access_key_id
)

func (b By) column() (string, error) {
	switch b {
	case ByConsumer:
		return "consumer_id", nil
	case ByAccessKey:
		return "access_key_id", nil
	}
	return "", fmt.Errorf("usage: unknown subject %d", int(b))
}

// Tokens returns the sum of total_tokens of the records of id that finished in [from, to).
func Tokens(ctx context.Context, rd *storage.Reader, by By, id string, from, to time.Time) (int64, error) {
	return read(ctx, rd, "COALESCE(SUM(total_tokens), 0)", by, id, from, to)
}

// Count returns the number of records of id that finished in [from, to).
func Count(ctx context.Context, rd *storage.Reader, by By, id string, from, to time.Time) (int64, error) {
	return read(ctx, rd, "COUNT(*)", by, id, from, to)
}

func read(ctx context.Context, rd *storage.Reader, agg string, by By, id string, from, to time.Time) (int64, error) {
	col, err := by.column()
	if err != nil {
		return 0, err
	}
	var n int64
	err = rd.QueryRow(ctx,
		`SELECT `+agg+` FROM use_record WHERE `+col+` = ? AND finished_at >= ? AND finished_at < ?`,
		id, from.UnixMilli(), to.UnixMilli()).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}
