package grants_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	admingrants "github.com/inCoderKaka/local-server/internal/admin/grants"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func (e *env) createRule(grantID *string, body string) admingrants.PolicyRule {
	e.t.Helper()
	e.tick()
	r, err := e.svc.CreateRule(ctx, e.admin, admingrants.NewRule{GrantID: grantID, Body: []byte(body)})
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

// storeBroken puts a rule straight into storage, as one written before rules were checked.
func (e *env) storeBroken(body string) string {
	e.t.Helper()
	var id string
	if err := e.db.Write(ctx, func(tx *storage.Tx) error {
		r, err := e.gr.PolicyRules.Create(tx, nil, "broken", 0, 1, body)
		id = r.ID
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func TestCreateRuleStoresTheValidatorsFormatVersionAndDefaults(t *testing.T) {
	e := newEnv(t)
	r := e.createRule(nil, bgRule)
	if r.Version != 1 || r.Position != 0 || r.Name != r.ID || r.GrantID != nil || r.Enabled == nil || !*r.Enabled || r.Body != bgRule {
		t.Fatalf("rule = %+v", r)
	}
	var fv int64
	if err := e.db.Reader().QueryRow(ctx, `SELECT format_version FROM grt_policy_rule WHERE id = ?`, r.ID).Scan(&fv); err != nil || fv != 1 {
		t.Fatalf("format_version = %d, %v", fv, err)
	}
	if n := e.count(`SELECT count(*) FROM grt_policy_rule_version WHERE rule_id = ? AND created_by = ?`, r.ID, e.admin); n != 1 {
		t.Fatal("version 1 does not name the admin")
	}
	if !strings.Contains(e.auditText(), "policy_rule.created") {
		t.Fatalf("audit = %s", e.auditText())
	}

	name, pos := "  Mine  ", int64(3)
	r2, err := e.svc.CreateRule(ctx, e.admin, admingrants.NewRule{Name: &name, Position: &pos, Body: []byte(denyRule)})
	if err != nil || r2.Name != "Mine" || r2.Position != 3 {
		t.Fatalf("rule = %+v, %v", r2, err)
	}
}

// A document the policy module refuses is a 400 whose text and param are fixed: no byte of the
// document comes back (req_001 D-31).
func TestCreateRuleRefusesWhatTheValidatorRefusesWithoutEchoingIt(t *testing.T) {
	e := newEnv(t)
	secret := "SECRET-PROMPT-TEXT"
	bodies := []struct{ body, param string }{
		{`{"format_version":1,"priority":"` + secret + `"}`, "rule"},
		{`{"format_version":2,"x":"` + secret + `"}`, "rule.format_version"},
		{`{"format_version":1,"` + secret + `":1}`, "rule"},
		{`not json ` + secret, "rule"},
		{`{"format_version":1,"when":{"properties":{"a":"` + strings.Repeat("x", 9000) + secret + `"}}}`, "rule"},
	}
	for _, c := range bodies {
		_, err := e.svc.CreateRule(ctx, e.admin, admingrants.NewRule{Body: []byte(c.body)})
		var ge *admingrants.Error
		if !errors.As(err, &ge) || ge.Kind != admingrants.KindInvalid || ge.Param != c.param {
			t.Fatalf("error = %#v for %.40s", err, c.body)
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(ge.Message, secret) || strings.Contains(ge.Param, secret) {
			t.Fatalf("the refusal quotes the document: %v", err)
		}
	}
	if n := e.count(`SELECT count(*) FROM grt_policy_rule`); n != 0 {
		t.Fatalf("%d rules stored after refusals", n)
	}
	// The rule size limit is a host setting.
	e.set("policy.rule.max_bytes", "20")
	_, err := e.svc.CreateRule(ctx, e.admin, admingrants.NewRule{Body: []byte(bgRule)})
	wantKind(t, err, admingrants.KindInvalid, "rule")
}

func TestCreateRuleChecksNameGrantAndPosition(t *testing.T) {
	e := newEnv(t)
	for _, name := range []string{"", "   ", strings.Repeat("n", 121)} {
		n := name
		_, err := e.svc.CreateRule(ctx, e.admin, admingrants.NewRule{Name: &n, Body: []byte(bgRule)})
		wantKind(t, err, admingrants.KindInvalid, "name")
	}
	neg := int64(-1)
	_, err := e.svc.CreateRule(ctx, e.admin, admingrants.NewRule{Position: &neg, Body: []byte(bgRule)})
	wantKind(t, err, admingrants.KindInvalid, "position")
	ghost := "0192e5a0-0004-7000-8000-000000000004"
	_, err = e.svc.CreateRule(ctx, e.admin, admingrants.NewRule{GrantID: &ghost, Body: []byte(bgRule)})
	wantKind(t, err, admingrants.KindInvalid, "grant_id")
}

// Replacing a rule stores a new immutable version, and the earlier ones stay in the history.
func TestReplaceRuleStoresANewImmutableVersion(t *testing.T) {
	e := newEnv(t)
	r := e.createRule(nil, bgRule)
	e.tick()
	body2 := `{"format_version":1,"priority":"interactive"}`
	name := "Renamed"
	r2, err := e.svc.ReplaceRule(ctx, e.admin, r.ID, admingrants.RuleChange{Name: &name, Body: []byte(body2)})
	if err != nil || r2.Version != 2 || r2.Name != "Renamed" || r2.Body != body2 || !r2.CreatedAt.After(r.CreatedAt) {
		t.Fatalf("rule = %+v, %v", r2, err)
	}
	// The same document and name again changes nothing: no version.
	r3, err := e.svc.ReplaceRule(ctx, e.admin, r.ID, admingrants.RuleChange{Body: []byte(body2)})
	if err != nil || r3.Version != 2 {
		t.Fatalf("rule = %+v, %v", r3, err)
	}
	vs, err := e.svc.RuleVersions(ctx, r.ID)
	if err != nil || len(vs) != 2 || vs[0].Version != 2 || vs[1].Version != 1 || vs[1].Body != bgRule || vs[1].Name != r.ID {
		t.Fatalf("versions = %+v, %v", vs, err)
	}
	if vs[0].Enabled == nil || vs[1].Enabled != nil {
		t.Fatalf("only the live version carries the switch: %+v", vs)
	}
	// An old version is never edited, and not deleted.
	for _, q := range []string{`UPDATE grt_policy_rule_version SET body = '{}'`, `DELETE FROM grt_policy_rule_version`} {
		if err := e.db.Write(ctx, func(tx *storage.Tx) error { _, err := tx.Exec(q); return err }); err == nil {
			t.Fatalf("%q was accepted", q)
		}
	}
	// A refused replacement changes nothing.
	_, err = e.svc.ReplaceRule(ctx, e.admin, r.ID, admingrants.RuleChange{Body: []byte(`{"format_version":1,"priority":"x"}`)})
	wantKind(t, err, admingrants.KindInvalid, "rule")
	if got, _ := e.svc.GetRule(ctx, r.ID); got.Version != 2 {
		t.Fatalf("rule = %+v", got)
	}
	if n := strings.Count(e.auditText(), "policy_rule.updated"); n != 2 {
		t.Fatalf("%d policy_rule.updated rows, want 2", n)
	}
	_, err = e.svc.ReplaceRule(ctx, e.admin, "0192e5a0-000d-7000-8000-00000000000d", admingrants.RuleChange{Body: []byte(bgRule)})
	wantKind(t, err, admingrants.KindNotFound, "")
}

// The switch is the rule's, not a version's; switching a rule off forgets its failures, and so does
// fixing its document.
func TestReplaceRuleSwitchAndFailureCounts(t *testing.T) {
	e := newEnv(t)
	r := e.createRule(nil, bgRule)
	off := false
	got, err := e.svc.ReplaceRule(ctx, e.admin, r.ID, admingrants.RuleChange{Enabled: &off, Body: []byte(bgRule)})
	if err != nil || got.Version != 1 || *got.Enabled {
		t.Fatalf("rule = %+v, %v", got, err)
	}
	if !slices.Equal(e.fail.clearedIDs(), []string{r.ID}) {
		t.Fatalf("cleared = %v, want the disabled rule", e.fail.clearedIDs())
	}
	on := true
	e.fail.set(r.ID, 3)
	if _, err = e.svc.ReplaceRule(ctx, e.admin, r.ID, admingrants.RuleChange{Enabled: &on, Body: []byte(bgRule)}); err != nil {
		t.Fatal(err)
	}
	if e.fail.RuleFailures(r.ID) != 3 || len(e.fail.clearedIDs()) != 1 {
		t.Fatal("an unchanged, enabled rule lost its failure count")
	}
	if _, err = e.svc.ReplaceRule(ctx, e.admin, r.ID, admingrants.RuleChange{Body: []byte(denyRule)}); err != nil {
		t.Fatal(err)
	}
	if e.fail.RuleFailures(r.ID) != 0 {
		t.Fatal("fixing the document did not clear the failure count")
	}
}

func TestDeleteRuleIsSoft(t *testing.T) {
	e := newEnv(t)
	r := e.createRule(nil, bgRule)
	if err := e.svc.DeleteRule(ctx, e.admin, r.ID); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.GetRule(ctx, r.ID)
	wantKind(t, err, admingrants.KindNotFound, "")
	_, err = e.svc.RuleVersions(ctx, r.ID)
	wantKind(t, err, admingrants.KindNotFound, "")
	_, err = e.svc.ReplaceRule(ctx, e.admin, r.ID, admingrants.RuleChange{Body: []byte(bgRule)})
	wantKind(t, err, admingrants.KindNotFound, "")
	wantKind(t, e.svc.DeleteRule(ctx, e.admin, r.ID), admingrants.KindNotFound, "")
	if rs, _ := e.svc.ListRules(ctx, nil); len(rs) != 0 {
		t.Fatalf("a deleted rule is listed: %+v", rs)
	}
	if n := e.count(`SELECT count(*) FROM grt_policy_rule WHERE id = ? AND deleted_at IS NOT NULL`, r.ID); n != 1 {
		t.Fatal("the rule row was removed")
	}
	if n := e.count(`SELECT count(*) FROM grt_policy_rule_version WHERE rule_id = ?`, r.ID); n != 1 {
		t.Fatal("the versions were removed")
	}
	if !strings.Contains(e.auditText(), "policy_rule.deleted") || !slices.Contains(e.fail.clearedIDs(), r.ID) {
		t.Fatal("the delete was not audited or did not clear the failures")
	}
	// The gateway's own list leaves it out too.
	if rs, _ := e.gr.PolicyRules.List(ctx, e.db.Reader(), nil); len(rs) != 0 {
		t.Fatalf("the evaluation list holds a deleted rule: %+v", rs)
	}
}

func TestListRulesByGrant(t *testing.T) {
	e := newEnv(t)
	e.openModels()
	c := e.newConsumer("App")
	v, err := e.svc.SetGrant(ctx, e.admin, c.ID, limits())
	if err != nil {
		t.Fatal(err)
	}
	global := e.createRule(nil, bgRule)
	mine := e.createRule(&v.GrantID, denyRule)
	all, err := e.svc.ListRules(ctx, nil)
	if err != nil || len(all) != 2 || all[0].ID != global.ID {
		t.Fatalf("all = %+v, %v", all, err)
	}
	own, err := e.svc.ListRules(ctx, &v.GrantID)
	if err != nil || len(own) != 1 || own[0].ID != mine.ID || own[0].GrantID == nil || *own[0].GrantID != v.GrantID {
		t.Fatalf("own = %+v, %v", own, err)
	}
	other := "0192e5a0-0004-7000-8000-000000000004"
	if none, _ := e.svc.ListRules(ctx, &other); len(none) != 0 {
		t.Fatalf("rules of another grant = %+v", none)
	}
}

// After policy.rule.max_failures failed evaluations a broken rule without deny is switched off in
// storage and audited; a rule with deny never is (req_001 D-73, PLT-B-020).
func TestBrokenRuleWithoutDenyIsSwitchedOffAfterFiveFailures(t *testing.T) {
	e := newEnv(t)
	broken := e.storeBroken(brokenRule)
	denying := e.storeBroken(`{"format_version":1,"deny":true,"priority":"nonsense"}`)
	unparsable := e.storeBroken(`not json at all`)
	healthy := e.createRule(nil, bgRule).ID

	for _, id := range []string{broken, denying, unparsable} {
		e.fail.set(id, 4)
	}
	e.fail.set(healthy, 0)
	if off, err := e.svc.DisableFailingRules(ctx); err != nil || len(off) != 0 {
		t.Fatalf("disabled %v after 4 failures, %v", off, err)
	}
	for _, id := range []string{broken, denying, unparsable} {
		e.fail.set(id, 5)
	}
	off, err := e.svc.DisableFailingRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(off)
	want := []string{broken, unparsable}
	slices.Sort(want)
	if !slices.Equal(off, want) {
		t.Fatalf("disabled %v, want %v", off, want)
	}
	enabled := func(id string) int {
		return e.count(`SELECT enabled FROM grt_policy_rule WHERE id = ?`, id)
	}
	if enabled(broken) != 0 || enabled(unparsable) != 0 || enabled(denying) != 1 || enabled(healthy) != 1 {
		t.Fatal("the wrong rules are switched off")
	}
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = 'policy_rule.auto_disabled' AND actor_id IS NULL`); n != 2 {
		t.Fatalf("%d auto_disabled audit rows, want 2 by the system", n)
	}
	// The audit row names the version of the document that was counted. policy_rule.created rows
	// also carry "version":1, so only the auto_disabled rows are counted.
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = 'policy_rule.auto_disabled' AND detail LIKE '%"version":1%'`); n != 2 {
		t.Fatalf("%d auto_disabled audit rows name version 1, want 2", n)
	}
	txt := e.auditText()
	if !strings.Contains(txt, `"failures":5`) || strings.Contains(txt, "nonsense") || strings.Contains(txt, "not json") || strings.Contains(txt, `"broken"`) {
		t.Fatalf("audit = %s", txt)
	}
	// Their failures are forgotten; the deny rule keeps counting and keeps refusing.
	if e.fail.RuleFailures(broken) != 0 || e.fail.RuleFailures(denying) != 5 {
		t.Fatal("failure counts after the sweep are wrong")
	}
	// A later sweep has nothing more to do.
	if off, err := e.svc.DisableFailingRules(ctx); err != nil || len(off) != 0 {
		t.Fatalf("second sweep disabled %v, %v", off, err)
	}
	// The limit is a host setting.
	e.fail.set(healthy, 2)
	e.set("policy.rule.max_failures", "2")
	// The sweeper trusts the gateway's count, and the gateway counts only evaluations that failed to
	// parse; a count set by hand on a rule that parses therefore switches it off too.
	if off, err := e.svc.DisableFailingRules(ctx); err != nil || len(off) != 1 || off[0] != healthy {
		t.Fatalf("with max_failures 2: %v, %v", off, err)
	}
}

// hookFailures reports a stale count of 5 for one rule and runs once, on the first question about
// it and without holding any lock, a callback that edits the rule. ClearRuleFailures does nothing:
// the count is stale because the edit committed before the count was cleared.
type hookFailures struct {
	mu   sync.Mutex
	id   string
	hook func()
}

func (h *hookFailures) RuleFailures(id string) int64 {
	h.mu.Lock()
	hook := h.hook
	if id == h.id {
		h.hook = nil
	}
	h.mu.Unlock()
	if hook != nil && id == h.id {
		hook()
	}
	return 5
}

func (h *hookFailures) ClearRuleFailures(string) {}

// A rule edited after it was listed is not switched off on the count of its older document, and
// no audit row is written for it.
func TestEditedRuleIsNotSwitchedOffOnAStaleCount(t *testing.T) {
	e := newEnv(t)
	id := e.storeBroken(brokenRule)
	h := &hookFailures{id: id}
	svc, err := admingrants.New(admingrants.Config{DB: e.db, Settings: e.st, Identity: e.ir, Grants: e.gr, Auth: e.au, Events: e.bus,
		Engine: e.eng, RuleFailures: h, Clock: func() time.Time { return e.now }})
	if err != nil {
		t.Fatal(err)
	}
	h.hook = func() {
		e.tick()
		if _, err := svc.ReplaceRule(ctx, e.admin, id, admingrants.RuleChange{Body: []byte(bgRule)}); err != nil {
			t.Error(err)
		}
	}
	off, err := svc.DisableFailingRules(ctx)
	if err != nil || len(off) != 0 {
		t.Fatalf("disabled %v, %v", off, err)
	}
	if e.count(`SELECT enabled FROM grt_policy_rule WHERE id = ?`, id) != 1 {
		t.Fatal("an edited rule was switched off")
	}
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = 'policy_rule.auto_disabled'`); n != 0 {
		t.Fatalf("%d auto_disabled audit rows, want none", n)
	}
}

// forgetfulFailures reports a count of 5 the first time it is asked about a rule and 0 after:
// the count was forgotten between the sweep's listing and its write.
type forgetfulFailures struct {
	mu    sync.Mutex
	asked bool
}

func (f *forgetfulFailures) RuleFailures(string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.asked {
		f.asked = true
		return 5
	}
	return 0
}

func (f *forgetfulFailures) ClearRuleFailures(string) {}

// A count that was forgotten after the sweep listed the rule does not switch it off, and no audit
// row is written for it.
func TestRuleWhoseCountWasForgottenIsNotSwitchedOff(t *testing.T) {
	e := newEnv(t)
	id := e.storeBroken(brokenRule)
	svc, err := admingrants.New(admingrants.Config{DB: e.db, Settings: e.st, Identity: e.ir, Grants: e.gr, Auth: e.au, Events: e.bus,
		Engine: e.eng, RuleFailures: &forgetfulFailures{}, Clock: func() time.Time { return e.now }})
	if err != nil {
		t.Fatal(err)
	}
	off, err := svc.DisableFailingRules(ctx)
	if err != nil || len(off) != 0 {
		t.Fatalf("disabled %v, %v", off, err)
	}
	if e.count(`SELECT enabled FROM grt_policy_rule WHERE id = ?`, id) != 1 {
		t.Fatal("a rule whose count was forgotten was switched off")
	}
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = 'policy_rule.auto_disabled'`); n != 0 {
		t.Fatalf("%d auto_disabled audit rows, want none", n)
	}
}

// orderFailures reports a count of 5 until it is cleared, and records which version of the rule
// the database held, for readers outside the writer, at the moment of the clear.
type orderFailures struct {
	t         *testing.T
	e         *env
	mu        sync.Mutex
	n         int64
	clearedAt int
}

func (o *orderFailures) RuleFailures(string) int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.n
}

func (o *orderFailures) ClearRuleFailures(id string) {
	var v int
	// The read pool sees only committed data, so this is the version before the edit's commit.
	if err := o.e.db.Reader().QueryRow(ctx, `SELECT version_no FROM grt_policy_rule WHERE id = ?`, id).Scan(&v); err != nil {
		o.t.Error(err)
	}
	o.mu.Lock()
	o.n, o.clearedAt = 0, v
	o.mu.Unlock()
}

// Fixing a rule forgets its failures in the same unit of work as the edit, before the new version
// is committed, so a sweep never sees the new version with the old document's count.
func TestReplaceRuleForgetsFailuresBeforeTheCommit(t *testing.T) {
	e := newEnv(t)
	id := e.storeBroken(brokenRule)
	o := &orderFailures{t: t, e: e, n: 5, clearedAt: -1}
	svc, err := admingrants.New(admingrants.Config{DB: e.db, Settings: e.st, Identity: e.ir, Grants: e.gr, Auth: e.au, Events: e.bus,
		Engine: e.eng, RuleFailures: o, Clock: func() time.Time { return e.now }})
	if err != nil {
		t.Fatal(err)
	}
	e.tick()
	if _, err := svc.ReplaceRule(ctx, e.admin, id, admingrants.RuleChange{Body: []byte(bgRule)}); err != nil {
		t.Fatal(err)
	}
	if o.clearedAt != 1 {
		t.Fatalf("failures were cleared when the stored version was %d, want 1 (before the commit)", o.clearedAt)
	}
	off, err := svc.DisableFailingRules(ctx)
	if err != nil || len(off) != 0 {
		t.Fatalf("disabled %v, %v", off, err)
	}
	if e.count(`SELECT enabled FROM grt_policy_rule WHERE id = ?`, id) != 1 {
		t.Fatal("a fixed rule was switched off")
	}
}

func TestSweeperIsOffWithoutAFailureCounter(t *testing.T) {
	e := newEnv(t)
	id := e.storeBroken(brokenRule)
	off, err := newServiceWithoutCounter(t, e).DisableFailingRules(ctx)
	if err != nil || off != nil {
		t.Fatalf("disabled %v, %v", off, err)
	}
	if e.count(`SELECT enabled FROM grt_policy_rule WHERE id = ?`, id) != 1 {
		t.Fatal("a rule was switched off with no counter")
	}
}

func TestRuleSettingsAreValidated(t *testing.T) {
	e := newEnv(t)
	for _, tc := range [][2]string{{"policy.rule.max_failures", "0"}, {"policy.rule.max_failures", "x"}, {"policy.rule.check_interval", "0s"}, {"policy.rule.check_interval", "-1m"}} {
		if err := e.st.Set(ctx, "host", tc[0], "", tc[1]); err == nil {
			t.Fatalf("%s = %s accepted", tc[0], tc[1])
		}
	}
	for _, key := range []string{"policy.rule.max_failures", "policy.rule.check_interval", "admin.model_default_allowed"} {
		d, ok := e.st.Definition(key)
		if !ok || d.Owner != "host" || d.Description == "" {
			t.Fatalf("%s is not a described host setting: %+v", key, d)
		}
	}
}

// The sweeper applies the host's interval and stops with its context.
func TestRuleSweeperRunsEveryIntervalUntilStopped(t *testing.T) {
	e := newEnv(t)
	e.set("policy.rule.check_interval", "10ms")
	id := e.storeBroken(brokenRule)
	e.fail.set(id, 5)
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { e.svc.RunRuleSweeper(runCtx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for e.count(`SELECT enabled FROM grt_policy_rule WHERE id = ?`, id) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the sweeper did not switch the rule off")
		}
		time.Sleep(5 * time.Millisecond)
	}
	stop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the sweeper did not stop with its context")
	}
}
