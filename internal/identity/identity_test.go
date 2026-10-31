package identity_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func open(t *testing.T) (*storage.DB, *identity.Repos) {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"), identity.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, identity.NewRepos(nil)
}

func str(s string) *string { return &s }

func TestPrincipalTypeRowIsOneToOne(t *testing.T) {
	db, r := open(t)
	ctx := context.Background()

	// A type row needs a principal.
	err := db.Write(ctx, func(tx *storage.Tx) error {
		_, err := r.Consumers.Create(tx, "no-such-principal", identity.ConsumerIndividual, nil, nil)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("consumer without principal: want FOREIGN KEY error, got %v", err)
	}

	var p identity.Principal
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) {
		if p, err = r.Principals.Create(tx, "consumer", str("friends"), nil); err != nil {
			return err
		}
		_, err = r.Consumers.Create(tx, p.ID, identity.ConsumerIndividual, str("Ada"), str("ada@example.com"))
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// principal_id is UNIQUE: a second consumer row for it is refused.
	err = db.Write(ctx, func(tx *storage.Tx) error {
		_, err := r.Consumers.Create(tx, p.ID, identity.ConsumerApplication, nil, nil)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("second consumer for one principal: want UNIQUE error, got %v", err)
	}

	// The same principal may not also be an admin user twice, and kind is checked.
	if err := db.Write(ctx, func(tx *storage.Tx) error {
		_, err := r.AdminUsers.Create(tx, p.ID, "ada", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err = db.Write(ctx, func(tx *storage.Tx) error {
		_, err := r.AdminUsers.Create(tx, p.ID, "ada2", nil)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("second admin user for one principal: want UNIQUE error, got %v", err)
	}
	err = db.Write(ctx, func(tx *storage.Tx) error {
		p2, err := r.Principals.Create(tx, "consumer", nil, nil)
		if err != nil {
			return err
		}
		_, err = r.Consumers.Create(tx, p2.ID, "robot", nil, nil)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("consumer kind robot: want CHECK error, got %v", err)
	}

	got, err := r.Consumers.GetByPrincipal(ctx, db.Reader(), p.ID)
	if err != nil || got.Kind != identity.ConsumerIndividual || got.Email == nil || *got.Email != "ada@example.com" {
		t.Fatalf("GetByPrincipal = %+v, %v", got, err)
	}
	if _, err := r.Consumers.Get(ctx, db.Reader(), "missing"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("Get missing = %v, want ErrNotFound", err)
	}
}

func TestRestrictOnSoftDeletedParents(t *testing.T) {
	db, r := open(t)
	ctx := context.Background()
	var p identity.Principal
	var c identity.Consumer
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) {
		if p, err = r.Principals.Create(tx, "consumer", nil, nil); err != nil {
			return err
		}
		if c, err = r.Consumers.Create(tx, p.ID, identity.ConsumerApplication, str("App"), nil); err != nil {
			return err
		}
		_, err = r.AccessKeys.Create(tx, identity.NewAccessKey{ConsumerID: c.ID, Prefix: "lsk_abc", SecretHash: "h"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Hard-deleting a principal that has a type row is refused, and so is deleting a
	// consumer that has an access key.
	err := db.Write(ctx, func(tx *storage.Tx) error { return r.Principals.Delete(tx, p.ID) })
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("delete principal with consumer: want FOREIGN KEY error, got %v", err)
	}
	err = db.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.Exec(`DELETE FROM idn_consumer WHERE id = ?`, c.ID)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("delete consumer with key: want FOREIGN KEY error, got %v", err)
	}

	// The supported path is the soft delete: personal fields erased, keys revoked, principal deleted, rows stay.
	if err := db.Write(ctx, func(tx *storage.Tx) error { return r.Consumers.SoftDelete(tx, c.ID) }); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Consumers.Get(ctx, db.Reader(), c.ID)
	if got.DisplayName != nil || got.Email != nil || got.RowVersion != 2 {
		t.Fatalf("after soft delete: %+v", got)
	}
	pp, _ := r.Principals.Get(ctx, db.Reader(), p.ID)
	if pp.Status != identity.StatusDeleted {
		t.Fatalf("principal status = %s", pp.Status)
	}
	keys, _ := r.AccessKeys.ListByConsumer(ctx, db.Reader(), c.ID)
	if len(keys) != 1 || keys[0].RevokedAt == nil {
		t.Fatalf("keys after soft delete: %+v", keys)
	}
	var n int
	if err := db.Reader().QueryRow(ctx, `SELECT count(*) FROM syn_change WHERE dataset = ? AND table_name = 'idn_consumer' AND pk = ?`, identity.ConsumerDataset, c.ID).Scan(&n); err != nil || n != 2 {
		t.Fatalf("captured changes = %d, %v; want 2 (insert, soft delete)", n, err)
	}
}

func TestInviteConsentAndSessions(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"), identity.Module()))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := identity.NewRepos(func() time.Time { return now })
	ctx := context.Background()

	var admin, cp identity.Principal
	var inv identity.Invite
	var c identity.Consumer
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) {
		if admin, err = r.Principals.Create(tx, "host_admin", nil, nil); err != nil {
			return err
		}
		if inv, err = r.Invites.Create(tx, "inv_1", "hash", admin.ID, nil, now.Add(time.Hour)); err != nil {
			return err
		}
		if cp, err = r.Principals.Create(tx, "consumer", nil, nil); err != nil {
			return err
		}
		if c, err = r.Consumers.Create(tx, cp.ID, identity.ConsumerIndividual, nil, nil); err != nil {
			return err
		}
		if err = r.Invites.Redeem(tx, inv.ID, c.ID); err != nil {
			return err
		}
		ref := identity.ConsentRef{PrincipalID: cp.ID, Purpose: "terms", PolicyVersion: "v1"}
		if _, err = r.Consents.Record(tx, ref, nil); err != nil {
			return err
		}
		_, err = r.Sessions.Create(tx, admin.ID, "tokhash", now.Add(time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *storage.Tx) error { return r.Invites.Redeem(tx, inv.ID, c.ID) }); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("redeem twice = %v, want ErrNotFound", err)
	}
	pend, _ := r.Invites.ListPending(ctx, db.Reader(), now)
	if len(pend) != 0 {
		t.Fatalf("pending = %d", len(pend))
	}

	ref := identity.ConsentRef{PrincipalID: cp.ID, Purpose: "terms", PolicyVersion: "v1"}
	cons, err := r.Consents.Find(ctx, db.Reader(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *storage.Tx) error { return r.Consents.Withdraw(tx, cons.ID) }); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Consents.Find(ctx, db.Reader(), ref); !errors.Is(err, identity.ErrNoConsent) {
		t.Fatalf("Find after withdraw = %v, want ErrNoConsent", err)
	}
	if s, err := r.Sessions.GetByTokenHash(ctx, db.Reader(), "tokhash"); err != nil || s.PrincipalID != admin.ID {
		t.Fatalf("session = %+v, %v", s, err)
	}
}
