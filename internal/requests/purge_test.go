package requests_test

import (
	"context"
	"testing"

	"github.com/inCoderKaka/local-server/internal/requests"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// The purge of a deleted consumer removes its requests and every comment on them, the host's too,
// leaves another consumer's rows alone, and leaves delete changes with ids only (req_001 D-136).
func TestPurgeConsumerRemovesItsRequestsAndComments(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a, b, adm := e.consumer("A"), e.consumer("B"), e.admin("Admin")

	q1, err := e.svc.Create(ctx, a.consumerID, a.principalID, requests.NewRequest{Message: hostile, Ask: []byte(`{"models":["m"]}`)})
	if err != nil {
		t.Fatal(err)
	}
	q2 := e.create(a, hostile+" again")
	if _, err := e.svc.Comment(ctx, a.consumerID, a.principalID, q1.ID, hostile); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Comment(ctx, "", adm.principalID, q1.ID, hostile); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Reject(ctx, adm.principalID, q2.ID, hostile); err != nil {
		t.Fatal(err)
	}
	other := e.create(b, "B asks")
	if _, err := e.svc.Comment(ctx, b.consumerID, b.principalID, other.ID, "B comments"); err != nil {
		t.Fatal(err)
	}

	var n int64
	if err := e.db.Write(ctx, func(tx *storage.Tx) error {
		var err error
		n, err = e.svc.PurgeConsumerTx(tx, a.consumerID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("purged %d requests, want 2", n)
	}
	if c := e.count(`SELECT count(*) FROM req_resource_request WHERE consumer_id = ?`, a.consumerID); c != 0 {
		t.Errorf("%d requests left", c)
	}
	if c := e.count(`SELECT count(*) FROM req_comment WHERE request_id IN (?, ?)`, q1.ID, q2.ID); c != 0 {
		t.Errorf("%d comments left", c)
	}
	if c := e.count(`SELECT count(*) FROM req_resource_request WHERE id = ?`, other.ID); c != 1 {
		t.Errorf("the other consumer's request changed: %d rows", c)
	}
	if c := e.count(`SELECT count(*) FROM req_comment WHERE request_id = ?`, other.ID); c < 1 {
		t.Error("the other consumer's comments are gone")
	}
	for _, id := range []string{q1.ID, q2.ID} {
		if c := e.count(`SELECT count(*) FROM syn_change WHERE dataset = ? AND pk = ? AND op = 'delete' AND version > 1`, requests.Dataset, id); c != 1 {
			t.Errorf("%d delete changes for %s, want 1", c, id)
		}
	}
	if c := e.count(`SELECT count(*) FROM syn_change WHERE dataset = ? AND pk = ?`, requests.Dataset, other.ID); c != 1 {
		t.Errorf("%d changes for the other consumer's request, want only its insert", c)
	}

	// A second purge finds nothing.
	if err := e.db.Write(ctx, func(tx *storage.Tx) error {
		var err error
		n, err = e.svc.PurgeConsumerTx(tx, a.consumerID)
		return err
	}); err != nil || n != 0 {
		t.Errorf("second purge = %d, %v; want 0", n, err)
	}

	rows, err := e.db.Reader().Query(ctx, `SELECT CAST(payload AS TEXT) FROM syn_change WHERE op = 'delete'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			t.Fatal(err)
		}
		mustNot(t, text, "syn_change")
		seen++
	}
	if seen != 2 {
		t.Errorf("%d delete changes to check, want 2", seen)
	}
	mustNot(t, e.log.String(), "the log")
}
