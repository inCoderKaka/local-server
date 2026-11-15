package requests

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Dataset is the syn_change dataset of resource request rows. A change carries ids, the state and
// times, never the consumer's text (req_001 D-31, D-40).
const Dataset = "resource_requests"

func newID() (string, error) {
	u, err := uuid.NewV7()
	return u.String(), err
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func tm(v int64) time.Time { return time.UnixMilli(v).UTC() }

func tmp(v *int64) *time.Time {
	if v == nil {
		return nil
	}
	t := tm(*v)
	return &t
}

const requestCols = `id, consumer_id, status, message, ask, created_at, expires_at, decided_at, resulting_version_id`

func scanRequest(r storage.Row) (Request, error) {
	var q Request
	var status string
	var ask *string
	var created, expires int64
	var decided *int64
	err := r.Scan(&q.ID, &q.ConsumerID, &status, &q.Message, &ask, &created, &expires, &decided, &q.ResultingVersionID)
	q.Status, q.CreatedAt, q.DecidedAt = Status(status), tm(created), tmp(decided)
	if ask != nil {
		q.Ask = json.RawMessage(*ask)
	}
	if q.Status == StatusOpen {
		t := tm(expires)
		q.ExpiresAt = &t
	}
	return q, err
}

func collect[T any](rows storage.Rows, scan func(storage.Row) (T, error)) ([]T, error) {
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, v)
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

func first[T any](rows storage.Rows, err error, scan func(storage.Row) (T, error)) (T, error) {
	var zero T
	if err != nil {
		return zero, err
	}
	out, err := collect(rows, scan)
	if err != nil {
		return zero, err
	}
	if len(out) == 0 {
		return zero, errNotFound
	}
	return out[0], nil
}

func getRead(ctx context.Context, rd *storage.Reader, id string) (Request, error) {
	rows, err := rd.Query(ctx, `SELECT `+requestCols+` FROM req_resource_request WHERE id = ?`, id)
	return first(rows, err, scanRequest)
}

func getTx(tx *storage.Tx, id string) (Request, error) {
	rows, err := tx.Query(`SELECT `+requestCols+` FROM req_resource_request WHERE id = ?`, id)
	return first(rows, err, scanRequest)
}

// A list cursor is "<created_at ms>.<id>" of the last request of the page: opaque to the caller,
// stable while requests are added.
func encodeCursor(q Request) string { return strconv.FormatInt(ms(q.CreatedAt), 10) + "." + q.ID }

func decodeCursor(c string) (createdMs int64, id string, err error) {
	n, rest, ok := strings.Cut(c, ".")
	if !ok {
		return 0, "", errBadCursor
	}
	createdMs, perr := strconv.ParseInt(n, 10, 64)
	if perr != nil || createdMs < 0 || strconv.FormatInt(createdMs, 10) != n {
		return 0, "", errBadCursor
	}
	if _, perr := uuid.Parse(rest); perr != nil {
		return 0, "", errBadCursor
	}
	return createdMs, rest, nil
}

// listRead returns up to limit requests, newest first, that match the filter and come after the
// cursor.
func listRead(ctx context.Context, rd *storage.Reader, q ListQuery, limit int) ([]Request, error) {
	var hasCursor int
	var curMs int64
	var curID string
	if q.Cursor != "" {
		var err error
		if curMs, curID, err = decodeCursor(q.Cursor); err != nil {
			return nil, err
		}
		hasCursor = 1
	}
	rows, err := rd.Query(ctx, `SELECT `+requestCols+` FROM req_resource_request
WHERE (? = '' OR status = ?) AND (? = '' OR consumer_id = ?) AND (? = 0 OR created_at < ? OR (created_at = ? AND id < ?))
ORDER BY created_at DESC, id DESC LIMIT ?`,
		string(q.Status), string(q.Status), q.ConsumerID, q.ConsumerID, hasCursor, curMs, curMs, curID, limit)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanRequest)
}

func countOpenTx(tx *storage.Tx, consumerID string) (int64, error) {
	rows, err := tx.Query(`SELECT count(*) FROM req_resource_request WHERE consumer_id = ? AND status = 'open'`, consumerID)
	if err != nil {
		return 0, err
	}
	n, err := collect(rows, func(r storage.Row) (int64, error) { var v int64; return v, r.Scan(&v) })
	if err != nil || len(n) == 0 {
		return 0, err
	}
	return n[0], nil
}

func insertTx(tx *storage.Tx, q Request, expires, now time.Time) error {
	var ask *string
	if len(q.Ask) > 0 {
		s := string(q.Ask)
		ask = &s
	}
	_, err := tx.Exec(`INSERT INTO req_resource_request (id, consumer_id, status, message, ask, created_at, expires_at, row_version, updated_at) VALUES (?, ?, 'open', ?, ?, ?, ?, 1, ?)`,
		q.ID, q.ConsumerID, q.Message, ask, ms(now), ms(expires), ms(now))
	return err
}

// finishTx moves an open request to a final state and returns the row's new version. A request
// that is not open any more is ErrNotOpen.
func finishTx(tx *storage.Tx, id string, to Status, now time.Time, by string, versionID *string) (int64, error) {
	var decidedBy *string
	if by != "" {
		decidedBy = &by
	}
	rows, err := tx.Query(`UPDATE req_resource_request SET status = ?, decided_at = ?, decided_by = ?, resulting_version_id = ?, row_version = row_version + 1, updated_at = ?
WHERE id = ? AND status = 'open' RETURNING row_version`, string(to), ms(now), decidedBy, versionID, ms(now), id)
	if err != nil {
		return 0, err
	}
	v, err := collect(rows, func(r storage.Row) (int64, error) { var n int64; return n, r.Scan(&n) })
	if err != nil {
		return 0, err
	}
	if len(v) == 0 {
		return 0, ErrNotOpen
	}
	return v[0], nil
}

func insertCommentTx(tx *storage.Tx, requestID, authorPrincipalID, body string, now time.Time) error {
	id, err := newID()
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO req_comment (id, request_id, author_principal_id, body, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, requestID, authorPrincipalID, body, ms(now))
	return err
}

type commentRow struct {
	id, authorID, body string
	createdAt          time.Time
}

func commentsRead(ctx context.Context, rd *storage.Reader, requestID string) ([]commentRow, error) {
	rows, err := rd.Query(ctx, `SELECT id, author_principal_id, body, created_at FROM req_comment WHERE request_id = ? ORDER BY created_at, id`, requestID)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r storage.Row) (commentRow, error) {
		var c commentRow
		var at int64
		err := r.Scan(&c.id, &c.authorID, &c.body, &at)
		c.createdAt = tm(at)
		return c, err
	})
}

// captureTx records the request row's change in syn_change, in the caller's unit of work.
func captureTx(tx *storage.Tx, q Request, version int64, op string) error {
	p := map[string]any{"id": q.ID, "consumer_id": q.ConsumerID, "status": q.Status, "created_at": q.CreatedAt.UnixMilli()}
	if q.DecidedAt != nil {
		p["decided_at"] = q.DecidedAt.UnixMilli()
	}
	if q.ResultingVersionID != nil {
		p["resulting_version_id"] = *q.ResultingVersionID
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return tx.Capture(storage.Change{Dataset: Dataset, Table: "req_resource_request", PK: q.ID, Version: version, Op: op, Payload: b})
}
