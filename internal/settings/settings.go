package settings

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Owner is the party responsible for a setting (D-54).
type Owner string

// The parties that own settings.
const (
	PlatformAdmin Owner = "platform_admin"
	Host          Owner = "host"
	Consumer      Owner = "consumer"
)

// Kind is the type of a setting's value.
type Kind string

// The supported value types.
const (
	String   Kind = "string"
	Int      Kind = "int"
	Bool     Kind = "bool"
	Duration Kind = "duration"
)

// Errors returned by Store.
var (
	ErrUnknownKey   = errors.New("settings: unknown key")
	ErrNotOwner     = errors.New("settings: party does not own this setting")
	ErrInvalidValue = errors.New("settings: value does not parse as the setting's type")
	ErrKind         = errors.New("settings: setting has a different type")
	ErrScope        = errors.New("settings: scope does not match the setting")
	ErrDefinition   = errors.New("settings: invalid or conflicting definition")
	// ErrRejected wraps the error of a write validator (SetValidator): the value parses but
	// its module refuses it now.
	ErrRejected = errors.New("settings: the owning module refuses this change")
)

// Def defines one setting.
type Def struct {
	// Key is the dotted name, unique across modules, for example "retention.interval".
	Key  string
	Kind Kind
	// Default is the value used until the owner sets one, in the text form Set accepts
	// (Duration uses time.ParseDuration syntax).
	Default string
	Owner   Owner
	// Description is plain language for the admin UI.
	Description string
	// Scoped means one value per scope (for example per consumer id) instead of one value.
	Scoped bool
}

func (d Def) validate() error {
	if d.Key == "" {
		return fmt.Errorf("%w: empty key", ErrDefinition)
	}
	switch d.Owner {
	case PlatformAdmin, Host, Consumer:
	default:
		return fmt.Errorf("%w: %s: unknown owner %q", ErrDefinition, d.Key, d.Owner)
	}
	if err := checkValue(d.Kind, d.Default); err != nil {
		return fmt.Errorf("%w: %s: default: %v", ErrDefinition, d.Key, err)
	}
	return nil
}

func checkValue(k Kind, v string) error {
	var err error
	switch k {
	case String:
	case Int:
		_, err = strconv.ParseInt(v, 10, 64)
	case Bool:
		_, err = strconv.ParseBool(v)
	case Duration:
		_, err = time.ParseDuration(v)
	default:
		return fmt.Errorf("unknown kind %q", k)
	}
	return err
}

// Module returns the storage registration of the settings migrations.
func Module() storage.Module {
	sub, err := fs.Sub(ownMigrations, "migrations")
	if err != nil {
		panic(err) // the embedded directory is fixed at build time
	}
	return storage.Module{Name: "settings", Prefixes: []string{"hst_"}, Migrations: sub}
}

// Keys of the settings this module registers for its own retention worker.
const (
	KeyRetentionInterval  = "retention.interval"
	KeyRetentionBatchRows = "retention.batch_rows"
)

// Store holds the registered definitions and reads and writes values.
type Store struct {
	db  *storage.DB
	now func() time.Time

	mu         sync.RWMutex
	defs       map[string]Def
	rules      []Rule
	validators map[string]func(ctx context.Context, value string) error
}

// New returns a Store over db, which must have been opened with Module(). It registers
// the settings of its own retention worker.
func New(db *storage.DB) *Store {
	s := &Store{db: db, now: time.Now, defs: map[string]Def{}}
	err := s.Register(
		Def{Key: KeyRetentionInterval, Kind: Duration, Default: "1h", Owner: Host,
			Description: "How often the retention worker deletes records older than their retention."},
		Def{Key: KeyRetentionBatchRows, Kind: Int, Default: "5000", Owner: Host,
			Description: "Most rows one retention delete removes at a time, so the database is never locked for long."},
	)
	if err != nil {
		panic(err) // fixed definitions
	}
	return s
}

// Register adds definitions. Registering a key again with an identical definition is
// accepted; a different one is refused.
func (s *Store) Register(defs ...Def) error {
	for _, d := range defs {
		if err := d.validate(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range defs {
		if old, ok := s.defs[d.Key]; ok && old != d {
			return fmt.Errorf("%w: %s is already registered differently", ErrDefinition, d.Key)
		}
		s.defs[d.Key] = d
	}
	return nil
}

// SetValidator makes fn run before every Set and Reset of key, with the value about to be
// stored (the default's value for Reset). A non-nil error refuses the write; Set and Reset
// return it wrapped in ErrRejected. It is for a rule only the owning module can know, for
// example a setting that is frozen once something exists (req_001 D-97). The key must be
// registered; a later call for the same key replaces the validator. fn runs before the write and not in its transaction.
func (s *Store) SetValidator(key string, fn func(ctx context.Context, value string) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.defs[key]; !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	if s.validators == nil {
		s.validators = map[string]func(context.Context, string) error{}
	}
	s.validators[key] = fn
	return nil
}

func (s *Store) validate(ctx context.Context, key, value string) error {
	s.mu.RLock()
	fn := s.validators[key]
	s.mu.RUnlock()
	if fn == nil {
		return nil
	}
	if err := fn(ctx, value); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrRejected, key, err)
	}
	return nil
}

// Definition returns the registered definition of key.
func (s *Store) Definition(key string) (Def, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.defs[key]
	return d, ok
}

func (s *Store) lookup(key, scope string) (Def, error) {
	d, ok := s.Definition(key)
	if !ok {
		return Def{}, fmt.Errorf("%w: %s", ErrUnknownKey, key)
	}
	if !d.Scoped && scope != "" {
		return Def{}, fmt.Errorf("%w: %s has a single value", ErrScope, key)
	}
	if d.Scoped && scope == "" {
		return Def{}, fmt.Errorf("%w: %s needs a scope", ErrScope, key)
	}
	return d, nil
}

// Get returns the stored text value of key in scope, or the default. Use "" as scope for a
// setting that is not Scoped.
func (s *Store) Get(ctx context.Context, key, scope string) (string, error) {
	d, err := s.lookup(key, scope)
	if err != nil {
		return "", err
	}
	rows, err := s.db.Reader().Query(ctx, `SELECT value FROM hst_setting WHERE key = ? AND scope = ?`, key, scope)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return d.Default, rows.Err()
	}
	var v string
	if err := rows.Scan(&v); err != nil {
		return "", err
	}
	return v, nil
}

func (s *Store) typed(ctx context.Context, k Kind, key, scope string) (string, error) {
	d, err := s.lookup(key, scope)
	if err != nil {
		return "", err
	}
	if d.Kind != k {
		return "", fmt.Errorf("%w: %s is %s, not %s", ErrKind, key, d.Kind, k)
	}
	return s.Get(ctx, key, scope)
}

// String returns a String setting.
func (s *Store) String(ctx context.Context, key, scope string) (string, error) {
	return s.typed(ctx, String, key, scope)
}

// Int returns an Int setting.
func (s *Store) Int(ctx context.Context, key, scope string) (int64, error) {
	v, err := s.typed(ctx, Int, key, scope)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(v, 10, 64)
}

// Bool returns a Bool setting.
func (s *Store) Bool(ctx context.Context, key, scope string) (bool, error) {
	v, err := s.typed(ctx, Bool, key, scope)
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(v)
}

// Duration returns a Duration setting.
func (s *Store) Duration(ctx context.Context, key, scope string) (time.Duration, error) {
	v, err := s.typed(ctx, Duration, key, scope)
	if err != nil {
		return 0, err
	}
	return time.ParseDuration(v)
}

// Set stores value for key in scope on behalf of party by. It fails with ErrNotOwner unless
// by owns the setting, and with ErrInvalidValue if value does not parse as the setting's
// type; it checks nothing else (D-54). Whether the caller is really that party (and, for a
// consumer-owned setting, that the scope is the caller's own id) is the caller's duty.
func (s *Store) Set(ctx context.Context, by Owner, key, scope, value string) error {
	d, err := s.lookup(key, scope)
	if err != nil {
		return err
	}
	if d.Owner != by {
		return fmt.Errorf("%w: %s is owned by %s, not %s", ErrNotOwner, key, d.Owner, by)
	}
	if err := checkValue(d.Kind, value); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalidValue, key, err)
	}
	if err := s.validate(ctx, key, value); err != nil {
		return err
	}
	at := s.now().UnixMilli()
	return s.db.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.Exec(`INSERT INTO hst_setting (key, scope, value, updated_by, updated_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (key, scope) DO UPDATE SET value = excluded.value, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
			key, scope, value, string(by), at)
		return err
	})
}

// Reset removes the stored value so the default applies again. The same owner check as Set.
func (s *Store) Reset(ctx context.Context, by Owner, key, scope string) error {
	d, err := s.lookup(key, scope)
	if err != nil {
		return err
	}
	if d.Owner != by {
		return fmt.Errorf("%w: %s is owned by %s, not %s", ErrNotOwner, key, d.Owner, by)
	}
	if err := s.validate(ctx, key, d.Default); err != nil {
		return err
	}
	return s.db.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.Exec(`DELETE FROM hst_setting WHERE key = ? AND scope = ?`, key, scope)
		return err
	})
}
