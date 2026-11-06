package consumers_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// A key's secret is in the result of Issue and nowhere else: the list shows the prefix, the
// database holds a hash.
func TestIssueShownOnce(t *testing.T) {
	e := newEnv(t)
	c := e.newConsumer("App")
	e.grant(c.ID, grants.NewVersion{}, "m1")

	exp := e.now.Add(48 * time.Hour)
	is := e.issue(c.ID, consumers.NewKey{Name: str("laptop"), ExpiresAt: &exp, BoundNode: str("node-1")})
	secret := is.Secret.Reveal()
	p, err := auth.ParseKey(secret)
	if err != nil || p.Prefix != is.Key.Prefix {
		t.Fatalf("secret %q does not belong to prefix %q: %v", secret, is.Key.Prefix, err)
	}
	if k := is.Key; k.Status != consumers.KeyActive || *k.Name != "laptop" || k.ConsumerID != c.ID || *k.BoundNode != "node-1" || !k.ExpiresAt.Equal(exp) || k.Narrowing != nil {
		t.Fatalf("issued key = %+v", k)
	}
	// The result prints as a placeholder.
	if s := is.Secret.String(); strings.Contains(s, p.Prefix) || s == secret {
		t.Errorf("secret prints as %q", s)
	}

	list, err := e.svc.ListAccessKeys(ctx, c.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list[0].Prefix != is.Key.Prefix || list[0].ID != is.Key.ID {
		t.Fatalf("listed key = %+v", list[0])
	}
	var hash string
	if err := e.db.Reader().QueryRow(ctx, `SELECT secret_hash FROM idn_access_key WHERE id = ?`, is.Key.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == "" || strings.Contains(hash, p.Secret.Reveal()) || hash == secret {
		t.Errorf("stored hash %q is not a hash of the secret", hash)
	}
	if n := e.count(`SELECT count(*) FROM idn_access_key WHERE secret_hash LIKE '%' || ? || '%' OR label LIKE '%' || ? || '%'`, p.Secret.Reveal(), p.Secret.Reveal()); n != 0 {
		t.Error("the secret is stored")
	}
	if err := e.authenticate(secret); !errors.Is(err, auth.ErrNodeMismatch) { // no WhoIs here: a bound key never matches
		t.Fatalf("bound key: %v", err)
	}
	if err := e.authenticate(e.issue(c.ID, consumers.NewKey{}).Secret.Reveal()); err != nil {
		t.Fatalf("issued key does not authenticate: %v", err)
	}
	if _, err := e.svc.ListAccessKeys(ctx, "no-such-consumer"); err == nil {
		t.Error("keys of an unknown consumer listed")
	} else {
		wantKind(t, err, consumers.KindNotFound, "")
	}

	// Refusals write nothing.
	past := e.now.Add(-time.Hour)
	for name, in := range map[string]consumers.NewKey{
		"past expiry": {ExpiresAt: &past},
		"blank name":  {Name: str(" ")},
		"blank node":  {BoundNode: str("")},
	} {
		if _, err := e.svc.IssueAccessKey(ctx, e.admin, c.ID, in); err == nil {
			t.Errorf("%s accepted", name)
		} else {
			wantKind(t, err, consumers.KindInvalid, "")
		}
	}
	if _, err := e.svc.IssueAccessKey(ctx, e.admin, "no-such-consumer", consumers.NewKey{}); err == nil {
		t.Error("key issued to an unknown consumer")
	} else {
		wantKind(t, err, consumers.KindNotFound, "")
	}
	if n := e.count(`SELECT count(*) FROM idn_access_key`); n != 2 {
		t.Errorf("%d keys, want 2 (refused issues write nothing)", n)
	}
}

func TestUpdateAndRevokeKey(t *testing.T) {
	e := newEnv(t)
	c := e.newConsumer("App")
	e.grant(c.ID, grants.NewVersion{}, "m1")
	is := e.issue(c.ID, consumers.NewKey{Name: str("first")})
	secret := is.Secret.Reveal()
	if err := e.authenticate(secret); err != nil { // warm cache
		t.Fatal(err)
	}

	// An update shows at once: expiry set, name cleared, a node binding that nothing can match.
	exp := e.now.Add(time.Hour)
	u, err := e.svc.UpdateAccessKey(ctx, e.admin, is.Key.ID, consumers.KeyChange{ExpiresAt: consumers.Value(exp), Name: consumers.Null[string](), BoundNode: consumers.Value("node-9")})
	if err != nil || u.Name != nil || !u.ExpiresAt.Equal(exp) || *u.BoundNode != "node-9" || u.Status != consumers.KeyActive {
		t.Fatalf("update = %+v, %v", u, err)
	}
	if err := e.authenticate(secret); !errors.Is(err, auth.ErrNodeMismatch) {
		t.Fatalf("bound key authenticated: %v (the cache still held the unbound key?)", err)
	}
	u, err = e.svc.UpdateAccessKey(ctx, e.admin, is.Key.ID, consumers.KeyChange{BoundNode: consumers.Null[string](), ExpiresAt: consumers.Null[time.Time]()})
	if err != nil || u.BoundNode != nil || u.ExpiresAt != nil {
		t.Fatalf("unbind = %+v, %v", u, err)
	}
	if err := e.authenticate(secret); err != nil {
		t.Fatalf("after unbind: %v", err)
	}
	// A no-op update writes nothing.
	before := e.count(`SELECT count(*) FROM aud_event WHERE action = ?`, consumers.ActionKeyUpdated)
	if _, err := e.svc.UpdateAccessKey(ctx, e.admin, is.Key.ID, consumers.KeyChange{}); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = ?`, consumers.ActionKeyUpdated); n != before {
		t.Errorf("%d key.updated rows, want %d", n, before)
	}

	// Unknown key, bad fields.
	if _, err := e.svc.UpdateAccessKey(ctx, e.admin, "no-such-key", consumers.KeyChange{Name: consumers.Value("x")}); err == nil {
		t.Error("unknown key updated")
	} else {
		wantKind(t, err, consumers.KindNotFound, "")
	}
	past := e.now.Add(-time.Minute)
	if _, err := e.svc.UpdateAccessKey(ctx, e.admin, is.Key.ID, consumers.KeyChange{ExpiresAt: consumers.Value(past)}); err == nil {
		t.Error("past expiry accepted")
	} else {
		wantKind(t, err, consumers.KindInvalid, "expires_at")
	}

	// An expired key is final.
	soon := e.now.Add(time.Minute)
	if _, err := e.svc.UpdateAccessKey(ctx, e.admin, is.Key.ID, consumers.KeyChange{ExpiresAt: consumers.Value(soon)}); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(2 * time.Minute)
	if _, err := e.svc.UpdateAccessKey(ctx, e.admin, is.Key.ID, consumers.KeyChange{Name: consumers.Value("again")}); err == nil {
		t.Fatal("expired key updated")
	} else {
		wantKind(t, err, consumers.KindConflict, "")
	}
	if l, _ := e.svc.ListAccessKeys(ctx, c.ID); l[0].Status != consumers.KeyExpired {
		t.Errorf("status = %s, want expired", l[0].Status)
	}

	// Revoke ends a key at once, even one that had expired; revoking again changes nothing.
	r, err := e.svc.RevokeAccessKey(ctx, e.admin, is.Key.ID)
	if err != nil || r.Status != consumers.KeyRevoked || r.RevokedAt == nil {
		t.Fatalf("revoke = %+v, %v", r, err)
	}
	if _, err := e.svc.RevokeAccessKey(ctx, e.admin, is.Key.ID); err != nil {
		t.Fatalf("second revoke: %v", err)
	}
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = ?`, auth.ActionKeyRevoked); n != 1 {
		t.Errorf("%d access_key.revoked rows, want 1", n)
	}
	if _, err := e.svc.UpdateAccessKey(ctx, e.admin, is.Key.ID, consumers.KeyChange{Name: consumers.Value("again")}); err == nil {
		t.Fatal("revoked key updated")
	} else {
		wantKind(t, err, consumers.KindConflict, "")
	}
	if _, err := e.svc.RevokeAccessKey(ctx, e.admin, "no-such-key"); err == nil {
		t.Error("unknown key revoked")
	} else {
		wantKind(t, err, consumers.KindNotFound, "")
	}

	// A live key revoked: it stops authenticating at once.
	k2 := e.issue(c.ID, consumers.NewKey{})
	if err := e.authenticate(k2.Secret.Reveal()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RevokeAccessKey(ctx, e.admin, k2.Key.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.authenticate(k2.Secret.Reveal()); !errors.Is(err, auth.ErrRevoked) {
		t.Fatalf("revoked key: %v", err)
	}
}

// A narrowing can only narrow the grant: models the grant lacks and numbers above the grant's are
// refused, on issue and on update. What it adds shows in the effective limits.
func TestNarrowingOnlyNarrows(t *testing.T) {
	e := newEnv(t)
	c := e.newConsumer("App")
	e.model("m2")
	e.model("other")
	e.grant(c.ID, grants.NewVersion{QuotaTokensPerDay: i64(1000), QuotaRequestsPerDay: i64(50), MaxConcurrency: i64(4)}, "m1", "m2")

	bad := map[string]struct {
		n     consumers.Narrowing
		param string
	}{
		"model not in grant":  {consumers.Narrowing{Models: []string{"other"}}, "narrowing.models"},
		"unknown model":       {consumers.Narrowing{Models: []string{"no-such-model"}}, "narrowing.models"},
		"blank model":         {consumers.Narrowing{Models: []string{""}}, "narrowing.models"},
		"tokens above grant":  {consumers.Narrowing{Quota: &consumers.Quota{Tokens: i64(1001)}}, "narrowing.quota.tokens"},
		"requests above":      {consumers.Narrowing{Quota: &consumers.Quota{Requests: i64(51)}}, "narrowing.quota.requests"},
		"concurrency above":   {consumers.Narrowing{Concurrency: i64(5)}, "narrowing.concurrency"},
		"concurrency zero":    {consumers.Narrowing{Concurrency: i64(0)}, "narrowing.concurrency"},
		"negative quota":      {consumers.Narrowing{Quota: &consumers.Quota{Tokens: i64(-1)}}, "narrowing.quota.tokens"},
		"context limit zero":  {consumers.Narrowing{ContextLimit: i64(0)}, "narrowing.context_limit"},
		"max output negative": {consumers.Narrowing{MaxOutputTokens: i64(-5)}, "narrowing.max_output_tokens"},
	}
	for name, c2 := range bad {
		n := c2.n
		if _, err := e.svc.IssueAccessKey(ctx, e.admin, c.ID, consumers.NewKey{Narrowing: &n}); err == nil {
			t.Errorf("issue: %s accepted", name)
		} else {
			wantKind(t, err, consumers.KindInvalid, c2.param)
		}
	}
	if n := e.count(`SELECT count(*) FROM idn_access_key`); n != 0 {
		t.Fatalf("%d keys after refused issues", n)
	}

	nar := consumers.Narrowing{Models: []string{"m1", "m1"}, Quota: &consumers.Quota{Tokens: i64(100), Requests: i64(0)}, Concurrency: i64(2), ContextLimit: i64(512), MaxOutputTokens: i64(64)}
	is := e.issue(c.ID, consumers.NewKey{Narrowing: &nar})
	got := is.Key.Narrowing
	if got == nil || len(got.Models) != 1 || got.Models[0] != "m1" || *got.Quota.Tokens != 100 || *got.Quota.Requests != 0 || *got.Concurrency != 2 || *got.ContextLimit != 512 || *got.MaxOutputTokens != 64 {
		t.Fatalf("issued narrowing = %+v", got)
	}
	l, err := e.svc.ListAccessKeys(ctx, c.ID)
	if err != nil || l[0].Narrowing == nil || l[0].Narrowing.Models[0] != "m1" || *l[0].Narrowing.Quota.Tokens != 100 {
		t.Fatalf("listed narrowing = %+v, %v", l, err)
	}
	id, err := e.au.Authenticate(ctx, "Bearer "+is.Secret.Reveal(), "100.64.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if len(id.Limits.Models) != 1 || id.Limits.Models[0].ModelID == "" || *id.Limits.QuotaTokensPerDay != 100 || *id.Limits.MaxConcurrency != 2 {
		t.Fatalf("effective limits = %+v", id.Limits)
	}

	// Update: the same checks; a narrowing replaces the old one; null removes it.
	wide := consumers.Narrowing{Quota: &consumers.Quota{Tokens: i64(5000)}}
	if _, err := e.svc.UpdateAccessKey(ctx, e.admin, is.Key.ID, consumers.KeyChange{Narrowing: consumers.Value(wide)}); err == nil {
		t.Fatal("update widened the grant")
	} else {
		wantKind(t, err, consumers.KindInvalid, "narrowing.quota.tokens")
	}
	tighter := consumers.Narrowing{Models: []string{"m2"}, Quota: &consumers.Quota{Tokens: i64(10)}}
	u, err := e.svc.UpdateAccessKey(ctx, e.admin, is.Key.ID, consumers.KeyChange{Narrowing: consumers.Value(tighter)})
	if err != nil || u.Narrowing.Models[0] != "m2" || *u.Narrowing.Quota.Tokens != 10 || u.Narrowing.Concurrency != nil {
		t.Fatalf("replaced narrowing = %+v, %v", u.Narrowing, err)
	}
	if id, _ = e.au.Authenticate(ctx, "Bearer "+is.Secret.Reveal(), "100.64.0.1:1"); id == nil || *id.Limits.QuotaTokensPerDay != 10 {
		t.Fatalf("the new narrowing did not apply at once: %+v", id)
	}
	u, err = e.svc.UpdateAccessKey(ctx, e.admin, is.Key.ID, consumers.KeyChange{Narrowing: consumers.Null[consumers.Narrowing]()})
	if err != nil || u.Narrowing != nil {
		t.Fatalf("removed narrowing = %+v, %v", u.Narrowing, err)
	}
	if id, _ = e.au.Authenticate(ctx, "Bearer "+is.Secret.Reveal(), "100.64.0.1:1"); id == nil || *id.Limits.QuotaTokensPerDay != 1000 {
		t.Fatalf("the removed narrowing still applies: %+v", id)
	}
	// An empty narrowing is none.
	if is2 := e.issue(c.ID, consumers.NewKey{Narrowing: &consumers.Narrowing{Quota: &consumers.Quota{}}}); is2.Key.Narrowing != nil {
		t.Errorf("empty narrowing stored as %+v", is2.Key.Narrowing)
	}
}

// A grant with caps bounds the per-model numbers of a narrowing by its largest cap; a consumer
// without a grant gets only the checks that need none.
func TestNarrowingAgainstCapsAndWithoutGrant(t *testing.T) {
	e := newEnv(t)
	noGrant := e.newConsumer("No grant yet")
	e.model("m1")
	if is := e.issue(noGrant.ID, consumers.NewKey{Narrowing: &consumers.Narrowing{Models: []string{"m1"}, Quota: &consumers.Quota{Tokens: i64(5)}}}); is.Key.Narrowing == nil {
		t.Fatal("narrowing of a consumer without a grant dropped")
	}
	if _, err := e.svc.IssueAccessKey(ctx, e.admin, noGrant.ID, consumers.NewKey{Narrowing: &consumers.Narrowing{Models: []string{"no-such-model"}}}); err == nil {
		t.Fatal("unknown model accepted")
	}

	c := e.newConsumer("Capped")
	if err := e.db.Write(ctx, func(tx *storage.Tx) error {
		g, err := e.gr.Grants.Create(tx, c.ID)
		if err != nil {
			return err
		}
		m, err := e.gr.Models.GetByName(ctx, e.db.Reader(), "m1")
		if err != nil {
			return err
		}
		v, err := e.gr.Versions.Create(tx, g.ID, grants.NewVersion{Models: []grants.VersionModel{{ModelID: m.ID, ContextCap: i64(4096), MaxOutputTokens: i64(512)}}})
		if err != nil {
			return err
		}
		return e.gr.Grants.SetCurrentVersion(tx, g.ID, v.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.IssueAccessKey(ctx, e.admin, c.ID, consumers.NewKey{Narrowing: &consumers.Narrowing{ContextLimit: i64(8192)}}); err == nil {
		t.Error("context limit above the grant's cap accepted")
	} else {
		wantKind(t, err, consumers.KindInvalid, "narrowing.context_limit")
	}
	if _, err := e.svc.IssueAccessKey(ctx, e.admin, c.ID, consumers.NewKey{Narrowing: &consumers.Narrowing{MaxOutputTokens: i64(513)}}); err == nil {
		t.Error("output limit above the grant's cap accepted")
	}
	e.issue(c.ID, consumers.NewKey{Narrowing: &consumers.Narrowing{ContextLimit: i64(4096), MaxOutputTokens: i64(512)}})
}
