package requests_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/requests"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

var t0 = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

// person is a consumer or an admin of the test: the id of its consumer row (consumers only) and
// of its principal.
type person struct{ consumerID, principalID string }

type env struct {
	t   *testing.T
	db  *storage.DB
	st  *settings.Store
	ir  *identity.Repos
	gr  *grants.Repos
	bus *events.Bus
	clk *clock
	log *lockedBuf
	svc *requests.Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"),
		settings.Module(), identity.Module(), grants.Module(), requests.Module(), audit.Module(), events.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{t: t, db: db, st: settings.New(db), clk: &clock{t: t0}, log: &lockedBuf{}}
	e.ir, e.gr = identity.NewRepos(e.clk.now), grants.NewRepos(e.clk.now)
	e.bus = events.New()
	e.svc, err = requests.New(requests.Config{DB: db, Settings: e.st, Identity: e.ir, Events: e.bus, Clock: e.clk.now,
		Logger: slog.New(slog.NewTextHandler(e.log, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) consumer(name string) person {
	e.t.Helper()
	var p person
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		pr, err := e.ir.Principals.Create(tx, "consumer", nil, nil)
		if err != nil {
			return err
		}
		c, err := e.ir.Consumers.Create(tx, pr.ID, identity.ConsumerApplication, &name, nil)
		p = person{consumerID: c.ID, principalID: pr.ID}
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *env) admin(name string) person {
	e.t.Helper()
	var p person
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		pr, err := e.ir.Principals.Create(tx, "host_admin", nil, nil)
		if err != nil {
			return err
		}
		_, err = e.ir.AdminUsers.Create(tx, pr.ID, name, &name)
		p = person{principalID: pr.ID}
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *env) set(key, value string) {
	e.t.Helper()
	if err := e.st.Set(context.Background(), settings.Host, key, "", value); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) count(q string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.Reader().QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) create(p person, message string) requests.Request {
	e.t.Helper()
	q, err := e.svc.Create(context.Background(), p.consumerID, p.principalID, requests.NewRequest{Message: message})
	if err != nil {
		e.t.Fatal(err)
	}
	return q
}

// applyGrant is a stand-in for the admin module's grant write: a grant and a version, written in
// the unit of work of the approval.
func (e *env) applyGrant(tx *storage.Tx, consumerID, reason string) (string, error) {
	g, err := e.gr.Grants.Create(tx, consumerID)
	if err != nil {
		return "", err
	}
	one := int64(1)
	v, err := e.gr.Versions.Create(tx, g.ID, grants.NewVersion{MaxConcurrency: &one, Reason: &reason})
	return v.ID, err
}

func kindOf(err error) requests.ErrorKind {
	var re *requests.Error
	if errors.As(err, &re) {
		return re.Kind
	}
	return 0
}

func paramOf(err error) string {
	var re *requests.Error
	if errors.As(err, &re) {
		return re.Param
	}
	return ""
}

func mustNot(t *testing.T, text, where string) {
	t.Helper()
	if strings.Contains(text, hostile) {
		t.Errorf("the consumer's text reached %s", where)
	}
}

// hostile is a text no stored fact, log line, audit row, event or sync change may repeat.
const hostile = "ZZsecret-text-7731"
