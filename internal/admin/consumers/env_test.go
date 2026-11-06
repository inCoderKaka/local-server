package consumers_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/usage"
)

var ctx = context.Background()

func i64(v int64) *int64   { return &v }
func str(v string) *string { return &v }

// env is a real SQLite database with the identity, grants, audit, events and usage tables, the
// auth service and the consumers service, all on one settable clock.
type env struct {
	t     *testing.T
	db    *storage.DB
	st    *settings.Store
	ir    *identity.Repos
	gr    *grants.Repos
	au    *auth.Service
	svc   *consumers.Service
	now   time.Time
	admin string // the principal id of a host admin, the actor of every write
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"),
		settings.Module(), identity.Module(), grants.Module(), audit.Module(), events.Module(), usage.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{t: t, db: db, st: settings.New(db), now: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)}
	clock := func() time.Time { return e.now }
	e.ir, e.gr = identity.NewRepos(clock), grants.NewRepos(clock)
	bus := events.New()
	if e.au, err = auth.New(auth.Config{DB: db, Identity: e.ir, Grants: e.gr, Events: bus, Settings: e.st, Clock: clock}); err != nil {
		t.Fatal(err)
	}
	if e.svc, err = consumers.New(consumers.Config{DB: db, Settings: e.st, Identity: e.ir, Grants: e.gr, Auth: e.au, Events: bus, Clock: clock}); err != nil {
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

// tick moves the clock on, so rows get distinct creation times.
func (e *env) tick() { e.now = e.now.Add(time.Second) }

func (e *env) newConsumer(name string) identity.ConsumerAccount {
	e.t.Helper()
	e.tick()
	c, err := e.svc.CreateConsumer(ctx, e.admin, consumers.NewConsumer{Kind: identity.ConsumerApplication, DisplayName: name})
	if err != nil {
		e.t.Fatal(err)
	}
	return c
}

// grant gives a consumer a grant with one current version allowing the named models.
func (e *env) grant(consumerID string, v grants.NewVersion, models ...string) {
	e.t.Helper()
	if err := e.db.Write(ctx, func(tx *storage.Tx) error {
		g, err := e.gr.Grants.Create(tx, consumerID)
		if err != nil {
			return err
		}
		for _, name := range models {
			m, err := e.gr.Models.GetByName(ctx, e.db.Reader(), name)
			if errors.Is(err, grants.ErrNotFound) {
				m, err = e.gr.Models.Create(tx, name)
			}
			if err != nil {
				return err
			}
			v.Models = append(v.Models, grants.VersionModel{ModelID: m.ID})
		}
		ver, err := e.gr.Versions.Create(tx, g.ID, v)
		if err != nil {
			return err
		}
		return e.gr.Grants.SetCurrentVersion(tx, g.ID, ver.ID)
	}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) model(name string) {
	e.t.Helper()
	if err := e.db.Write(ctx, func(tx *storage.Tx) error { _, err := e.gr.Models.Create(tx, name); return err }); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) issue(consumerID string, in consumers.NewKey) consumers.IssuedKey {
	e.t.Helper()
	is, err := e.svc.IssueAccessKey(ctx, e.admin, consumerID, in)
	if err != nil {
		e.t.Fatal(err)
	}
	return is
}

func (e *env) authenticate(secret string) error {
	_, err := e.au.Authenticate(ctx, "Bearer "+secret, "100.64.0.1:1")
	return err
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

// auditText returns the detail text of every audit row, joined.
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

// wantKind fails unless err is a *consumers.Error of the kind (and param, when not empty).
func wantKind(t *testing.T, err error, kind consumers.ErrorKind, param string) {
	t.Helper()
	var ce *consumers.Error
	if !errors.As(err, &ce) || ce.Kind != kind || (param != "" && ce.Param != param) {
		t.Fatalf("error = %v (%#v), want kind %d param %q", err, ce, kind, param)
	}
}

// grantsVersion is a grant version with no limits of its own.
func grantsVersion() grants.NewVersion { return grants.NewVersion{} }
