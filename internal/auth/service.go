package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// KeyCacheTTL is the setting holding how long a resolved key stays in the cache; 0 turns the
// cache off. Change events clear the cache at once; the TTL bounds how stale an entry can get if
// an event is missed (D-38, D-39).
const KeyCacheTTL = "auth.cache_ttl"

// Audit actions written by this package.
const (
	ActionKeyIssued  = "access_key.issued"
	ActionKeyRevoked = "access_key.revoked"
)

// Authentication errors. Callers map every one of them except ErrNoGrant to the same
// invalid_api_key answer, so a response never tells which check failed.
var (
	ErrInvalidKey   = errors.New("auth: invalid access key") // malformed, unknown prefix or wrong secret
	ErrRevoked      = errors.New("auth: access key revoked")
	ErrExpired      = errors.New("auth: access key expired")
	ErrGrantExpired = errors.New("auth: grant version expired")
	ErrNoGrant      = errors.New("auth: consumer has no current grant version")
	ErrNodeMismatch = errors.New("auth: access key is bound to another node")
	// ErrNodeLookup accompanies ErrNodeMismatch when the bound node could not be looked up. The
	// secret had already matched, so this is not a failed guess.
	ErrNodeLookup = errors.New("auth: bound node lookup failed")

	ErrConsumerNotActive = errors.New("auth: consumer is not active")
)

// WhoIs is the port that names the transport node a connection came from (D-39). The transport
// module's adapters implement it; auth imports no transport.
type WhoIs interface {
	// NodeOf returns the id of the node behind remoteAddr, or an error if it cannot tell.
	NodeOf(ctx context.Context, remoteAddr string) (string, error)
}

// Config is what New needs.
type Config struct {
	DB       *storage.DB
	Identity *identity.Repos
	Grants   *grants.Repos
	Events   events.Notifier
	Settings *settings.Store
	// WhoIs checks bound keys. With none set, a bound key never authenticates.
	WhoIs WhoIs
	// Clock is the time source; nil means time.Now.
	Clock func() time.Time
	// Logger receives cache problems (never secrets); nil means slog.Default().
	Logger *slog.Logger
}

// Defs are the auth settings; New registers them.
func Defs() []settings.Def {
	return []settings.Def{{Key: KeyCacheTTL, Kind: settings.Duration, Default: "60s", Owner: settings.Host,
		Description: "How long a looked-up access key, its consumer and grant stay in memory. Changes to keys and grants clear it at once; 0 turns it off."}}
}

// Service issues, revokes and authenticates access keys.
type Service struct {
	cfg Config
	now func() time.Time
	log *slog.Logger

	mu    sync.Mutex
	gen   uint64
	cache map[string]*entry // by key prefix
}

type resolved struct {
	principalID string
	grantID     string
	version     grants.Version
	narrowing   Narrowing
}

type entry struct {
	key      identity.AccessKey
	res      *resolved // nil until the secret has been verified once
	loadedAt time.Time
	gen      uint64 // cache generation when the key row was read
}

// New builds the service, registers its settings and subscribes its cache to the change events.
func New(cfg Config) (*Service, error) {
	if cfg.DB == nil || cfg.Identity == nil || cfg.Grants == nil || cfg.Events == nil || cfg.Settings == nil {
		return nil, errors.New("auth: DB, Identity, Grants, Events and Settings are required")
	}
	s := &Service{cfg: cfg, now: cfg.Clock, log: cfg.Logger, cache: map[string]*entry{}}
	if s.now == nil {
		s.now = time.Now
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if err := cfg.Settings.Register(Defs()...); err != nil {
		return nil, err
	}
	err := cfg.Events.Subscribe("auth.cache", func(context.Context, events.Event) error {
		s.Invalidate()
		return nil
	}, events.AccessKeyRevoked, events.GrantChanged)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Invalidate clears the cache. The change-event subscription calls it. A caller that changes a
// key with identity.AccessKeyRepo.Update, soft-deletes a consumer, or changes a principal's
// status must call it after its unit of work commits; otherwise the change shows only when the
// cache TTL runs out.
func (s *Service) Invalidate() {
	s.mu.Lock()
	s.gen++
	s.cache = map[string]*entry{}
	s.mu.Unlock()
}

// IssueParams describes a key to issue.
type IssueParams struct {
	ConsumerID string
	ActorID    string // principal who issues it; empty means the system
	Label      *string
	Narrowing  Narrowing
	BoundNode  string // empty means not bound
	ExpiresAt  *time.Time
}

// Issued is a new key. Key.Reveal() is the only copy of the key and is shown once.
type Issued struct {
	AccessKey identity.AccessKey
	Key       telemetry.Redacted
}

// String, GoString and LogValue hide the key from logs and %v output.
func (Issued) String() string       { return telemetry.RedactedText }
func (Issued) GoString() string     { return telemetry.RedactedText }
func (Issued) LogValue() slog.Value { return slog.StringValue(telemetry.RedactedText) }

// Issue creates a key for a consumer inside the caller's unit of work and audits it in the
// same transaction. Only the prefix and a hash of the secret are stored (D-25, D-40).
func (s *Service) Issue(tx *storage.Tx, p IssueParams) (Issued, error) {
	nar, err := ParseNarrowing(mustJSON(p.Narrowing))
	if err != nil {
		return Issued{}, err
	}
	text, err := nar.Text()
	if err != nil {
		return Issued{}, err
	}
	var bound *string
	if p.BoundNode != "" {
		bound = &p.BoundNode
	}
	prefix, secret, err := newKey()
	if err != nil {
		return Issued{}, fmt.Errorf("auth: generate key: %w", err)
	}
	k, err := s.cfg.Identity.AccessKeys.Create(tx, identity.NewAccessKey{ConsumerID: p.ConsumerID, Prefix: prefix,
		SecretHash: HashSecret(secret), Label: p.Label, NarrowingJSON: text, BoundNode: bound, ExpiresAt: p.ExpiresAt})
	if err != nil {
		return Issued{}, fmt.Errorf("auth: store key: %w", err)
	}
	detail := map[string]any{"key_id": k.ID, "prefix": prefix, "consumer_id": p.ConsumerID}
	if bound != nil {
		detail["bound_node"] = *bound
	}
	if p.ExpiresAt != nil {
		detail["expires_at"] = p.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if text != nil {
		detail["narrowing"] = json.RawMessage(*text)
	}
	if _, err := audit.RecordAt(tx, audit.Clock(s.now), audit.Event{Action: ActionKeyIssued, ActorID: p.ActorID, Detail: detail}); err != nil {
		return Issued{}, err
	}
	return Issued{AccessKey: k, Key: telemetry.Redact(FormatKey(prefix, secret))}, nil
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v) // Narrowing always marshals
	return string(b)
}

// Revoke ends a key inside the caller's unit of work, audits it and emits access_key.revoked in
// the same transaction. The unit must be run by the Events notifier's Write, or Emit refuses.
// Subscribers, including this service's cache, run after commit. An already-revoked key returns
// ErrRevoked.
func (s *Service) Revoke(tx *storage.Tx, keyID, actorID string) error {
	k, err := s.cfg.Identity.AccessKeys.GetTx(tx, keyID)
	if err != nil {
		return err
	}
	if k.RevokedAt != nil {
		return ErrRevoked
	}
	if err := s.cfg.Identity.AccessKeys.Revoke(tx, keyID); err != nil {
		return err
	}
	detail := map[string]any{"key_id": keyID, "consumer_id": k.ConsumerID}
	if _, err := audit.RecordAt(tx, audit.Clock(s.now), audit.Event{Action: ActionKeyRevoked, ActorID: actorID, Detail: detail}); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"key_id": keyID})
	if err != nil {
		return err
	}
	_, err = s.cfg.Events.Emit(tx, events.Event{Type: events.AccessKeyRevoked, ActorPrincipalID: actorID, Payload: payload})
	return err
}

// Identity is who a valid key stands for and what it may do.
type Identity struct {
	KeyID          string
	Prefix         string
	ConsumerID     string
	PrincipalID    string
	GrantID        string
	GrantVersionID string
	GrantVersionNo int64
	Limits         Limits
}

// dummyHash is compared against when a prefix is unknown, so unknown and known prefixes cost
// about the same.
var dummyHash = HashSecret("no such key")

// Authenticate checks the value of an Authorization header and returns the identity behind it.
// remoteAddr is passed to the WhoIs port for bound keys. Secrets are never logged.
func (s *Service) Authenticate(ctx context.Context, header, remoteAddr string) (*Identity, error) {
	p, err := ParseHeader(header)
	if err != nil {
		return nil, ErrInvalidKey
	}
	ttl := s.ttl(ctx)
	e, err := s.lookup(ctx, p.Prefix, ttl)
	if err != nil {
		return nil, err
	}
	stored := dummyHash
	if e != nil {
		stored = e.key.SecretHash
	}
	if !secretMatches(p.Secret.Reveal(), stored) || e == nil {
		return nil, ErrInvalidKey
	}
	now := s.now()
	switch {
	case e.key.RevokedAt != nil:
		return nil, ErrRevoked
	case e.key.ExpiresAt != nil && !now.Before(*e.key.ExpiresAt):
		return nil, ErrExpired
	}
	if e.res == nil {
		if e, err = s.resolve(ctx, e, ttl); err != nil {
			return nil, err
		}
	}
	if v := e.res.version; v.ExpiresAt != nil && !now.Before(*v.ExpiresAt) {
		return nil, ErrGrantExpired
	}
	if e.key.BoundNode != nil {
		if s.cfg.WhoIs == nil || *e.key.BoundNode == "" {
			return nil, ErrNodeMismatch
		}
		node, err := s.cfg.WhoIs.NodeOf(ctx, remoteAddr)
		if err != nil {
			return nil, fmt.Errorf("%w: %w: %v", ErrNodeMismatch, ErrNodeLookup, err)
		}
		if node != *e.key.BoundNode {
			return nil, ErrNodeMismatch
		}
	}
	r := e.res
	return &Identity{KeyID: e.key.ID, Prefix: e.key.Prefix, ConsumerID: e.key.ConsumerID, PrincipalID: r.principalID,
		GrantID: r.grantID, GrantVersionID: r.version.ID, GrantVersionNo: r.version.VersionNo, Limits: narrow(r.version, r.narrowing)}, nil
}

func (s *Service) ttl(ctx context.Context) time.Duration {
	d, err := s.cfg.Settings.Duration(ctx, KeyCacheTTL, "")
	if err != nil {
		s.log.Warn("auth: cache ttl unreadable, cache off", "err", err)
		return 0
	}
	return d
}

// lookup returns the cached entry for a prefix or loads the key row. A nil entry with a nil
// error means the prefix is unknown.
func (s *Service) lookup(ctx context.Context, prefix string, ttl time.Duration) (*entry, error) {
	s.mu.Lock()
	gen := s.gen
	if e, ok := s.cache[prefix]; ok && ttl > 0 && s.now().Sub(e.loadedAt) < ttl {
		s.mu.Unlock()
		return e, nil
	}
	s.mu.Unlock()
	k, err := s.cfg.Identity.AccessKeys.GetByPrefix(ctx, s.cfg.DB.Reader(), prefix)
	if errors.Is(err, identity.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("auth: load key: %w", err)
	}
	e := &entry{key: k, loadedAt: s.now(), gen: gen}
	s.store(prefix, e, ttl)
	return e, nil
}

// resolve loads the consumer, grant and current version of a verified key and caches them.
func (s *Service) resolve(ctx context.Context, e *entry, ttl time.Duration) (*entry, error) {
	rd := s.cfg.DB.Reader()
	c, err := s.cfg.Identity.Consumers.Get(ctx, rd, e.key.ConsumerID)
	if err != nil {
		return nil, fmt.Errorf("auth: load consumer: %w", err)
	}
	pr, err := s.cfg.Identity.Principals.Get(ctx, rd, c.PrincipalID)
	if err != nil {
		return nil, fmt.Errorf("auth: load principal: %w", err)
	}
	if pr.Status != identity.StatusActive {
		return nil, ErrConsumerNotActive
	}
	g, err := s.cfg.Grants.Grants.GetByConsumer(ctx, rd, c.ID)
	if errors.Is(err, grants.ErrNotFound) {
		return nil, ErrNoGrant
	}
	if err != nil {
		return nil, fmt.Errorf("auth: load grant: %w", err)
	}
	v, err := s.cfg.Grants.Versions.Current(ctx, rd, g.ID)
	if errors.Is(err, grants.ErrNotFound) {
		return nil, ErrNoGrant
	}
	if err != nil {
		return nil, fmt.Errorf("auth: load grant version: %w", err)
	}
	var nar Narrowing
	if e.key.NarrowingJSON != nil {
		// A narrowing that cannot be read must not become "no narrowing": fail closed.
		if nar, err = ParseNarrowing(*e.key.NarrowingJSON); err != nil {
			return nil, fmt.Errorf("auth: key %s: %w", e.key.ID, err)
		}
	}
	ne := &entry{key: e.key, loadedAt: e.loadedAt, gen: e.gen, res: &resolved{principalID: c.PrincipalID, grantID: g.ID, version: v, narrowing: nar}}
	s.store(e.key.Prefix, ne, ttl)
	return ne, nil
}

// store caches e unless the cache is off or was cleared since e's key row was read, so an entry
// loaded before a change event never outlives it.
func (s *Service) store(prefix string, e *entry, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	s.mu.Lock()
	if s.gen == e.gen {
		s.cache[prefix] = e
	}
	s.mu.Unlock()
}
