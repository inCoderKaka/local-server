package usage

import (
	"context"
	"fmt"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// The request counter (use_request_counter, migration 4) counts every call the gateway admitted,
// metered or not, per subject, quota window length and fixed window (req_001 D-107). It holds
// numbers and ids only (D-31). The limits module owns the meaning of a window; this package only
// stores the counts.

func (b By) kind() (string, error) {
	switch b {
	case ByConsumer:
		return "consumer", nil
	case ByAccessKey:
		return "access_key", nil
	}
	return "", fmt.Errorf("usage: unknown subject %d", int(b))
}

// AddRequest raises by one the counter of subject id for the fixed window of the given length
// that starts at start, in the caller's unit of work. The row is created on the first call.
func AddRequest(tx *storage.Tx, by By, id string, window time.Duration, start time.Time) error {
	kind, err := by.kind()
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO use_request_counter (subject_kind, subject_id, window_ms, window_start_ms, request_count) VALUES (?, ?, ?, ?, 1)
ON CONFLICT (subject_kind, subject_id, window_ms, window_start_ms) DO UPDATE SET request_count = request_count + 1`,
		kind, id, window.Milliseconds(), start.UnixMilli())
	return err
}

// RequestCount returns the counter of subject id for the window of the given length that starts
// at start; 0 when no call was counted in it.
func RequestCount(ctx context.Context, rd *storage.Reader, by By, id string, window time.Duration, start time.Time) (int64, error) {
	kind, err := by.kind()
	if err != nil {
		return 0, err
	}
	rows, err := rd.Query(ctx, `SELECT request_count FROM use_request_counter WHERE subject_kind = ? AND subject_id = ? AND window_ms = ? AND window_start_ms = ?`,
		kind, id, window.Milliseconds(), start.UnixMilli())
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var n int64
	if rows.Next() {
		if err := rows.Scan(&n); err != nil {
			return 0, err
		}
	}
	return n, rows.Err()
}

// PruneRequestCounters deletes the counters whose window ended before cutoff and returns how many.
// A window that has not ended is never deleted.
func PruneRequestCounters(tx *storage.Tx, cutoff time.Time) (int64, error) {
	c := cutoff.UnixMilli()
	// window_start_ms < c lets the index on window_start_ms bound the scan; the second condition
	// is the exact one: the window's end, not its start.
	res, err := tx.Exec(`DELETE FROM use_request_counter WHERE window_start_ms < ? AND window_start_ms + window_ms < ?`, c, c)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
