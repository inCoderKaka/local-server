package identity_test

import (
	"context"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// DeleteByPrincipal removes every credential of one principal, revoked or not, and no one else's.
func TestCredentialDeleteByPrincipal(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	db, r := openAt(t, &now)
	ctx := context.Background()
	a, b := newConsumer(t, db, r, "A"), newConsumer(t, db, r, "B")
	if err := db.Write(ctx, func(tx *storage.Tx) error {
		c, err := r.Credentials.Create(tx, a.PrincipalID, identity.CredentialPassword, "a-login", "hash")
		if err != nil {
			return err
		}
		if _, err := r.Credentials.Create(tx, a.PrincipalID, identity.CredentialPasskey, "a-passkey", "pub"); err != nil {
			return err
		}
		if err := r.Credentials.Revoke(tx, c.ID); err != nil {
			return err
		}
		_, err = r.Credentials.Create(tx, b.PrincipalID, identity.CredentialPassword, "b-login", "hash")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) { n, err = r.Credentials.DeleteByPrincipal(tx, a.PrincipalID); return }); err != nil || n != 2 {
		t.Fatalf("deleted %d, %v; want 2", n, err)
	}
	if got, _ := r.Credentials.ListByPrincipal(ctx, db.Reader(), a.PrincipalID); len(got) != 0 {
		t.Errorf("credentials left: %+v", got)
	}
	if got, _ := r.Credentials.ListByPrincipal(ctx, db.Reader(), b.PrincipalID); len(got) != 1 {
		t.Errorf("another principal's credentials changed: %+v", got)
	}
}

// EraseNotesRedeemedBy clears the notes of the invites one consumer redeemed and leaves the rows
// and every other invite alone.
func TestInviteEraseNotesRedeemedBy(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	db, r := openAt(t, &now)
	ctx := context.Background()
	c := newConsumer(t, db, r, "A")
	var mine, other identity.Invite
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) {
		admin, err := r.Principals.Create(tx, "host_admin", nil, nil)
		if err != nil {
			return err
		}
		note := "for A"
		if mine, err = r.Invites.Create(tx, "p1", "h1", admin.ID, &note, now.Add(time.Hour)); err != nil {
			return err
		}
		if other, err = r.Invites.Create(tx, "p2", "h2", admin.ID, &note, now.Add(time.Hour)); err != nil {
			return err
		}
		return r.Invites.Redeem(tx, mine.ID, c.ID)
	}); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) { n, err = r.Invites.EraseNotesRedeemedBy(tx, c.ID); return }); err != nil || n != 1 {
		t.Fatalf("erased %d, %v; want 1", n, err)
	}
	m, _ := r.Invites.Get(ctx, db.Reader(), mine.ID)
	o, _ := r.Invites.Get(ctx, db.Reader(), other.ID)
	if m.Note != nil || m.RedeemedConsumerID == nil || *m.RedeemedConsumerID != c.ID || m.RedeemedAt == nil {
		t.Errorf("redeemed invite = %+v", m)
	}
	if o.Note == nil {
		t.Error("another invite's note was erased")
	}
}
