package hostkeys

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

type env struct {
	db     *storage.DB
	st     *settings.Store
	master []byte
	now    time.Time
	keys   *Keys
	code   string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	keyring.MockInit()
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"), settings.Module(), host.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	master, _, err := LoadMasterKey(filepath.Join(t.TempDir(), "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{db: db, st: settings.New(db), master: master, now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	e.keys, e.code = e.open(t)
	return e
}

func (e *env) open(t *testing.T) (*Keys, string) {
	t.Helper()
	k, code, err := open(context.Background(), e.db, e.st, e.master, func() time.Time { return e.now })
	if err != nil {
		t.Fatal(err)
	}
	return k, code
}

func TestSubkeysChainToIdentity(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	k := e.keys
	pks, err := k.PublicKeys(ctx)
	if err != nil || len(pks) != 3 {
		t.Fatalf("PublicKeys = %d keys, %v; want identity, receipt, checkpoint", len(pks), err)
	}
	for _, pk := range pks {
		if err := VerifyKey(k.HostID(), pk); err != nil {
			t.Errorf("%s key %s does not chain to identity: %v", pk.Purpose, pk.ID, err)
		}
	}
	// A key signed for another purpose, or by someone else, does not verify.
	forged := pks[1]
	forged.Purpose = Checkpoint
	if VerifyKey(k.HostID(), forged) == nil {
		t.Error("receipt certificate verified as a checkpoint key")
	}

	msg := []byte("tree head")
	for _, p := range []Purpose{Receipt, Checkpoint} {
		id, sig, err := k.Sign(p, msg)
		if err != nil {
			t.Fatal(err)
		}
		var pub ed25519.PublicKey
		for _, pk := range pks {
			if pk.ID == id && pk.Purpose == p {
				pub = pk.Key
			}
		}
		if pub == nil || !ed25519.Verify(pub, msg, sig) {
			t.Errorf("%s signature does not verify under published key %s", p, id)
		}
		if ed25519.Verify(k.IdentityPublicKey(), msg, sig) {
			t.Errorf("%s signature verifies under the identity key; subkeys must be separate keys", p)
		}
	}
	if _, _, err := k.Sign(Identity, msg); !errors.Is(err, ErrNotSigningPurpose) {
		t.Errorf("Sign(Identity) = %v; want ErrNotSigningPurpose", err)
	}
}

func TestRotateCheckpointKeepsRetiredKey(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	k := e.keys
	oldID := k.CurrentKeyID(Checkpoint)
	_, oldSig, _ := k.Sign(Checkpoint, []byte("old head"))

	e.now = e.now.Add(time.Hour)
	newID, err := k.Rotate(ctx, Checkpoint)
	if err != nil || newID == oldID || k.CurrentKeyID(Checkpoint) != newID {
		t.Fatalf("Rotate = %q, %v (old %q)", newID, err, oldID)
	}
	pks, _ := k.PublicKeys(ctx)
	var old, cur *PublicKey
	for i := range pks {
		switch pks[i].ID {
		case oldID:
			old = &pks[i]
		case newID:
			cur = &pks[i]
		}
	}
	if old == nil || cur == nil {
		t.Fatalf("old key kept = %v, new key present = %v", old != nil, cur != nil)
	}
	if old.RetiredAt == nil || !old.RetiredAt.Equal(e.now) || cur.RetiredAt != nil {
		t.Errorf("retired_at: old %v, new %v; want old set to rotation time, new empty", old.RetiredAt, cur.RetiredAt)
	}
	if !ed25519.Verify(old.Key, []byte("old head"), oldSig) {
		t.Error("a retired key no longer verifies its old signatures")
	}
	if err := VerifyKey(k.HostID(), *cur); err != nil {
		t.Errorf("rotated key does not chain to identity: %v", err)
	}
	// The receipt key was not touched, and a restart loads the new key.
	k2, code := e.open(t)
	if code != "" || k2.CurrentKeyID(Checkpoint) != newID || k2.HostID() != k.HostID() {
		t.Errorf("reopen: code %q, checkpoint %q, host %q", code, k2.CurrentKeyID(Checkpoint), k2.HostID())
	}
}

func TestEnsureCurrentUsesHostSetting(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	k := e.keys
	if got, err := e.st.Duration(ctx, KeyCheckpointRotation, ""); err != nil || got != 8760*time.Hour {
		t.Fatalf("default checkpoint interval = %v, %v", got, err)
	}
	e.now = e.now.Add(100 * 24 * time.Hour) // past the receipt default (30 d), before the checkpoint default (1 y)
	cp := k.CurrentKeyID(Checkpoint)
	rotated, err := k.EnsureCurrent(ctx)
	if err != nil || len(rotated) != 1 || rotated[0] != Receipt || k.CurrentKeyID(Checkpoint) != cp {
		t.Fatalf("EnsureCurrent = %v, %v; want only receipt rotated", rotated, err)
	}
	if err := e.st.Set(ctx, settings.Host, KeyCheckpointRotation, "", "24h"); err != nil {
		t.Fatal(err)
	}
	rotated, err = k.EnsureCurrent(ctx)
	if err != nil || len(rotated) != 1 || rotated[0] != Checkpoint || k.CurrentKeyID(Checkpoint) == cp {
		t.Fatalf("EnsureCurrent after setting = %v, %v; want checkpoint rotated", rotated, err)
	}
	if err := e.st.Set(ctx, settings.Host, KeyCheckpointRotation, "", "0s"); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(10000 * time.Hour)
	if rotated, _ = k.EnsureCurrent(ctx); len(rotated) != 1 || rotated[0] != Receipt {
		t.Fatalf("zero interval must not rotate; rotated %v", rotated)
	}
}

func TestSealRoundTrip(t *testing.T) {
	e := newEnv(t)
	key := e.keys.DataKey()
	pt := []byte("queued job content")
	sealed, err := Seal(key, pt, []byte("job-1"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, pt) {
		t.Fatal("sealed bytes contain the plaintext")
	}
	got, err := Unseal(key, sealed, []byte("job-1"))
	if err != nil || !bytes.Equal(got, pt) {
		t.Fatalf("Unseal = %q, %v", got, err)
	}
	if _, err := Unseal(key, sealed, []byte("job-2")); !errors.Is(err, ErrOpen) {
		t.Errorf("wrong aad = %v; want ErrOpen", err)
	}
	if _, err := Unseal(e.master, sealed, []byte("job-1")); !errors.Is(err, ErrOpen) {
		t.Errorf("wrong key = %v; want ErrOpen", err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := Unseal(key, sealed, []byte("job-1")); !errors.Is(err, ErrOpen) {
		t.Errorf("tampered = %v; want ErrOpen", err)
	}
}

func TestSecretsNotStoredInTheClear(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	dk := e.keys.DataKey()
	for _, q := range []string{`SELECT data_key_sealed FROM hst_host`, `SELECT data_key_recovery FROM hst_host`, `SELECT private_sealed FROM hst_key`} {
		rows, err := e.db.Reader().Query(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var b []byte
			if err := rows.Scan(&b); err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(b, dk) {
				t.Errorf("%s holds the data key in the clear", q)
			}
		}
		rows.Close()
	}
	// A different master key cannot open the keys.
	_, _, err := open(ctx, e.db, e.st, bytes.Repeat([]byte{7}, KeySize), time.Now)
	if err == nil {
		t.Fatal("Open with the wrong master key succeeded")
	}
}

func TestRecoveryCodeShownOnce(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if len(e.code) == 0 || strings.Count(e.code, "-") != 12 {
		t.Fatalf("recovery code %q is not 13 groups", e.code)
	}
	dk, err := e.keys.RecoverDataKey(ctx, strings.ToLower(e.code))
	if err != nil || !bytes.Equal(dk, e.keys.DataKey()) {
		t.Fatalf("RecoverDataKey = %v", err)
	}
	if _, err := e.keys.RecoverDataKey(ctx, "AAAA-BBBB"); !errors.Is(err, ErrBadRecoveryCode) {
		t.Errorf("bad code = %v", err)
	}
	_, wrong, _ := newRecoveryCode()
	if _, err := e.keys.RecoverDataKey(ctx, wrong); !errors.Is(err, ErrBadRecoveryCode) {
		t.Errorf("wrong code = %v", err)
	}
	if _, code := e.open(t); code != "" {
		t.Errorf("second Open returned a recovery code %q; it must be shown once", code)
	}
	// Neither the code nor its bytes are in the database.
	rows, _ := e.db.Reader().Query(ctx, `SELECT data_key_recovery FROM hst_host`)
	defer rows.Close()
	raw, _ := parseRecoveryCode(e.code)
	for rows.Next() {
		var b []byte
		_ = rows.Scan(&b)
		if bytes.Contains(b, raw) || strings.Contains(string(b), e.code) {
			t.Error("recovery code stored in the database")
		}
	}
}

func TestMasterKeyInKeyring(t *testing.T) {
	keyring.MockInit()
	path := filepath.Join(t.TempDir(), "master.key")
	k1, src, err := LoadMasterKey(path)
	if err != nil || src != SourceKeyring {
		t.Fatalf("LoadMasterKey = %v, %v", src, err)
	}
	k2, src, err := LoadMasterKey(path)
	if err != nil || src != SourceKeyring || !bytes.Equal(k1, k2) {
		t.Fatalf("second load = %v, %v, same key %v", src, err, bytes.Equal(k1, k2))
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a file was written although the keyring works")
	}
}

func TestMasterKeyFileFallback(t *testing.T) {
	keyring.MockInitWithError(errors.New("no secret service"))
	path := filepath.Join(t.TempDir(), "sub", "master.key")
	k1, src, err := LoadMasterKey(path)
	if err != nil || src != SourceFile {
		t.Fatalf("LoadMasterKey = %v, %v; want file fallback", src, err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("fallback file mode = %o; want 600", fi.Mode().Perm())
	}
	k2, src, err := LoadMasterKey(path)
	if err != nil || src != SourceFile || !bytes.Equal(k1, k2) {
		t.Fatalf("reload = %v, %v", src, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadMasterKey(path); err == nil {
			t.Error("a group/other-readable master key file was accepted")
		}
	}
}

func TestMasterKeyFileKeptWhenKeyringReturns(t *testing.T) {
	keyring.MockInitWithError(errors.New("down"))
	path := filepath.Join(t.TempDir(), "master.key")
	k1, _, err := LoadMasterKey(path)
	if err != nil {
		t.Fatal(err)
	}
	keyring.MockInit() // keyring now works but holds nothing
	k2, src, err := LoadMasterKey(path)
	if err != nil || src != SourceFile || !bytes.Equal(k1, k2) {
		t.Fatalf("after keyring returns: %v, %v, same key %v", src, err, bytes.Equal(k1, k2))
	}
}
