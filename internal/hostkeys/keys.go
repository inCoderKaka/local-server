// Package hostkeys holds the host's identity: the Ed25519 identity key (whose public key is the
// host's global id, D-25), the rotating receipt-signing subkey and the dedicated checkpoint-signing
// key (D-27, D-65), both certified by the identity key, and the host data key with its one-time
// recovery code (D-41). Private keys and the data key are sealed at rest with the host master key
// (D-40) and are never logged.
package hostkeys

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// Purpose says what a key signs.
type Purpose string

// The key purposes, as stored in hst_key.purpose.
const (
	Identity   Purpose = "identity"
	Receipt    Purpose = "receipt"
	Checkpoint Purpose = "checkpoint"
)

// Keys of the settings this package registers (D-38, D-54). Both are owned by the host.
const (
	KeyReceiptRotation    = "hostkeys.receipt_key_rotation_interval"
	KeyCheckpointRotation = "hostkeys.checkpoint_key_rotation_interval"
)

// Errors returned by this package.
var (
	ErrNotSigningPurpose = errors.New("hostkeys: only receipt and checkpoint keys sign on request")
	ErrBadCertificate    = errors.New("hostkeys: key certificate does not verify against the host identity")
)

// PublicKey is a published host key: what a verifier needs to check a signature, or to check
// that the key chains to the host identity.
type PublicKey struct {
	ID        string
	Purpose   Purpose
	Key       ed25519.PublicKey
	Signature []byte // identity's signature over the certificate; nil for the identity key
	CreatedAt time.Time
	RetiredAt *time.Time // set once the key stops signing; the key stays for verification
}

// Certificate returns the bytes the identity key signed to certify this key, which Signature
// covers; nil for the identity key. /v1/keys publishes them with the signature (req_001 D-96).
func (pk PublicKey) Certificate(hostID string) []byte {
	if pk.Purpose == Identity {
		return nil
	}
	return certificate(hostID, pk.Purpose, pk.Key, pk.CreatedAt)
}

type signer struct {
	id      string
	priv    ed25519.PrivateKey
	created time.Time
}

// Keys is the open key custody of this host.
type Keys struct {
	db       *storage.DB
	settings *settings.Store
	now      func() time.Time
	master   []byte

	hostID      string
	identityPub ed25519.PublicKey
	dataKey     []byte

	rotMu   sync.Mutex // serializes rotations: the DB write and the in-memory signer update happen in the same order
	mu      sync.RWMutex
	signers map[Purpose]signer
}

// Open returns the key custody on db, which must have been opened with host.Module() and
// settings.Module(). masterKey is from LoadMasterKey. On the first run it creates the host
// identity, the first receipt and checkpoint keys and the host data key, and returns the
// recovery code; the code is shown to the host once and is not stored, so on every later
// run the returned code is "".
func Open(ctx context.Context, db *storage.DB, st *settings.Store, masterKey []byte) (*Keys, string, error) {
	return open(ctx, db, st, masterKey, time.Now)
}

func open(ctx context.Context, db *storage.DB, st *settings.Store, masterKey []byte, now func() time.Time) (*Keys, string, error) {
	if len(masterKey) != KeySize {
		return nil, "", fmt.Errorf("hostkeys: master key must be %d bytes", KeySize)
	}
	err := st.Register(
		settings.Def{Key: KeyReceiptRotation, Kind: settings.Duration, Default: "720h", Owner: settings.Host,
			Description: "How long a receipt-signing key signs before it is replaced. Zero turns automatic rotation off. The old key is kept for verification."},
		settings.Def{Key: KeyCheckpointRotation, Kind: settings.Duration, Default: "8760h", Owner: settings.Host,
			Description: "How long a ledger checkpoint-signing key signs before it is replaced; rotate rarely so verifiers can follow the log. Zero turns automatic rotation off. The old key is kept for verification."},
	)
	if err != nil {
		return nil, "", err
	}
	k := &Keys{db: db, settings: st, now: now, master: append([]byte(nil), masterKey...), signers: map[Purpose]signer{}}
	code, err := k.bootstrap(ctx)
	if err != nil {
		return nil, "", err
	}
	if err := k.load(ctx); err != nil {
		return nil, "", err
	}
	return k, code, nil
}

func keyID(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:8])
}

func hostIDOf(pub ed25519.PublicKey) string { return base64.RawURLEncoding.EncodeToString(pub) }

func ms(t time.Time) int64 { return t.UnixMilli() }

func keyAAD(id string, p Purpose) []byte { return []byte("hst_key:" + id + ":" + string(p)) }
func dataKeyAAD(hostID string) []byte    { return []byte("hst_host.data_key:" + hostID) }

// certificate is the byte string the identity key signs to certify a subkey.
func certificate(hostID string, p Purpose, pub ed25519.PublicKey, created time.Time) []byte {
	return []byte("local-server/key-certificate/v1\n" + hostID + "\n" + string(p) + "\n" +
		base64.RawURLEncoding.EncodeToString(pub) + "\n" + strconv.FormatInt(ms(created), 10))
}

type newKey struct {
	id     string
	pub    ed25519.PublicKey
	sealed []byte
	sig    []byte
	signer signer
}

func (k *Keys) mintKey(hostID string, p Purpose, identity ed25519.PrivateKey, created time.Time) (newKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return newKey{}, err
	}
	id := keyID(pub)
	sealed, err := Seal(k.master, priv.Seed(), keyAAD(id, p))
	if err != nil {
		return newKey{}, err
	}
	var sig []byte
	if p != Identity {
		sig = ed25519.Sign(identity, certificate(hostID, p, pub, created))
	}
	return newKey{id: id, pub: pub, sealed: sealed, sig: sig, signer: signer{id: id, priv: priv, created: created}}, nil
}

const insertKey = `INSERT INTO hst_key (id, host_id, purpose, public_key, private_sealed, signature, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`

// Bootstrapped reports whether this database already holds the host. Startup passes
// !Bootstrapped as create to LoadMasterKey, so an existing install never gets a new master key.
func Bootstrapped(ctx context.Context, db *storage.DB) (bool, error) {
	rows, err := db.Reader().Query(ctx, `SELECT count(*) FROM hst_host`)
	if err != nil {
		return false, err
	}
	var n int
	if rows.Next() {
		err = rows.Scan(&n)
	}
	if cerr := rows.Close(); err == nil {
		err = errors.Join(rows.Err(), cerr)
	}
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// bootstrap creates the host on the first run. It returns the recovery code, or "".
func (k *Keys) bootstrap(ctx context.Context) (string, error) {
	done, err := Bootstrapped(ctx, k.db)
	if err != nil {
		return "", err
	}
	if done {
		return "", nil
	}

	created := k.now()
	idKey, err := k.mintKey("", Identity, nil, created)
	if err != nil {
		return "", err
	}
	hostID := hostIDOf(idKey.pub)
	receipt, err := k.mintKey(hostID, Receipt, idKey.signer.priv, created)
	if err != nil {
		return "", err
	}
	checkpoint, err := k.mintKey(hostID, Checkpoint, idKey.signer.priv, created)
	if err != nil {
		return "", err
	}
	dataKey, err := randomKey()
	if err != nil {
		return "", err
	}
	dataSealed, err := Seal(k.master, dataKey, dataKeyAAD(hostID))
	if err != nil {
		return "", err
	}
	code := recoveryCodeOf(dataKey)
	raced := false
	err = k.db.Write(ctx, func(tx *storage.Tx) error {
		// Re-check inside the unit: an Open that raced us may already have created the host; then ours is discarded.
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM hst_host`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			raced = true
			return nil
		}
		if _, err := tx.Exec(`INSERT INTO hst_host (id, created_at, data_key_sealed) VALUES (?, ?, ?)`,
			hostID, ms(created), dataSealed); err != nil {
			return err
		}
		for _, nk := range []struct {
			p Purpose
			k newKey
		}{{Identity, idKey}, {Receipt, receipt}, {Checkpoint, checkpoint}} {
			var sig any
			if nk.k.sig != nil {
				sig = nk.k.sig
			}
			if _, err := tx.Exec(insertKey, nk.k.id, hostID, string(nk.p), []byte(nk.k.pub), nk.k.sealed, sig, ms(created)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if raced {
		return "", nil
	}
	return code, nil
}

// load reads the host row and the live signing keys into memory.
func (k *Keys) load(ctx context.Context) error {
	rows, err := k.db.Reader().Query(ctx, `SELECT id, data_key_sealed FROM hst_host`)
	if err != nil {
		return err
	}
	var hostID string
	var dataSealed []byte
	if rows.Next() {
		err = rows.Scan(&hostID, &dataSealed)
	} else {
		err = errors.New("hostkeys: no host row")
	}
	if cerr := rows.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	pubBytes, err := base64.RawURLEncoding.DecodeString(hostID)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return errors.New("hostkeys: stored host id is not an Ed25519 public key")
	}
	k.hostID, k.identityPub = hostID, ed25519.PublicKey(pubBytes)
	if k.dataKey, err = Unseal(k.master, dataSealed, dataKeyAAD(hostID)); err != nil {
		return fmt.Errorf("hostkeys: host data key: %w (is this the right master key?)", err)
	}

	rows, err = k.db.Reader().Query(ctx, `SELECT id, purpose, public_key, private_sealed, created_at FROM hst_key WHERE host_id = ? AND retired_at IS NULL`, hostID)
	if err != nil {
		return err
	}
	defer rows.Close()
	live := map[Purpose]signer{}
	for rows.Next() {
		var id, purpose string
		var pub, sealed []byte
		var created int64
		if err := rows.Scan(&id, &purpose, &pub, &sealed, &created); err != nil {
			return err
		}
		p := Purpose(purpose)
		seed, err := Unseal(k.master, sealed, keyAAD(id, p))
		if err != nil {
			return fmt.Errorf("hostkeys: %s key %s: %w", purpose, id, err)
		}
		priv := ed25519.NewKeyFromSeed(seed)
		if !priv.Public().(ed25519.PublicKey).Equal(ed25519.PublicKey(pub)) {
			return fmt.Errorf("hostkeys: %s key %s: stored public key does not match the private key", purpose, id)
		}
		live[p] = signer{id: id, priv: priv, created: time.UnixMilli(created).UTC()}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range []Purpose{Receipt, Checkpoint} {
		if _, ok := live[p]; !ok {
			return fmt.Errorf("hostkeys: no live %s key", p)
		}
	}
	k.mu.Lock()
	k.signers = live
	k.mu.Unlock()
	return nil
}

// HostID returns the host's global id: the base64url text of the identity public key.
func (k *Keys) HostID() string { return k.hostID }

// IdentityPublicKey returns the host identity public key.
func (k *Keys) IdentityPublicKey() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), k.identityPub...)
}

// DataKey returns a copy of the host data key. Callers seal content with Seal and must
// never log or store the key.
func (k *Keys) DataKey() []byte { return append([]byte(nil), k.dataKey...) }

// Sign signs msg with the live key of purpose Receipt or Checkpoint and returns that key's id.
func (k *Keys) Sign(p Purpose, msg []byte) (keyID string, sig []byte, err error) {
	if p != Receipt && p != Checkpoint {
		return "", nil, ErrNotSigningPurpose
	}
	k.mu.RLock()
	s := k.signers[p]
	k.mu.RUnlock()
	return s.id, ed25519.Sign(s.priv, msg), nil
}

// SignWithKeyID signs the message that build returns for the live key of purpose Receipt or
// Checkpoint, and returns the id of the key that signed it. build is called with the id of the
// key about to sign, so a message that names its signing key (a JWS header) names the right
// one even if the key rotates meanwhile: the key and its id are taken together, once
// (PLT-B-022; req_001 D-27). build must not call back into Keys.Sign.
func (k *Keys) SignWithKeyID(p Purpose, build func(keyID string) []byte) (keyID string, sig []byte, err error) {
	if p != Receipt && p != Checkpoint {
		return "", nil, ErrNotSigningPurpose
	}
	k.mu.RLock()
	s := k.signers[p]
	k.mu.RUnlock()
	return s.id, ed25519.Sign(s.priv, build(s.id)), nil
}

// CurrentKeyID returns the id of the live key of purpose p.
func (k *Keys) CurrentKeyID(p Purpose) string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.signers[p].id
}

// Rotate retires the live key of purpose Receipt or Checkpoint (sets retired_at, keeps the row
// for verification) and creates a new one certified by the identity key. It returns the new id.
func (k *Keys) Rotate(ctx context.Context, p Purpose) (string, error) {
	if p != Receipt && p != Checkpoint {
		return "", ErrNotSigningPurpose
	}
	k.rotMu.Lock()
	defer k.rotMu.Unlock()
	return k.rotate(ctx, p)
}

// rotate does the work of Rotate; the caller holds rotMu.
func (k *Keys) rotate(ctx context.Context, p Purpose) (string, error) {
	var nk newKey
	err := k.db.Write(ctx, func(tx *storage.Tx) error {
		var idID string
		var idSealed []byte
		rows, err := tx.Query(`SELECT id, private_sealed FROM hst_key WHERE host_id = ? AND purpose = 'identity'`, k.hostID)
		if err != nil {
			return err
		}
		if rows.Next() {
			err = rows.Scan(&idID, &idSealed)
		} else {
			err = errors.New("hostkeys: identity key row missing")
		}
		if cerr := rows.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		seed, err := Unseal(k.master, idSealed, keyAAD(idID, Identity))
		if err != nil {
			return err
		}
		created := k.now()
		if nk, err = k.mintKey(k.hostID, p, ed25519.NewKeyFromSeed(seed), created); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE hst_key SET retired_at = ? WHERE host_id = ? AND purpose = ? AND retired_at IS NULL`,
			ms(created), k.hostID, string(p)); err != nil {
			return err
		}
		_, err = tx.Exec(insertKey, nk.id, k.hostID, string(p), []byte(nk.pub), nk.sealed, nk.sig, ms(created))
		return err
	})
	if err != nil {
		return "", err
	}
	k.mu.Lock()
	k.signers[p] = nk.signer
	k.mu.Unlock()
	return nk.id, nil
}

// EnsureCurrent rotates each signing key whose age has reached its rotation interval setting
// and returns the purposes it rotated. A worker calls it periodically.
func (k *Keys) EnsureCurrent(ctx context.Context) ([]Purpose, error) {
	var rotated []Purpose
	for _, c := range []struct {
		p   Purpose
		key string
	}{{Receipt, KeyReceiptRotation}, {Checkpoint, KeyCheckpointRotation}} {
		every, err := k.settings.Duration(ctx, c.key, "")
		if err != nil {
			return rotated, err
		}
		k.rotMu.Lock()
		k.mu.RLock()
		created := k.signers[c.p].created
		k.mu.RUnlock()
		if every <= 0 || k.now().Sub(created) < every {
			k.rotMu.Unlock()
			continue
		}
		_, err = k.rotate(ctx, c.p)
		k.rotMu.Unlock()
		if err != nil {
			return rotated, err
		}
		rotated = append(rotated, c.p)
	}
	return rotated, nil
}

// PublicKeys returns every key of the host, the identity key and the retired ones included,
// oldest first. This is what /v1/keys publishes.
func (k *Keys) PublicKeys(ctx context.Context) ([]PublicKey, error) {
	rows, err := k.db.Reader().Query(ctx, `SELECT id, purpose, public_key, signature, created_at, retired_at FROM hst_key WHERE host_id = ? ORDER BY created_at, purpose`, k.hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublicKey
	for rows.Next() {
		var pk PublicKey
		var purpose string
		var pub []byte
		var created int64
		var retired *int64
		if err := rows.Scan(&pk.ID, &purpose, &pub, &pk.Signature, &created, &retired); err != nil {
			return nil, err
		}
		pk.Purpose, pk.Key, pk.CreatedAt = Purpose(purpose), ed25519.PublicKey(pub), time.UnixMilli(created).UTC()
		if retired != nil {
			t := time.UnixMilli(*retired).UTC()
			pk.RetiredAt = &t
		}
		out = append(out, pk)
	}
	return out, rows.Err()
}

// VerifyKey checks that pk chains to the host identity named by hostID (the id a verifier
// holds): the identity key must be the host id itself, any other key must carry a valid
// identity signature over its certificate.
func VerifyKey(hostID string, pk PublicKey) error {
	idPub, err := base64.RawURLEncoding.DecodeString(hostID)
	if err != nil || len(idPub) != ed25519.PublicKeySize {
		return ErrBadCertificate
	}
	if pk.Purpose == Identity {
		if !ed25519.PublicKey(idPub).Equal(pk.Key) {
			return ErrBadCertificate
		}
		return nil
	}
	if len(pk.Key) != ed25519.PublicKeySize || !ed25519.Verify(idPub, certificate(hostID, pk.Purpose, pk.Key, pk.CreatedAt), pk.Signature) {
		return ErrBadCertificate
	}
	return nil
}

// RecoverDataKey returns the host data key from the recovery code, which is that key (D-41). It
// needs no master key and no database, so a restore on a new machine can decrypt a snapshot with it.
func RecoverDataKey(code string) ([]byte, error) { return parseRecoveryCode(code) }
