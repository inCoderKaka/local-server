package identity_test

import (
	"context"
	"errors"
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

func countRows(t *testing.T, db *storage.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Reader().QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Purge removes one deleted consumer's consents, credentials, sessions and access keys and
// unlinks its invite; it keeps the tombstone, and it leaves another consumer alone (req_001 D-136).
func TestConsumerPurge(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	db, r := openAt(t, &now)
	ctx := context.Background()
	a, b := newConsumer(t, db, r, "A"), newConsumer(t, db, r, "B")
	var invA identity.Invite
	if err := db.Write(ctx, func(tx *storage.Tx) error {
		admin, err := r.Principals.Create(tx, "host_admin", nil, nil)
		if err != nil {
			return err
		}
		for _, c := range []identity.Consumer{a, b} {
			ev := "evidence"
			if _, err := r.Consents.Record(tx, identity.ConsentRef{PrincipalID: c.PrincipalID, Purpose: "terms", PolicyVersion: "v1"}, &ev); err != nil {
				return err
			}
			if _, err := r.Credentials.Create(tx, c.PrincipalID, identity.CredentialPassword, "login-"+c.ID, "hash"); err != nil {
				return err
			}
			if _, err := r.Sessions.Create(tx, c.PrincipalID, "tok-"+c.ID, now.Add(time.Hour)); err != nil {
				return err
			}
			for _, p := range []string{"k1", "k2"} {
				if _, err := r.AccessKeys.Create(tx, identity.NewAccessKey{ConsumerID: c.ID, Prefix: p + "-" + c.ID, SecretHash: "h"}); err != nil {
					return err
				}
			}
		}
		if invA, err = r.Invites.Create(tx, "pa", "ha", admin.ID, nil, now.Add(time.Hour)); err != nil {
			return err
		}
		return r.Invites.Redeem(tx, invA.ID, a.ID)
	}); err != nil {
		t.Fatal(err)
	}

	// A principal that is not deleted is refused and nothing changes.
	err := db.Write(ctx, func(tx *storage.Tx) error { _, err := r.Consumers.Purge(tx, a.ID); return err })
	if err == nil {
		t.Fatal("purged a consumer whose principal is active")
	}
	if n := countRows(t, db, `SELECT count(*) FROM idn_consent WHERE principal_id = ?`, a.PrincipalID); n != 1 {
		t.Fatalf("refused purge changed consents: %d", n)
	}
	if err := db.Write(ctx, func(tx *storage.Tx) error { _, err := r.Consumers.Purge(tx, "nope"); return err }); !errors.Is(err, identity.ErrNotFound) {
		t.Errorf("unknown consumer: %v, want ErrNotFound", err)
	}

	now = now.Add(time.Minute)
	if err := db.Write(ctx, func(tx *storage.Tx) error { return r.Consumers.SoftDelete(tx, a.ID) }); err != nil {
		t.Fatal(err)
	}
	cutoff := now.Add(time.Second)
	if got, err := r.Consumers.ListPurgeable(ctx, db.Reader(), now, "", 10); err != nil || len(got) != 0 {
		t.Errorf("listed %v, %v with the cutoff at the deletion time; want none (strictly older)", got, err)
	}
	got, err := r.Consumers.ListPurgeable(ctx, db.Reader(), cutoff, "", 10)
	if err != nil || len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("listed %v, %v; want only A (B is active)", got, err)
	}
	if got, _ := r.Consumers.ListPurgeable(ctx, db.Reader(), cutoff, a.ID, 10); len(got) != 0 {
		t.Errorf("listed %v after A", got)
	}

	var n identity.PurgeCounts
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) { n, err = r.Consumers.Purge(tx, a.ID); return }); err != nil {
		t.Fatal(err)
	}
	if want := (identity.PurgeCounts{Consents: 1, Credentials: 1, Sessions: 1, AccessKeys: 2, InvitesUnlinked: 1}); n != want {
		t.Errorf("counts = %+v, want %+v", n, want)
	}
	for _, q := range []string{
		`SELECT count(*) FROM idn_consent WHERE principal_id = ?`,
		`SELECT count(*) FROM idn_credential WHERE principal_id = ?`,
		`SELECT count(*) FROM idn_session WHERE principal_id = ?`,
	} {
		if c := countRows(t, db, q, a.PrincipalID); c != 0 {
			t.Errorf("%s left %d", q, c)
		}
	}
	if c := countRows(t, db, `SELECT count(*) FROM idn_access_key WHERE consumer_id = ?`, a.ID); c != 0 {
		t.Errorf("%d access keys left", c)
	}
	if c := countRows(t, db, `SELECT count(*) FROM idn_invite WHERE id = ? AND redeemed_consumer_id IS NULL AND redeemed_at IS NOT NULL`, invA.ID); c != 1 {
		t.Error("invite not unlinked, or its redeemed_at was cleared")
	}
	if c := countRows(t, db, `SELECT count(*) FROM idn_consumer c JOIN idn_principal p ON p.id = c.principal_id WHERE c.id = ? AND p.status = 'deleted'`, a.ID); c != 1 {
		t.Error("tombstone gone")
	}
	for _, q := range []string{
		`SELECT count(*) FROM idn_consent WHERE principal_id = ?`,
		`SELECT count(*) FROM idn_credential WHERE principal_id = ?`,
		`SELECT count(*) FROM idn_session WHERE principal_id = ?`,
	} {
		if c := countRows(t, db, q, b.PrincipalID); c != 1 {
			t.Errorf("B: %s = %d, want 1", q, c)
		}
	}
	if c := countRows(t, db, `SELECT count(*) FROM idn_access_key WHERE consumer_id = ?`, b.ID); c != 2 {
		t.Errorf("B has %d access keys, want 2", c)
	}
	// Purged: no longer listed, so a second pass finds nothing.
	if got, _ := r.Consumers.ListPurgeable(ctx, db.Reader(), cutoff, "", 10); len(got) != 0 {
		t.Errorf("a purged consumer is listed again: %v", got)
	}
	var again identity.PurgeCounts
	if err := db.Write(ctx, func(tx *storage.Tx) (err error) { again, err = r.Consumers.Purge(tx, a.ID); return }); err != nil || again.Total() != 0 {
		t.Errorf("second purge = %+v, %v; want nothing", again, err)
	}
}
