package requests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// Audit actions written by this package. The host's decisions are audited; a consumer's own
// calls are not (the audit log is the record of admin actions, req_001 D-07).
const (
	ActionApproved = "resource_request.approved"
	ActionRejected = "resource_request.rejected"
)

// Config is what New needs. Everything except Clock and Logger is required.
type Config struct {
	DB       *storage.DB
	Settings *settings.Store
	Identity *identity.Repos
	// Events runs every write, so that a request's change and its domain event are one unit of
	// work (req_001 D-29, D-39 F3).
	Events events.Notifier
	// Clock nil means time.Now.
	Clock func() time.Time
	// Logger receives problems, with ids only, never the consumer's text; nil means
	// slog.Default().
	Logger *slog.Logger
}

// Service is the logic behind the resource request routes of the consumer API and the admin API.
// It is safe for concurrent use.
type Service struct {
	cfg Config
	now func() time.Time
	log *slog.Logger
}

// New registers the settings of this package and returns the service.
func New(cfg Config) (*Service, error) {
	if cfg.DB == nil || cfg.Settings == nil || cfg.Identity == nil || cfg.Events == nil {
		return nil, errors.New("requests: DB, Settings, Identity and Events are required")
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

func (s *Service) write(ctx context.Context, fn func(tx *storage.Tx) error) error {
	return s.cfg.Events.Write(ctx, s.cfg.DB, fn)
}

// checkText refuses a text that is empty or longer than requests.max_comment_chars.
func (s *Service) checkText(ctx context.Context, param, text string) error {
	if strings.TrimSpace(text) == "" {
		return invalid(param, "The text must not be empty.")
	}
	max, err := s.cfg.Settings.Int(ctx, KeyMaxCommentChars, "")
	if err != nil {
		return err
	}
	if int64(utf8.RuneCountInString(text)) > max {
		return invalid(param, "The text is longer than this host allows.")
	}
	return nil
}

// Create opens a resource request for the consumer. The consumer's open requests are counted in
// the unit of work that inserts the new one, so two calls at once cannot pass the limit
// (requests.max_open_per_consumer). The request and its resource_request.created event are one
// unit of work.
func (s *Service) Create(ctx context.Context, consumerID, principalID string, in NewRequest) (Request, error) {
	if err := s.checkText(ctx, "message", in.Message); err != nil {
		return Request{}, err
	}
	if len(in.Ask) > 0 && !isObject(in.Ask) {
		return Request{}, invalid("ask", "The ask must be an object.")
	}
	maxOpen, err := s.cfg.Settings.Int(ctx, KeyMaxOpen, "")
	if err != nil {
		return Request{}, err
	}
	ttl, err := s.cfg.Settings.Duration(ctx, KeyTTL, "")
	if err != nil {
		return Request{}, err
	}
	id, err := newID()
	if err != nil {
		return Request{}, err
	}
	now := s.now()
	q := Request{ID: id, ConsumerID: consumerID, Status: StatusOpen, Message: in.Message, Ask: in.Ask, CreatedAt: now.UTC()}
	err = s.write(ctx, func(tx *storage.Tx) error {
		a, err := s.cfg.Identity.Consumers.GetAccountTx(tx, consumerID)
		if errors.Is(err, identity.ErrNotFound) {
			return errNoConsumer
		}
		if err != nil {
			return err
		}
		if a.Status == identity.StatusDeleted {
			return errDeleted
		}
		n, err := countOpenTx(tx, consumerID)
		if err != nil {
			return err
		}
		if n >= maxOpen {
			return errTooMany
		}
		if err := insertTx(tx, q, now.Add(ttl), now); err != nil {
			return err
		}
		if err := captureTx(tx, q, 1, "insert"); err != nil {
			return err
		}
		return s.emit(tx, events.ResourceRequestCreated, principalID, "", map[string]any{"request_id": id, "consumer_id": consumerID})
	})
	if err != nil {
		return Request{}, wrap("create", err)
	}
	return s.fetch(ctx, id, "")
}

func isObject(raw json.RawMessage) bool {
	var obj map[string]json.RawMessage
	return json.Unmarshal(raw, &obj) == nil && obj != nil
}

func (s *Service) emit(tx *storage.Tx, t events.Type, actor, target string, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.cfg.Events.Emit(tx, events.Event{Type: t, ActorPrincipalID: actor, TargetPrincipalID: target, Payload: b})
	return err
}

// List returns one page of requests, newest first, without comments. A ConsumerID in the query
// keeps one consumer's requests.
func (s *Service) List(ctx context.Context, q ListQuery) (Page, error) {
	if q.Status != "" && !q.Status.Valid() {
		return Page{}, invalid("status", "The status must be open, approved, rejected, withdrawn or expired.")
	}
	n, err := s.pageSize(ctx, q.Limit)
	if err != nil {
		return Page{}, err
	}
	items, err := listRead(ctx, s.cfg.DB.Reader(), q, n+1)
	if err != nil {
		return Page{}, err
	}
	var page Page
	if len(items) > n {
		items = items[:n]
		page.NextCursor = encodeCursor(items[n-1])
	}
	page.Items = items
	return page, nil
}

// pageSize is the number of rows of one page: the limit asked for, capped by requests.page_size,
// which is also the default. A limit below 1 is refused.
func (s *Service) pageSize(ctx context.Context, limit *int) (int, error) {
	n, err := s.cfg.Settings.Int(ctx, KeyPageSize, "")
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

// Get returns the request with its comments. A consumerID that is not empty must be the asking
// consumer's: another consumer's request is not found, the same answer as a request that does
// not exist.
func (s *Service) Get(ctx context.Context, consumerID, id string) (Request, error) {
	return s.fetch(ctx, id, consumerID)
}

// fetch reads a request and its comments.
func (s *Service) fetch(ctx context.Context, id, consumerID string) (Request, error) {
	rd := s.cfg.DB.Reader()
	q, err := getRead(ctx, rd, id)
	if err != nil {
		return Request{}, err
	}
	if consumerID != "" && q.ConsumerID != consumerID {
		return Request{}, errNotFound
	}
	rows, err := commentsRead(ctx, rd, id)
	if err != nil {
		return Request{}, err
	}
	q.Comments = make([]Comment, 0, len(rows))
	authors := map[string]Author{}
	for _, c := range rows {
		a, ok := authors[c.authorID]
		if !ok {
			if a, err = s.author(ctx, c.authorID); err != nil {
				return Request{}, err
			}
			authors[c.authorID] = a
		}
		q.Comments = append(q.Comments, Comment{ID: c.id, Author: a, Body: c.body, CreatedAt: c.createdAt})
	}
	return q, nil
}

// author names a principal: its kind and the display name of its consumer or admin row.
func (s *Service) author(ctx context.Context, principalID string) (Author, error) {
	rd := s.cfg.DB.Reader()
	p, err := s.cfg.Identity.Principals.Get(ctx, rd, principalID)
	if err != nil {
		return Author{}, err
	}
	a := Author{PrincipalID: principalID, Kind: p.Kind}
	switch p.Kind {
	case "consumer":
		c, err := s.cfg.Identity.Consumers.GetByPrincipal(ctx, rd, principalID)
		if err != nil && !errors.Is(err, identity.ErrNotFound) {
			return Author{}, err
		}
		if err == nil {
			a.DisplayName = c.DisplayName
		}
	case "host_admin":
		u, err := s.cfg.Identity.AdminUsers.GetByPrincipal(ctx, rd, principalID)
		if err != nil && !errors.Is(err, identity.ErrNotFound) {
			return Author{}, err
		}
		if err == nil {
			a.DisplayName = u.DisplayName
		}
	}
	return a, nil
}

// loadOpenTx reads a request inside the unit of work and refuses one that is another consumer's
// (when consumerID is not empty) or not open.
func loadOpenTx(tx *storage.Tx, id, consumerID string) (Request, error) {
	q, err := getTx(tx, id)
	if err != nil {
		return Request{}, err
	}
	if consumerID != "" && q.ConsumerID != consumerID {
		return Request{}, errNotFound
	}
	if q.Status != StatusOpen {
		return Request{}, ErrNotOpen
	}
	return q, nil
}

// Comment adds a comment to an open request and returns the request with its comments. A
// consumerID that is not empty means the consumer's own call; empty means the host's.
func (s *Service) Comment(ctx context.Context, consumerID, authorPrincipalID, id, body string) (Request, error) {
	if err := s.checkText(ctx, "body", body); err != nil {
		return Request{}, err
	}
	err := s.write(ctx, func(tx *storage.Tx) error {
		if _, err := loadOpenTx(tx, id, consumerID); err != nil {
			return err
		}
		return insertCommentTx(tx, id, authorPrincipalID, body, s.now())
	})
	if err != nil {
		return Request{}, wrap("comment", err)
	}
	return s.fetch(ctx, id, "")
}

// Withdraw moves the consumer's open request to withdrawn.
func (s *Service) Withdraw(ctx context.Context, consumerID, principalID, id string) (Request, error) {
	err := s.write(ctx, func(tx *storage.Tx) error {
		q, err := loadOpenTx(tx, id, consumerID)
		if err != nil {
			return err
		}
		now := s.now()
		v, err := finishTx(tx, id, StatusWithdrawn, now, principalID, nil)
		if err != nil {
			return err
		}
		q.Status, q.DecidedAt = StatusWithdrawn, ptr(now.UTC())
		return captureTx(tx, q, v, "update")
	})
	if err != nil {
		return Request{}, wrap("withdraw", err)
	}
	return s.fetch(ctx, id, "")
}

// ApplyGrant makes the new grant version of the consumer inside tx, which is the unit of work
// that approves the request, and returns the new version's id. The admin module passes it, built
// from the grant operations it already has, so this module neither repeats their checks nor
// imports them. reason is the text for the version's reason column. An error from it rolls the
// whole approval back.
type ApplyGrant func(tx *storage.Tx, consumerID, reason string) (versionID string, err error)

// Approve makes the new grant version through apply, makes the request approved, writes the
// optional comment, the audit row and the resource_request.decided event, and records the sync
// change, all in one unit of work: if any step fails nothing is left behind. apply writes the
// grant.changed event in the same unit. A request that is not open is a conflict.
func (s *Service) Approve(ctx context.Context, adminPrincipalID, id, comment string, apply ApplyGrant) (Request, error) {
	if comment != "" {
		if err := s.checkText(ctx, "comment", comment); err != nil {
			return Request{}, err
		}
	}
	err := s.write(ctx, func(tx *storage.Tx) error {
		q, err := loadOpenTx(tx, id, "")
		if err != nil {
			return err
		}
		vid, err := apply(tx, q.ConsumerID, "resource_request:"+id)
		if err != nil {
			return err
		}
		return s.decideTx(tx, q, StatusApproved, adminPrincipalID, comment, &vid)
	})
	if err != nil {
		return Request{}, wrap("approve", err)
	}
	return s.fetch(ctx, id, "")
}

// Reject makes an open request rejected, with an optional comment for the consumer.
func (s *Service) Reject(ctx context.Context, adminPrincipalID, id, comment string) (Request, error) {
	if comment != "" {
		if err := s.checkText(ctx, "comment", comment); err != nil {
			return Request{}, err
		}
	}
	err := s.write(ctx, func(tx *storage.Tx) error {
		q, err := loadOpenTx(tx, id, "")
		if err != nil {
			return err
		}
		return s.decideTx(tx, q, StatusRejected, adminPrincipalID, comment, nil)
	})
	if err != nil {
		return Request{}, wrap("reject", err)
	}
	return s.fetch(ctx, id, "")
}

// decideTx is the part of an approval or a rejection after the grant: the status, the optional
// comment, the audit row, the domain event and the sync change.
func (s *Service) decideTx(tx *storage.Tx, q Request, to Status, adminPrincipalID, comment string, versionID *string) error {
	now := s.now()
	v, err := finishTx(tx, q.ID, to, now, adminPrincipalID, versionID)
	if err != nil {
		return err
	}
	if comment != "" {
		if err := insertCommentTx(tx, q.ID, adminPrincipalID, comment, now); err != nil {
			return err
		}
	}
	acct, err := s.cfg.Identity.Consumers.GetAccountTx(tx, q.ConsumerID)
	if err != nil {
		return err
	}
	detail := map[string]any{"request_id": q.ID, "consumer_id": q.ConsumerID}
	payload := map[string]any{"request_id": q.ID, "consumer_id": q.ConsumerID, "status": string(to)}
	action := ActionRejected
	if versionID != nil {
		action = ActionApproved
		detail["grant_version_id"] = *versionID
		payload["grant_version_id"] = *versionID
	}
	if _, err := audit.RecordAt(tx, audit.Clock(s.now), audit.Event{Action: action, ActorID: adminPrincipalID, TargetID: acct.PrincipalID, Detail: detail}); err != nil {
		return err
	}
	if err := s.emit(tx, events.ResourceRequestDecided, adminPrincipalID, acct.PrincipalID, payload); err != nil {
		return err
	}
	q.Status, q.DecidedAt, q.ResultingVersionID = to, ptr(now.UTC()), versionID
	return captureTx(tx, q, v, "update")
}

func ptr[T any](v T) *T { return &v }

func wrap(what string, err error) error { return fmt.Errorf("requests: %s: %w", what, err) }
