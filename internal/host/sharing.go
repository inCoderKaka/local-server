package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// State is whether the host is serving (D-06).
type State string

// The sharing states. Paused is stored (the manual switch); Quiet is derived from the
// schedule and the clock and is never stored. Paused wins over Quiet.
const (
	Sharing State = "sharing"
	Paused  State = "paused"
	Quiet   State = "quiet"
)

// Audit actions written by Pause and Resume.
const (
	ActionPaused  = "host.paused"
	ActionResumed = "host.resumed"
)

// ErrNoHost is returned when the install has no hst_host row yet (the host identity is created
// by internal/hostkeys at first run).
var ErrNoHost = errors.New("host: no host row")

// Clock returns the current time. Tests pass a fixed one.
type Clock func() time.Time

// SharingState controls and reports the host's sharing state.
type SharingState struct {
	db    *storage.DB
	st    *settings.Store
	bus   events.Notifier
	clock Clock
}

// NewSharing registers the quiet-hours settings with st and returns the controller. A nil
// clock means time.Now.
func NewSharing(db *storage.DB, st *settings.Store, bus events.Notifier, clock Clock) (*SharingState, error) {
	if err := st.Register(Defs()...); err != nil {
		return nil, err
	}
	if clock == nil {
		clock = time.Now
	}
	return &SharingState{db: db, st: st, bus: bus, clock: clock}, nil
}

// Pause stops the host serving by hand. By is the acting principal id ("" for the system). It
// changed reports whether the state changed: pausing a paused host writes nothing. A change
// writes the audit row, the state and one host.paused event in one unit of work.
func (s *SharingState) Pause(ctx context.Context, by string) (changed bool, err error) {
	return s.switchTo(ctx, by, Paused)
}

// Resume undoes Pause. It writes an audit row but emits no event (the event set has no
// resumed type). Resuming a host that is not paused writes nothing.
func (s *SharingState) Resume(ctx context.Context, by string) (changed bool, err error) {
	return s.switchTo(ctx, by, Sharing)
}

func (s *SharingState) switchTo(ctx context.Context, by string, to State) (bool, error) {
	from := Sharing
	if to == Sharing {
		from = Paused
	}
	changed := false
	err := s.bus.Write(ctx, s.db, func(tx *storage.Tx) error {
		hostID, err := oneString(tx.Query(`SELECT id FROM hst_host`))
		if err != nil {
			return err
		}
		at := s.clock()
		res, err := tx.Exec(`UPDATE hst_host SET sharing_state = ?, sharing_state_changed_at = ? WHERE id = ? AND sharing_state = ?`,
			string(to), at.UnixMilli(), hostID, string(from))
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return nil // already in the requested state
		}
		changed = true
		detail := map[string]string{"host_id": hostID, "from": string(from), "to": string(to)}
		action := ActionResumed
		if to == Paused {
			action = ActionPaused
		}
		if _, err := audit.RecordAt(tx, audit.Clock(s.clock), audit.Event{Action: action, ActorID: by, Detail: detail}); err != nil {
			return err
		}
		if to != Paused {
			return nil
		}
		payload, err := json.Marshal(map[string]string{"host_id": hostID, "sharing_state": string(to)})
		if err != nil {
			return err
		}
		_, err = s.bus.Emit(tx, events.Event{Type: events.HostPaused, ActorPrincipalID: by, Payload: payload})
		return err
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}

// StoredPaused reports the manual switch alone.
func (s *SharingState) StoredPaused(ctx context.Context) (bool, error) {
	v, err := oneString(s.db.Reader().Query(ctx, `SELECT sharing_state FROM hst_host`))
	if err != nil {
		return false, err
	}
	return v == string(Paused), nil
}

// Effective returns the state at the clock's current time.
func (s *SharingState) Effective(ctx context.Context) (State, error) {
	return s.EffectiveAt(ctx, s.clock())
}

// EffectiveAt returns the state at t: Paused if the manual switch is on, else Quiet if t is
// inside the quiet-hours window, else Sharing. If the quiet-hours settings are unusable it
// returns ErrInvalidSchedule together with the manual state, so a caller can still honour a
// pause.
func (s *SharingState) EffectiveAt(ctx context.Context, t time.Time) (State, error) {
	paused, err := s.StoredPaused(ctx)
	if err != nil {
		return "", err
	}
	if paused {
		return Paused, nil
	}
	sch, err := LoadSchedule(ctx, s.st)
	if err != nil {
		return Sharing, err
	}
	if sch.Quiet(t) {
		return Quiet, nil
	}
	return Sharing, nil
}

// oneString reads the single text column of the first row, or returns ErrNoHost when there is
// no row. (This package may not import database/sql, so it cannot test for sql.ErrNoRows.)
func oneString(rows storage.Rows, err error) (string, error) {
	if err != nil {
		return "", fmt.Errorf("host: read hst_host: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", fmt.Errorf("host: read hst_host: %w", err)
		}
		return "", ErrNoHost
	}
	var v string
	if err := rows.Scan(&v); err != nil {
		return "", fmt.Errorf("host: read hst_host: %w", err)
	}
	return v, nil
}
