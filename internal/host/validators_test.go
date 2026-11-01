package host_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	_ "time/tzdata" // so LoadLocation behaves the same on every CI OS

	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func TestQuietHoursValidators(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"), settings.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := settings.New(db)
	for _, d := range host.Defs() {
		if err := st.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	if err := host.RegisterValidators(st); err != nil {
		t.Fatal(err)
	}
	get := func(key string) string {
		t.Helper()
		v, err := st.String(ctx, key, "")
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct {
		key  string
		bad  []string
		good string
	}{
		{host.KeyQuietStart, []string{"25:00", "9:5", ""}, "21:30"},
		{host.KeyQuietEnd, []string{"25:00", "9:5", ""}, "06:15"},
		{host.KeyQuietTimeZone, []string{"Mars/Olympus", ""}, "Europe/Berlin"},
	} {
		before := get(c.key)
		for _, v := range c.bad {
			if err := st.Set(ctx, settings.Host, c.key, "", v); !errors.Is(err, settings.ErrRejected) {
				t.Errorf("%s: Set(%q) = %v; want ErrRejected", c.key, v, err)
			}
			if got := get(c.key); got != before {
				t.Errorf("%s: value %q after refused Set(%q); want %q", c.key, got, v, before)
			}
		}
		if err := st.Set(ctx, settings.Host, c.key, "", c.good); err != nil {
			t.Errorf("%s: Set(%q) = %v", c.key, c.good, err)
		}
		if got := get(c.key); got != c.good {
			t.Errorf("%s = %q, want %q", c.key, got, c.good)
		}
		if err := st.Reset(ctx, settings.Host, c.key, ""); err != nil {
			t.Errorf("%s: Reset = %v", c.key, err)
		}
		if got := get(c.key); got != before {
			t.Errorf("%s after Reset = %q, want default %q", c.key, got, before)
		}
	}
	if err := st.Set(ctx, settings.Host, host.KeyQuietTimeZone, "", "Local"); err != nil {
		t.Errorf("Set(Local) = %v", err)
	}
}
