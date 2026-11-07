package ledger

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ogen-go/ogen"
	"golang.org/x/mod/sumdb/tlog"

	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/usage"
)

type env struct {
	db   *storage.DB
	keys *hostkeys.Keys
	l    *Ledger
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"),
		settings.Module(), host.Module(), usage.Module(), Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	master := make([]byte, hostkeys.KeySize)
	if _, err := rand.Read(master); err != nil {
		t.Fatal(err)
	}
	keys, _, err := hostkeys.Open(ctx, db, settings.New(db), master)
	if err != nil {
		t.Fatal(err)
	}
	return &env{db: db, keys: keys, l: New(db, keys)}
}

func (e *env) receiptKey(t *testing.T, kid string) ed25519.PublicKey {
	t.Helper()
	pks, err := e.keys.PublicKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, pk := range pks {
		if pk.ID == kid && pk.Purpose == hostkeys.Receipt {
			return pk.Key
		}
	}
	t.Fatalf("no receipt key %s published", kid)
	return nil
}

func (e *env) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var t0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func sample(i int) Usage {
	return Usage{
		UsageID:          NewUsageID(),
		ConsumerID:       "11111111-1111-4111-8111-111111111111",
		AccessKeyID:      "22222222-2222-4222-8222-222222222222",
		GrantVersionID:   "33333333-3333-4333-8333-333333333333",
		Operation:        OpChatCompletions,
		Model:            "llama3.2:3b",
		Outcome:          OutcomeCompleted,
		PromptTokens:     int64(10 + i),
		CompletionTokens: int64(20 + i),
		StartedAt:        t0,
		FinishedAt:       t0.Add(1500 * time.Millisecond),
	}
}

func TestRecordReceiptVerifiesWithReceiptKey(t *testing.T) {
	e := newEnv(t)
	u := sample(0)
	rec, err := e.l.Record(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	if rec.LeafIndex != 0 || rec.TreeSize != 1 {
		t.Fatalf("first leaf = index %d size %d; want 0 and 1", rec.LeafIndex, rec.TreeSize)
	}
	kid, err := ReceiptKeyID(rec.Receipt.JWS)
	if err != nil || kid != e.keys.CurrentKeyID(hostkeys.Receipt) {
		t.Fatalf("receipt kid = %q, %v; want the live receipt key %q", kid, err, e.keys.CurrentKeyID(hostkeys.Receipt))
	}
	r, err := Verify(rec.Receipt.JWS, e.receiptKey(t, kid))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if r.Metering != Metered || r.UsageID != u.UsageID || r.HostKID != e.keys.HostID() || r.TotalTokens != 30 ||
		r.LeafIndex == nil || *r.LeafIndex != 0 || r.TreeSize == nil || *r.TreeSize != 1 || r.JobID != nil {
		t.Fatalf("payload = %+v", r)
	}
	// The record carries the receipt's signature and no content column exists.
	var sig []byte
	var gotKid string
	var latency int64
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT receipt_signature, receipt_kid, latency_ms FROM use_record WHERE id = ?`, u.UsageID).Scan(&sig, &gotKid, &latency); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(rec.Receipt.JWS, "."+base64.RawURLEncoding.EncodeToString(sig)) || gotKid != kid || latency != 1500 {
		t.Fatalf("stored signature/kid/latency = %x %s %d", sig, gotKid, latency)
	}
	var change struct {
		dataset, pk string
		payload     []byte
	}
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT dataset, pk, payload FROM syn_change`).Scan(&change.dataset, &change.pk, &change.payload); err != nil {
		t.Fatal(err)
	}
	leaf, _ := CanonicalLeaf(u)
	if change.dataset != "usage" || change.pk != u.UsageID || string(change.payload) != string(leaf) {
		t.Fatalf("syn_change = %+v", change)
	}
}

func TestVerifyAcceptsRotatedKeyReceipts(t *testing.T) {
	e := newEnv(t)
	old, err := e.l.Record(context.Background(), sample(0))
	if err != nil {
		t.Fatal(err)
	}
	oldKid, _ := ReceiptKeyID(old.Receipt.JWS)
	if _, err := e.keys.Rotate(context.Background(), hostkeys.Receipt); err != nil {
		t.Fatal(err)
	}
	cur, err := e.l.Record(context.Background(), sample(1))
	if err != nil {
		t.Fatal(err)
	}
	curKid, _ := ReceiptKeyID(cur.Receipt.JWS)
	if curKid == oldKid {
		t.Fatal("rotation did not change the receipt key")
	}
	if _, err := Verify(old.Receipt.JWS, e.receiptKey(t, oldKid)); err != nil {
		t.Fatalf("old receipt: %v", err)
	}
	if _, err := Verify(cur.Receipt.JWS, e.receiptKey(t, curKid)); err != nil {
		t.Fatalf("new receipt: %v", err)
	}
	if _, err := Verify(cur.Receipt.JWS, e.receiptKey(t, oldKid)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("new receipt under the old key = %v; want ErrBadSignature", err)
	}
}

// hashReader reads ldg_hash outside a unit of work.
type hashReader struct {
	t  *testing.T
	db *storage.DB
}

func (h hashReader) ReadHashes(idx []int64) ([]tlog.Hash, error) {
	out := make([]tlog.Hash, len(idx))
	for i, x := range idx {
		var b []byte
		if err := h.db.Reader().QueryRow(context.Background(), `SELECT hash FROM ldg_hash WHERE hash_index = ?`, x).Scan(&b); err != nil {
			return nil, err
		}
		copy(out[i][:], b)
	}
	return out, nil
}

func TestLeafHashesMatchTlogAndProofsVerify(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	const n = 13
	us := make([]Usage, n)
	for i := range us {
		us[i] = sample(i)
		if i%3 == 0 {
			us[i].JobID = "44444444-4444-4444-8444-444444444444"
		}
		if _, err := e.l.Record(ctx, us[i]); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := int64(e.count(t, "ldg_hash")), tlog.StoredHashCount(n); got != want {
		t.Fatalf("ldg_hash rows = %d; want tlog.StoredHashCount(%d) = %d", got, n, want)
	}
	hr := hashReader{t, e.db}
	root, err := tlog.TreeHash(n, hr)
	if err != nil {
		t.Fatal(err)
	}
	for i, u := range us {
		leaf, err := CanonicalLeaf(u)
		if err != nil {
			t.Fatal(err)
		}
		want := tlog.RecordHash(leaf)
		var got []byte
		var id string
		if err := e.db.Reader().QueryRow(ctx, `SELECT leaf_hash, usage_id FROM ldg_leaf WHERE leaf_index = ?`, i).Scan(&got, &id); err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want[:]) || id != u.UsageID {
			t.Fatalf("leaf %d hash/id mismatch", i)
		}
		// A consumer with the leaf bytes, a proof and the root checks inclusion.
		proof, err := tlog.ProveRecord(n, int64(i), hr)
		if err != nil {
			t.Fatal(err)
		}
		if err := tlog.CheckRecord(proof, n, root, int64(i), want); err != nil {
			t.Fatalf("inclusion proof of leaf %d: %v", i, err)
		}
	}
}

func TestConcurrentRecordsHaveMonotonicGaplessIndexes(t *testing.T) {
	e := newEnv(t)
	const n = 48
	type res struct {
		rec Recorded
		err error
	}
	out := make([]res, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec, err := e.l.Record(context.Background(), sample(i))
			out[i] = res{rec, err}
		}()
	}
	wg.Wait()
	idx := make([]int, 0, n)
	for _, r := range out {
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.rec.TreeSize != r.rec.LeafIndex+1 {
			t.Fatalf("tree size %d for leaf %d", r.rec.TreeSize, r.rec.LeafIndex)
		}
		idx = append(idx, int(r.rec.LeafIndex))
	}
	sort.Ints(idx)
	for i, v := range idx {
		if v != i {
			t.Fatalf("leaf indexes = %v; want 0..%d with no gap or repeat", idx, n-1)
		}
	}
	if _, err := tlog.TreeHash(n, hashReader{t, e.db}); err != nil {
		t.Fatal(err)
	}
}

func TestUnmeteredReceiptHasNoLeaf(t *testing.T) {
	e := newEnv(t)
	u := sample(0)
	s, err := e.l.SignUnmetered(u)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Verify(s.JWS, e.receiptKey(t, s.KeyID))
	if err != nil {
		t.Fatal(err)
	}
	if r.Metering != Unmetered || r.LeafIndex != nil || r.TreeSize != nil || r.PromptTokens != 10 || r.CompletionTokens != 20 {
		t.Fatalf("payload = %+v", r)
	}
	raw := payloadOf(t, s.JWS)
	if strings.Contains(raw, "leaf_index") || strings.Contains(raw, "tree_size") {
		t.Fatalf("unmetered payload names a leaf: %s", raw)
	}
	for _, table := range []string{"use_record", "ldg_leaf", "ldg_hash", "syn_change"} {
		if n := e.count(t, table); n != 0 {
			t.Fatalf("SignUnmetered wrote %d rows to %s", n, table)
		}
	}
	// A metered claim built by hand without a leaf, or an unmetered one with a leaf, is refused.
	bad := u.receipt("x")
	bad.Metering = Metered
	if _, err := e.l.signReceipt(bad); err == nil {
		t.Fatal("signed a metered receipt without leaf_index")
	}
	one := int64(1)
	bad.Metering, bad.LeafIndex, bad.TreeSize = Unmetered, &one, &one
	if _, err := e.l.signReceipt(bad); err == nil {
		t.Fatal("signed an unmetered receipt with a leaf")
	}
	neg, zero := int64(-1), int64(0)
	bad.Metering, bad.LeafIndex, bad.TreeSize = Metered, &neg, &zero
	if _, err := e.l.signReceipt(bad); err == nil {
		t.Fatal("signed a metered receipt with leaf_index -1")
	}
}

func payloadOf(t *testing.T, jws string) string {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(strings.Split(jws, ".")[1])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTamperedReceiptFailsVerify(t *testing.T) {
	e := newEnv(t)
	rec, err := e.l.Record(context.Background(), sample(0))
	if err != nil {
		t.Fatal(err)
	}
	key := e.receiptKey(t, rec.Receipt.KeyID)
	parts := strings.Split(rec.Receipt.JWS, ".")
	enc := base64.RawURLEncoding

	forged := strings.Replace(payloadOf(t, rec.Receipt.JWS), `"completion_tokens":20`, `"completion_tokens":2`, 1)
	if _, err := Verify(parts[0]+"."+enc.EncodeToString([]byte(forged))+"."+parts[2], key); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered payload = %v; want ErrBadSignature", err)
	}
	sig, _ := enc.DecodeString(parts[2])
	sig[0] ^= 1
	if _, err := Verify(parts[0]+"."+parts[1]+"."+enc.EncodeToString(sig), key); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered signature = %v; want ErrBadSignature", err)
	}
	other, _, _ := ed25519.GenerateKey(nil)
	if _, err := Verify(rec.Receipt.JWS, other); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("wrong key = %v; want ErrBadSignature", err)
	}
	for name, bad := range map[string]string{
		"two parts": parts[0] + "." + parts[1],
		"empty":     "",
		"alg none":  enc.EncodeToString([]byte(`{"alg":"none","kid":"k","typ":"ls-receipt+jws"}`)) + "." + parts[1] + "." + parts[2],
		"wrong typ": enc.EncodeToString([]byte(`{"alg":"Ed25519","kid":"k","typ":"JWT"}`)) + "." + parts[1] + "." + parts[2],
		"padded":    parts[0] + "." + parts[1] + "." + parts[2] + "==",
		// base64 decoders skip CR and LF; the signature still covers the raw text (PLT-B-024).
		"newline in header":    parts[0][:10] + "\n" + parts[0][10:] + "." + parts[1] + "." + parts[2],
		"newline in payload":   parts[0] + "." + parts[1][:10] + "\r\n" + parts[1][10:] + "." + parts[2],
		"newline in signature": parts[0] + "." + parts[1] + "." + parts[2][:10] + "\n" + parts[2][10:],
	} {
		if _, err := Verify(bad, key); err == nil {
			t.Errorf("%s: Verify accepted it", name)
		}
	}
}

func TestReceiptWithNewlineRejectedByParseHeader(t *testing.T) {
	e := newEnv(t)
	rec, err := e.l.Record(context.Background(), sample(0))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(rec.Receipt.JWS, ".")
	if _, err := ReceiptKeyID(parts[0][:10] + "\n" + parts[0][10:] + "." + parts[1] + "." + parts[2]); !errors.Is(err, ErrMalformedReceipt) {
		t.Fatalf("ReceiptKeyID with a newline = %v; want ErrMalformedReceipt", err)
	}
}

func TestMalformedReceiptShapes(t *testing.T) {
	pub := fuzzKey().Public().(ed25519.PublicKey)
	if _, err := ReceiptKeyID("a.b.c.d"); !errors.Is(err, ErrMalformedReceipt) {
		t.Fatalf("ReceiptKeyID with 4 parts = %v; want ErrMalformedReceipt", err)
	}
	if _, err := Verify("a.b.c.d", pub); !errors.Is(err, ErrMalformedReceipt) {
		t.Fatalf("Verify with 4 parts = %v; want ErrMalformedReceipt", err)
	}
	for _, h := range []string{
		`{"alg":"Ed25519","kid":"k1","typ":"ls-receipt+jws"}}`,
		`{"ALG":"Ed25519","kid":"k1","typ":"ls-receipt+jws"}`,
	} {
		jws := base64.RawURLEncoding.EncodeToString([]byte(h)) + ".x.y"
		if _, err := ReceiptKeyID(jws); !errors.Is(err, ErrMalformedReceipt) {
			t.Fatalf("ReceiptKeyID(%s) = %v; want ErrMalformedReceipt", h, err)
		}
	}
	max, min := int64(math.MaxInt64), int64(math.MinInt64)
	r := fuzzReceipt()
	r.LeafIndex, r.TreeSize = &max, &min
	if _, err := Verify(fuzzJWS(fuzzHeader, r), pub); !errors.Is(err, ErrMalformedReceipt) {
		t.Fatalf("Verify with wrapped tree_size = %v; want ErrMalformedReceipt", err)
	}
}

func TestTokenCountOverflowRefused(t *testing.T) {
	u := goldenUsage()
	u.PromptTokens, u.CompletionTokens = math.MaxInt64, 1
	if _, err := CanonicalLeaf(u); err == nil {
		t.Fatal("CanonicalLeaf accepted overflowing token counts")
	}
}

func TestRolledBackUnitLeavesNothing(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	boom := errors.New("boom")
	err := e.db.Write(ctx, func(tx *storage.Tx) error {
		if _, err := e.l.RecordTx(tx, sample(0)); err != nil {
			return err
		}
		if _, err := e.l.RecordTx(tx, sample(1)); err != nil { // sees the first leaf: index 1
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Write = %v; want boom", err)
	}
	for _, table := range []string{"use_record", "ldg_leaf", "ldg_hash", "syn_change"} {
		if n := e.count(t, table); n != 0 {
			t.Fatalf("%s has %d rows after a rolled-back unit", table, n)
		}
	}
	rec, err := e.l.Record(ctx, sample(2))
	if err != nil || rec.LeafIndex != 0 {
		t.Fatalf("after rollback Record = %+v, %v; want leaf 0", rec, err)
	}
	// Two records in one unit take consecutive leaves.
	var a, b Recorded
	if err := e.db.Write(ctx, func(tx *storage.Tx) (err error) {
		if a, err = e.l.RecordTx(tx, sample(3)); err != nil {
			return err
		}
		b, err = e.l.RecordTx(tx, sample(4))
		return err
	}); err != nil || a.LeafIndex != 1 || b.LeafIndex != 2 || b.TreeSize != 3 {
		t.Fatalf("one unit, two records = %+v %+v, %v", a, b, err)
	}
}

func TestRecordRejectsBadInputWithoutWriting(t *testing.T) {
	e := newEnv(t)
	for name, mut := range map[string]func(*Usage){
		"bad uuid":      func(u *Usage) { u.ConsumerID = "nope" },
		"bad op":        func(u *Usage) { u.Operation = "images" },
		"bad outcome":   func(u *Usage) { u.Outcome = "ok" },
		"negative":      func(u *Usage) { u.PromptTokens = -1 },
		"empty model":   func(u *Usage) { u.Model = "" },
		"ends first":    func(u *Usage) { u.FinishedAt = u.StartedAt.Add(-time.Second) },
		"unset started": func(u *Usage) { u.StartedAt = time.Time{} },
	} {
		u := sample(0)
		mut(&u)
		if _, err := e.l.Record(context.Background(), u); err == nil {
			t.Errorf("%s: Record accepted it", name)
		}
	}
	if n := e.count(t, "use_record") + e.count(t, "ldg_leaf"); n != 0 {
		t.Fatalf("%d rows written for invalid input", n)
	}
}

func TestRecordsAndLeavesAreImmutable(t *testing.T) {
	e := newEnv(t)
	if _, err := e.l.Record(context.Background(), sample(0)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE use_record SET prompt_tokens = 0`, `DELETE FROM use_record`,
		`UPDATE ldg_leaf SET leaf_index = 9`, `DELETE FROM ldg_leaf`,
		`UPDATE ldg_hash SET hash = x'00'`, `DELETE FROM ldg_hash`,
	} {
		err := e.db.Write(context.Background(), func(tx *storage.Tx) error { _, err := tx.Exec(q); return err })
		if err == nil {
			t.Errorf("%q succeeded", q)
		}
	}
}

// ---- golden tests with fixed keys ----

type fixedSigner struct{ priv ed25519.PrivateKey }

func newFixedSigner() fixedSigner {
	return fixedSigner{ed25519.NewKeyFromSeed([]byte("local-server golden receipt key!"))}
}

func (s fixedSigner) SignWithKeyID(_ hostkeys.Purpose, build func(string) []byte) (string, []byte, error) {
	return "golden-kid", ed25519.Sign(s.priv, build("golden-kid")), nil
}
func (s fixedSigner) HostID() string { return "golden-host" }

func goldenUsage() Usage {
	return Usage{
		UsageID:          "0192f1a0-7b3c-7d4e-8a5b-6c7d8e9f0a1b",
		ConsumerID:       "11111111-1111-4111-8111-111111111111",
		AccessKeyID:      "22222222-2222-4222-8222-222222222222",
		GrantVersionID:   "33333333-3333-4333-8333-333333333333",
		Operation:        OpChatCompletions,
		Model:            `model "é"/1`,
		Outcome:          OutcomeCompleted,
		PromptTokens:     12,
		CompletionTokens: 34,
		StartedAt:        time.Date(2026, 10, 31, 12, 0, 0, 5_000_000, time.UTC),
		FinishedAt:       time.Date(2026, 10, 31, 12, 0, 2, 0, time.UTC),
	}
}

const goldenLeaf = `{"access_key_id":"22222222-2222-4222-8222-222222222222","completion_tokens":34,"consumer_id":"11111111-1111-4111-8111-111111111111","finished_at":"2026-10-31T12:00:02.000Z","grant_version_id":"33333333-3333-4333-8333-333333333333","job_id":null,"model":"model \"é\"/1","operation":"chat.completions","outcome":"completed","prompt_tokens":12,"started_at":"2026-10-31T12:00:00.005Z","total_tokens":46,"usage_id":"0192f1a0-7b3c-7d4e-8a5b-6c7d8e9f0a1b","v":1}`

func TestGoldenCanonicalLeaf(t *testing.T) {
	got, err := CanonicalLeaf(goldenUsage())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != goldenLeaf {
		t.Fatalf("canonical leaf changed (an external contract, bump v):\n got %s\nwant %s", got, goldenLeaf)
	}
	if h := tlog.RecordHash(got); h.String() != goldenLeafHash {
		t.Fatalf("leaf hash = %s; want %s", h, goldenLeafHash)
	}
	// Keys ascend, as RFC 8785 requires.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(m))
	dec := json.NewDecoder(strings.NewReader(string(got)))
	dec.Token()
	for dec.More() {
		k, _ := dec.Token()
		keys = append(keys, k.(string))
		var skip json.RawMessage
		dec.Decode(&skip)
	}
	if !sort.StringsAreSorted(keys) {
		t.Fatalf("leaf keys not ascending: %v", keys)
	}
	// Control characters use the short or \u00xx forms, other text stays raw UTF-8.
	if got := jcsString("a\n\x01 \\"); got != "\"a\\n\\u0001 \\\\\"" {
		t.Fatalf("jcsString = %q", got)
	}
}

const (
	goldenLeafHash  = "SAL1a7bloyyn6+C2cHaj8a7WwBpyXMEIG8X4dCYA+Qs="
	goldenMetered   = "eyJhbGciOiJFZDI1NTE5Iiwia2lkIjoiZ29sZGVuLWtpZCIsInR5cCI6ImxzLXJlY2VpcHQrandzIn0.eyJ2IjoxLCJtZXRlcmluZyI6Im1ldGVyZWQiLCJ1c2FnZV9pZCI6IjAxOTJmMWEwLTdiM2MtN2Q0ZS04YTViLTZjN2Q4ZTlmMGExYiIsImhvc3Rfa2lkIjoiZ29sZGVuLWhvc3QiLCJjb25zdW1lcl9pZCI6IjExMTExMTExLTExMTEtNDExMS04MTExLTExMTExMTExMTExMSIsImFjY2Vzc19rZXlfaWQiOiIyMjIyMjIyMi0yMjIyLTQyMjItODIyMi0yMjIyMjIyMjIyMjIiLCJncmFudF92ZXJzaW9uX2lkIjoiMzMzMzMzMzMtMzMzMy00MzMzLTgzMzMtMzMzMzMzMzMzMzMzIiwib3BlcmF0aW9uIjoiY2hhdC5jb21wbGV0aW9ucyIsIm1vZGVsIjoibW9kZWwgXCLDqVwiLzEiLCJvdXRjb21lIjoiY29tcGxldGVkIiwicHJvbXB0X3Rva2VucyI6MTIsImNvbXBsZXRpb25fdG9rZW5zIjozNCwidG90YWxfdG9rZW5zIjo0Niwic3RhcnRlZF9hdCI6IjIwMjYtMTAtMzFUMTI6MDA6MDAuMDA1WiIsImZpbmlzaGVkX2F0IjoiMjAyNi0xMC0zMVQxMjowMDowMi4wMDBaIiwibGVhZl9pbmRleCI6NiwidHJlZV9zaXplIjo3fQ.1FQwKE0zUVLbpk1RBKwGMYSC8VQ3gtztCAYHduI4DioDtK7XnOk7CN6APmvOa5XwC-S3XT5IQE4ekCjcdIH_Cw"
	goldenUnmetered = "eyJhbGciOiJFZDI1NTE5Iiwia2lkIjoiZ29sZGVuLWtpZCIsInR5cCI6ImxzLXJlY2VpcHQrandzIn0.eyJ2IjoxLCJtZXRlcmluZyI6InVubWV0ZXJlZCIsInVzYWdlX2lkIjoiMDE5MmYxYTAtN2IzYy03ZDRlLThhNWItNmM3ZDhlOWYwYTFiIiwiaG9zdF9raWQiOiJnb2xkZW4taG9zdCIsImNvbnN1bWVyX2lkIjoiMTExMTExMTEtMTExMS00MTExLTgxMTEtMTExMTExMTExMTExIiwiYWNjZXNzX2tleV9pZCI6IjIyMjIyMjIyLTIyMjItNDIyMi04MjIyLTIyMjIyMjIyMjIyMiIsImdyYW50X3ZlcnNpb25faWQiOiIzMzMzMzMzMy0zMzMzLTQzMzMtODMzMy0zMzMzMzMzMzMzMzMiLCJvcGVyYXRpb24iOiJjaGF0LmNvbXBsZXRpb25zIiwibW9kZWwiOiJtb2RlbCBcIsOpXCIvMSIsIm91dGNvbWUiOiJjb21wbGV0ZWQiLCJwcm9tcHRfdG9rZW5zIjoxMiwiY29tcGxldGlvbl90b2tlbnMiOjM0LCJ0b3RhbF90b2tlbnMiOjQ2LCJzdGFydGVkX2F0IjoiMjAyNi0xMC0zMVQxMjowMDowMC4wMDVaIiwiZmluaXNoZWRfYXQiOiIyMDI2LTEwLTMxVDEyOjAwOjAyLjAwMFoifQ.8be_6n0SqY5OqZu5-dhgDBTznftt552pdTcbKyqgfnP26tJq_QIDf9EmbkZm_EpAnyMxeAQ6CqHK7JhRon-vAQ"
)

func TestGoldenReceipts(t *testing.T) {
	l := New(nil, newFixedSigner())
	pub := newFixedSigner().priv.Public().(ed25519.PublicKey)
	n, size := int64(6), int64(7)
	r := goldenUsage().receipt("")
	r.Metering, r.LeafIndex, r.TreeSize = Metered, &n, &size
	m, err := l.signReceipt(r)
	if err != nil {
		t.Fatal(err)
	}
	u, err := l.SignUnmetered(goldenUsage())
	if err != nil {
		t.Fatal(err)
	}
	if m.JWS != goldenMetered {
		t.Errorf("metered receipt changed:\n got %s\nwant %s", m.JWS, goldenMetered)
	}
	if u.JWS != goldenUnmetered {
		t.Errorf("unmetered receipt changed:\n got %s\nwant %s", u.JWS, goldenUnmetered)
	}
	hdr, _ := base64.RawURLEncoding.DecodeString(strings.Split(m.JWS, ".")[0])
	if string(hdr) != `{"alg":"Ed25519","kid":"golden-kid","typ":"ls-receipt+jws"}` {
		t.Fatalf("header = %s", hdr)
	}
	for _, s := range []string{goldenMetered, goldenUnmetered} {
		if _, err := Verify(s, pub); err != nil {
			t.Errorf("golden receipt does not verify: %v", err)
		}
	}
}

// TestEstimatedMarkerStaysOutOfTheLeaf guards the canonical leaf (req_001 D-108): the estimated
// marker is on the signed receipt only, so the leaf bytes and hash are the same with it set and
// the golden vector holds.
func TestEstimatedMarkerStaysOutOfTheLeaf(t *testing.T) {
	plain, est := goldenUsage(), goldenUsage()
	est.PromptTokensEstimated = true
	a, err := CanonicalLeaf(plain)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalLeaf(est)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("the marker changed the leaf:\n plain %s\n marked %s", a, b)
	}
	if string(b) != goldenLeaf {
		t.Fatalf("leaf = %s; want the golden vector %s", b, goldenLeaf)
	}
	if strings.Contains(string(b), "estimated") {
		t.Fatalf("leaf names the marker: %s", b)
	}
}

// TestEstimatedMarkerOnReceipts: both the metered and the unmetered receipt carry the marker as
// true when the usage has it and omit it otherwise; the stored row has no column for it.
func TestEstimatedMarkerOnReceipts(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	payload := func(jws string) map[string]any {
		t.Helper()
		raw, err := decodeB64(strings.Split(jws, ".")[1])
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	est := sample(0)
	est.Outcome, est.PromptTokensEstimated = OutcomeAborted, true
	rec, err := e.l.Record(ctx, est)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := payload(rec.Receipt.JWS)["prompt_tokens_estimated"]; !ok || v != true {
		t.Errorf("metered receipt marker = %v, %v; want true", v, ok)
	}
	r, err := Verify(rec.Receipt.JWS, e.receiptKey(t, rec.Receipt.KeyID))
	if err != nil || !r.PromptTokensEstimated {
		t.Errorf("Verify = %+v, %v; want the marker", r, err)
	}
	un, err := e.l.SignUnmetered(est)
	if err != nil {
		t.Fatal(err)
	}
	if v := payload(un.JWS)["prompt_tokens_estimated"]; v != true {
		t.Errorf("unmetered receipt marker = %v; want true", v)
	}
	plain, err := e.l.Record(ctx, sample(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := payload(plain.Receipt.JWS)["prompt_tokens_estimated"]; ok {
		t.Error("a receipt of an exact count carries the marker")
	}
	pu, err := e.l.SignUnmetered(sample(2))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := payload(pu.JWS)["prompt_tokens_estimated"]; ok {
		t.Error("an unmetered receipt of an exact count carries the marker")
	}
}

// TestReceiptMatchesOpenAPISchema keeps Receipt equal to the UsageReceipt schema in
// api/openapi.yaml until PLT-F-052 generates the type: same property names, same required
// set, same enums.
func TestReceiptMatchesOpenAPISchema(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ogen.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	sch := spec.Components.Schemas["UsageReceipt"]
	if sch == nil {
		t.Fatal("UsageReceipt missing from the spec")
	}
	props := map[string]*ogen.Schema{}
	for _, p := range sch.Properties {
		props[p.Name] = p.Schema
	}
	var fields, required []string
	rt := reflect.TypeOf(Receipt{})
	for i := range rt.NumField() {
		name, opts, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		fields = append(fields, name)
		if !strings.Contains(opts, "omitempty") {
			required = append(required, name)
		}
	}
	var specProps []string
	for k := range props {
		specProps = append(specProps, k)
	}
	sort.Strings(fields)
	sort.Strings(specProps)
	if !reflect.DeepEqual(fields, specProps) {
		t.Fatalf("Receipt fields %v differ from UsageReceipt properties %v", fields, specProps)
	}
	specReq := append([]string(nil), sch.Required...)
	sort.Strings(required)
	sort.Strings(specReq)
	if !reflect.DeepEqual(required, specReq) {
		t.Fatalf("Receipt required fields %v differ from the schema's %v", required, specReq)
	}
	if p := props["prompt_tokens_estimated"]; p == nil || p.Type != "boolean" {
		t.Errorf("prompt_tokens_estimated in the schema = %+v; want an optional boolean", p)
	}
	enum := func(name string) []string {
		var out []string
		for _, e := range props[name].Enum {
			var s string
			if err := json.Unmarshal(e, &s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		return out
	}
	if got := enum("metering"); !reflect.DeepEqual(got, []string{Metered, Unmetered}) {
		t.Errorf("metering enum = %v", got)
	}
	if got := enum("operation"); !reflect.DeepEqual(got, []string{OpChatCompletions, OpEmbeddings}) {
		t.Errorf("operation enum = %v", got)
	}
	if got := enum("outcome"); !reflect.DeepEqual(got, []string{OutcomeCompleted, OutcomeAborted, OutcomeFailed}) {
		t.Errorf("outcome enum = %v", got)
	}
}
