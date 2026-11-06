package grants

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// Audit actions written by this package.
const (
	ActionGrantChanged       = "grant.changed"
	ActionModelUpdated       = "model.updated"
	ActionPolicyRuleCreated  = "policy_rule.created"
	ActionPolicyRuleUpdated  = "policy_rule.updated"
	ActionPolicyRuleDeleted  = "policy_rule.deleted"
	ActionPolicyRuleDisabled = "policy_rule.auto_disabled"
)

// Auth is the part of auth.Service this package uses: forgetting cached keys once a grant has
// changed.
type Auth interface {
	Invalidate()
}

// Engine is the part of the engine port this package uses.
type Engine interface {
	ListModels(ctx context.Context) ([]engine.Model, error)
	Loaded(ctx context.Context) ([]engine.LoadedModel, error)
}

// RuleFailures is the part of the gateway this package uses: how many evaluations a policy rule
// has failed by not parsing, and forgetting them (*gateway.Gateway). It is a port so that the
// admin module does not import the gateway.
type RuleFailures interface {
	RuleFailures(ruleID string) int64
	ClearRuleFailures(ruleID string)
}

// Config is what New needs. Everything except RuleFailures, Clock and Logger is required.
type Config struct {
	DB       *storage.DB
	Settings *settings.Store
	Identity *identity.Repos
	Grants   *grants.Repos
	Auth     Auth
	// Events runs every grant write, so grant.changed is emitted in the unit of the new version.
	Events events.Notifier
	Engine Engine
	// RuleFailures nil switches the automatic disabling of broken rules off.
	RuleFailures RuleFailures
	// Clock nil means time.Now.
	Clock func() time.Time
	// Logger receives problems, with ids only; nil means slog.Default().
	Logger *slog.Logger
}

// Service is the logic behind the admin API's grant, model and policy rule operations. It is safe
// for concurrent use.
type Service struct {
	cfg Config
	now func() time.Time
	log *slog.Logger
}

// New registers the settings of this package and returns the service.
func New(cfg Config) (*Service, error) {
	if cfg.DB == nil || cfg.Settings == nil || cfg.Identity == nil || cfg.Grants == nil || cfg.Auth == nil || cfg.Events == nil || cfg.Engine == nil {
		return nil, errors.New("admin grants: DB, Settings, Identity, Grants, Auth, Events and Engine are required")
	}
	s := &Service{cfg: cfg, now: cfg.Clock, log: cfg.Logger}
	if s.now == nil {
		s.now = time.Now
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if err := registerSettings(cfg.Settings); err != nil {
		return nil, err
	}
	return s, nil
}

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
	// Cursor is the NextCursor of the previous page; empty starts at the newest.
	Cursor string
	// Limit is the page size asked for; nil asks for the setting's size.
	Limit *int
}

// pageSize is the number of rows of one page: the limit asked for, capped by admin.list_page_size,
// which is also the default. A limit below 1 is refused.
func (s *Service) pageSize(ctx context.Context, limit *int) (int, error) {
	n, err := s.cfg.Settings.Int(ctx, consumers.KeyListPageSize, "")
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

// A version cursor is "v<number>" of the last version of the page, in decimal: opaque to the
// caller, stable while versions are added.
func encodeVersionCursor(no int64) string { return "v" + strconv.FormatInt(no, 10) }

func decodeVersionCursor(c string) (int64, error) {
	if c == "" {
		return 0, nil
	}
	if len(c) < 2 || c[0] != 'v' {
		return 0, errBadCursor
	}
	n, err := strconv.ParseInt(c[1:], 10, 64)
	if err != nil || n < 1 || strconv.FormatInt(n, 10) != c[1:] {
		return 0, errBadCursor
	}
	return n, nil
}

var errBadCursor = invalid("cursor", "The cursor is not valid. Use the next_cursor of the previous page.")

func isNotFound(err error) bool { return errors.Is(err, grants.ErrNotFound) }

func wrap(what string, err error) error { return fmt.Errorf("admin grants: %s: %w", what, err) }

func ptrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
