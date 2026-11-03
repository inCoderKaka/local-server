package credential

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func newDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"), settings.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newStore(t *testing.T) *settings.Store { return settings.New(newDB(t)) }

func newHasher(t *testing.T) (*Hasher, *settings.Store) {
	t.Helper()
	st := newStore(t)
	h, err := NewHasher(st)
	if err != nil {
		t.Fatal(err)
	}
	return h, st
}

func TestHashRoundTrip(t *testing.T) {
	ctx := context.Background()
	h, _ := newHasher(t)
	enc, err := h.Hash(ctx, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("hash = %q; want the PHC form at OWASP's minimum cost", enc)
	}
	if strings.Contains(enc, "correct") {
		t.Fatal("the hash contains the password")
	}
	if ok, err := Verify("correct horse battery", enc); err != nil || !ok {
		t.Fatalf("Verify(right) = %v, %v", ok, err)
	}
	if ok, err := Verify("correct horse batterz", enc); err != nil || ok {
		t.Fatalf("Verify(wrong) = %v, %v", ok, err)
	}
	other, _ := h.Hash(ctx, "correct horse battery")
	if other == enc {
		t.Fatal("two hashes of one password are equal: the salt is not random")
	}
}

// Verify reads the cost from the stored hash, so a hash made before a cost change still works.
func TestVerifyUsesParamsOfTheStoredHash(t *testing.T) {
	ctx := context.Background()
	h, st := newHasher(t)
	old, err := h.Hash(ctx, "a password of some length")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set(ctx, settings.Host, KeyIterations, "", "3"); err != nil {
		t.Fatal(err)
	}
	fresh, err := h.Hash(ctx, "a password of some length")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fresh, "t=3") || !strings.Contains(old, "t=2") {
		t.Fatalf("costs: old %q, new %q", old, fresh)
	}
	for _, enc := range []string{old, fresh} {
		if ok, err := Verify("a password of some length", enc); err != nil || !ok {
			t.Errorf("Verify(%.30s...) = %v, %v", enc, ok, err)
		}
	}
}

func TestCostSettingsRefuseBelowOWASPMinimumAndAboveMaximum(t *testing.T) {
	ctx := context.Background()
	_, st := newHasher(t)
	for _, c := range []struct{ key, bad, good string }{
		{KeyMemoryKiB, "19455", "19456"},
		{KeyMemoryKiB, "1048577", "32768"},
		{KeyIterations, "1", "3"},
		{KeyIterations, "65", "4"},
		{KeyParallelism, "0", "2"},
		{KeyParallelism, "65", "4"},
		{KeyMinLength, "0", "8"},
	} {
		err := st.Set(ctx, settings.Host, c.key, "", c.bad)
		if !errors.Is(err, settings.ErrRejected) {
			t.Errorf("Set(%s, %s) = %v; want ErrRejected", c.key, c.bad, err)
		}
		if err := st.Set(ctx, settings.Host, c.key, "", c.good); err != nil {
			t.Errorf("Set(%s, %s) = %v", c.key, c.good, err)
		}
	}
	// A reset to the default is at the minimum, so it is accepted.
	if err := st.Reset(ctx, settings.Host, KeyMemoryKiB, ""); err != nil {
		t.Errorf("Reset: %v", err)
	}
}

// A stored value written around the validators is clamped, never used as given.
func TestCurrentParamsClampsAStoredValue(t *testing.T) {
	ctx := context.Background()
	// A Store without the validators writes the value, as a row written some other way would.
	db := newDB(t)
	st2 := settings.New(db)
	if err := st2.Register(PasswordDefs()...); err != nil {
		t.Fatal(err)
	}
	if err := st2.Set(ctx, settings.Host, KeyMemoryKiB, "", "1"); err != nil {
		t.Fatal(err)
	}
	h := &Hasher{st: st2}
	p, err := h.CurrentParams(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.MemoryKiB != MinMemoryKiB {
		t.Fatalf("memory = %d, want the clamp to %d", p.MemoryKiB, MinMemoryKiB)
	}
}

func TestVerifyRefusesMalformedHashes(t *testing.T) {
	good, err := HashWith("pw", Params{MemoryKiB: 64, Iterations: 1, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(good, "$")
	with := func(i int, v string) string {
		p := append([]string(nil), parts...)
		p[i] = v
		return strings.Join(p, "$")
	}
	for name, enc := range map[string]string{
		"empty":            "",
		"no dollar":        "argon2id",
		"wrong algorithm":  with(1, "argon2i"),
		"wrong version":    with(2, "v=16"),
		"missing param":    with(3, "m=64,t=1"),
		"reordered params": with(3, "t=1,m=64,p=1"),
		"huge memory":      with(3, "m=4294967295,t=1,p=1"),
		"huge iterations":  with(3, "m=64,t=4294967295,p=1"),
		"huge parallelism": with(3, "m=64,t=1,p=255"),
		"zero iterations":  with(3, "m=64,t=0,p=1"),
		"zero parallelism": with(3, "m=64,t=1,p=0"),
		"memory below 8p":  with(3, "m=4,t=1,p=1"),
		"signed number":    with(3, "m=+64,t=1,p=1"),
		"not base64 salt":  with(4, "!!!!!!!!!!!!"),
		"short salt":       with(4, "AAAA"),
		"short key":        with(5, "AAAA"),
		"extra field":      good + "$x",
		"long salt":        with(4, strings.Repeat("A", 200)),
	} {
		if ok, err := Verify("pw", enc); ok || !errors.Is(err, ErrMalformedHash) {
			t.Errorf("%s: Verify = %v, %v; want ErrMalformedHash", name, ok, err)
		}
	}
	if ok, err := Verify("pw", good); err != nil || !ok {
		t.Fatalf("the unmodified hash does not verify: %v, %v", ok, err)
	}
}

func TestHashWithRefusesUnsafeCost(t *testing.T) {
	for _, p := range []Params{{0, 1, 1}, {64, 0, 1}, {64, 1, 0}, {MaxMemoryKiB + 1, 1, 1}, {64, MaxIterations + 1, 1}} {
		if _, err := HashWith("pw", p); !errors.Is(err, ErrMalformedHash) {
			t.Errorf("HashWith(%+v) = %v; want an error", p, err)
		}
	}
}

func TestPasswordPolicy(t *testing.T) {
	ctx := context.Background()
	h, st := newHasher(t)
	if err := h.CheckPolicy(ctx, "short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("short password = %v", err)
	}
	if err := h.CheckPolicy(ctx, "exactly12chr"); err != nil {
		t.Errorf("12 characters = %v", err)
	}
	// Characters, not bytes: 12 two-byte runes pass, 11 do not.
	if err := h.CheckPolicy(ctx, strings.Repeat("é", 12)); err != nil {
		t.Errorf("12 runes = %v", err)
	}
	if err := h.CheckPolicy(ctx, strings.Repeat("é", 11)); !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("11 runes = %v", err)
	}
	if err := st.Set(ctx, settings.Host, KeyMinLength, "", "20"); err != nil {
		t.Fatal(err)
	}
	if err := h.CheckPolicy(ctx, "exactly12chr"); !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("setting 20, 12 characters = %v", err)
	}
}

// Burn costs about what Verify costs, so an unknown login name is not faster than a wrong
// password. The check is loose: a timing test must not be flaky.
func TestBurnCostsAboutAsMuchAsVerify(t *testing.T) {
	ctx := context.Background()
	h, _ := newHasher(t)
	enc, err := h.Hash(ctx, "a password of some length")
	if err != nil {
		t.Fatal(err)
	}
	timeit := func(f func()) time.Duration {
		best := time.Hour
		for range 3 {
			start := time.Now()
			f()
			best = min(best, time.Since(start))
		}
		return best
	}
	verify := timeit(func() { Verify("another password", enc) })
	burn := timeit(func() { h.Burn(ctx, "another password") })
	if burn < verify/3 || burn > verify*3 {
		t.Errorf("Burn took %v and Verify %v; want the same order of magnitude", burn, verify)
	}
}
