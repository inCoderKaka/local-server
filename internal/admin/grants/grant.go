package grants

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/storage"
)

var _ Auth = (*auth.Service)(nil)

// ModelLimit is one model a grant allows, by the engine's name, with its caps.
type ModelLimit struct {
	Model           string
	ContextLimit    int64
	MaxOutputTokens int64
}

// Limits are the terms of a grant version.
type Limits struct {
	Models []ModelLimit
	// Quota is the deprecated single quota; Quotas wins when both are given (req_001 D-107). On
	// output Quota is the alias derived from Quotas.
	Quota  *consumers.Quota
	Quotas []grants.QuotaEntry
	// Concurrency is the most requests at once; on output a grant that sets none shows the host's
	// scheduler.default_grant_concurrency.
	Concurrency       int64
	OnContextOverflow string
	// AllowedProperties nil allows any request property; an empty list allows none.
	AllowedProperties []string
	ExpiresAt         *time.Time
}

// GrantVersion is one grant version as the admin API shows it.
type GrantVersion struct {
	ID         string
	GrantID    string
	ConsumerID string
	Version    int64
	CreatedAt  time.Time
	// ResourceRequestID is the resource request whose approval made the version, if any.
	ResourceRequestID *string
	Limits            Limits
	// Warnings is what the host ignored in the request, set by SetGrant only.
	Warnings []string
}

// VersionPage is one page of grant versions, newest first.
type VersionPage struct {
	Items      []GrantVersion
	NextCursor string // empty on the last page
}

// account returns the consumer or errNoConsumer.
func (s *Service) account(ctx context.Context, consumerID string) (identity.ConsumerAccount, error) {
	a, err := s.cfg.Identity.Consumers.GetAccount(ctx, s.cfg.DB.Reader(), consumerID)
	if errors.Is(err, identity.ErrNotFound) {
		return a, errNoConsumer
	}
	return a, err
}

// GetGrant returns the consumer's current grant version. A consumer or grant that does not exist,
// or a grant that has no version yet, is not found.
func (s *Service) GetGrant(ctx context.Context, consumerID string) (GrantVersion, error) {
	if _, err := s.account(ctx, consumerID); err != nil {
		return GrantVersion{}, err
	}
	rd := s.cfg.DB.Reader()
	g, err := s.cfg.Grants.Grants.GetByConsumer(ctx, rd, consumerID)
	if isNotFound(err) || (err == nil && g.CurrentVersionID == nil) {
		return GrantVersion{}, errNoGrant
	}
	if err != nil {
		return GrantVersion{}, err
	}
	v, err := s.cfg.Grants.Versions.Get(ctx, rd, *g.CurrentVersionID)
	if err != nil {
		return GrantVersion{}, err
	}
	views, err := s.views(ctx, consumerID, []grants.Version{v})
	if err != nil {
		return GrantVersion{}, err
	}
	return views[0], nil
}

// ListGrantVersions returns one page of the consumer's grant versions, newest first. A consumer
// without a grant has none.
func (s *Service) ListGrantVersions(ctx context.Context, consumerID string, q ListQuery) (VersionPage, error) {
	if _, err := s.account(ctx, consumerID); err != nil {
		return VersionPage{}, err
	}
	n, err := s.pageSize(ctx, q.Limit)
	if err != nil {
		return VersionPage{}, err
	}
	before, err := decodeVersionCursor(q.Cursor)
	if err != nil {
		return VersionPage{}, err
	}
	rd := s.cfg.DB.Reader()
	g, err := s.cfg.Grants.Grants.GetByConsumer(ctx, rd, consumerID)
	if isNotFound(err) {
		return VersionPage{}, nil
	}
	if err != nil {
		return VersionPage{}, err
	}
	vs, err := s.cfg.Grants.Versions.ListPage(ctx, rd, g.ID, before, n+1)
	if err != nil {
		return VersionPage{}, err
	}
	var page VersionPage
	if len(vs) > n {
		vs = vs[:n]
		page.NextCursor = encodeVersionCursor(vs[n-1].VersionNo)
	}
	if page.Items, err = s.views(ctx, consumerID, vs); err != nil {
		return VersionPage{}, err
	}
	return page, nil
}

// views turns stored versions into the shown form: models by name, the quota alias, the host's
// default concurrency where the version sets none.
func (s *Service) views(ctx context.Context, consumerID string, vs []grants.Version) ([]GrantVersion, error) {
	rd := s.cfg.DB.Reader()
	all, err := s.cfg.Grants.Models.List(ctx, rd, true)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(all))
	for _, m := range all {
		names[m.ID] = m.Name
	}
	legacy, err := limits.LegacyWindow(ctx, s.cfg.Settings)
	if err != nil {
		return nil, err
	}
	defConc, err := s.cfg.Settings.Int(ctx, scheduler.KeyGrantConcurrency, "")
	if err != nil {
		return nil, err
	}
	out := make([]GrantVersion, len(vs))
	for i, v := range vs {
		l := Limits{OnContextOverflow: v.OnContextOverflow, ExpiresAt: v.ExpiresAt, Concurrency: defConc, Models: []ModelLimit{}}
		if v.MaxConcurrency != nil {
			l.Concurrency = *v.MaxConcurrency
		}
		for _, m := range v.Models {
			ml := ModelLimit{Model: names[m.ModelID]}
			// A cap that is empty (the host default) shows as 0; the admin API always writes both.
			if m.ContextCap != nil {
				ml.ContextLimit = *m.ContextCap
			}
			if m.MaxOutputTokens != nil {
				ml.MaxOutputTokens = *m.MaxOutputTokens
			}
			l.Models = append(l.Models, ml)
		}
		slices.SortFunc(l.Models, func(a, b ModelLimit) int { return strings.Compare(a.Model, b.Model) })
		l.Quotas = v.AllQuotas(legacy)
		grants.SortQuotas(l.Quotas)
		l.Quota = consumers.QuotaAlias(l.Quotas, legacy)
		if v.AllowedProperties != nil {
			l.AllowedProperties = []string{}
			// Text that is not a list of names makes the gateway fail every request on the grant
			// (I-113); it shows as no property allowed, which is what happens.
			_ = json.Unmarshal([]byte(*v.AllowedProperties), &l.AllowedProperties)
			if l.AllowedProperties == nil {
				l.AllowedProperties = []string{}
			}
		}
		out[i] = GrantVersion{ID: v.ID, GrantID: v.GrantID, ConsumerID: consumerID, Version: v.VersionNo, CreatedAt: v.CreatedAt, Limits: l}
	}
	return out, nil
}

// SetGrant makes a new grant version for the consumer and makes it current, even when it equals
// the current one (req_001 D-25). It creates the grant when the consumer has none. The version,
// its audit row and its grant.changed event are one unit of work; afterwards the key cache is
// cleared.
func (s *Service) SetGrant(ctx context.Context, actor, consumerID string, in Limits) (GrantVersion, error) {
	acct, err := s.account(ctx, consumerID)
	if err != nil {
		return GrantVersion{}, err
	}
	if acct.Status == identity.StatusDeleted {
		return GrantVersion{}, errDeleted
	}
	nv, warnings, err := s.newVersion(ctx, in)
	if err != nil {
		return GrantVersion{}, err
	}
	nv.CreatedBy = ptrOrNil(actor)
	var created grants.Version
	err = s.write(ctx, func(tx *storage.Tx) error {
		g, err := s.cfg.Grants.Grants.GetByConsumerTx(tx, consumerID)
		if isNotFound(err) {
			g, err = s.cfg.Grants.Grants.Create(tx, consumerID)
		}
		if err != nil {
			return err
		}
		if created, err = s.cfg.Grants.Versions.Create(tx, g.ID, nv); err != nil {
			return err
		}
		if err := s.record(tx, audit.Event{Action: ActionGrantChanged, ActorID: actor, TargetID: acct.PrincipalID,
			Detail: map[string]any{"consumer_id": consumerID, "grant_id": g.ID, "version": created.VersionNo}}); err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]any{"consumer_id": consumerID, "grant_id": g.ID, "version": created.VersionNo})
		if err != nil {
			return err
		}
		_, err = s.cfg.Events.Emit(tx, events.Event{Type: events.GrantChanged, ActorPrincipalID: actor, TargetPrincipalID: acct.PrincipalID, Payload: payload})
		return err
	})
	if err != nil {
		return GrantVersion{}, wrap("set grant", err)
	}
	// grant.changed already clears the cache through the auth service's subscription; clearing
	// it here too keeps the promise even where the service is not subscribed.
	s.cfg.Auth.Invalidate()
	views, err := s.views(ctx, consumerID, []grants.Version{created})
	if err != nil {
		return GrantVersion{}, err
	}
	views[0].Warnings = warnings
	return views[0], nil
}
