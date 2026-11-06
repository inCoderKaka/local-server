package consumers

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// Access key states, derived from the key row and the clock (req_001 D-38).
const (
	KeyActive  = "active"
	KeyExpired = "expired"
	KeyRevoked = "revoked"
)

// Quota is an extra allowance per quota window for one key. A nil number adds no limit; 0 allows
// none. The window is the host's setting, not the caller's (req_001 D-98).
type Quota struct {
	Tokens   *int64
	Requests *int64
}

// Narrowing is what a key adds on top of its consumer's grant, with models by name. It can only
// narrow the grant (req_001 D-25).
type Narrowing struct {
	Models          []string
	Quota           *Quota
	ContextLimit    *int64
	MaxOutputTokens *int64
	Concurrency     *int64
}

// Key is an access key as the admin sees it. It holds no secret and no hash.
type Key struct {
	ID         string
	ConsumerID string
	Prefix     string
	Name       *string
	Status     string
	Narrowing  *Narrowing
	BoundNode  *string
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
}

// NewKey is a key to issue.
type NewKey struct {
	Name      *string
	ExpiresAt *time.Time
	Narrowing *Narrowing
	BoundNode *string
}

// KeyChange is a change to a key; a field left out stays as it is. A field given as null is
// cleared.
type KeyChange struct {
	Name      Field[string]
	ExpiresAt Field[time.Time]
	Narrowing Field[Narrowing]
	BoundNode Field[string]
}

// IssuedKey is a new key and its secret. The secret is redacted in any log line or error; the
// one caller that answers the request calls Secret.Reveal.
type IssuedKey struct {
	Key    Key
	Secret telemetry.Redacted
}

func keyStatus(k identity.AccessKey, now time.Time) string {
	switch {
	case k.RevokedAt != nil:
		return KeyRevoked
	case k.ExpiresAt != nil && !now.Before(*k.ExpiresAt):
		return KeyExpired
	}
	return KeyActive
}

// modelNames maps model ids to names for the keys of one answer.
type modelNames struct {
	s     *Service
	ctx   context.Context
	rd    *storage.Reader
	names map[string]string
}

func (s *Service) modelNames(ctx context.Context) *modelNames {
	return &modelNames{s: s, ctx: ctx, rd: s.cfg.DB.Reader(), names: map[string]string{}}
}

// name returns the name of a model; a model the host no longer knows is shown by its id.
func (m *modelNames) name(id string) (string, error) {
	if n, ok := m.names[id]; ok {
		return n, nil
	}
	mo, err := m.s.cfg.Grants.Models.Get(m.ctx, m.rd, id)
	n := id
	switch {
	case err == nil:
		n = mo.Name
	case !errors.Is(err, grants.ErrNotFound):
		return "", err
	}
	m.names[id] = n
	return n, nil
}

// view builds the admin view of a stored key. A narrowing that cannot be read is shown as none
// and logged by key id: the auth service refuses such a key, so it cannot be used.
func (m *modelNames) view(k identity.AccessKey, now time.Time) (Key, error) {
	out := Key{ID: k.ID, ConsumerID: k.ConsumerID, Prefix: k.Prefix, Name: k.Label, Status: keyStatus(k, now),
		BoundNode: k.BoundNode, CreatedAt: k.CreatedAt, ExpiresAt: k.ExpiresAt, RevokedAt: k.RevokedAt}
	if k.NarrowingJSON == nil {
		return out, nil
	}
	n, err := auth.ParseNarrowing(*k.NarrowingJSON)
	if err != nil {
		m.s.log.WarnContext(m.ctx, "consumers: a stored narrowing cannot be read", "key_id", k.ID)
		return out, nil
	}
	nv := &Narrowing{ContextLimit: n.ContextCap, MaxOutputTokens: n.MaxOutputTokens, Concurrency: n.MaxConcurrency}
	if n.QuotaTokensPerDay != nil || n.QuotaRequestsPerDay != nil {
		nv.Quota = &Quota{Tokens: n.QuotaTokensPerDay, Requests: n.QuotaRequestsPerDay}
	}
	for _, id := range n.ModelIDs {
		name, err := m.name(id)
		if err != nil {
			return Key{}, err
		}
		nv.Models = append(nv.Models, name)
	}
	out.Narrowing = nv
	return out, nil
}

// ListAccessKeys returns a consumer's keys, oldest first, with their state.
func (s *Service) ListAccessKeys(ctx context.Context, consumerID string) ([]Key, error) {
	if _, err := s.GetConsumer(ctx, consumerID); err != nil {
		return nil, err
	}
	keys, err := s.cfg.Identity.AccessKeys.ListByConsumer(ctx, s.cfg.DB.Reader(), consumerID)
	if err != nil {
		return nil, err
	}
	now, names := s.now(), s.modelNames(ctx)
	out := make([]Key, 0, len(keys))
	for _, k := range keys {
		v, err := names.view(k, now)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// checkKeyFields validates the fields every key request shares. A past expiry is refused: to end
// a key now, revoke it.
func (s *Service) checkKeyFields(name *string, expires *time.Time, node *string) error {
	if name != nil && strings.TrimSpace(*name) == "" {
		return invalid("name", "The name must not be empty.")
	}
	if expires != nil && !expires.After(s.now()) {
		return invalid("expires_at", "The expiry must be in the future. To end a key now, revoke it.")
	}
	if node != nil && strings.TrimSpace(*node) == "" {
		return invalid("bound_node", "The node must not be empty.")
	}
	return nil
}

// IssueAccessKey issues a key for a consumer that is not deleted, through the auth service, in
// one unit of work with its audit row. Only the prefix and a hash of the secret are stored
// (req_001 D-25, D-40); the secret is in the result and nowhere else.
func (s *Service) IssueAccessKey(ctx context.Context, actor, consumerID string, in NewKey) (IssuedKey, error) {
	if err := s.checkKeyFields(in.Name, in.ExpiresAt, in.BoundNode); err != nil {
		return IssuedKey{}, err
	}
	if _, err := s.GetConsumer(ctx, consumerID); err != nil {
		return IssuedKey{}, err
	}
	nar, view, err := s.narrowingFor(ctx, consumerID, in.Narrowing)
	if err != nil {
		return IssuedKey{}, err
	}
	p := auth.IssueParams{ConsumerID: consumerID, ActorID: actor, Label: in.Name, Narrowing: nar, ExpiresAt: in.ExpiresAt}
	if in.BoundNode != nil {
		p.BoundNode = *in.BoundNode
	}
	var out IssuedKey
	err = s.write(ctx, func(tx *storage.Tx) error {
		a, err := s.consumerTx(tx, consumerID)
		if err != nil {
			return err
		}
		if a.Status == identity.StatusDeleted {
			return conflict("A deleted consumer cannot get a new access key.")
		}
		is, err := s.cfg.Auth.Issue(tx, p)
		if err != nil {
			return err
		}
		k := is.AccessKey
		out = IssuedKey{Secret: is.Key, Key: Key{ID: k.ID, ConsumerID: k.ConsumerID, Prefix: k.Prefix, Name: k.Label, Status: keyStatus(k, s.now()),
			Narrowing: view, BoundNode: k.BoundNode, CreatedAt: k.CreatedAt, ExpiresAt: k.ExpiresAt}}
		return nil
	})
	if err != nil {
		return IssuedKey{}, err
	}
	return out, nil
}

// UpdateAccessKey changes the name, expiry, narrowing or bound node of a key. A revoked or
// expired key is final and returns a conflict (req_001 D-54). A narrowing is checked against
// the consumer's current grant, as when a key is issued. A change that leaves everything as it
// is writes nothing.
func (s *Service) UpdateAccessKey(ctx context.Context, actor, keyID string, ch KeyChange) (Key, error) {
	var name, node *string
	var expires *time.Time
	if ch.Name.Set && !ch.Name.Null {
		name = &ch.Name.Value
	}
	if ch.ExpiresAt.Set && !ch.ExpiresAt.Null {
		expires = &ch.ExpiresAt.Value
	}
	if ch.BoundNode.Set && !ch.BoundNode.Null {
		node = &ch.BoundNode.Value
	}
	if err := s.checkKeyFields(name, expires, node); err != nil {
		return Key{}, err
	}
	rd := s.cfg.DB.Reader()
	cur, err := s.cfg.Identity.AccessKeys.Get(ctx, rd, keyID)
	if isNotFound(err) {
		return Key{}, errNoKey
	}
	if err != nil {
		return Key{}, err
	}
	var nar auth.Narrowing
	if ch.Narrowing.Set && !ch.Narrowing.Null {
		if nar, _, err = s.narrowingFor(ctx, cur.ConsumerID, &ch.Narrowing.Value); err != nil {
			return Key{}, err
		}
	}
	var out identity.AccessKey
	changed := false
	err = s.write(ctx, func(tx *storage.Tx) error {
		k, err := s.cfg.Identity.AccessKeys.GetTx(tx, keyID)
		if isNotFound(err) {
			return errNoKey
		}
		if err != nil {
			return err
		}
		out = k
		switch keyStatus(k, s.now()) {
		case KeyRevoked:
			return conflict("A revoked access key cannot be changed.")
		case KeyExpired:
			return conflict("An expired access key cannot be changed.")
		}
		label, narrowing, bound, exp := k.Label, k.NarrowingJSON, k.BoundNode, k.ExpiresAt
		var fields []string
		if ch.Name.Set {
			if v := name; !samePtr(label, v) {
				label = v
				fields = append(fields, "name")
			}
		}
		if ch.ExpiresAt.Set {
			if !sameTime(exp, expires) {
				exp = expires
				fields = append(fields, "expires_at")
			}
		}
		if ch.Narrowing.Set {
			var text *string
			if !ch.Narrowing.Null {
				if text, err = nar.Text(); err != nil {
					return err
				}
			}
			if !samePtr(narrowing, text) {
				narrowing = text
				fields = append(fields, "narrowing")
			}
		}
		if ch.BoundNode.Set {
			if !samePtr(bound, node) {
				bound = node
				fields = append(fields, "bound_node")
			}
		}
		if len(fields) == 0 {
			return nil
		}
		if err := s.cfg.Identity.AccessKeys.Update(tx, keyID, label, narrowing, bound, exp); err != nil {
			return err
		}
		// Field names only: the key's name is personal data and the audit trail is kept forever.
		if err := s.record(tx, audit.Event{Action: ActionKeyUpdated, ActorID: actor,
			Detail: map[string]any{"key_id": keyID, "consumer_id": k.ConsumerID, "fields": fields}}); err != nil {
			return err
		}
		changed = true
		out, err = s.cfg.Identity.AccessKeys.GetTx(tx, keyID)
		return err
	})
	if err != nil {
		return Key{}, err
	}
	if changed {
		s.cfg.Auth.Invalidate() // a key update emits no event the key cache listens to
	}
	return s.modelNames(ctx).view(out, s.now())
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.UnixMilli() == b.UnixMilli()
}

// RevokeAccessKey ends a key at once through the auth service, which audits it and emits
// access_key.revoked so the key cache forgets it. A key that is already revoked stays so and
// writes nothing.
func (s *Service) RevokeAccessKey(ctx context.Context, actor, keyID string) (Key, error) {
	var out identity.AccessKey
	err := s.write(ctx, func(tx *storage.Tx) error {
		k, err := s.cfg.Identity.AccessKeys.GetTx(tx, keyID)
		if isNotFound(err) {
			return errNoKey
		}
		if err != nil {
			return err
		}
		out = k
		if k.RevokedAt != nil {
			return nil
		}
		if err := s.cfg.Auth.Revoke(tx, keyID, actor); err != nil {
			return err
		}
		out, err = s.cfg.Identity.AccessKeys.GetTx(tx, keyID)
		return err
	})
	if err != nil {
		return Key{}, err
	}
	return s.modelNames(ctx).view(out, s.now())
}

// narrowingFor checks a requested narrowing and returns it in its stored form (models by id) and
// in its shown form (normalised). A nil or empty narrowing is none. A model must be known to the
// host; when the consumer has a current grant version, a narrowing wider than it is refused:
// a model the grant does not allow, or a number above the grant's own (req_001 D-25). A consumer
// without a grant yet gets only the checks that need no grant.
func (s *Service) narrowingFor(ctx context.Context, consumerID string, n *Narrowing) (auth.Narrowing, *Narrowing, error) {
	if n == nil {
		return auth.Narrowing{}, nil, nil
	}
	view := &Narrowing{ContextLimit: n.ContextLimit, MaxOutputTokens: n.MaxOutputTokens, Concurrency: n.Concurrency}
	if n.Quota != nil && (n.Quota.Tokens != nil || n.Quota.Requests != nil) {
		view.Quota = &Quota{Tokens: n.Quota.Tokens, Requests: n.Quota.Requests}
	}
	for _, c := range []struct {
		param string
		v     *int64
		min   int64
	}{
		{"narrowing.context_limit", view.ContextLimit, 1},
		{"narrowing.max_output_tokens", view.MaxOutputTokens, 1},
		{"narrowing.concurrency", view.Concurrency, 1},
		{"narrowing.quota.tokens", quotaPart(view.Quota, true), 0},
		{"narrowing.quota.requests", quotaPart(view.Quota, false), 0},
	} {
		if c.v != nil && *c.v < c.min {
			return auth.Narrowing{}, nil, invalid(c.param, "The value is below the lowest allowed.")
		}
	}
	rd := s.cfg.DB.Reader()
	st := auth.Narrowing{QuotaTokensPerDay: quotaPart(view.Quota, true), QuotaRequestsPerDay: quotaPart(view.Quota, false),
		MaxConcurrency: view.Concurrency, ContextCap: view.ContextLimit, MaxOutputTokens: view.MaxOutputTokens}
	for _, name := range n.Models {
		if strings.TrimSpace(name) == "" {
			return auth.Narrowing{}, nil, invalid("narrowing.models", "A model name must not be empty.")
		}
		if slices.Contains(view.Models, name) {
			continue
		}
		m, err := s.cfg.Grants.Models.GetByName(ctx, rd, name)
		if errors.Is(err, grants.ErrNotFound) {
			return auth.Narrowing{}, nil, invalid("narrowing.models", "A model in the narrowing is not known to this host.")
		}
		if err != nil {
			return auth.Narrowing{}, nil, err
		}
		view.Models = append(view.Models, name)
		st.ModelIDs = append(st.ModelIDs, m.ID)
	}
	g, err := s.cfg.Grants.Grants.GetByConsumer(ctx, rd, consumerID)
	if err == nil {
		var v grants.Version
		if v, err = s.cfg.Grants.Versions.Current(ctx, rd, g.ID); err == nil {
			if e := widerThan(v, st); e != nil {
				return auth.Narrowing{}, nil, e
			}
		}
	}
	if err != nil && !errors.Is(err, grants.ErrNotFound) {
		return auth.Narrowing{}, nil, err
	}
	if view.Quota == nil && view.ContextLimit == nil && view.MaxOutputTokens == nil && view.Concurrency == nil && len(view.Models) == 0 {
		return auth.Narrowing{}, nil, nil
	}
	return st, view, nil
}

func quotaPart(q *Quota, tokens bool) *int64 {
	switch {
	case q == nil:
		return nil
	case tokens:
		return q.Tokens
	}
	return q.Requests
}

// widerThan reports the first part of n that is wider than the grant version v, or nil. A number
// is wider when the grant has a figure and n's is above it; a per-model cap is wider only when
// every model of the grant has a cap and n's is above the largest.
func widerThan(v grants.Version, n auth.Narrowing) *Error {
	for _, id := range n.ModelIDs {
		if !slices.ContainsFunc(v.Models, func(m grants.VersionModel) bool { return m.ModelID == id }) {
			return invalid("narrowing.models", "A model in the narrowing is not allowed by the consumer's grant.")
		}
	}
	for _, c := range []struct {
		param      string
		key, grant *int64
	}{
		{"narrowing.quota.tokens", n.QuotaTokensPerDay, v.QuotaTokensPerDay},
		{"narrowing.quota.requests", n.QuotaRequestsPerDay, v.QuotaRequestsPerDay},
		{"narrowing.concurrency", n.MaxConcurrency, v.MaxConcurrency},
		{"narrowing.context_limit", n.ContextCap, largestCap(v, func(m grants.VersionModel) *int64 { return m.ContextCap })},
		{"narrowing.max_output_tokens", n.MaxOutputTokens, largestCap(v, func(m grants.VersionModel) *int64 { return m.MaxOutputTokens })},
	} {
		if c.key != nil && c.grant != nil && *c.key > *c.grant {
			return invalid(c.param, "The value is wider than the consumer's grant allows.")
		}
	}
	return nil
}

// largestCap is the largest cap among the grant's models, or nil when the grant has no model or
// a model without a cap (then the host's default applies and no cap is a bound for the key).
func largestCap(v grants.Version, get func(grants.VersionModel) *int64) *int64 {
	if len(v.Models) == 0 {
		return nil
	}
	var top *int64
	for _, m := range v.Models {
		c := get(m)
		if c == nil {
			return nil
		}
		if top == nil || *c > *top {
			top = c
		}
	}
	return top
}
