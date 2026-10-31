package checkpoint

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/mod/sumdb/tlog"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/usage"
)

var t0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// fakeClock fires its timers only when the test says so.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers chan *fakeTimer
}

type fakeTimer struct {
	c       chan time.Time
	d       time.Duration
	stopped bool
}

func (t *fakeTimer) C() <-chan time.Time { return t.c }
func (t *fakeTimer) Stop() bool          { t.stopped = true; return true }

func newFakeClock() *fakeClock { return &fakeClock{now: t0, timers: make(chan *fakeTimer, 16)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *fakeClock) NewTimer(d time.Duration) Timer {
	t := &fakeTimer{c: make(chan time.Time, 1), d: d}
	c.timers <- t
	return t
}

// countingKeys wraps the real keys and counts EnsureCurrent calls.
type countingKeys struct {
	*hostkeys.Keys
	mu     sync.Mutex
	ensure int
}

func (k *countingKeys) EnsureCurrent(ctx context.Context) ([]hostkeys.Purpose, error) {
	k.mu.Lock()
	k.ensure++
	k.mu.Unlock()
	return k.Keys.EnsureCurrent(ctx)
}

func (k *countingKeys) count() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.ensure
}

type env struct {
	db    *storage.DB
	keys  *countingKeys
	st    *settings.Store
	l     *ledger.Ledger
	svc   *Service
	clock *fakeClock
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"),
		settings.Module(), host.Module(), usage.Module(), ledger.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	master := make([]byte, hostkeys.KeySize)
	if _, err := rand.Read(master); err != nil {
		t.Fatal(err)
	}
	st := settings.New(db)
	hk, _, err := hostkeys.Open(ctx, db, st, master)
	if err != nil {
		t.Fatal(err)
	}
	keys := &countingKeys{Keys: hk}
	clock := newFakeClock()
	svc, err := newService(db, keys, st, clock)
	if err != nil {
		t.Fatal(err)
	}
	return &env{db: db, keys: keys, st: st, l: ledger.New(db, hk), svc: svc, clock: clock}
}

func sample(i int) ledger.Usage {
	return ledger.Usage{
		UsageID:          ledger.NewUsageID(),
		ConsumerID:       "11111111-1111-4111-8111-111111111111",
		AccessKeyID:      "22222222-2222-4222-8222-222222222222",
		GrantVersionID:   "33333333-3333-4333-8333-333333333333",
		Operation:        ledger.OpChatCompletions,
		Model:            "llama3.2:3b",
		Outcome:          ledger.OutcomeCompleted,
		PromptTokens:     int64(10 + i),
		CompletionTokens: int64(20 + i),
		StartedAt:        t0,
		FinishedAt:       t0.Add(time.Second),
	}
}

func (e *env) record(t *testing.T, n int) []ledger.Recorded {
	t.Helper()
	var out []ledger.Recorded
	for i := 0; i < n; i++ {
		r, err := e.l.Record(context.Background(), sample(i))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func (e *env) build(t *testing.T) Checkpoint {
	t.Helper()
	cp, made, err := e.svc.Build(context.Background())
	if err != nil || !made {
		t.Fatalf("Build = %v, made %v", err, made)
	}
	return cp
}

func (e *env) verifierKeys(t *testing.T) []string {
	t.Helper()
	ks, err := e.svc.KeySet(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return ks.VerifierKeys()
}

// staleKeys returns a stale key list on its first PublicKeys call and the live one after.
type staleKeys struct {
	Keys
	stale []hostkeys.PublicKey
	calls int
}

func (k *staleKeys) PublicKeys(ctx context.Context) ([]hostkeys.PublicKey, error) {
	k.calls++
	if k.calls == 1 {
		return k.stale, nil
	}
	return k.Keys.PublicKeys(ctx)
}

func TestBuildSignsWithACheckpointKeyRotatedAfterTheSnapshot(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.record(t, 1)
	stale, err := e.keys.PublicKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	newID, err := e.keys.Rotate(ctx, hostkeys.Checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	orig := e.svc.keys
	sk := &staleKeys{Keys: orig, stale: stale}
	e.svc.keys = sk
	cp := e.build(t)
	e.svc.keys = orig
	if sk.calls != 2 {
		t.Fatalf("PublicKeys called %d times; want 2", sk.calls)
	}
	if cp.KeyID != newID {
		t.Fatalf("checkpoint key %s; want the rotated key %s", cp.KeyID, newID)
	}
	if _, err := Verify(cp.Note, e.verifierKeys(t)...); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestInclusionProofVerifiesAgainstLaterCheckpoint(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	recs := e.record(t, 3)
	// The receipt of leaf 1 says tree_size 2; no checkpoint exists yet.
	if recs[1].TreeSize != 2 {
		t.Fatalf("receipt tree size = %d", recs[1].TreeSize)
	}
	e.record(t, 4)
	cp := e.build(t) // covers 7 leaves: later than the receipt's own tree size
	if cp.TreeSize != 7 {
		t.Fatalf("checkpoint size = %d", cp.TreeSize)
	}
	e.record(t, 2)
	e.build(t) // a newer checkpoint must not change which one is "first covering"

	p, err := e.svc.ProofForUsage(ctx, recs[1].UsageID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Checkpoint.TreeSize != 7 || p.LeafIndex != 1 {
		t.Fatalf("proof = leaf %d against size %d; want leaf 1 against the first covering checkpoint, size 7", p.LeafIndex, p.Checkpoint.TreeSize)
	}
	if err := tlog.CheckRecord(p.Proof, p.Checkpoint.TreeSize, p.Checkpoint.RootHash, p.LeafIndex, p.LeafHash); err != nil {
		t.Fatalf("CheckRecord: %v", err)
	}
	// The leaf hash is the hash of the canonical leaf the receipt's fields rebuild.
	leaf, err := ledger.CanonicalLeaf(sample2(recs[1], 1))
	if err != nil {
		t.Fatal(err)
	}
	if want := tlog.RecordHash(leaf); want != p.LeafHash {
		t.Fatal("leaf hash is not the hash of the canonical leaf")
	}
	// A wrong leaf hash must fail.
	bad := p.LeafHash
	bad[0] ^= 1
	if tlog.CheckRecord(p.Proof, p.Checkpoint.TreeSize, p.Checkpoint.RootHash, p.LeafIndex, bad) == nil {
		t.Fatal("CheckRecord accepted a wrong leaf hash")
	}
	if got := len(EncodeHashes(p.Proof)); got != len(p.Proof) || got == 0 {
		t.Fatalf("EncodeHashes returned %d of %d", got, len(p.Proof))
	}
	// The stored note verifies and matches the stored root.
	v, err := Verify(p.Checkpoint.Note, e.verifierKeys(t)...)
	if err != nil || v.TreeSize != 7 || v.RootHash != p.Checkpoint.RootHash {
		t.Fatalf("Verify = %+v, %v", v, err)
	}
}

// sample2 rebuilds the Usage of record i (sample is deterministic apart from the usage id).
func sample2(r ledger.Recorded, i int) ledger.Usage {
	u := sample(i)
	u.UsageID = r.UsageID
	return u
}

func TestConsistencyProofBetweenCheckpoints(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.record(t, 3)
	old := e.build(t)
	e.record(t, 5)
	cur := e.build(t)
	got, proof, err := e.svc.Consistency(ctx, old.TreeSize)
	if err != nil || got.TreeSize != cur.TreeSize {
		t.Fatalf("Consistency = %v, %v", got.TreeSize, err)
	}
	if err := tlog.CheckTree(proof, cur.TreeSize, cur.RootHash, old.TreeSize, old.RootHash); err != nil {
		t.Fatalf("CheckTree: %v", err)
	}
	if tlog.CheckTree(proof, cur.TreeSize, cur.RootHash, old.TreeSize, tlog.Hash{1}) == nil {
		t.Fatal("CheckTree accepted a wrong old root")
	}
	for _, from := range []int64{0, -1, cur.TreeSize + 1} {
		if _, _, err := e.svc.Consistency(ctx, from); !errors.Is(err, ErrBadTreeSize) {
			t.Fatalf("Consistency(%d) = %v; want ErrBadTreeSize", from, err)
		}
	}
}

func TestNoteVerifiesWithPublishedKeyAndFailsWhenTampered(t *testing.T) {
	e := newEnv(t)
	e.record(t, 4)
	cp := e.build(t)
	vks := e.verifierKeys(t)
	if len(vks) != 1 {
		t.Fatalf("want 1 published checkpoint verifier key, got %d", len(vks))
	}
	v, err := Verify(cp.Note, vks...)
	if err != nil {
		t.Fatal(err)
	}
	wantOrigin := "local-server/ledger/" + e.keys.HostID()
	if v.Origin != wantOrigin || v.KeyName != wantOrigin || v.TreeSize != 4 || v.RootHash != cp.RootHash {
		t.Fatalf("Verify = %+v", v)
	}
	// Shape: text lines, blank line, one em dash signature line.
	lines := strings.Split(cp.Note, "\n")
	if len(lines) != 6 || lines[3] != "" || !strings.HasPrefix(lines[4], "— "+wantOrigin+" ") || lines[5] != "" {
		t.Fatalf("note shape: %q", cp.Note)
	}

	tampered := []string{
		strings.Replace(cp.Note, "\n4\n", "\n5\n", 1),                      // size
		strings.Replace(cp.Note, wantOrigin+"\n", "other\n", 1),            // origin
		cp.Note[:len(cp.Note)-6] + "AAAA\n",                                // signature bytes
		strings.Replace(cp.Note, lines[2], strings.Repeat("A", 43)+"=", 1), // root
	}
	for i, n := range tampered {
		if _, err := Verify(n, vks...); err == nil {
			t.Fatalf("tampered note %d verified", i)
		}
	}
	// A key of another host does not verify it.
	other := newEnv(t)
	if _, err := Verify(cp.Note, other.verifierKeys(t)...); err == nil {
		t.Fatal("note verified with another host's key")
	}
	if _, err := Verify(cp.Note); !errors.Is(err, ErrBadNote) {
		t.Fatalf("no keys = %v; want ErrBadNote", err)
	}
}

func TestRetiredCheckpointKeyStillVerifiesOldNotes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.record(t, 2)
	old := e.build(t)
	oldKey := e.keys.CurrentKeyID(hostkeys.Checkpoint)
	if _, err := e.keys.Rotate(ctx, hostkeys.Checkpoint); err != nil {
		t.Fatal(err)
	}
	e.record(t, 2)
	cur := e.build(t)
	if cur.KeyID == oldKey || old.KeyID != oldKey {
		t.Fatalf("key ids: old note %s, new note %s, retired %s", old.KeyID, cur.KeyID, oldKey)
	}
	ks, err := e.svc.KeySet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	vks := ks.VerifierKeys()
	if len(vks) != 2 {
		t.Fatalf("published checkpoint keys = %d; want current and retired", len(vks))
	}
	for _, cp := range []Checkpoint{old, cur} {
		if _, err := Verify(cp.Note, vks...); err != nil {
			t.Fatalf("size %d: %v", cp.TreeSize, err)
		}
	}
	// Each key alone verifies only its own notes.
	var retired, live JWK
	for _, j := range ks.Keys {
		if j.LsRole == "checkpoint" && j.Kid == oldKey {
			retired = j
		} else if j.LsRole == "checkpoint" {
			live = j
		}
	}
	if retired.LsRetiredAt == nil || live.LsRetiredAt != nil {
		t.Fatalf("retired_at: retired %v live %v", retired.LsRetiredAt, live.LsRetiredAt)
	}
	if _, err := Verify(old.Note, *retired.LsNoteVerifierKey); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(old.Note, *live.LsNoteVerifierKey); err == nil {
		t.Fatal("old note verified with the new key")
	}
	if _, err := Verify(cur.Note, *retired.LsNoteVerifierKey); err == nil {
		t.Fatal("new note verified with the retired key")
	}
}

func TestLeafNotYetCovered(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.Latest(ctx); !errors.Is(err, ErrNoCheckpoint) {
		t.Fatalf("Latest on an empty ledger = %v", err)
	}
	if _, made, err := e.svc.Build(ctx); err != nil || made {
		t.Fatalf("Build on an empty ledger = %v, %v", made, err)
	}
	recs := e.record(t, 2)
	_, err := e.svc.ProofForUsage(ctx, recs[1].UsageID)
	var nyc *NotYetCoveredError
	if !errors.Is(err, ErrNotYetCovered) || !errors.As(err, &nyc) || nyc.LeafIndex != 1 || nyc.RetryAfter != time.Minute {
		t.Fatalf("ProofForUsage = %v; want NotYetCoveredError, retry after the max interval", err)
	}
	if err := e.st.Set(ctx, settings.Host, KeyMaxInterval, "", "17s"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ProofForUsage(ctx, recs[0].UsageID); !errors.As(err, &nyc) || nyc.RetryAfter != 17*time.Second {
		t.Fatalf("retry after = %v", err)
	}
	// A checkpoint over leaf 0 only would not cover leaf 1: here Build covers both.
	e.build(t)
	if _, err := e.svc.ProofForUsage(ctx, recs[1].UsageID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ProofForUsage(ctx, ledger.NewUsageID()); !errors.Is(err, ErrUnknownUsage) {
		t.Fatalf("unknown usage = %v", err)
	}
	// Nothing new: Build is a no-op.
	if _, made, err := e.svc.Build(ctx); err != nil || made {
		t.Fatalf("second Build = %v, %v", made, err)
	}
}

func (e *env) set(t *testing.T, key, v string) {
	t.Helper()
	if err := e.st.Set(context.Background(), settings.Host, key, "", v); err != nil {
		t.Fatal(err)
	}
}

func (e *env) latestSize(t *testing.T) int64 {
	t.Helper()
	cp, err := e.svc.Latest(context.Background())
	if errors.Is(err, ErrNoCheckpoint) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return cp.TreeSize
}

func TestWorkerCadence(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.set(t, KeyMinLeaves, "3")
	e.set(t, KeyMaxInterval, "30s")
	e.set(t, KeyKeyCheckInterval, "10m")
	w := NewWorker(e.svc, nil)

	step := func(want int64, why string) {
		t.Helper()
		if err := w.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		if got := e.latestSize(t); got != want {
			t.Fatalf("%s: latest checkpoint size = %d; want %d", why, got, want)
		}
	}
	step(0, "empty ledger")
	e.record(t, 1)
	step(1, "the first leaf with no checkpoint yet is covered at once")
	e.record(t, 1)
	step(1, "one new leaf, interval not elapsed")
	e.clock.Advance(29 * time.Second)
	step(1, "29s")
	e.clock.Advance(time.Second)
	step(2, "30s with one new leaf")
	e.clock.Advance(time.Hour)
	step(2, "time passed but no new leaf")
	e.record(t, 2)
	step(4, "the interval had long passed, so a new leaf is covered at once")
	e.record(t, 2)
	step(4, "two new leaves, below N, interval not elapsed")
	e.record(t, 1)
	step(7, "N new leaves, interval not elapsed")

	// Key rotation checks run on their own interval.
	// Key checks ran in the first cycle (+0s) and in the first cycle after the 1h jump (+1h30s); the check interval is 10m.
	if got := e.keys.count(); got != 2 {
		t.Fatalf("EnsureCurrent calls = %d; want 2 (at start and after the 1h jump, interval 10m)", got)
	}
	e.clock.Advance(9 * time.Minute)
	step(7, "no leaf")
	if got := e.keys.count(); got != 2 {
		t.Fatalf("EnsureCurrent calls = %d; want still 2 before the interval", got)
	}
	e.clock.Advance(time.Minute)
	step(7, "no leaf")
	if got := e.keys.count(); got != 3 {
		t.Fatalf("EnsureCurrent calls = %d; want 3", got)
	}
}

func TestWorkerRunLoopUsesPollIntervalAndStops(t *testing.T) {
	e := newEnv(t)
	e.set(t, KeyPollInterval, "7s")
	w := NewWorker(e.svc, nil)
	ctx, cancel := context.WithCancel(context.Background())
	e.record(t, 1)
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	tm := <-e.clock.timers // the first cycle ran and covered the leaf, then waits poll
	if tm.d != 7*time.Second {
		t.Fatalf("poll timer = %s", tm.d)
	}
	if got := e.latestSize(t); got != 1 {
		t.Fatalf("latest size = %d; want 1", got)
	}
	tm.c <- e.clock.Now()
	tm2 := <-e.clock.timers // the second cycle found nothing new
	if tm2.d != 7*time.Second {
		t.Fatalf("poll timer = %s", tm2.d)
	}
	if got := e.latestSize(t); got != 1 {
		t.Fatalf("latest size = %d; want 1", got)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	if !tm2.stopped {
		t.Fatal("Run did not stop its timer")
	}
}

func TestKeySetMatchesOpenAPIAndVerifiesReceipts(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	recs := e.record(t, 1)
	oldReceiptKey := e.keys.CurrentKeyID(hostkeys.Receipt)
	if _, err := e.keys.Rotate(ctx, hostkeys.Receipt); err != nil {
		t.Fatal(err)
	}
	cur, err := e.l.Record(ctx, sample(9))
	if err != nil {
		t.Fatal(err)
	}
	ks, err := e.svc.KeySet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ks.Keys) != 4 { // identity, 2 receipt, 1 checkpoint
		t.Fatalf("keys = %d", len(ks.Keys))
	}
	raw, err := json.Marshal(ks)
	if err != nil {
		t.Fatal(err)
	}
	var gen api.HostKeySet
	if err := json.Unmarshal(raw, &gen); err != nil {
		t.Fatalf("the generated HostKeySet does not decode the key set: %v\n%s", err, raw)
	}
	if err := gen.Validate(); err != nil {
		t.Fatalf("key set does not validate against the schema: %v", err)
	}
	var identity, retiredReceipt *JWK
	for i, j := range ks.Keys {
		if j.LsRole == "identity" {
			identity = &ks.Keys[i]
		}
		if j.Kid == oldReceiptKey {
			retiredReceipt = &ks.Keys[i]
		}
	}
	if identity == nil || identity.Kid != e.keys.HostID() || identity.LsParentKid != nil || identity.LsRetiredAt != nil {
		t.Fatalf("identity entry = %+v", identity)
	}
	if retiredReceipt == nil || retiredReceipt.LsRetiredAt == nil || retiredReceipt.LsParentKid == nil || *retiredReceipt.LsParentKid != e.keys.HostID() {
		t.Fatalf("retired receipt entry = %+v", retiredReceipt)
	}
	// Both the retired-key receipt and the current-key receipt verify from the list alone.
	for _, r := range []ledger.Recorded{recs[0], cur} {
		got, err := ks.VerifyReceipt(r.Receipt.JWS)
		if err != nil || got.UsageID != r.UsageID {
			t.Fatalf("VerifyReceipt = %+v, %v", got, err)
		}
	}
	// A receipt of another host is refused, and so is one that names an unknown kid.
	other := newEnv(t)
	oc := other.record(t, 1)
	if _, err := ks.VerifyReceipt(oc[0].Receipt.JWS); !errors.Is(err, ErrNoKey) {
		t.Fatalf("other host's receipt = %v; want ErrNoKey", err)
	}
	// A set without an identity entry fails closed.
	noID := KeySet{}
	for _, j := range ks.Keys {
		if j.LsRole != "identity" {
			noID.Keys = append(noID.Keys, j)
		}
	}
	if _, err := noID.VerifyReceipt(cur.Receipt.JWS); !errors.Is(err, ErrWrongHost) {
		t.Fatalf("set without identity = %v; want ErrWrongHost", err)
	}
	if _, err := ks.VerifyReceipt("not.a.jws"); err == nil {
		t.Fatal("garbage verified")
	}
	// A client verifies every endorsement from the published set alone (D-96) ...
	if err := ks.VerifyEndorsement(); err != nil {
		t.Fatal(err)
	}
	for _, j := range ks.Keys {
		if (j.LsRole == "identity") != (j.LsEndorsement == nil) {
			t.Fatalf("endorsement on %s key: %+v", j.LsRole, j.LsEndorsement)
		}
	}
	// ... and a swapped, altered or missing endorsement fails.
	var a, b int // two non-identity entries, one checkpoint and one receipt key
	for i, j := range ks.Keys {
		switch j.LsRole {
		case "checkpoint":
			a = i
		case "receipt":
			b = i
		}
	}
	tamper := func(name string, f func(set KeySet)) {
		t.Helper()
		var set KeySet
		raw, err := json.Marshal(ks)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &set); err != nil {
			t.Fatal(err)
		}
		f(set)
		if err := set.VerifyEndorsement(); !errors.Is(err, ErrBadEndorsement) {
			t.Fatalf("%s: VerifyEndorsement = %v; want ErrBadEndorsement", name, err)
		}
	}
	tamper("missing", func(s KeySet) { s.Keys[a].LsEndorsement = nil })
	tamper("signature of another key", func(s KeySet) {
		s.Keys[a].LsEndorsement.Signature = s.Keys[b].LsEndorsement.Signature
	})
	tamper("certificate of another key", func(s KeySet) {
		s.Keys[a].LsEndorsement.Certificate = s.Keys[b].LsEndorsement.Certificate
	})
	tamper("role changed", func(s KeySet) { s.Keys[a].LsRole, s.Keys[b].LsRole = "receipt", "checkpoint" })
	tamper("key replaced", func(s KeySet) { s.Keys[a].X = s.Keys[b].X })
	tamper("parent is another key", func(s KeySet) {
		other := "not-the-identity"
		s.Keys[a].LsParentKid = &other
	})
	tamper("kid of another key", func(s KeySet) { s.Keys[a].Kid = s.Keys[b].Kid })
	tamper("use changed", func(s KeySet) { s.Keys[a].Use = "enc" })
	tamper("two identity entries", func(s KeySet) { s.Keys[a].LsRole = "identity" })
	tamper("line feed in the signature", func(s KeySet) {
		sig := s.Keys[a].LsEndorsement.Signature
		s.Keys[a].LsEndorsement.Signature = sig[:4] + "\n" + sig[4:]
	})
	// A line feed in a receipt key's x is refused by VerifyReceipt too.
	{
		var set KeySet
		raw, err := json.Marshal(ks)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &set); err != nil {
			t.Fatal(err)
		}
		for i := range set.Keys {
			if set.Keys[i].Kid == e.keys.CurrentKeyID(hostkeys.Receipt) {
				set.Keys[i].X = set.Keys[i].X[:4] + "\n" + set.Keys[i].X[4:]
			}
		}
		if _, err := set.VerifyReceipt(cur.Receipt.JWS); err == nil {
			t.Fatal("VerifyReceipt accepted a receipt key with a line feed in x")
		}
	}
	// The keys chain to the host identity.
	pks, _ := e.keys.PublicKeys(ctx)
	for _, pk := range pks {
		if err := hostkeys.VerifyKey(e.keys.HostID(), pk); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCheckpointRowIsImmutable(t *testing.T) {
	e := newEnv(t)
	e.record(t, 1)
	e.build(t)
	for _, q := range []string{`UPDATE ldg_checkpoint SET note = 'x'`, `DELETE FROM ldg_checkpoint`} {
		err := e.db.Write(context.Background(), func(tx *storage.Tx) error { _, err := tx.Exec(q); return err })
		if err == nil {
			t.Fatalf("%q succeeded", q)
		}
	}
}

// The origin can be set until the first checkpoint is signed and never after (req_001 D-97).
func TestOriginSettingRefusedOnceACheckpointExists(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.record(t, 1)
	e.set(t, KeyOrigin, "example.org/log-a") // no checkpoint yet: allowed
	cp := e.build(t)
	if !strings.HasPrefix(cp.Note, "example.org/log-a\n") {
		t.Fatalf("note = %q", cp.Note)
	}
	err := e.st.Set(ctx, settings.Host, KeyOrigin, "", "example.org/log-b")
	if !errors.Is(err, settings.ErrRejected) || !errors.Is(err, ErrOriginLocked) {
		t.Fatalf("Set after the first checkpoint = %v; want ErrRejected and ErrOriginLocked", err)
	}
	if err := e.st.Reset(ctx, settings.Host, KeyOrigin, ""); !errors.Is(err, ErrOriginLocked) {
		t.Fatalf("Reset after the first checkpoint = %v; want ErrOriginLocked", err)
	}
	if v, _ := e.st.String(ctx, KeyOrigin, ""); v != "example.org/log-a" {
		t.Fatalf("origin = %q after the refused writes", v)
	}
	// Other settings of the package stay writable.
	e.set(t, KeyMinLeaves, "5")
}

func TestBadOriginSettingIsRefused(t *testing.T) {
	e := newEnv(t)
	e.record(t, 1)
	err := e.st.Set(context.Background(), settings.Host, KeyOrigin, "", "has space")
	if !errors.Is(err, settings.ErrRejected) || !errors.Is(err, ErrBadKeyName) {
		t.Fatalf("Set = %v; want ErrRejected and ErrBadKeyName", err)
	}
	for _, bad := range []string{"example.org/a b", "example.org/a\u2003b", "example.org/\xff"} {
		err := e.st.Set(context.Background(), settings.Host, KeyOrigin, "", bad)
		if !errors.Is(err, settings.ErrRejected) || !errors.Is(err, ErrBadKeyName) {
			t.Fatalf("Set %q = %v; want ErrRejected and ErrBadKeyName", bad, err)
		}
	}
	e.build(t) // the default origin is still in force
	// The first checkpoint locks the origin (D-97), so a valid origin is set in a fresh log.
	e = newEnv(t)
	e.record(t, 1)
	e.set(t, KeyOrigin, "example.org/my-ledger")
	cp := e.build(t)
	if v, err := Verify(cp.Note, e.verifierKeys(t)...); err != nil || v.Origin != "example.org/my-ledger" {
		t.Fatalf("Verify = %+v, %v", v, err)
	}
}

func TestVerifierKeyRefusesUnicodeSpaceAndInvalidUTF8(t *testing.T) {
	pub := fuzzKey().Public().(ed25519.PublicKey)
	for _, name := range []string{"example.org/a b", "example.org/\xff"} {
		if _, err := VerifierKey(name, pub); !errors.Is(err, ErrBadKeyName) {
			t.Fatalf("VerifierKey(%q) = %v; want ErrBadKeyName", name, err)
		}
	}
}

func TestVerifyRefusesTwoTextsOfOneVerifierKey(t *testing.T) {
	e := newEnv(t)
	e.record(t, 1)
	cp := e.build(t)
	vk := e.verifierKeys(t)[0]
	vk2 := vk[:len(vk)-8] + "\n" + vk[len(vk)-8:]
	if _, err := Verify(cp.Note, vk, vk2); !errors.Is(err, ErrBadNote) {
		t.Fatalf("Verify with two texts of one key = %v; want ErrBadNote", err)
	}
}

func TestSettingValidatorsRefuseValuesThatStallTheWorker(t *testing.T) {
	e := newEnv(t)
	for key, v := range map[string]string{
		KeyMinLeaves: "0", KeyMaxInterval: "0s", KeyPollInterval: "-1s", KeyKeyCheckInterval: "0s",
	} {
		err := e.st.Set(context.Background(), settings.Host, key, "", v)
		if !errors.Is(err, settings.ErrRejected) {
			t.Fatalf("Set %s=%s = %v; want ErrRejected", key, v, err)
		}
	}
}
