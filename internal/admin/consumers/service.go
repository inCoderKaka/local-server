package consumers

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// principalKindConsumer is the idn_principal.kind of a consumer.
const principalKindConsumer = "consumer"

// Audit actions written by this package. Access key issue and revoke are the auth module's
// (auth.ActionKeyIssued, auth.ActionKeyRevoked) and are written by the calls to it.
const (
	ActionConsumerCreated     = "consumer.created"
	ActionConsumerUpdated     = "consumer.updated"
	ActionConsumerSuspended   = "consumer.suspended"
	ActionConsumerReactivated = "consumer.reactivated"
	ActionConsumerDeleted     = "consumer.deleted"
	ActionKeyUpdated          = "access_key.updated"
	ActionInviteCreated       = "invite.created"
	ActionInviteRevoked       = "invite.revoked"
)

// Auth is the part of auth.Service this package uses.
type Auth interface {
	Issue(tx *storage.Tx, p auth.IssueParams) (auth.Issued, error)
	Revoke(tx *storage.Tx, keyID, actorID string) error
	Invalidate()
}

var _ Auth = (*auth.Service)(nil)

// Config is what New needs. Everything except Clock and Logger is required.
type Config struct {
	DB       *storage.DB
	Settings *settings.Store
	Identity *identity.Repos
	Grants   *grants.Repos
	Auth     Auth
	// Events runs every write, so that auth.Service.Revoke can emit its event in the same unit.
	Events events.Notifier
	// Clock nil means time.Now. Use the clock of the auth service, so a key's expiry means the
	// same thing here and when the key is checked.
	Clock func() time.Time
	// Logger receives problems, with ids only; nil means slog.Default().
	Logger *slog.Logger
}

// Service is the logic behind the admin API's consumer, access key and invite operations.
// It is safe for concurrent use.
type Service struct {
	cfg Config
	now func() time.Time
	log *slog.Logger
}

// New registers the settings of this package and returns the service.
func New(cfg Config) (*Service, error) {
	if cfg.DB == nil || cfg.Settings == nil || cfg.Identity == nil || cfg.Grants == nil || cfg.Auth == nil || cfg.Events == nil {
		return nil, errors.New("consumers: DB, Settings, Identity, Grants, Auth and Events are required")
	}
	s := &Service{cfg: cfg, now: cfg.Clock, log: cfg.Logger}
	if s.now == nil {
		s.now = time.Now
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	// Quota entries are checked against limits.quota_window_min, limits.quota_window_max and
	// limits.quota_window, which the limits module owns. Registering the same definitions is
	// allowed, so this works in whichever order the modules are built.
	if err := cfg.Settings.Register(limits.Defs()...); err != nil {
		return nil, err
	}
	if err := registerSettings(cfg.Settings); err != nil {
		return nil, err
	}
	return s, nil
}

// Field is an optional value of a change request: not given (the zero value), given as null, or
// given with a value.
type Field[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// Value is a Field given with a value.
func Value[T any](v T) Field[T] { return Field[T]{Set: true, Value: v} }

// Null is a Field given as null.
func Null[T any]() Field[T] { return Field[T]{Set: true, Null: true} }

// write runs fn as one unit of work through the events Notifier.
func (s *Service) write(ctx context.Context, fn func(tx *storage.Tx) error) error {
	return s.cfg.Events.Write(ctx, s.cfg.DB, fn)
}

func (s *Service) record(tx *storage.Tx, ev audit.Event) error {
	_, err := audit.RecordAt(tx, audit.Clock(s.now), ev)
	return err
}

// ListQuery asks for one page of a list.
type ListQuery struct {
	// Status keeps the consumers in this state (consumers only); empty keeps those not deleted.
	Status string
	// Cursor is the NextCursor of the previous page; empty starts at the newest.
	Cursor string
	// Limit is the page size asked for; nil asks for the setting's size.
	Limit *int
}

// pageSize is the number of rows of one page: the limit asked for, capped by the setting, which
// is also the default. A limit below 1 is refused.
func (s *Service) pageSize(ctx context.Context, limit *int) (int, error) {
	n, err := s.cfg.Settings.Int(ctx, KeyListPageSize, "")
	if err != nil {
		return 0, err
	}
	n = max(n, 1)
	n = min(n, int64(math.MaxInt-1)) // the query asks for one row more than a page
	if limit == nil {
		return int(n), nil
	}
	if *limit < 1 {
		return 0, invalid("limit", "The limit must be at least 1.")
	}
	return int(min(int64(*limit), n)), nil
}

// A cursor is the base64 (URL alphabet, no padding) of "<created_at in Unix ms>:<id>" of the last
// row of the page: opaque to the caller, stable while rows are added.
func encodeCursor(k identity.PageKey) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(k.CreatedAt.UnixMilli(), 10) + ":" + k.ID))
}

func decodeCursor(c string) (*identity.PageKey, error) {
	if c == "" {
		return nil, nil
	}
	bad := invalid("cursor", "The cursor is not valid. Use the next_cursor of the previous page.")
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return nil, bad
	}
	ms, id, ok := strings.Cut(string(raw), ":")
	if !ok {
		return nil, bad
	}
	at, err := strconv.ParseInt(ms, 10, 64)
	if err != nil {
		return nil, bad
	}
	if u, err := uuid.Parse(id); err != nil || u.String() != id {
		return nil, bad
	}
	return &identity.PageKey{CreatedAt: time.UnixMilli(at).UTC(), ID: id}, nil
}

// isNotFound reports whether err says a row does not exist.
func isNotFound(err error) bool { return errors.Is(err, identity.ErrNotFound) }

func wrap(what string, err error) error { return fmt.Errorf("consumers: %s: %w", what, err) }
