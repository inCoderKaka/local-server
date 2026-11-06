package identity_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// openAt opens a database with a settable clock for the identity repositories.
func openAt(t *testing.T, now *time.Time) (*storage.DB, *identity.Repos) {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"), identity.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, identity.NewRepos(func() time.Time { return *now })
}

func newConsumer(t *testing.T, db *storage.DB, r *identity.Repos, name string) identity.Consumer {
	t.Helper()
	var c identity.Consumer
	if err := db.Write(context.Background(), func(tx *storage.Tx) error {
		p, err := r.Principals.Create(tx, "consumer", nil, nil)
		if err != nil {
			return err
		}
		c, err = r.Consumers.Create(tx, p.ID, identity.ConsumerIndividual, &name, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConsumerAccountLogin(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	db, r := openAt(t, &now)
	ctx := context.Background()
	c := newConsumer(t, db, r, "Ada")

	a, err := r.Consumers.GetAccount(ctx, db.Reader(), c.ID)
	if err != nil || a.Status != identity.StatusActive || a.Username != nil || a.HasLogin || a.ID != c.ID || *a.DisplayName != "Ada" {
		t.Fatalf("key-only account = %+v, %v", a, err)
	}

	// A passkey is a login but gives no username; a password gives both.
	var pw identity.Credential
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) {
		if _, err = r.Credentials.Create(tx, c.PrincipalID, identity.CredentialPasskey, "pk-1", "pub"); err != nil {
			return err
		}
		pw, err = r.Credentials.Create(tx, c.PrincipalID, identity.CredentialPassword, "ada", "hash")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	a, err = r.Consumers.GetAccount(ctx, db.Reader(), c.ID)
	if err != nil || !a.HasLogin || a.Username == nil || *a.Username != "ada" {
		t.Fatalf("account with password = %+v, %v", a, err)
	}

	// Revoking the password leaves the passkey: still a login, no username.
	if err := db.Write(ctx, func(tx *storage.Tx) error { return r.Credentials.Revoke(tx, pw.ID) }); err != nil {
		t.Fatal(err)
	}
	a, _ = r.Consumers.GetAccount(ctx, db.Reader(), c.ID)
	if !a.HasLogin || a.Username != nil {
		t.Fatalf("account after password revoke = %+v", a)
	}
	var viaTx identity.ConsumerAccount
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) { viaTx, err = r.Consumers.GetAccountTx(tx, c.ID); return err }); err != nil || viaTx.HasLogin != a.HasLogin {
		t.Fatalf("GetAccountTx = %+v, %v", viaTx, err)
	}

	// Deleting the consumer records when, on the principal.
	now = now.Add(time.Hour)
	if err := db.Write(ctx, func(tx *storage.Tx) error { return r.Consumers.SoftDelete(tx, c.ID) }); err != nil {
		t.Fatal(err)
	}
	a, _ = r.Consumers.GetAccount(ctx, db.Reader(), c.ID)
	if a.Status != identity.StatusDeleted || !a.StatusChangedAt.Equal(now) || a.DisplayName != nil {
		t.Fatalf("deleted account = %+v", a)
	}
	if _, err := r.Consumers.GetAccount(ctx, db.Reader(), "missing"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("missing account = %v", err)
	}
	if err := db.Write(ctx, func(tx *storage.Tx) error { _, err := r.Consumers.GetAccountTx(tx, "missing"); return err }); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("missing account in a unit = %v", err)
	}
}

func TestListAccountsPagesNewestFirst(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	db, r := openAt(t, &now)
	ctx := context.Background()
	// Five consumers; the 3rd and 4th are created in the same millisecond, so the id breaks the tie.
	var ids []string
	for i, name := range []string{"a", "b", "c", "d", "e"} {
		if i != 3 {
			now = now.Add(time.Second)
		}
		ids = append(ids, newConsumer(t, db, r, name).ID)
	}
	var suspended identity.Consumer
	if err := db.Write(ctx, func(tx *storage.Tx) error {
		a, err := r.Consumers.GetAccountTx(tx, ids[1])
		suspended = a.Consumer
		if err != nil {
			return err
		}
		return r.Principals.SetStatus(tx, a.PrincipalID, identity.StatusSuspended)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Write(ctx, func(tx *storage.Tx) error { return r.Consumers.SoftDelete(tx, ids[0]) }); err != nil {
		t.Fatal(err)
	}

	var got []string
	var after *identity.PageKey
	for {
		page, err := r.Consumers.ListAccounts(ctx, db.Reader(), "", after, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range page {
			got = append(got, a.ID)
			after = &identity.PageKey{CreatedAt: a.CreatedAt, ID: a.ID}
		}
		if len(page) < 2 {
			break
		}
	}
	// Newest first, the deleted one left out: e, d, c, b.
	want := []string{ids[4], ids[3], ids[2], ids[1]}
	if len(got) != len(want) {
		t.Fatalf("listed %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("listed %v, want %v", got, want)
		}
	}

	only, err := r.Consumers.ListAccounts(ctx, db.Reader(), identity.StatusSuspended, nil, 10)
	if err != nil || len(only) != 1 || only[0].ID != suspended.ID || only[0].Status != identity.StatusSuspended {
		t.Fatalf("suspended = %+v, %v", only, err)
	}
	del, err := r.Consumers.ListAccounts(ctx, db.Reader(), identity.StatusDeleted, nil, 10)
	if err != nil || len(del) != 1 || del[0].ID != ids[0] {
		t.Fatalf("deleted = %+v, %v", del, err)
	}
	// The status filter and the position combine.
	none, err := r.Consumers.ListAccounts(ctx, db.Reader(), identity.StatusSuspended, &identity.PageKey{CreatedAt: suspended.CreatedAt, ID: suspended.ID}, 10)
	if err != nil || len(none) != 0 {
		t.Fatalf("after the only suspended consumer: %+v, %v", none, err)
	}
}

func TestInviteGetTxAndListPage(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	db, r := openAt(t, &now)
	ctx := context.Background()
	var admin identity.Principal
	var ids []string
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) {
		if admin, err = r.Principals.Create(tx, "host_admin", nil, nil); err != nil {
			return err
		}
		for _, p := range []string{"p1", "p2", "p3"} {
			now = now.Add(time.Second)
			inv, err := r.Invites.Create(tx, p, "h-"+p, admin.ID, nil, now.Add(time.Hour))
			if err != nil {
				return err
			}
			ids = append(ids, inv.ID)
		}
		got, err := r.Invites.GetTx(tx, ids[0])
		if err != nil || got.Prefix != "p1" {
			t.Errorf("GetTx = %+v, %v", got, err)
		}
		_, err = r.Invites.GetTx(tx, "missing")
		if !errors.Is(err, identity.ErrNotFound) {
			t.Errorf("GetTx missing = %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	page, err := r.Invites.ListPage(ctx, db.Reader(), nil, 2)
	if err != nil || len(page) != 2 || page[0].ID != ids[2] || page[1].ID != ids[1] {
		t.Fatalf("first page = %+v, %v", page, err)
	}
	rest, err := r.Invites.ListPage(ctx, db.Reader(), &identity.PageKey{CreatedAt: page[1].CreatedAt, ID: page[1].ID}, 2)
	if err != nil || len(rest) != 1 || rest[0].ID != ids[0] {
		t.Fatalf("second page = %+v, %v", rest, err)
	}
}

func TestListByOwnerTx(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	db, r := openAt(t, &now)
	ctx := context.Background()
	c := newConsumer(t, db, r, "Ada")
	if err := db.Write(ctx, func(tx *storage.Tx) error {
		if _, err := r.AccessKeys.Create(tx, identity.NewAccessKey{ConsumerID: c.ID, Prefix: "k1", SecretHash: "h"}); err != nil {
			return err
		}
		cred, err := r.Credentials.Create(tx, c.PrincipalID, identity.CredentialPassword, "ada", "hash")
		if err != nil {
			return err
		}
		if _, err := r.Sessions.Create(tx, c.PrincipalID, "tok", now.Add(time.Hour)); err != nil {
			return err
		}
		// A unit sees its own earlier writes.
		keys, err := r.AccessKeys.ListByConsumerTx(tx, c.ID)
		if err != nil || len(keys) != 1 || keys[0].Prefix != "k1" {
			t.Errorf("keys = %+v, %v", keys, err)
		}
		creds, err := r.Credentials.ListByPrincipalTx(tx, c.PrincipalID)
		if err != nil || len(creds) != 1 || creds[0].ID != cred.ID {
			t.Errorf("credentials = %+v, %v", creds, err)
		}
		sess, err := r.Sessions.ListByPrincipalTx(tx, c.PrincipalID)
		if err != nil || len(sess) != 1 || sess[0].TokenHash != "tok" {
			t.Errorf("sessions = %+v, %v", sess, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
