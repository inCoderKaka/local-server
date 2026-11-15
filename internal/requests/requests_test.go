package requests_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/requests"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func TestCreateListGetAndComments(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c, adm := e.consumer("Example App"), e.admin("Example Admin")

	q, err := e.svc.Create(ctx, c.consumerID, c.principalID, requests.NewRequest{Message: "More context please", Ask: []byte(`{"context_limit":8192}`)})
	if err != nil {
		t.Fatal(err)
	}
	if q.Status != requests.StatusOpen || q.ConsumerID != c.consumerID || q.ExpiresAt == nil || q.DecidedAt != nil {
		t.Fatalf("new request = %+v", q)
	}
	if want := t0.Add(336 * time.Hour); !q.ExpiresAt.Equal(want) {
		t.Errorf("expires at %v, want the default TTL of 14 days: %v", q.ExpiresAt, want)
	}
	if string(q.Ask) != `{"context_limit":8192}` || q.Comments == nil || len(q.Comments) != 0 {
		t.Errorf("ask %s, comments %v", q.Ask, q.Comments)
	}

	e.clk.advance(time.Minute)
	if _, err := e.svc.Comment(ctx, c.consumerID, c.principalID, q.ID, "I run a long document"); err != nil {
		t.Fatal(err)
	}
	e.clk.advance(time.Minute)
	got, err := e.svc.Comment(ctx, "", adm.principalID, q.ID, "How long?")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Comments) != 2 {
		t.Fatalf("%d comments, want 2", len(got.Comments))
	}
	first, second := got.Comments[0], got.Comments[1]
	if first.Body != "I run a long document" || first.Author.Kind != "consumer" || first.Author.PrincipalID != c.principalID ||
		first.Author.DisplayName == nil || *first.Author.DisplayName != "Example App" {
		t.Errorf("first comment = %+v", first)
	}
	if second.Author.Kind != "host_admin" || second.Author.PrincipalID != adm.principalID || *second.Author.DisplayName != "Example Admin" {
		t.Errorf("second comment = %+v", second)
	}

	// A list has no comments; the single read has them.
	page, err := e.svc.List(ctx, requests.ListQuery{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Comments != nil {
		t.Fatalf("list = %+v, %v", page, err)
	}
	one, err := e.svc.Get(ctx, c.consumerID, q.ID)
	if err != nil || len(one.Comments) != 2 {
		t.Fatalf("get = %+v, %v", one, err)
	}
	// A comment does not change the state, and a request that does not exist is not found.
	if one.Status != requests.StatusOpen {
		t.Errorf("status %s after comments", one.Status)
	}
	if _, err := e.svc.Get(ctx, "", "0192e5a0-0006-7000-8000-000000000006"); kindOf(err) != requests.KindNotFound {
		t.Errorf("unknown id: %v", err)
	}
	if n := e.count(`SELECT count(*) FROM ntf_event WHERE type = 'resource_request.created'`); n != 1 {
		t.Errorf("%d created events, want 1", n)
	}
}

func TestPagingNewestFirstAndStatusFilter(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c := e.consumer("A")
	e.set(requests.KeyMaxOpen, "10")
	var ids []string
	for i := 0; i < 5; i++ {
		e.clk.advance(time.Second)
		ids = append(ids, e.create(c, "r").ID)
	}
	if _, err := e.svc.Withdraw(ctx, c.consumerID, c.principalID, ids[1]); err != nil {
		t.Fatal(err)
	}
	two := 2
	p1, err := e.svc.List(ctx, requests.ListQuery{Limit: &two})
	if err != nil || len(p1.Items) != 2 || p1.NextCursor == "" {
		t.Fatalf("page 1 = %+v, %v", p1, err)
	}
	p2, err := e.svc.List(ctx, requests.ListQuery{Limit: &two, Cursor: p1.NextCursor})
	if err != nil || len(p2.Items) != 2 || p2.NextCursor == "" {
		t.Fatalf("page 2 = %+v, %v", p2, err)
	}
	p3, err := e.svc.List(ctx, requests.ListQuery{Limit: &two, Cursor: p2.NextCursor})
	if err != nil || len(p3.Items) != 1 || p3.NextCursor != "" {
		t.Fatalf("page 3 = %+v, %v", p3, err)
	}
	var got []string
	for _, p := range []requests.Page{p1, p2, p3} {
		for _, q := range p.Items {
			got = append(got, q.ID)
		}
	}
	for i, want := range []string{ids[4], ids[3], ids[2], ids[1], ids[0]} {
		if got[i] != want {
			t.Fatalf("order %v, want newest first %v", got, ids)
		}
	}
	open, err := e.svc.List(ctx, requests.ListQuery{Status: requests.StatusOpen})
	if err != nil || len(open.Items) != 4 {
		t.Fatalf("open = %d, %v", len(open.Items), err)
	}
	wd, err := e.svc.List(ctx, requests.ListQuery{Status: requests.StatusWithdrawn})
	if err != nil || len(wd.Items) != 1 || wd.Items[0].ID != ids[1] {
		t.Fatalf("withdrawn = %+v, %v", wd, err)
	}
	if _, err := e.svc.List(ctx, requests.ListQuery{Cursor: "not-a-cursor"}); kindOf(err) != requests.KindInvalid || paramOf(err) != "cursor" {
		t.Errorf("bad cursor: %v", err)
	}
	zero := 0
	if _, err := e.svc.List(ctx, requests.ListQuery{Limit: &zero}); kindOf(err) != requests.KindInvalid || paramOf(err) != "limit" {
		t.Errorf("limit 0: %v", err)
	}
	// requests.page_size caps the page and is the default.
	e.set(requests.KeyPageSize, "3")
	big := 100
	pg, err := e.svc.List(ctx, requests.ListQuery{Limit: &big})
	if err != nil || len(pg.Items) != 3 || pg.NextCursor == "" {
		t.Errorf("capped page = %d items, cursor %q, %v", len(pg.Items), pg.NextCursor, err)
	}
}

// Every final state refuses a comment and every decision with a conflict, and nothing changes.
func TestFinalRequestsRefuseFurtherChanges(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c, adm := e.consumer("A"), e.admin("Admin")
	e.set(requests.KeyMaxOpen, "10")

	finals := map[requests.Status]func(id string) error{
		requests.StatusWithdrawn: func(id string) error {
			_, err := e.svc.Withdraw(ctx, c.consumerID, c.principalID, id)
			return err
		},
		requests.StatusRejected: func(id string) error {
			_, err := e.svc.Reject(ctx, adm.principalID, id, "No")
			return err
		},
		requests.StatusApproved: func(id string) error {
			_, err := e.svc.Approve(ctx, adm.principalID, id, "Yes", e.applyGrant)
			return err
		},
		requests.StatusExpired: func(id string) error {
			e.clk.advance(400 * time.Hour)
			n, err := e.svc.ExpireDue(ctx)
			if err == nil && n < 1 {
				t.Error("nothing expired")
			}
			return err
		},
	}
	for want, make := range finals {
		q := e.create(c, "ask")
		if err := make(q.ID); err != nil {
			t.Fatalf("%s: %v", want, err)
		}
		before := e.count(`SELECT count(*) FROM req_comment`)
		versions := e.count(`SELECT count(*) FROM grt_version`)
		now, _ := e.svc.Get(ctx, "", q.ID)
		if now.Status != want || now.DecidedAt == nil || now.ExpiresAt != nil {
			t.Fatalf("%s: request = %+v", want, now)
		}
		calls := map[string]error{}
		_, calls["comment by consumer"] = e.svc.Comment(ctx, c.consumerID, c.principalID, q.ID, "more")
		_, calls["comment by host"] = e.svc.Comment(ctx, "", adm.principalID, q.ID, "more")
		_, calls["withdraw"] = e.svc.Withdraw(ctx, c.consumerID, c.principalID, q.ID)
		_, calls["reject"] = e.svc.Reject(ctx, adm.principalID, q.ID, "")
		_, calls["approve"] = e.svc.Approve(ctx, adm.principalID, q.ID, "", e.applyGrant)
		for name, err := range calls {
			if !errors.Is(err, requests.ErrNotOpen) || kindOf(err) != requests.KindConflict {
				t.Errorf("%s after %s: %v, want the not-open conflict", name, want, err)
			}
		}
		if e.count(`SELECT count(*) FROM req_comment`) != before || e.count(`SELECT count(*) FROM grt_version`) != versions {
			t.Errorf("a refused call after %s left a row behind", want)
		}
		after, _ := e.svc.Get(ctx, "", q.ID)
		if after.Status != want || !after.DecidedAt.Equal(*now.DecidedAt) {
			t.Errorf("%s: the state changed to %+v", want, after)
		}
	}
}

// A request past its TTL refuses every action even before the sweeper has moved it to expired.
func TestRequestPastItsTTLRefusesActionsBeforeTheSweep(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c, adm := e.consumer("A"), e.admin("Admin")
	e.set(requests.KeyTTL, "1h")
	q := e.create(c, "ask")
	e.clk.advance(2 * time.Hour)

	before := e.count(`SELECT count(*) FROM req_comment`)
	versions := e.count(`SELECT count(*) FROM grt_version`)
	calls := map[string]error{}
	_, calls["comment by consumer"] = e.svc.Comment(ctx, c.consumerID, c.principalID, q.ID, "more")
	_, calls["comment by host"] = e.svc.Comment(ctx, "", adm.principalID, q.ID, "more")
	_, calls["withdraw"] = e.svc.Withdraw(ctx, c.consumerID, c.principalID, q.ID)
	_, calls["reject"] = e.svc.Reject(ctx, adm.principalID, q.ID, "")
	_, calls["approve"] = e.svc.Approve(ctx, adm.principalID, q.ID, "", e.applyGrant)
	for name, err := range calls {
		if !errors.Is(err, requests.ErrNotOpen) || kindOf(err) != requests.KindConflict {
			t.Errorf("%s past the TTL: %v, want the not-open conflict", name, err)
		}
	}
	if e.count(`SELECT count(*) FROM req_comment`) != before || e.count(`SELECT count(*) FROM grt_version`) != versions {
		t.Error("a refused call past the TTL left a row behind")
	}
	if got, err := e.svc.Get(ctx, "", q.ID); err != nil || got.Status != requests.StatusOpen {
		t.Errorf("before the sweep: %+v, %v, want still open", got, err)
	}
	if n, err := e.svc.ExpireDue(ctx); err != nil || n != 1 {
		t.Fatalf("expired %d, %v, want 1", n, err)
	}
	if got, err := e.svc.Get(ctx, "", q.ID); err != nil || got.Status != requests.StatusExpired {
		t.Errorf("after the sweep: %+v, %v, want expired", got, err)
	}
}

func TestOpenRequestCapPerConsumer(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a, b := e.consumer("A"), e.consumer("B")
	var ids []string
	for i := 0; i < 5; i++ { // the default is 5
		ids = append(ids, e.create(a, "ask").ID)
	}
	_, err := e.svc.Create(ctx, a.consumerID, a.principalID, requests.NewRequest{Message: "one too many"})
	if kindOf(err) != requests.KindConflict {
		t.Fatalf("6th create: %v, want the conflict", err)
	}
	if n := e.count(`SELECT count(*) FROM req_resource_request WHERE consumer_id = ?`, a.consumerID); n != 5 {
		t.Errorf("%d requests, want 5", n)
	}
	// Another consumer has its own count; a final request does not count; the limit is a setting.
	e.create(b, "ask")
	if _, err := e.svc.Withdraw(ctx, a.consumerID, a.principalID, ids[0]); err != nil {
		t.Fatal(err)
	}
	e.create(a, "ask again")
	e.set(requests.KeyMaxOpen, "6")
	e.create(a, "ask a sixth")
	if _, err := e.svc.Create(ctx, a.consumerID, a.principalID, requests.NewRequest{Message: "seventh"}); kindOf(err) != requests.KindConflict {
		t.Errorf("7th create with a limit of 6: %v", err)
	}
	if err := e.st.Set(ctx, "host", requests.KeyMaxOpen, "", "0"); err == nil {
		t.Error("a limit of 0 was accepted")
	}
}

// Two creates at once cannot both pass the last free place.
func TestOpenRequestCapHoldsUnderConcurrency(t *testing.T) {
	e := newEnv(t)
	c := e.consumer("A")
	e.set(requests.KeyMaxOpen, "3")
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.svc.Create(context.Background(), c.consumerID, c.principalID, requests.NewRequest{Message: "ask"}); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 3 || e.count(`SELECT count(*) FROM req_resource_request`) != 3 {
		t.Errorf("%d creates succeeded, want 3", ok)
	}
}

func TestTextLengthCaps(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c, adm := e.consumer("A"), e.admin("Admin")
	e.set(requests.KeyMaxCommentChars, "10")

	_, err := e.svc.Create(ctx, c.consumerID, c.principalID, requests.NewRequest{Message: strings.Repeat("x", 11)})
	if kindOf(err) != requests.KindInvalid || paramOf(err) != "message" {
		t.Errorf("11 characters of message: %v", err)
	}
	// The limit counts characters, not bytes.
	q, err := e.svc.Create(ctx, c.consumerID, c.principalID, requests.NewRequest{Message: strings.Repeat("é", 10)})
	if err != nil {
		t.Fatalf("10 two-byte characters: %v", err)
	}
	if _, err := e.svc.Comment(ctx, c.consumerID, c.principalID, q.ID, strings.Repeat("é", 11)); kindOf(err) != requests.KindInvalid || paramOf(err) != "body" {
		t.Errorf("11 characters of comment: %v", err)
	}
	if _, err := e.svc.Comment(ctx, "", adm.principalID, q.ID, strings.Repeat("y", 10)); err != nil {
		t.Errorf("10 characters of comment: %v", err)
	}
	for _, empty := range []string{"", "   \n"} {
		if _, err := e.svc.Comment(ctx, "", adm.principalID, q.ID, empty); kindOf(err) != requests.KindInvalid {
			t.Errorf("empty comment %q: %v", empty, err)
		}
		if _, err := e.svc.Create(ctx, c.consumerID, c.principalID, requests.NewRequest{Message: empty}); kindOf(err) != requests.KindInvalid {
			t.Errorf("empty message %q: %v", empty, err)
		}
	}
	// A decision's comment has the same cap and refuses before anything is written.
	if _, err := e.svc.Reject(ctx, adm.principalID, q.ID, strings.Repeat("z", 11)); kindOf(err) != requests.KindInvalid || paramOf(err) != "comment" {
		t.Errorf("long rejection comment: %v", err)
	}
	if got, _ := e.svc.Get(ctx, "", q.ID); got.Status != requests.StatusOpen {
		t.Errorf("status %s after a refused rejection", got.Status)
	}
	// The default is 4,000.
	e2 := newEnv(t)
	c2 := e2.consumer("A")
	if _, err := e2.svc.Create(ctx, c2.consumerID, c2.principalID, requests.NewRequest{Message: strings.Repeat("x", 4000)}); err != nil {
		t.Errorf("4000 characters: %v", err)
	}
	if _, err := e2.svc.Create(ctx, c2.consumerID, c2.principalID, requests.NewRequest{Message: strings.Repeat("x", 4001)}); kindOf(err) != requests.KindInvalid {
		t.Errorf("4001 characters: %v", err)
	}
}

// A consumer sees and changes only its own requests; another's id is not found, never forbidden.
func TestConsumerIsolation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a, b := e.consumer("A"), e.consumer("B")
	qa := e.create(a, "A's ask")
	e.create(b, "B's ask")

	if _, err := e.svc.Get(ctx, b.consumerID, qa.ID); kindOf(err) != requests.KindNotFound {
		t.Errorf("get: %v", err)
	}
	if _, err := e.svc.Comment(ctx, b.consumerID, b.principalID, qa.ID, "hello"); kindOf(err) != requests.KindNotFound {
		t.Errorf("comment: %v", err)
	}
	if _, err := e.svc.Withdraw(ctx, b.consumerID, b.principalID, qa.ID); kindOf(err) != requests.KindNotFound {
		t.Errorf("withdraw: %v", err)
	}
	_, missing := e.svc.Get(ctx, b.consumerID, "0192e5a0-0006-7000-8000-000000000006")
	_, other := e.svc.Get(ctx, b.consumerID, qa.ID)
	if missing == nil || other == nil || missing.Error() != other.Error() {
		t.Errorf("another consumer's id answers differently from an unknown one: %v / %v", other, missing)
	}
	page, err := e.svc.List(ctx, requests.ListQuery{ConsumerID: b.consumerID})
	if err != nil || len(page.Items) != 1 || page.Items[0].ConsumerID != b.consumerID {
		t.Errorf("list of B = %+v, %v", page, err)
	}
	if got, _ := e.svc.Get(ctx, "", qa.ID); got.Status != requests.StatusOpen || len(got.Comments) != 0 {
		t.Errorf("A's request was changed by B: %+v", got)
	}
	// The host sees everybody's.
	if all, _ := e.svc.List(ctx, requests.ListQuery{}); len(all.Items) != 2 {
		t.Errorf("host list = %d, want 2", len(all.Items))
	}
}

func TestApproveIsOneUnitOfWork(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c, adm := e.consumer("A"), e.admin("Admin")
	q := e.create(c, "More please")
	e.clk.advance(time.Hour)

	got, err := e.svc.Approve(ctx, adm.principalID, q.ID, "Approved with limits", e.applyGrant)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != requests.StatusApproved || got.ResultingVersionID == nil || got.DecidedAt == nil || got.ExpiresAt != nil {
		t.Fatalf("approved = %+v", got)
	}
	if len(got.Comments) != 1 || got.Comments[0].Author.Kind != "host_admin" || got.Comments[0].Body != "Approved with limits" {
		t.Errorf("comments = %+v", got.Comments)
	}
	// The version exists, is the grant's current one, and says why it was made.
	var current, reason string
	if err := e.db.Reader().QueryRow(ctx, `SELECT g.current_version_id, v.reason FROM grt_grant g JOIN grt_version v ON v.id = g.current_version_id WHERE g.consumer_id = ?`, c.consumerID).Scan(&current, &reason); err != nil {
		t.Fatal(err)
	}
	if current != *got.ResultingVersionID || reason != "resource_request:"+q.ID {
		t.Errorf("current version %q reason %q, want %q for the request", current, reason, *got.ResultingVersionID)
	}
	// The audit row, the domain event and the sync change are written too.
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = 'resource_request.approved' AND actor_id = ? AND target_id = ?`, adm.principalID, c.principalID); n != 1 {
		t.Errorf("%d approval audit rows, want 1", n)
	}
	if n := e.count(`SELECT count(*) FROM ntf_event WHERE type = 'resource_request.decided' AND target_principal_id = ?`, c.principalID); n != 1 {
		t.Errorf("%d decided events, want 1", n)
	}
	if n := e.count(`SELECT count(*) FROM syn_change WHERE dataset = ? AND pk = ? AND op = 'update' AND version = 2`, requests.Dataset, q.ID); n != 1 {
		t.Errorf("%d approval sync changes, want 1", n)
	}
}

func TestFailedApprovalLeavesNothingBehind(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c, adm := e.consumer("A"), e.admin("Admin")
	q := e.create(c, "More please")
	snapshot := func() [6]int {
		return [6]int{
			e.count(`SELECT count(*) FROM grt_grant`), e.count(`SELECT count(*) FROM grt_version`),
			e.count(`SELECT count(*) FROM req_comment`), e.count(`SELECT count(*) FROM aud_event`),
			e.count(`SELECT count(*) FROM ntf_event`), e.count(`SELECT count(*) FROM syn_change`),
		}
	}
	before := snapshot()

	// The grant function fails after it wrote the version.
	boom := errors.New("grant write failed")
	_, err := e.svc.Approve(ctx, adm.principalID, q.ID, "ok", func(tx *storage.Tx, consumerID, reason string) (string, error) {
		if _, err := e.applyGrant(tx, consumerID, reason); err != nil {
			return "", err
		}
		return "", boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the grant error", err)
	}
	if after := snapshot(); after != before {
		t.Errorf("rows after a failed grant: %v, before: %v", after, before)
	}

	// A step after the grant fails: the request's own update. The grant version it wrote is
	// rolled back with it.
	if err := e.db.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.Exec(`CREATE TRIGGER test_no_approve BEFORE UPDATE OF status ON req_resource_request WHEN NEW.status = 'approved' BEGIN SELECT RAISE(ABORT, 'test: refused'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Approve(ctx, adm.principalID, q.ID, "ok", e.applyGrant); err == nil {
		t.Fatal("approval succeeded with the status update refused")
	}
	if after := snapshot(); after != before {
		t.Errorf("rows after a failed update: %v, before: %v", after, before)
	}
	if got, _ := e.svc.Get(ctx, "", q.ID); got.Status != requests.StatusOpen || got.ResultingVersionID != nil {
		t.Errorf("request = %+v, want it still open", got)
	}
}

func TestRejectWritesTheDecision(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c, adm := e.consumer("A"), e.admin("Admin")
	q := e.create(c, "More please")

	got, err := e.svc.Reject(ctx, adm.principalID, q.ID, "Not now")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != requests.StatusRejected || got.ResultingVersionID != nil || len(got.Comments) != 1 {
		t.Fatalf("rejected = %+v", got)
	}
	if n := e.count(`SELECT count(*) FROM aud_event WHERE action = 'resource_request.rejected'`); n != 1 {
		t.Errorf("%d rejection audit rows", n)
	}
	if n := e.count(`SELECT count(*) FROM grt_version`); n != 0 {
		t.Errorf("a rejection made %d grant versions", n)
	}
	// No comment is also fine.
	q2 := e.create(c, "Again")
	got, err = e.svc.Reject(ctx, adm.principalID, q2.ID, "")
	if err != nil || len(got.Comments) != 0 {
		t.Errorf("reject without a comment = %+v, %v", got, err)
	}
}

func TestExpireDueExpiresOnlyOpenRequestsPastTheTTL(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c, adm := e.consumer("A"), e.admin("Admin")
	e.set(requests.KeyMaxOpen, "20")
	e.set(requests.KeyTTL, "24h")

	old := e.create(c, "old, still open")
	approved := e.create(c, "old, approved")
	withdrawn := e.create(c, "old, withdrawn")
	rejected := e.create(c, "old, rejected")
	if _, err := e.svc.Approve(ctx, adm.principalID, approved.ID, "", e.applyGrant); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Withdraw(ctx, c.consumerID, c.principalID, withdrawn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Reject(ctx, adm.principalID, rejected.ID, ""); err != nil {
		t.Fatal(err)
	}

	// Nothing is due yet: the sweeper does not touch a request inside its TTL.
	e.clk.advance(23 * time.Hour)
	fresh := e.create(c, "opened later")
	if n, err := e.svc.ExpireDue(ctx); err != nil || n != 0 {
		t.Fatalf("before the TTL: %d, %v", n, err)
	}

	e.clk.advance(90 * time.Minute) // old ones are past 24h, the fresh one is 90 minutes old
	n, err := e.svc.ExpireDue(ctx)
	if err != nil || n != 1 {
		t.Fatalf("expired %d, %v, want 1", n, err)
	}
	status := func(id string) requests.Status {
		q, err := e.svc.Get(ctx, "", id)
		if err != nil {
			t.Fatal(err)
		}
		return q.Status
	}
	for id, want := range map[string]requests.Status{old.ID: requests.StatusExpired, approved.ID: requests.StatusApproved,
		withdrawn.ID: requests.StatusWithdrawn, rejected.ID: requests.StatusRejected, fresh.ID: requests.StatusOpen} {
		if got := status(id); got != want {
			t.Errorf("request %s is %s, want %s", id, got, want)
		}
	}
	q, _ := e.svc.Get(ctx, "", old.ID)
	if q.DecidedAt == nil || !q.DecidedAt.Equal(e.clk.now()) || q.ExpiresAt != nil {
		t.Errorf("expired request = %+v", q)
	}
	if n := e.count(`SELECT count(*) FROM syn_change WHERE dataset = ? AND pk = ? AND version = 2`, requests.Dataset, old.ID); n != 1 {
		t.Errorf("%d expiry sync changes, want 1", n)
	}
	// Idempotent: a second pass finds nothing.
	if n, err := e.svc.ExpireDue(ctx); err != nil || n != 0 {
		t.Errorf("second pass: %d, %v", n, err)
	}
	// An expired request is final.
	if _, err := e.svc.Comment(ctx, c.consumerID, c.principalID, old.ID, "still there?"); !errors.Is(err, requests.ErrNotOpen) {
		t.Errorf("comment on an expired request: %v", err)
	}
	// The cap counts only open requests: the expired one made room.
	if n := e.count(`SELECT count(*) FROM req_resource_request WHERE status = 'open'`); n != 1 {
		t.Errorf("%d open requests, want 1", n)
	}
}

func TestSweeperExpiresInTheBackgroundAndStops(t *testing.T) {
	e := newEnv(t)
	c := e.consumer("A")
	e.set(requests.KeyTTL, "1h")
	e.set(requests.KeySweepInterval, "10ms")
	q := e.create(c, "ask")
	e.clk.advance(2 * time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.svc.RunSweeper(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := e.svc.Get(context.Background(), "", q.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == requests.StatusExpired {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the sweeper did not expire the request")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the sweeper did not stop")
	}
}

func TestSettingsAreHostOwnedWithTheDecidedDefaults(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for key, want := range map[string]string{requests.KeyTTL: "336h", requests.KeyMaxOpen: "5", requests.KeyMaxCommentChars: "4000",
		requests.KeySweepInterval: "1m", requests.KeyPageSize: "50"} {
		def, ok := e.st.Definition(key)
		if !ok || def.Default != want || def.Owner != settings.Host || def.Description == "" {
			t.Errorf("%s = %+v, want default %s owned by the host", key, def, want)
		}
	}
	// A TTL change applies to requests opened after it, and a request keeps its end time.
	c := e.consumer("A")
	first := e.create(c, "ask")
	e.set(requests.KeyTTL, "1h")
	second := e.create(c, "ask")
	if !first.ExpiresAt.Equal(t0.Add(336*time.Hour)) || !second.ExpiresAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("end times %v and %v", first.ExpiresAt, second.ExpiresAt)
	}
	if err := e.st.Set(ctx, settings.Host, requests.KeyTTL, "", "0s"); err == nil {
		t.Error("a TTL of 0s was accepted")
	}
}

// The consumer's text stays in the request and comment rows. It never reaches the log, an audit
// detail, a domain event or a sync change (req_001 D-31, D-40), also when a call fails.
func TestTheTextNeverLeavesItsRows(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	c, adm := e.consumer("A"), e.admin("Admin")
	q, err := e.svc.Create(ctx, c.consumerID, c.principalID, requests.NewRequest{Message: hostile, Ask: []byte(`{"models":["m"]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Comment(ctx, c.consumerID, c.principalID, q.ID, hostile); err != nil {
		t.Fatal(err)
	}
	e.set(requests.KeyMaxCommentChars, "5")
	_, err = e.svc.Comment(ctx, "", adm.principalID, q.ID, hostile) // refused: too long
	if err == nil {
		t.Fatal("a long comment was accepted")
	}
	mustNot(t, err.Error(), "an error")
	e.set(requests.KeyMaxCommentChars, "4000")
	if _, err := e.svc.Approve(ctx, adm.principalID, q.ID, hostile, e.applyGrant); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Comment(ctx, "", adm.principalID, q.ID, hostile) // refused: final
	if err == nil {
		t.Fatal("a comment on an approved request was accepted")
	}
	mustNot(t, err.Error(), "an error")

	for table, col := range map[string]string{"aud_event": "detail", "ntf_event": "payload", "syn_change": "payload"} {
		rows, err := e.db.Reader().Query(ctx, `SELECT CAST(`+col+` AS TEXT) FROM `+table)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for rows.Next() {
			var text string
			if err := rows.Scan(&text); err != nil {
				t.Fatal(err)
			}
			mustNot(t, text, table)
			n++
		}
		rows.Close()
		if n == 0 {
			t.Errorf("%s has no row to check", table)
		}
	}
	mustNot(t, e.log.String(), "the log")
}
