package settings

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/inCoderKaka/local-server/internal/storage"
)

func applyStore(t *testing.T) *Store {
	t.Helper()
	s := newStore(t)
	if err := s.Register(
		Def{Key: "z.name", Kind: String, Default: "x", Owner: Host},
		Def{Key: "a.count", Kind: Int, Default: "8", Owner: Host},
		Def{Key: "m.flag", Kind: Bool, Default: "false", Owner: Host},
		Def{Key: "c.other", Kind: Int, Default: "1", Owner: Consumer},
		Def{Key: "d.scoped", Kind: Duration, Default: "1h", Owner: Host, Scoped: true},
	); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDefinitionsSortedAndCopied(t *testing.T) {
	s := applyStore(t)
	defs := s.Definitions()
	var keys []string
	for _, d := range defs {
		keys = append(keys, d.Key)
	}
	for i := 1; i < len(keys); i++ {
		if keys[i-1] >= keys[i] {
			t.Fatalf("keys not sorted: %v", keys)
		}
	}
	defs[0].Key = "changed"
	if s.Definitions()[0].Key == "changed" {
		t.Fatal("Definitions returned the store's own slice")
	}
}

func TestApplyWritesAllAndReportsChanges(t *testing.T) {
	ctx := context.Background()
	s := applyStore(t)
	var seen []Change
	got, err := s.Apply(ctx, Host, []Update{{Key: "z.name", Value: "hello"}, {Key: "a.count", Value: "8"}, {Key: "m.flag", Value: "true"}},
		func(tx *storage.Tx, ch []Change) error { seen = ch; return nil })
	if err != nil {
		t.Fatal(err)
	}
	want := []Change{{"m.flag", "false", "true"}, {"z.name", "x", "hello"}} // a.count: 8 is already the default
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(seen, want) {
		t.Fatalf("changes = %v, callback saw %v; want %v", got, seen, want)
	}
	if v, _ := s.String(ctx, "z.name", ""); v != "hello" {
		t.Errorf("z.name = %q", v)
	}
	if b, _ := s.Bool(ctx, "m.flag", ""); !b {
		t.Error("m.flag not set")
	}
	// Reset is a change to the default; resetting what is already default is not a change.
	got, err = s.Apply(ctx, Host, []Update{{Key: "z.name", Reset: true}, {Key: "a.count", Reset: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Change{{"z.name", "hello", "x"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("reset changes = %v, want %v", got, want)
	}
	if v, _ := s.String(ctx, "z.name", ""); v != "x" {
		t.Errorf("z.name after reset = %q", v)
	}
}

func TestApplyOneBadKeyAppliesNothing(t *testing.T) {
	ctx := context.Background()
	s := applyStore(t)
	for name, bad := range map[string]Update{
		"unknown":    {Key: "nope", Value: "1"},
		"not owner":  {Key: "c.other", Value: "2"},
		"bad value":  {Key: "a.count", Value: "many"},
		"scoped key": {Key: "d.scoped", Value: "1h"},
	} {
		_, err := s.Apply(ctx, Host, []Update{{Key: "z.name", Value: "changed"}, bad}, nil)
		var ue *UpdateError
		if !errors.As(err, &ue) || ue.Key != bad.Key {
			t.Errorf("%s: err = %v; want an *UpdateError for %s", name, err, bad.Key)
		}
		if v, _ := s.String(ctx, "z.name", ""); v != "x" {
			t.Errorf("%s: the good key was applied (%q)", name, v)
		}
	}
	_, err := s.Apply(ctx, Host, []Update{{Key: "c.other", Value: "2"}}, nil)
	if !errors.Is(err, ErrNotOwner) {
		t.Errorf("not owner = %v", err)
	}
	_, err = s.Apply(ctx, Host, []Update{{Key: "nope", Value: "2"}}, nil)
	if !errors.Is(err, ErrUnknownKey) {
		t.Errorf("unknown = %v", err)
	}
	_, err = s.Apply(ctx, Host, []Update{{Key: "a.count", Value: "x"}}, nil)
	if !errors.Is(err, ErrInvalidValue) {
		t.Errorf("bad value = %v", err)
	}
	_, err = s.Apply(ctx, Host, []Update{{Key: "a.count", Value: "1"}, {Key: "a.count", Value: "2"}}, nil)
	if !errors.Is(err, ErrInvalidValue) {
		t.Errorf("duplicate key = %v", err)
	}
}

func TestApplyValidatorsRefuseTheWholeBatch(t *testing.T) {
	ctx := context.Background()
	s := applyStore(t)
	if err := s.SetValidator("a.count", func(_ context.Context, v string) error {
		if v == "13" {
			return errors.New("unlucky")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTxValidator("m.flag", func(_ *storage.Tx, v string) error {
		if v == "true" {
			return errors.New("not now")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Apply(ctx, Host, []Update{{Key: "z.name", Value: "changed"}, {Key: "a.count", Value: "13"}}, nil)
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("validator: %v", err)
	}
	_, err = s.Apply(ctx, Host, []Update{{Key: "z.name", Value: "changed"}, {Key: "m.flag", Value: "true"}}, nil)
	var ue *UpdateError
	if !errors.Is(err, ErrRejected) || !errors.As(err, &ue) || ue.Key != "m.flag" {
		t.Fatalf("tx validator: %v", err)
	}
	if v, _ := s.String(ctx, "z.name", ""); v != "x" {
		t.Errorf("a refused batch wrote z.name = %q", v)
	}
}

// The callback is in the same unit of work: its error rolls the settings back, and its writes
// roll back with them.
func TestApplyCallbackSharesTheUnitOfWork(t *testing.T) {
	ctx := context.Background()
	s := applyStore(t)
	boom := fmt.Errorf("audit failed")
	_, err := s.Apply(ctx, Host, []Update{{Key: "z.name", Value: "changed"}}, func(tx *storage.Tx, _ []Change) error {
		if _, err := tx.Exec(`INSERT INTO tst_event (id, at, kind) VALUES (1, 1, 'x')`); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if v, _ := s.String(ctx, "z.name", ""); v != "x" {
		t.Errorf("z.name = %q after a failed callback", v)
	}
	var n int
	if err := s.db.Reader().QueryRow(ctx, `SELECT count(*) FROM tst_event`).Scan(&n); err != nil || n != 0 {
		t.Errorf("the callback's row survived the rollback: %d, %v", n, err)
	}
	// And a good callback commits with the setting.
	if _, err := s.Apply(ctx, Host, []Update{{Key: "z.name", Value: "changed"}}, func(tx *storage.Tx, _ []Change) error {
		_, err := tx.Exec(`INSERT INTO tst_event (id, at, kind) VALUES (1, 1, 'x')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Reader().QueryRow(ctx, `SELECT count(*) FROM tst_event`).Scan(&n); err != nil || n != 1 {
		t.Errorf("committed callback rows = %d, %v", n, err)
	}
}
