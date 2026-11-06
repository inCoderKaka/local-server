package grants_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	admingrants "github.com/inCoderKaka/local-server/internal/admin/grants"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/engine/fake"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/usage"
)

var ctx = context.Background()

func i64(v int64) *int64 { return &v }

// countingAuth counts the key cache clears the service asks for, and passes them on.
type countingAuth struct {
	*auth.Service
	mu sync.Mutex
	n  int
}

func (a *countingAuth) Invalidate() {
	a.mu.Lock()
	a.n++
	a.mu.Unlock()
	a.Service.Invalidate()
}

func (a *countingAuth) count() int { a.mu.Lock(); defer a.mu.Unlock(); return a.n }

// failures is a stand-in for the gateway's counter of failed rule evaluations.
type failures struct {
	mu      sync.Mutex
	n       map[string]int64
	cleared []string
}

func (f *failures) RuleFailures(id string) int64 { f.mu.Lock(); defer f.mu.Unlock(); return f.n[id] }
func (f *failures) ClearRuleFailures(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.n, id)
	f.cleared = append(f.cleared, id)
}
func (f *failures) set(id string, n int64) { f.mu.Lock(); f.n[id] = n; f.mu.Unlock() }
func (f *failures) clearedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cleared...)
}

// env is a real SQLite database with the identity, grants, audit, events and usage tables, the auth
// service, the consumers service (to make consumers and keys), the fake engine and the grants
// service, all on one settable clock.
type env struct {
	t       *testing.T
	db      *storage.DB
	st      *settings.Store
	ir      *identity.Repos
	gr      *grants.Repos
	au      *countingAuth
	cons    *consumers.Service
	svc     *admingrants.Service
	eng     *fake.Engine
	fail    *failures
	bus     events.Notifier
	now     time.Time
	admin   string
	emitted []events.Event
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"),
		settings.Module(), identity.Module(), grants.Module(), audit.Module(), events.Module(), usage.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{t: t, db: db, st: settings.New(db), now: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC),
		eng:  fake.New(fake.WithModels(fake.ModelSpec{Name: "m1", ContextLength: 4096}, fake.ModelSpec{Name: "m2", ContextLength: 4096})),
		fail: &failures{n: map[string]int64{}}}
	clock := func() time.Time { return e.now }
	e.ir, e.gr = identity.NewRepos(clock), grants.NewRepos(clock)
	bus := events.New()
	e.bus = bus
	svc, err := auth.New(auth.Config{DB: db, Identity: e.ir, Grants: e.gr, Events: bus, Settings: e.st, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	e.au = &countingAuth{Service: svc}
	if err := bus.Subscribe("test.capture", func(_ context.Context, ev events.Event) error {
		e.emitted = append(e.emitted, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if e.cons, err = consumers.New(consumers.Config{DB: db, Settings: e.st, Identity: e.ir, Grants: e.gr, Auth: e.au, Events: bus, Clock: clock}); err != nil {
		t.Fatal(err)
	}
	if e.svc, err = admingrants.New(admingrants.Config{DB: db, Settings: e.st, Identity: e.ir, Grants: e.gr, Auth: e.au, Events: bus,
		Engine: e.eng, RuleFailures: e.fail, Clock: clock}); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *storage.Tx) error {
		p, err := e.ir.Principals.Create(tx, "host_admin", nil, nil)
		e.admin = p.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) tick() { e.now = e.now.Add(time.Second) }

func (e *env) newConsumer(name string) identity.ConsumerAccount {
	e.t.Helper()
	e.tick()
	c, err := e.cons.CreateConsumer(ctx, e.admin, consumers.NewConsumer{Kind: identity.ConsumerApplication, DisplayName: name})
	if err != nil {
		e.t.Fatal(err)
	}
	return c
}

// openModels turns on admin.model_default_allowed, so every model stored from now on is open for
// sharing; models already stored keep their switch.
func (e *env) openModels() {
	e.t.Helper()
	e.set(admingrants.KeyModelDefaultAllowed, "true")
}

func (e *env) set(key, value string) {
	e.t.Helper()
	if err := e.st.Set(ctx, settings.Host, key, "", value); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) count(q string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.Reader().QueryRow(ctx, q, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// auditText returns the action and detail of every audit row, joined.
func (e *env) auditText() string {
	e.t.Helper()
	rows, err := e.db.Reader().Query(ctx, `SELECT action || ' ' || detail FROM aud_event ORDER BY created_at, id`)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			e.t.Fatal(err)
		}
		b.WriteString(s + "\n")
	}
	return b.String()
}

// wantKind fails unless err is a *Error of the kind and, when not empty, the param.
func wantKind(t *testing.T, err error, kind admingrants.ErrorKind, param string) {
	t.Helper()
	var ge *admingrants.Error
	if !errors.As(err, &ge) || ge.Kind != kind || (param != "" && ge.Param != param) {
		t.Fatalf("error = %v (%#v), want kind %d param %q", err, ge, kind, param)
	}
}

// limits is a valid set of grant terms allowing the named models.
func limits(models ...string) admingrants.Limits {
	l := admingrants.Limits{Concurrency: 2, OnContextOverflow: "refuse", AllowedProperties: []string{"background"}}
	for _, m := range models {
		l.Models = append(l.Models, admingrants.ModelLimit{Model: m, ContextLimit: 4096, MaxOutputTokens: 1024})
	}
	return l
}

const (
	bgRule   = `{"format_version":1,"when":{"properties":{"background":true}},"priority":"background","caps":{"max_output_tokens":512}}`
	denyRule = `{"format_version":1,"deny":true}`
	// brokenRule parses as JSON but breaks the rule format; it can only come from before the
	// host checked rules when they were written.
	brokenRule = `{"format_version":1,"priority":"nonsense"}`
)

// downEngine fails every call.
type downEngine struct{}

func (downEngine) ListModels(context.Context) ([]engine.Model, error) {
	return nil, errors.New("connection refused")
}
func (downEngine) Loaded(context.Context) ([]engine.LoadedModel, error) {
	return nil, errors.New("connection refused")
}

// rebuild returns e with a new service over the same database, using e.eng.
func rebuild(t *testing.T, e *env) *env { return rebuildWith(t, e, e.eng) }

// rebuildWith returns e with a new service over the same database and the given engine.
func rebuildWith(t *testing.T, e *env, eng admingrants.Engine) *env {
	t.Helper()
	clock := func() time.Time { return e.now }
	svc, err := admingrants.New(admingrants.Config{DB: e.db, Settings: e.st, Identity: e.ir, Grants: e.gr, Auth: e.au, Events: e.bus,
		Engine: eng, RuleFailures: e.fail, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	cp := *e
	cp.svc = svc
	return &cp
}

// newServiceWithoutCounter is e's service with no RuleFailures.
func newServiceWithoutCounter(t *testing.T, e *env) *admingrants.Service {
	t.Helper()
	svc, err := admingrants.New(admingrants.Config{DB: e.db, Settings: e.st, Identity: e.ir, Grants: e.gr, Auth: e.au, Events: e.bus, Engine: e.eng})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}
