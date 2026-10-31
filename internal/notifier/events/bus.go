package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Handler receives a committed event. A returned error or panic is logged and does not
// affect the committed action or the other subscribers.
type Handler func(ctx context.Context, ev Event) error

// Notifier is the port other modules emit domain events through (D-29, D-39).
type Notifier interface {
	// Emit writes ev to ntf_event inside the unit of work tx and returns it with ID and
	// OccurredAt filled. Subscribers are called only if tx's unit of work commits, and only
	// when the unit was run through Write.
	Emit(tx *storage.Tx, ev Event) (Event, error)
	// Write runs fn as one unit of work (storage.DB.Write) and, once it has committed,
	// calls the subscribers of the events fn emitted. If fn or the commit fails, no event
	// row exists and no subscriber is called.
	Write(ctx context.Context, db *storage.DB, fn func(*storage.Tx) error) error
	// Subscribe registers h under name for the given types; no types means every type.
	Subscribe(name string, h Handler, types ...Type) error
}

// Bus is the Notifier backed by ntf_event.
type Bus struct {
	log *slog.Logger
	now func() time.Time

	mu      sync.Mutex
	subs    []subscription
	pending map[*storage.Tx][]Event
}

type subscription struct {
	name  string
	h     Handler
	types map[Type]bool // nil means every type
}

// Option configures a Bus.
type Option func(*Bus)

// WithLogger sets the logger for subscriber failures (default slog.Default()).
func WithLogger(l *slog.Logger) Option { return func(b *Bus) { b.log = l } }

// WithClock sets the clock used for OccurredAt (default time.Now).
func WithClock(now func() time.Time) Option { return func(b *Bus) { b.now = now } }

// New returns a Bus.
func New(opts ...Option) *Bus {
	b := &Bus{log: slog.Default(), now: time.Now, pending: map[*storage.Tx][]Event{}}
	for _, o := range opts {
		o(b)
	}
	return b
}

var _ Notifier = (*Bus)(nil)

// Subscribe implements Notifier.
func (b *Bus) Subscribe(name string, h Handler, types ...Type) error {
	if name == "" || h == nil {
		return errors.New("events: subscribe needs a name and a handler")
	}
	var set map[Type]bool
	if len(types) > 0 {
		set = map[Type]bool{}
		for _, t := range types {
			if !t.Known() {
				return fmt.Errorf("events: subscribe %s: unknown event type %q", name, t)
			}
			set[t] = true
		}
	}
	b.mu.Lock()
	b.subs = append(b.subs, subscription{name: name, h: h, types: set})
	b.mu.Unlock()
	return nil
}

// Emit implements Notifier.
func (b *Bus) Emit(tx *storage.Tx, ev Event) (Event, error) {
	if !ev.Type.Known() {
		return Event{}, fmt.Errorf("events: unknown event type %q", ev.Type)
	}
	payload, err := normalizePayload(ev.Payload)
	if err != nil {
		return Event{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Event{}, fmt.Errorf("events: new id: %w", err)
	}
	ev.ID, ev.Payload = id.String(), payload
	ev.OccurredAt = time.UnixMilli(b.now().UnixMilli()).UTC()
	_, err = tx.Exec(`INSERT INTO ntf_event (id, type, occurred_at, actor_principal_id, target_principal_id, payload) VALUES (?, ?, ?, ?, ?, ?)`,
		ev.ID, string(ev.Type), ev.OccurredAt.UnixMilli(), nullable(ev.ActorPrincipalID), nullable(ev.TargetPrincipalID), string(payload))
	if err != nil {
		return Event{}, fmt.Errorf("events: insert event: %w", err)
	}
	b.mu.Lock()
	if p, ok := b.pending[tx]; ok {
		b.pending[tx] = append(p, ev)
	}
	b.mu.Unlock()
	return ev, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Write implements Notifier.
func (b *Bus) Write(ctx context.Context, db *storage.DB, fn func(*storage.Tx) error) error {
	var unit *storage.Tx
	err := db.Write(ctx, func(tx *storage.Tx) error {
		unit = tx
		b.mu.Lock()
		b.pending[tx] = nil
		b.mu.Unlock()
		return fn(tx)
	})
	b.mu.Lock()
	evs := b.pending[unit]
	delete(b.pending, unit)
	b.mu.Unlock()
	if err != nil {
		return err
	}
	// The action is committed; subscribers must not be cut short by the caller's cancellation.
	b.dispatch(context.WithoutCancel(ctx), evs)
	return nil
}

func (b *Bus) dispatch(ctx context.Context, evs []Event) {
	if len(evs) == 0 {
		return
	}
	b.mu.Lock()
	subs := append([]subscription(nil), b.subs...)
	b.mu.Unlock()
	for _, ev := range evs {
		for _, s := range subs {
			if s.types != nil && !s.types[ev.Type] {
				continue
			}
			b.call(ctx, s, ev)
		}
	}
}

// call runs one subscriber. It logs the failure with ids only, never the payload.
func (b *Bus) call(ctx context.Context, s subscription, ev Event) {
	defer func() {
		if p := recover(); p != nil {
			b.log.Error("event subscriber panicked", "subscriber", s.name, "event_id", ev.ID, "event_type", string(ev.Type))
		}
	}()
	if err := s.h(ctx, ev); err != nil {
		b.log.Error("event subscriber failed", "subscriber", s.name, "event_id", ev.ID, "event_type", string(ev.Type), "err", err)
	}
}
