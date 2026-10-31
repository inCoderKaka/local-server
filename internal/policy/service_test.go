package policy_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/inCoderKaka/local-server/internal/policy"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

type fakeSource struct {
	rules []policy.StoredRule
	asked []string
	err   error
}

func (f *fakeSource) Rules(_ context.Context, grantID string) ([]policy.StoredRule, error) {
	f.asked = append(f.asked, grantID)
	return f.rules, f.err
}

func newService(t *testing.T, src policy.RuleSource) (*policy.Service, *settings.Store) {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"), settings.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := settings.New(db)
	s, err := policy.New(st, src)
	if err != nil {
		t.Fatal(err)
	}
	return s, st
}

func TestServiceEvaluate(t *testing.T) {
	ctx := context.Background()
	src := &fakeSource{rules: []policy.StoredRule{global("bg", 0, backgroundHours)}}
	s, st := newService(t, src)
	in := policy.Input{Properties: []string{"background"}, Model: "llama3.1:8b", Time: at(3, 0), Grant: grant()}
	d, err := s.Evaluate(ctx, in)
	if err != nil || d.Priority != policy.Background {
		t.Fatalf("got %+v, %v", d, err)
	}
	if len(src.asked) != 1 || src.asked[0] != "g1" {
		t.Fatalf("source asked for %v", src.asked)
	}
	in.Time = at(9, 0)
	if _, err := s.Evaluate(ctx, in); !errors.Is(err, policy.ErrOutsideWindow) {
		t.Fatalf("outside window: %v", err)
	}

	// The limits are read from settings on every call.
	in.Time = at(3, 0)
	if err := st.Set(ctx, settings.Host, policy.KeyPropertiesMaxCount, "", "0"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Evaluate(ctx, in); !errors.Is(err, policy.ErrInvalidProperties) {
		t.Fatalf("max_count 0: %v", err)
	}
	if err := st.Set(ctx, settings.Host, policy.KeyPropertiesMaxCount, "", "16"); err != nil {
		t.Fatal(err)
	}
	if err := st.Set(ctx, settings.Host, policy.KeyRuleMaxBytes, "", "10"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Evaluate(ctx, in); !errors.Is(err, policy.ErrInvalidRule) {
		t.Fatalf("rule max_bytes 10: %v", err)
	}

	boom := errors.New("boom")
	src.err = boom
	if _, err := s.Evaluate(ctx, in); !errors.Is(err, boom) {
		t.Fatalf("source error: %v", err)
	}
}

func TestDefaults(t *testing.T) {
	_, st := newService(t, &fakeSource{})
	lim, err := policy.LoadLimits(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if lim.Properties.MaxBytes != 1024 || lim.Properties.MaxCount != 16 || lim.RuleMaxBytes != 8192 {
		t.Fatalf("defaults = %+v", lim)
	}
}
