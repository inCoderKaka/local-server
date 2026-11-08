package consumers_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

const aDay = 24 * time.Hour

// seeded is a consumer with the rows a soft delete leaves behind: two access keys (one revoked
// before the delete), a consent record with evidence, a session and an invite it redeemed.
type seeded struct {
	identity.ConsumerAccount
	keys     []string
	inviteID string
}

// seed creates a consumer with data, uses its key once and, when del, deletes it at the clock.
func (e *env) seed(name string, del bool) seeded {
	e.t.Helper()
	c := e.newConsumer(name)
	s := seeded{ConsumerAccount: c}
	k1 := e.issue(c.ID, consumers.NewKey{Name: str(name + " laptop")})
	k2 := e.issue(c.ID, consumers.NewKey{})
	s.keys = []string{k1.Key.ID, k2.Key.ID}
	if _, err := e.svc.RevokeAccessKey(ctx, e.admin, k2.Key.ID); err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.Write(ctx, func(tx *storage.Tx) error {
		if _, err := e.ir.Consents.Record(tx, identity.ConsentRef{PrincipalID: c.PrincipalID, Purpose: "service_terms", PolicyVersion: "v1"}, str(`{"ip":"100.64.0.9"}`)); err != nil {
			return err
		}
		if _, err := e.ir.Sessions.Create(tx, c.PrincipalID, "hash-"+name, e.now.Add(time.Hour)); err != nil {
			return err
		}
		inv, err := e.ir.Invites.Create(tx, "pfx-"+name, "hash", e.admin, nil, e.now.Add(time.Hour))
		if err != nil {
			return err
		}
		s.inviteID = inv.ID
		if err := e.ir.Invites.Redeem(tx, inv.ID, c.ID); err != nil {
			return err
		}
		at := e.now.UnixMilli()
		_, err = tx.Exec(`INSERT INTO use_record (id, consumer_id, access_key_id, grant_version_id, operation, model, outcome,
prompt_tokens, completion_tokens, total_tokens, latency_ms, started_at, finished_at, receipt_kid, receipt_signature, created_at)
VALUES (?, ?, ?, 'gv', 'chat.completions', 'm1', 'completed', 1, 2, 3, 1, ?, ?, 'kid', x'00', ?)`, "u-"+name, c.ID, k1.Key.ID, at, at, at)
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	if del {
		e.tick()
		if err := e.svc.DeleteConsumer(ctx, e.admin, c.ID); err != nil {
			e.t.Fatal(err)
		}
	}
	return s
}

func (e *env) personalRows(s seeded) (consents, sessions, keys int) {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM idn_consent WHERE principal_id = ?`, s.PrincipalID),
		e.count(`SELECT count(*) FROM idn_session WHERE principal_id = ?`, s.PrincipalID),
		e.count(`SELECT count(*) FROM idn_access_key WHERE consumer_id = ?`, s.ID)
}

func (e *env) purgedAudits() int {
	e.t.Helper()
	return e.count(`SELECT count(*) FROM aud_event WHERE action = ?`, consumers.ActionConsumerPurged)
}

func (e *env) purge() int {
	e.t.Helper()
	n, err := e.svc.PurgeDeleted(ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

// The default retention is 90 days and the host owns it (req_001 D-134, D-38).
func TestDeletedRetentionDefaultIsNinetyDaysOwnedByHost(t *testing.T) {
	e := newEnv(t)
	d, ok := e.st.Definition(consumers.KeyDeletedRetention)
	if !ok || d.Owner != settings.Host || d.Kind != settings.Duration || d.Default != "2160h" || d.Scoped {
		t.Fatalf("definition = %+v, %v", d, ok)
	}
	if got, err := e.st.Duration(ctx, consumers.KeyDeletedRetention, ""); err != nil || got != 90*aDay {
		t.Fatalf("retention = %v, %v; want 90 days", got, err)
	}
}

// A soft delete alone keeps the consent rows, evidence included (req_001 D-106).
func TestSoftDeleteLeavesConsentRowsUntouched(t *testing.T) {
	e := newEnv(t)
	s := e.seed("Ada", true)
	if n := e.count(`SELECT count(*) FROM idn_consent WHERE principal_id = ? AND evidence IS NOT NULL`, s.PrincipalID); n != 1 {
		t.Fatalf("%d consent rows with evidence after the soft delete, want 1", n)
	}
	if n := e.purge(); n != 0 {
		t.Fatalf("purged %d right after the delete", n)
	}
}

// After the retention the purge removes consents, sessions and access keys, clears its invite's
// reference to it, keeps the tombstone, the usage and the grants, audits once, and a second run does nothing
// (req_001 D-136, D-134, D-106).
func TestPurgeRemovesPersonalRowsKeepsTombstone(t *testing.T) {
	e := newEnv(t)
	e.model("m1")
	s := e.seed("Zyxwv", false)
	e.grant(s.ID, grantsVersion(), "m1")
	e.tick()
	if err := e.svc.DeleteConsumer(ctx, e.admin, s.ID); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(90*aDay + time.Hour)

	if n := e.purge(); n != 1 {
		t.Fatalf("purged %d, want 1", n)
	}
	if c, ss, k := e.personalRows(s); c != 0 || ss != 0 || k != 0 {
		t.Errorf("left: %d consents, %d sessions, %d keys", c, ss, k)
	}
	if n := e.count(`SELECT count(*) FROM idn_invite WHERE id = ? AND redeemed_consumer_id IS NULL AND redeemed_at IS NOT NULL`, s.inviteID); n != 1 {
		t.Error("the invite row is gone or still names the consumer")
	}
	if n := e.count(`SELECT count(*) FROM idn_consumer c JOIN idn_principal p ON p.id = c.principal_id WHERE c.id = ? AND p.status = 'deleted'`, s.ID); n != 1 {
		t.Error("the tombstone (consumer and deleted principal) is gone")
	}
	if n := e.count(`SELECT count(*) FROM use_record WHERE consumer_id = ?`, s.ID); n != 1 {
		t.Errorf("%d usage rows, want 1", n)
	}
	if n := e.count(`SELECT count(*) FROM grt_grant WHERE consumer_id = ?`, s.ID); n != 1 {
		t.Errorf("%d grants, want 1", n)
	}

	var actor *string
	var target, detail string
	if err := e.db.Reader().QueryRow(ctx, `SELECT actor_id, target_id, detail FROM aud_event WHERE action = ?`, consumers.ActionConsumerPurged).Scan(&actor, &target, &detail); err != nil {
		t.Fatal(err)
	}
	if actor != nil || target != s.PrincipalID {
		t.Errorf("audit actor %v target %q; want system and %q", actor, target, s.PrincipalID)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(detail), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"consumer_id": s.ID, "consents": 1.0, "credentials": 0.0, "sessions": 1.0, "access_keys": 2.0, "invite_refs_cleared": 1.0}
	if len(got) != len(want) {
		t.Errorf("detail = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("detail[%s] = %v, want %v", k, got[k], v)
		}
	}
	if strings.Contains(e.auditText(), "Zyxwv") || strings.Contains(e.auditText(), "100.64.0.9") {
		t.Error("an audit row holds personal data")
	}

	e.now = e.now.Add(aDay)
	if n := e.purge(); n != 0 {
		t.Errorf("second pass purged %d", n)
	}
	if n := e.purgedAudits(); n != 1 {
		t.Errorf("%d purge audit rows, want 1", n)
	}
}

// A consumer inside the window, and one that is still active, are kept untouched.
func TestPurgeKeepsConsumersInsideWindowAndNotDeleted(t *testing.T) {
	e := newEnv(t)
	recent := e.seed("Recent", true)
	active := e.seed("Active", false)
	suspended := e.seed("Suspended", false)
	if _, err := e.svc.SuspendConsumer(ctx, e.admin, suspended.ID); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(89 * aDay)
	late := e.seed("Late", true) // deleted 89 days after the others
	e.now = e.now.Add(2 * aDay)  // recent is 91 days old, late is 2 days old; active and suspended are older still

	if n := e.purge(); n != 1 {
		t.Fatalf("purged %d, want only the one deleted 91 days ago", n)
	}
	if c, ss, k := e.personalRows(recent); c+ss+k != 0 {
		t.Errorf("the old deleted consumer keeps %d/%d/%d", c, ss, k)
	}
	for _, s := range []seeded{active, suspended, late} {
		if c, ss, k := e.personalRows(s); c != 1 || ss != 1 || k != 2 {
			t.Errorf("%s changed: %d consents, %d sessions, %d keys", s.ID, c, ss, k)
		}
	}
}

// Lowering the retention purges consumers that are already deleted; zero or less keeps them forever.
func TestPurgeFollowsTheSettingNow(t *testing.T) {
	e := newEnv(t)
	s := e.seed("Ada", true)
	e.now = e.now.Add(10 * aDay)
	if n := e.purge(); n != 0 {
		t.Fatalf("purged %d inside the default window", n)
	}
	e.set(consumers.KeyDeletedRetention, "0")
	e.now = e.now.Add(1000 * aDay)
	if n := e.purge(); n != 0 {
		t.Fatalf("purged %d with retention 0, want keep forever", n)
	}
	e.set(consumers.KeyDeletedRetention, "-1h")
	if n := e.purge(); n != 0 {
		t.Fatalf("purged %d with a negative retention", n)
	}
	e.set(consumers.KeyDeletedRetention, "240h")
	e.now = e.now.Add(-999 * aDay)
	if n := e.purge(); n != 1 {
		t.Fatalf("purged %d after lowering the retention to 10 days, want 1", n)
	}
	if c, ss, k := e.personalRows(s); c+ss+k != 0 {
		t.Errorf("rows left: %d/%d/%d", c, ss, k)
	}
}

// One consumer failing does not stop the others; its unit rolls back with no audit row. Batches
// of one row exercise the paging past a consumer that keeps failing.
func TestPurgeContinuesPastAFailingConsumer(t *testing.T) {
	e := newEnv(t)
	bad := e.seed("Bad", true)
	good1 := e.seed("Good1", true)
	good2 := e.seed("Good2", true)
	e.set(settings.KeyRetentionBatchRows, "1")
	if err := e.db.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.Exec(`CREATE TRIGGER block_purge BEFORE DELETE ON idn_access_key WHEN old.consumer_id = '` + bad.ID + `' BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(91 * aDay)

	n, err := e.svc.PurgeDeleted(ctx)
	if n != 2 || err == nil || !strings.Contains(err.Error(), bad.ID) {
		t.Fatalf("purged %d, err %v; want 2 and an error naming the bad consumer", n, err)
	}
	if c, ss, k := e.personalRows(bad); c != 1 || ss != 1 || k != 2 {
		t.Errorf("the failing consumer changed: %d/%d/%d", c, ss, k)
	}
	for _, g := range []seeded{good1, good2} {
		if c, ss, k := e.personalRows(g); c+ss+k != 0 {
			t.Errorf("%s not purged: %d/%d/%d", g.ID, c, ss, k)
		}
	}
	if n := e.purgedAudits(); n != 2 {
		t.Errorf("%d purge audit rows, want 2", n)
	}
}

// RunPurge purges at start, waits, and returns when its context ends.
func TestRunPurgeStopsOnCancel(t *testing.T) {
	e := newEnv(t)
	s := e.seed("Ada", true)
	e.now = e.now.Add(91 * aDay)
	e.set(settings.KeyRetentionInterval, "1h")
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { e.svc.RunPurge(runCtx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for e.purgedAudits() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if c, ss, k := e.personalRows(s); c+ss+k != 0 {
		t.Errorf("not purged at start: %d/%d/%d", c, ss, k)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunPurge did not stop after the context ended")
	}
}
