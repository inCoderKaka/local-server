package consumers_test

import (
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func TestCreateListRevokeInvite(t *testing.T) {
	e := newEnv(t)
	e.set(consumers.KeyInviteLinkBaseURL, "https://my-host.example.ts.net/")
	e.tick()
	is, err := e.svc.CreateInvite(ctx, e.admin, consumers.NewInvite{Note: str("for Ada")})
	if err != nil {
		t.Fatal(err)
	}
	token := is.Token.Reveal()
	p, err := auth.ParseInviteToken(token)
	if err != nil {
		t.Fatalf("token %q: %v", token, err)
	}
	if want := "https://my-host.example.ts.net/invite#" + token; is.Link.Reveal() != want {
		t.Fatalf("link = %q, want %q", is.Link.Reveal(), want)
	}
	if is.Token.String() == token || is.Link.String() == is.Link.Reveal() {
		t.Error("token or link prints in the clear")
	}
	inv := is.Invite
	if inv.Status != consumers.InvitePending || *inv.Note != "for Ada" || !inv.CreatedAt.Equal(e.now) || inv.RedeemedConsumerID != nil {
		t.Fatalf("invite = %+v", inv)
	}
	// The default expiry is the invites.ttl setting (24h), read at creation.
	if want := e.now.Add(24 * time.Hour); !inv.ExpiresAt.Equal(want) {
		t.Fatalf("expires %v, want %v", inv.ExpiresAt, want)
	}
	// Only a hash is stored; the token cannot be read back.
	var stored, prefix string
	if err := e.db.Reader().QueryRow(ctx, `SELECT token_hash, prefix FROM idn_invite WHERE id = ?`, inv.ID).Scan(&stored, &prefix); err != nil {
		t.Fatal(err)
	}
	if prefix != p.Prefix || !auth.InviteSecretMatches(p.Secret.Reveal(), stored) || strings.Contains(stored, p.Secret.Reveal()) {
		t.Errorf("stored invite: prefix %q hash %q", prefix, stored)
	}

	// A changed TTL applies to the next invite only; an empty base gives a relative link.
	e.set(consumers.KeyInviteTTL, "1h")
	e.set(consumers.KeyInviteLinkBaseURL, "")
	e.tick()
	is2, err := e.svc.CreateInvite(ctx, e.admin, consumers.NewInvite{})
	if err != nil {
		t.Fatal(err)
	}
	if want := e.now.Add(time.Hour); !is2.Invite.ExpiresAt.Equal(want) {
		t.Fatalf("second invite expires %v, want %v", is2.Invite.ExpiresAt, want)
	}
	if is2.Invite.Note != nil || is2.Link.Reveal() != "/invite#"+is2.Token.Reveal() {
		t.Fatalf("second invite = %+v link %q", is2.Invite, is2.Link.Reveal())
	}
	if is.Invite.ExpiresAt.Sub(is.Invite.CreatedAt) != 24*time.Hour {
		t.Error("the first invite changed with the setting")
	}
	// A given expiry wins, and must be in the future.
	at := e.now.Add(5 * time.Minute)
	e.tick()
	is3, err := e.svc.CreateInvite(ctx, e.admin, consumers.NewInvite{ExpiresAt: &at})
	if err != nil || !is3.Invite.ExpiresAt.Equal(at) {
		t.Fatalf("given expiry: %+v, %v", is3.Invite, err)
	}
	past := e.now.Add(-time.Second)
	if _, err := e.svc.CreateInvite(ctx, e.admin, consumers.NewInvite{ExpiresAt: &past}); err == nil {
		t.Error("past expiry accepted")
	} else {
		wantKind(t, err, consumers.KindInvalid, "expires_at")
	}
	if _, err := e.svc.CreateInvite(ctx, "", consumers.NewInvite{}); err == nil {
		t.Error("invite without an admin accepted")
	}

	// The list: newest first, states derived, no token anywhere in it.
	e.now = e.now.Add(10 * time.Minute) // the third invite has expired
	page, err := e.svc.ListInvites(ctx, consumers.ListQuery{})
	if err != nil || len(page.Items) != 3 || page.NextCursor != "" {
		t.Fatalf("list = %+v, %v", page, err)
	}
	if page.Items[0].ID != is3.Invite.ID || page.Items[0].Status != consumers.InviteExpired || page.Items[1].ID != is2.Invite.ID || page.Items[2].ID != inv.ID {
		t.Fatalf("list order or states = %+v", page.Items)
	}
	one := 1
	p1, err := e.svc.ListInvites(ctx, consumers.ListQuery{Limit: &one})
	if err != nil || len(p1.Items) != 1 || p1.NextCursor == "" {
		t.Fatalf("page 1 = %+v, %v", p1, err)
	}
	p2, err := e.svc.ListInvites(ctx, consumers.ListQuery{Limit: &one, Cursor: p1.NextCursor})
	if err != nil || len(p2.Items) != 1 || p2.Items[0].ID != is2.Invite.ID {
		t.Fatalf("page 2 = %+v, %v", p2, err)
	}
	if _, err := e.svc.ListInvites(ctx, consumers.ListQuery{Cursor: "garbage"}); err == nil {
		t.Error("bad cursor accepted")
	} else {
		wantKind(t, err, consumers.KindInvalid, "cursor")
	}
	if text := e.auditText(); strings.Contains(text, token) || strings.Contains(text, p.Secret.Reveal()) {
		t.Error("an audit row holds the token")
	}

	// Revoke: a pending invite ends; any other state is a conflict.
	r, err := e.svc.RevokeInvite(ctx, e.admin, inv.ID)
	if err != nil || r.Status != consumers.InviteRevoked || *r.Note != "for Ada" {
		t.Fatalf("revoke = %+v, %v", r, err)
	}
	for name, id := range map[string]string{"revoked": inv.ID, "expired": is3.Invite.ID} {
		if _, err := e.svc.RevokeInvite(ctx, e.admin, id); err == nil {
			t.Errorf("%s invite revoked", name)
		} else {
			wantKind(t, err, consumers.KindConflict, "")
		}
	}
	// A redeemed invite is final too.
	c := e.newConsumer("Redeemer")
	if err := e.db.Write(ctx, func(tx *storage.Tx) error { return e.ir.Invites.Redeem(tx, is2.Invite.ID, c.ID) }); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RevokeInvite(ctx, e.admin, is2.Invite.ID); err == nil {
		t.Error("redeemed invite revoked")
	} else {
		wantKind(t, err, consumers.KindConflict, "")
	}
	if l, _ := e.svc.ListInvites(ctx, consumers.ListQuery{}); l.Items[1].Status != consumers.InviteRedeemed || *l.Items[1].RedeemedConsumerID != c.ID {
		t.Errorf("redeemed invite = %+v", l.Items[1])
	}
	if _, err := e.svc.RevokeInvite(ctx, e.admin, "no-such-invite"); err == nil {
		t.Error("unknown invite revoked")
	} else {
		wantKind(t, err, consumers.KindNotFound, "")
	}
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = ?`, consumers.ActionInviteRevoked); n != 1 {
		t.Errorf("%d invite.revoked rows, want 1", n)
	}
	// A revoked invite cannot be redeemed.
	if err := e.db.Write(ctx, func(tx *storage.Tx) error { return e.ir.Invites.Redeem(tx, inv.ID, c.ID) }); err == nil {
		t.Error("revoked invite redeemed")
	}
}
