package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// KeyListPageSize is the setting holding the page size List uses when the filter gives none (D-38).
const KeyListPageSize = "audit.list_page_size"

// Errors returned by this package.
var (
	ErrInvalidEvent  = errors.New("audit: invalid event")
	ErrInvalidCursor = errors.New("audit: invalid cursor")
)

// Event is one thing that happened. Detail is a structured value that is stored as a JSON
// object; it must never contain prompts, responses or secrets (D-31, D-40).
type Event struct {
	// Action names what happened, for example "grant.changed". Required.
	Action string
	// ActorID is the principal who did it; empty means the system.
	ActorID string
	// TargetID is the principal it was done to; empty means none.
	TargetID string
	// Detail is any value that marshals to a JSON object; nil means {}.
	Detail any
}

// Stored is an aud_event row.
type Stored struct {
	ID        string
	Action    string
	ActorID   string
	TargetID  string
	Detail    json.RawMessage
	CreatedAt time.Time
}

// Clock returns the current time.
type Clock func() time.Time

// forbidden are detail keys (compared lower-case) that name content or secrets. Record refuses
// an event carrying one anywhere in its detail, so a careless caller fails loudly (D-31, D-40).
var forbidden = map[string]bool{
	"prompt": true, "response": true, "completion": true, "messages": true, "content": true,
	"secret": true, "password": true, "token": true, "api_key": true, "access_key": true,
	"private_key": true, "authorization": true,
}

func checkDetail(v any) error {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if forbidden[strings.ToLower(k)] {
				return fmt.Errorf("%w: detail key %q may hold content or a secret", ErrInvalidEvent, k)
			}
			if err := checkDetail(e); err != nil {
				return err
			}
		}
	case []any:
		for _, e := range x {
			if err := checkDetail(e); err != nil {
				return err
			}
		}
	}
	return nil
}

// encodeDetail returns the JSON text of detail after checking it is an object without
// forbidden keys.
func encodeDetail(detail any) (string, error) {
	if detail == nil {
		return "{}", nil
	}
	b, err := json.Marshal(detail)
	if err != nil {
		return "", fmt.Errorf("%w: detail: %v", ErrInvalidEvent, err)
	}
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err != nil || obj == nil {
		return "", fmt.Errorf("%w: detail must be a JSON object", ErrInvalidEvent)
	}
	if err := checkDetail(obj); err != nil {
		return "", err
	}
	return string(b), nil
}

// Record appends ev inside the caller's unit of work: it commits or rolls back with the
// change it records. A nil clock means time.Now. It returns the new event's id.
func Record(tx *storage.Tx, ev Event) (string, error) { return RecordAt(tx, nil, ev) }

// RecordAt is Record with an explicit clock, for callers (and tests) that own one.
func RecordAt(tx *storage.Tx, clock Clock, ev Event) (string, error) {
	if ev.Action == "" {
		return "", fmt.Errorf("%w: empty action", ErrInvalidEvent)
	}
	detail, err := encodeDetail(ev.Detail)
	if err != nil {
		return "", err
	}
	if clock == nil {
		clock = time.Now
	}
	u, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	id := u.String()
	if _, err := tx.Exec(`INSERT INTO aud_event (id, action, actor_id, target_id, detail, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, ev.Action, nullable(ev.ActorID), nullable(ev.TargetID), detail, clock().UnixMilli()); err != nil {
		return "", fmt.Errorf("audit: record %s: %w", ev.Action, err)
	}
	return id, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Defs are the audit settings; register them with settings.Store.Register.
func Defs() []settings.Def {
	return []settings.Def{{Key: KeyListPageSize, Kind: settings.Int, Default: "50", Owner: settings.Host,
		Description: "How many audit events a list returns when the caller does not ask for a number."}}
}

// Filter selects events. Zero fields do not filter. Newest events come first.
type Filter struct {
	Action   string // exact action name
	ActorID  string
	TargetID string
	Since    time.Time // inclusive; zero means no lower bound
	Until    time.Time // exclusive; zero means no upper bound
	// Limit is the page size; 0 uses the audit.list_page_size setting.
	Limit int
	// Cursor is the NextCursor of the previous page; empty starts at the newest event.
	Cursor string
}

// Page is one page of events. NextCursor is empty on the last page.
type Page struct {
	Events     []Stored
	NextCursor string
}

// Log reads the audit trail.
type Log struct{ settings *settings.Store }

// NewLog registers the audit settings with st and returns the reader.
func NewLog(st *settings.Store) (*Log, error) {
	if err := st.Register(Defs()...); err != nil {
		return nil, err
	}
	return &Log{settings: st}, nil
}

// List returns one page of events matching f, newest first.
func (l *Log) List(ctx context.Context, rd *storage.Reader, f Filter) (Page, error) {
	limit := f.Limit
	if limit < 0 {
		return Page{}, fmt.Errorf("%w: negative limit", ErrInvalidEvent)
	}
	if limit == 0 {
		n, err := l.settings.Int(ctx, KeyListPageSize, "")
		if err != nil {
			return Page{}, err
		}
		limit = int(n)
	}
	if limit < 1 {
		limit = 1
	}
	q := `SELECT id, action, actor_id, target_id, detail, created_at FROM aud_event WHERE 1=1`
	var args []any
	if f.Action != "" {
		q += ` AND action = ?`
		args = append(args, f.Action)
	}
	if f.ActorID != "" {
		q += ` AND actor_id = ?`
		args = append(args, f.ActorID)
	}
	if f.TargetID != "" {
		q += ` AND target_id = ?`
		args = append(args, f.TargetID)
	}
	if !f.Since.IsZero() {
		q += ` AND created_at >= ?`
		args = append(args, f.Since.UnixMilli())
	}
	if !f.Until.IsZero() {
		q += ` AND created_at < ?`
		args = append(args, f.Until.UnixMilli())
	}
	if f.Cursor != "" {
		at, id, err := parseCursor(f.Cursor)
		if err != nil {
			return Page{}, err
		}
		q += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		args = append(args, at, at, id)
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := rd.Query(ctx, q, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	var p Page
	for rows.Next() {
		var s Stored
		var actor, target *string
		var detail string
		var at int64
		if err := rows.Scan(&s.ID, &s.Action, &actor, &target, &detail, &at); err != nil {
			return Page{}, err
		}
		if actor != nil {
			s.ActorID = *actor
		}
		if target != nil {
			s.TargetID = *target
		}
		s.Detail = json.RawMessage(detail)
		s.CreatedAt = time.UnixMilli(at).UTC()
		p.Events = append(p.Events, s)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	if len(p.Events) > limit {
		p.Events = p.Events[:limit]
		last := p.Events[limit-1]
		p.NextCursor = strconv.FormatInt(last.CreatedAt.UnixMilli(), 10) + ":" + last.ID
	}
	return p, nil
}

func parseCursor(c string) (int64, string, error) {
	a, id, ok := strings.Cut(c, ":")
	if !ok || id == "" {
		return 0, "", ErrInvalidCursor
	}
	at, err := strconv.ParseInt(a, 10, 64)
	if err != nil {
		return 0, "", ErrInvalidCursor
	}
	return at, id, nil
}
