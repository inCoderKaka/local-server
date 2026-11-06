package consumers_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func TestCreateGetUpdate(t *testing.T) {
	e := newEnv(t)
	e.tick()
	c, err := e.svc.CreateConsumer(ctx, e.admin, consumers.NewConsumer{Kind: identity.ConsumerIndividual, DisplayName: "  Ada Lovelace ", Email: str("ada@example.invalid")})
	if err != nil {
		t.Fatal(err)
	}
	if *c.DisplayName != "Ada Lovelace" || *c.Email != "ada@example.invalid" || c.Status != identity.StatusActive || c.HasLogin || c.Username != nil || c.Kind != identity.ConsumerIndividual {
		t.Fatalf("created = %+v", c)
	}
	got, err := e.svc.GetConsumer(ctx, c.ID)
	if err != nil || got.ID != c.ID || *got.DisplayName != "Ada Lovelace" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := e.svc.GetConsumer(ctx, "no-such-consumer"); err == nil {
		t.Fatal("unknown consumer found")
	} else {
		wantKind(t, err, consumers.KindNotFound, "")
	}

	// A change of one field leaves the other; null removes the email.
	e.tick()
	u, err := e.svc.UpdateConsumer(ctx, e.admin, c.ID, consumers.ConsumerChange{DisplayName: consumers.Value("Ada B")})
	if err != nil || *u.DisplayName != "Ada B" || *u.Email != "ada@example.invalid" || u.RowVersion != c.RowVersion+1 {
		t.Fatalf("update name = %+v, %v", u, err)
	}
	u, err = e.svc.UpdateConsumer(ctx, e.admin, c.ID, consumers.ConsumerChange{Email: consumers.Null[string]()})
	if err != nil || u.Email != nil || *u.DisplayName != "Ada B" {
		t.Fatalf("update email = %+v, %v", u, err)
	}
	// Nothing changed: nothing written.
	before := e.count(`SELECT count(*) FROM aud_event WHERE action = ?`, consumers.ActionConsumerUpdated)
	u2, err := e.svc.UpdateConsumer(ctx, e.admin, c.ID, consumers.ConsumerChange{DisplayName: consumers.Value("Ada B")})
	if err != nil || u2.RowVersion != u.RowVersion || e.count(`SELECT count(*) FROM aud_event WHERE action = ?`, consumers.ActionConsumerUpdated) != before {
		t.Fatalf("no-op update changed state: %+v, %v", u2, err)
	}

	// Refusals.
	for name, c := range map[string]struct {
		in    consumers.NewConsumer
		param string
	}{
		"bad kind":   {consumers.NewConsumer{Kind: "robot", DisplayName: "X"}, "kind"},
		"blank name": {consumers.NewConsumer{Kind: identity.ConsumerApplication, DisplayName: "   "}, "display_name"},
		"bad email":  {consumers.NewConsumer{Kind: identity.ConsumerApplication, DisplayName: "X", Email: str("not an email")}, "email"},
		"name+email": {consumers.NewConsumer{Kind: identity.ConsumerApplication, DisplayName: "X", Email: str("Ada <ada@example.invalid>")}, "email"},
	} {
		if _, err := e.svc.CreateConsumer(ctx, e.admin, c.in); err == nil {
			t.Errorf("%s: accepted", name)
		} else {
			wantKind(t, err, consumers.KindInvalid, c.param)
		}
	}
	if _, err := e.svc.UpdateConsumer(ctx, e.admin, c.ID, consumers.ConsumerChange{DisplayName: consumers.Null[string]()}); err == nil {
		t.Error("display name removed")
	}
	if _, err := e.svc.UpdateConsumer(ctx, e.admin, "no-such-consumer", consumers.ConsumerChange{DisplayName: consumers.Value("Z")}); err == nil {
		t.Error("unknown consumer updated")
	} else {
		wantKind(t, err, consumers.KindNotFound, "")
	}
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = ?`, consumers.ActionConsumerCreated); n != 1 {
		t.Errorf("%d consumer.created rows, want 1 (refused creations write nothing)", n)
	}
}

func TestListPagingAndStatusFilter(t *testing.T) {
	e := newEnv(t)
	var ids []string // oldest first
	for i := range 5 {
		ids = append(ids, e.newConsumer(fmt.Sprintf("Consumer %d", i)).ID)
	}
	if _, err := e.svc.SuspendConsumer(ctx, e.admin, ids[1]); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteConsumer(ctx, e.admin, ids[0]); err != nil {
		t.Fatal(err)
	}

	list := func(q consumers.ListQuery) (out []string, next string) {
		t.Helper()
		p, err := e.svc.ListConsumers(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range p.Items {
			out = append(out, c.ID)
		}
		return out, p.NextCursor
	}
	two := 2

	// Newest first, deleted left out, in pages that join up.
	all, next := list(consumers.ListQuery{})
	if want := []string{ids[4], ids[3], ids[2], ids[1]}; fmt.Sprint(all) != fmt.Sprint(want) || next != "" {
		t.Fatalf("all = %v next %q, want %v", all, next, want)
	}
	p1, cur := list(consumers.ListQuery{Limit: &two})
	if len(p1) != 2 || cur == "" || p1[0] != ids[4] || p1[1] != ids[3] {
		t.Fatalf("page 1 = %v cursor %q", p1, cur)
	}
	p2, cur2 := list(consumers.ListQuery{Limit: &two, Cursor: cur})
	if len(p2) != 2 || cur2 != "" || p2[0] != ids[2] || p2[1] != ids[1] {
		t.Fatalf("page 2 = %v cursor %q", p2, cur2)
	}
	// A row added after page 1 does not shift page 2.
	newest := e.newConsumer("Newest").ID
	p2b, _ := list(consumers.ListQuery{Limit: &two, Cursor: cur})
	if fmt.Sprint(p2b) != fmt.Sprint(p2) {
		t.Fatalf("page 2 moved after an insert: %v, want %v", p2b, p2)
	}
	if first, _ := list(consumers.ListQuery{Limit: &two}); first[0] != newest {
		t.Fatalf("newest not first: %v", first)
	}

	// Status filter; the deleted ones only when asked for.
	if got, _ := list(consumers.ListQuery{Status: identity.StatusSuspended}); fmt.Sprint(got) != fmt.Sprint([]string{ids[1]}) {
		t.Fatalf("suspended = %v", got)
	}
	if got, _ := list(consumers.ListQuery{Status: identity.StatusDeleted}); fmt.Sprint(got) != fmt.Sprint([]string{ids[0]}) {
		t.Fatalf("deleted = %v", got)
	}
	if got, _ := list(consumers.ListQuery{Status: identity.StatusActive}); len(got) != 4 {
		t.Fatalf("active = %v", got)
	}

	// The page size setting is the default and the cap; a limit below 1 and a bad cursor or status are refused.
	e.set(consumers.KeyListPageSize, "3")
	if got, next := list(consumers.ListQuery{}); len(got) != 3 || next == "" {
		t.Fatalf("default page = %v next %q", got, next)
	}
	ten := 10
	if got, _ := list(consumers.ListQuery{Limit: &ten}); len(got) != 3 {
		t.Fatalf("a limit above the setting returned %d rows", len(got))
	}
	zero := 0
	if _, err := e.svc.ListConsumers(ctx, consumers.ListQuery{Limit: &zero}); err == nil {
		t.Fatal("limit 0 accepted")
	} else {
		wantKind(t, err, consumers.KindInvalid, "limit")
	}
	for _, bad := range []string{"x", "!!!", "MTIzOmFiYw", "bm90LWEtY3Vyc29y"} {
		if _, err := e.svc.ListConsumers(ctx, consumers.ListQuery{Cursor: bad}); err == nil {
			t.Errorf("cursor %q accepted", bad)
		} else {
			wantKind(t, err, consumers.KindInvalid, "cursor")
		}
	}
	if _, err := e.svc.ListConsumers(ctx, consumers.ListQuery{Status: "gone"}); err == nil {
		t.Error("unknown status accepted")
	} else {
		wantKind(t, err, consumers.KindInvalid, "status")
	}
}

// A key authenticates before a suspension, not during it, and again after the reactivation. The
// cache is warm in each step, so only Auth.Invalidate makes the change show at once.
func TestSuspendReactivateInvalidatesAuthCache(t *testing.T) {
	e := newEnv(t)
	c := e.newConsumer("App")
	e.grant(c.ID, grantsVersion(), "m1")
	secret := e.issue(c.ID, consumers.NewKey{}).Secret.Reveal()

	if err := e.authenticate(secret); err != nil {
		t.Fatalf("before suspend: %v", err)
	}
	if err := e.authenticate(secret); err != nil { // warms the cache
		t.Fatal(err)
	}
	s, err := e.svc.SuspendConsumer(ctx, e.admin, c.ID)
	if err != nil || s.Status != identity.StatusSuspended {
		t.Fatalf("suspend = %+v, %v", s, err)
	}
	if err := e.authenticate(secret); !errors.Is(err, auth.ErrConsumerNotActive) {
		t.Fatalf("while suspended: %v, want ErrConsumerNotActive", err)
	}
	// Suspending again changes nothing and audits nothing.
	if _, err := e.svc.SuspendConsumer(ctx, e.admin, c.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = ?`, consumers.ActionConsumerSuspended); n != 1 {
		t.Fatalf("%d consumer.suspended rows, want 1", n)
	}
	r, err := e.svc.ReactivateConsumer(ctx, e.admin, c.ID)
	if err != nil || r.Status != identity.StatusActive {
		t.Fatalf("reactivate = %+v, %v", r, err)
	}
	if err := e.authenticate(secret); err != nil {
		t.Fatalf("after reactivate: %v", err)
	}

	// A deleted consumer is never reactivated, and cannot be suspended or changed.
	if err := e.svc.DeleteConsumer(ctx, e.admin, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ReactivateConsumer(ctx, e.admin, c.ID); err == nil {
		t.Fatal("deleted consumer reactivated")
	} else {
		wantKind(t, err, consumers.KindConflict, "")
	}
	if _, err := e.svc.SuspendConsumer(ctx, e.admin, c.ID); err == nil {
		t.Fatal("deleted consumer suspended")
	} else {
		wantKind(t, err, consumers.KindConflict, "")
	}
	if _, err := e.svc.UpdateConsumer(ctx, e.admin, c.ID, consumers.ConsumerChange{DisplayName: consumers.Value("Back")}); err == nil {
		t.Fatal("deleted consumer changed")
	} else {
		wantKind(t, err, consumers.KindConflict, "")
	}
	if _, err := e.svc.IssueAccessKey(ctx, e.admin, c.ID, consumers.NewKey{}); err == nil {
		t.Fatal("key issued to a deleted consumer")
	} else {
		wantKind(t, err, consumers.KindConflict, "")
	}
	for _, id := range []string{"no-such-consumer"} {
		if _, err := e.svc.SuspendConsumer(ctx, e.admin, id); err == nil {
			t.Error("unknown consumer suspended")
		} else {
			wantKind(t, err, consumers.KindNotFound, "")
		}
	}
}

// Delete: personal fields, login credentials and key names are erased, every key is revoked
// through the auth service, sessions end, the principal is deleted for good, and the usage rows
// stay. No audit row holds a personal string.
func TestDeleteErasesPersonalFieldsRevokesKeysKeepsUsage(t *testing.T) {
	e := newEnv(t)
	e.tick()
	c, err := e.svc.CreateConsumer(ctx, e.admin, consumers.NewConsumer{Kind: identity.ConsumerIndividual, DisplayName: "Zyxwv Quux", Email: str("zyxwv@example.invalid")})
	if err != nil {
		t.Fatal(err)
	}
	e.grant(c.ID, grantsVersion(), "m1")
	k1 := e.issue(c.ID, consumers.NewKey{Name: str("Zyxwv laptop")})
	k2 := e.issue(c.ID, consumers.NewKey{})
	k3 := e.issue(c.ID, consumers.NewKey{Name: str("Zyxwv old")})
	if _, err := e.svc.RevokeAccessKey(ctx, e.admin, k3.Key.ID); err != nil { // already revoked before the delete
		t.Fatal(err)
	}
	if err := e.authenticate(k1.Secret.Reveal()); err != nil { // warm cache
		t.Fatal(err)
	}

	// A login, a session, an invite the consumer redeemed.
	var sessionID, inviteID string
	if err := e.db.Write(ctx, func(tx *storage.Tx) error {
		if _, err := e.ir.Credentials.Create(tx, c.PrincipalID, identity.CredentialPassword, "zyxwv-login", "hash"); err != nil {
			return err
		}
		if _, err := e.ir.Credentials.Create(tx, c.PrincipalID, identity.CredentialPasskey, "zyxwv-passkey", "pub"); err != nil {
			return err
		}
		s, err := e.ir.Sessions.Create(tx, c.PrincipalID, "token-hash", e.now.Add(time.Hour))
		sessionID = s.ID
		if err != nil {
			return err
		}
		inv, err := e.ir.Invites.Create(tx, "pfx", "hash", e.admin, str("for Zyxwv Quux"), e.now.Add(time.Hour))
		inviteID = inv.ID
		if err != nil {
			return err
		}
		return e.ir.Invites.Redeem(tx, inv.ID, c.ID)
	}); err != nil {
		t.Fatal(err)
	}
	a, _ := e.svc.GetConsumer(ctx, c.ID)
	if !a.HasLogin || a.Username == nil || *a.Username != "zyxwv-login" {
		t.Fatalf("login not visible before delete: %+v", a)
	}

	// Usage the consumer made.
	at := e.now.UnixMilli()
	if err := e.db.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.Exec(`INSERT INTO use_record (id, consumer_id, access_key_id, grant_version_id, operation, model, outcome,
prompt_tokens, completion_tokens, total_tokens, latency_ms, started_at, finished_at, receipt_kid, receipt_signature, created_at)
VALUES ('u1', ?, ?, 'gv', 'chat.completions', 'm1', 'completed', 1, 2, 3, 1, ?, ?, 'kid', x'00', ?)`, c.ID, k1.Key.ID, at, at, at)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	e.tick()
	if err := e.svc.DeleteConsumer(ctx, e.admin, c.ID); err != nil {
		t.Fatal(err)
	}

	// The consumer row stays, without its personal fields; the principal is deleted.
	got, err := e.svc.GetConsumer(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != identity.StatusDeleted || got.DisplayName != nil || got.Email != nil || got.HasLogin || got.Username != nil || !got.StatusChangedAt.Equal(e.now.Truncate(time.Millisecond)) {
		t.Fatalf("after delete = %+v", got)
	}
	if n := e.count(`SELECT count(*) FROM idn_credential WHERE principal_id = ?`, c.PrincipalID); n != 0 {
		t.Errorf("%d credentials left", n)
	}
	if n := e.count(`SELECT count(*) FROM idn_session WHERE principal_id = ? AND revoked_at IS NULL`, c.PrincipalID); n != 0 {
		t.Errorf("%d live sessions left", n)
	}
	if n := e.count(`SELECT count(*) FROM idn_session WHERE id = ?`, sessionID); n != 1 {
		t.Error("session row removed; it should stay, revoked")
	}
	if n := e.count(`SELECT count(*) FROM idn_invite WHERE id = ? AND note IS NULL AND redeemed_consumer_id = ?`, inviteID, c.ID); n != 1 {
		t.Error("the note of the redeemed invite was not erased, or the invite row changed")
	}
	if n := e.count(`SELECT count(*) FROM idn_access_key WHERE consumer_id = ? AND revoked_at IS NULL`, c.ID); n != 0 {
		t.Errorf("%d keys not revoked", n)
	}
	if n := e.count(`SELECT count(*) FROM idn_access_key WHERE consumer_id = ? AND label IS NOT NULL`, c.ID); n != 0 {
		t.Errorf("%d key names left", n)
	}
	if n := e.count(`SELECT count(*) FROM idn_access_key WHERE consumer_id = ?`, c.ID); n != 3 {
		t.Errorf("%d key rows, want 3 (kept for metering)", n)
	}
	if n := e.count(`SELECT count(*) FROM use_record WHERE consumer_id = ?`, c.ID); n != 1 {
		t.Errorf("%d usage rows, want 1", n)
	}
	for i, k := range []consumers.IssuedKey{k1, k2, k3} {
		if err := e.authenticate(k.Secret.Reveal()); !errors.Is(err, auth.ErrRevoked) {
			t.Errorf("key %d after delete: %v, want ErrRevoked", i+1, err)
		}
	}
	// Each live key was revoked through the auth service (audit row and event); the key revoked
	// earlier was not revoked twice.
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = ?`, auth.ActionKeyRevoked); n != 3 {
		t.Errorf("%d access_key.revoked rows, want 3 (k3 once, k1 and k2 by the delete)", n)
	}
	if n := e.count(`SELECT count(*) FROM ntf_event WHERE type = 'access_key.revoked'`); n != 3 {
		t.Errorf("%d access_key.revoked events, want 3", n)
	}
	rows := e.auditText()
	if !strings.Contains(rows, consumers.ActionConsumerDeleted+` {"consumer_id":"`+c.ID+`","revoked_keys":2}`) {
		t.Errorf("no consumer.deleted row with the count in:\n%s", rows)
	}
	// Deleting again changes nothing.
	if err := e.svc.DeleteConsumer(ctx, e.admin, c.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = ?`, consumers.ActionConsumerDeleted); n != 1 {
		t.Errorf("%d consumer.deleted rows, want 1", n)
	}
	if err := e.svc.DeleteConsumer(ctx, e.admin, "no-such-consumer"); err == nil {
		t.Error("unknown consumer deleted")
	} else {
		wantKind(t, err, consumers.KindNotFound, "")
	}
	// The deleted consumer is listed only when asked for.
	if p, _ := e.svc.ListConsumers(ctx, consumers.ListQuery{}); len(p.Items) != 0 {
		t.Errorf("deleted consumer listed by default: %+v", p.Items)
	}
	if p, _ := e.svc.ListConsumers(ctx, consumers.ListQuery{Status: identity.StatusDeleted}); len(p.Items) != 1 {
		t.Errorf("deleted consumer not listed by status: %+v", p.Items)
	}
}

// The audit trail is kept forever, so no row of any operation holds a display name, an email, a
// key name, an invite note, a key or a token.
func TestAuditRowsHoldNoPersonalData(t *testing.T) {
	e := newEnv(t)
	e.tick()
	c, err := e.svc.CreateConsumer(ctx, e.admin, consumers.NewConsumer{Kind: identity.ConsumerIndividual, DisplayName: "Mmqzv Person", Email: str("mmqzv@example.invalid")})
	if err != nil {
		t.Fatal(err)
	}
	e.grant(c.ID, grantsVersion(), "m1")
	if _, err := e.svc.UpdateConsumer(ctx, e.admin, c.ID, consumers.ConsumerChange{DisplayName: consumers.Value("Nnqzv Renamed"), Email: consumers.Value("nnqzv@example.invalid")}); err != nil {
		t.Fatal(err)
	}
	k := e.issue(c.ID, consumers.NewKey{Name: str("Mmqzv key name"), BoundNode: str("node-1"),
		Narrowing: &consumers.Narrowing{Models: []string{"m1"}, Concurrency: i64(1)}})
	if _, err := e.svc.UpdateAccessKey(ctx, e.admin, k.Key.ID, consumers.KeyChange{Name: consumers.Value("Mmqzv renamed key")}); err != nil {
		t.Fatal(err)
	}
	inv, err := e.svc.CreateInvite(ctx, e.admin, consumers.NewInvite{Note: str("Mmqzv invite note")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RevokeInvite(ctx, e.admin, inv.Invite.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SuspendConsumer(ctx, e.admin, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ReactivateConsumer(ctx, e.admin, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RevokeAccessKey(ctx, e.admin, k.Key.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteConsumer(ctx, e.admin, c.ID); err != nil {
		t.Fatal(err)
	}
	text := e.auditText()
	for _, secret := range []string{"Mmqzv", "Nnqzv", "mmqzv", "nnqzv", "example.invalid", k.Secret.Reveal(), inv.Token.Reveal(), strings.SplitN(inv.Token.Reveal(), "_", 4)[3]} {
		if strings.Contains(text, secret) {
			t.Errorf("audit rows hold %q:\n%s", secret, text)
		}
	}
	// And the events table, which the Notifier keeps, holds no secret either.
	rows, err := e.db.Reader().Query(ctx, `SELECT payload FROM ntf_event`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(p, k.Secret.Reveal()) || strings.Contains(p, "Mmqzv") {
			t.Errorf("event payload holds personal data: %s", p)
		}
	}
	for _, action := range []string{consumers.ActionConsumerCreated, consumers.ActionConsumerUpdated, consumers.ActionConsumerSuspended,
		consumers.ActionConsumerReactivated, consumers.ActionConsumerDeleted, consumers.ActionKeyUpdated, consumers.ActionInviteCreated,
		consumers.ActionInviteRevoked, auth.ActionKeyIssued, auth.ActionKeyRevoked} {
		if !strings.Contains(text, action+" ") {
			t.Errorf("no %s audit row", action)
		}
	}
	// Every consumer action names the admin as the actor and the consumer's principal as the target.
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action LIKE 'consumer.%' AND (actor_id <> ? OR target_id <> ?)`, e.admin, c.PrincipalID); n != 0 {
		t.Errorf("%d consumer.* rows with the wrong actor or target", n)
	}
}

func TestSettingsValidators(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		key, value string
		ok         bool
	}{
		{consumers.KeyListPageSize, "10", true},
		{consumers.KeyListPageSize, "0", false},
		{consumers.KeyListPageSize, "-3", false},
		{consumers.KeyInviteTTL, "72h", true},
		{consumers.KeyInviteTTL, "0s", false},
		{consumers.KeyInviteTTL, "-1h", false},
		{consumers.KeyInviteTTL, "soon", false},
		{consumers.KeyInviteLinkBaseURL, "", true},
		{consumers.KeyInviteLinkBaseURL, "https://my-host.example.ts.net", true},
		{consumers.KeyInviteLinkBaseURL, "http://localhost:8486/app", true},
		{consumers.KeyInviteLinkBaseURL, "my-host.example.ts.net", false},
		{consumers.KeyInviteLinkBaseURL, "ftp://my-host.example", false},
		{consumers.KeyInviteLinkBaseURL, "https://user:pw@my-host.example", false},
		{consumers.KeyInviteLinkBaseURL, "https://my-host.example/?a=b", false},
		{consumers.KeyInviteLinkBaseURL, "https://my-host.example/#x", false},
		{consumers.KeyInviteLinkBaseURL, "https://", false},
		{consumers.KeyInviteLinkBaseURL, "https://my host.example", false},
	} {
		err := e.st.Set(ctx, settings.Host, c.key, "", c.value)
		if (err == nil) != c.ok {
			t.Errorf("%s = %q: err %v, want ok=%v", c.key, c.value, err, c.ok)
		}
	}
	for _, d := range consumers.Defs() {
		if d.Owner != settings.Host {
			t.Errorf("%s is owned by %s, want host (req_001 D-54)", d.Key, d.Owner)
		}
	}
}
