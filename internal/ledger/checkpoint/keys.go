package checkpoint

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/ledger"
)

// JWK is one entry of the /v1/keys JWK Set: an Ed25519 public key as an RFC 8037 OKP JWK
// with local-server members prefixed ls_. Its JSON form is the HostPublicKey schema of
// api/openapi.yaml.
type JWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	// Kid is the key id a receipt or checkpoint names: the host id for the identity key
	// (D-94), the hostkeys key id for the others.
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	// LsRole is "identity", "receipt" or "checkpoint".
	LsRole            string       `json:"ls_role"`
	LsParentKid       *string      `json:"ls_parent_kid"`
	LsEndorsement     *Endorsement `json:"ls_endorsement"`
	LsNoteVerifierKey *string      `json:"ls_note_verifier_key"`
	LsCreatedAt       string       `json:"ls_created_at"`
	LsRetiredAt       *string      `json:"ls_retired_at"`
}

// Endorsement is the HostKeyEndorsement schema: the identity key's signature over a key's
// certificate bytes, with those bytes, both base64url without padding (req_001 D-96).
type Endorsement struct {
	Certificate string `json:"certificate"`
	Signature   string `json:"signature"`
}

// KeySet is the /v1/keys body: the HostKeySet schema.
type KeySet struct {
	Keys []JWK `json:"keys"`
}

// KeySet returns every host key, current and retired, as a JWK Set (RFC 7517) for /v1/keys
// (D-27, D-65, D-94).
func (s *Service) KeySet(ctx context.Context) (KeySet, error) {
	origin, err := s.origin(ctx)
	if err != nil {
		return KeySet{}, err
	}
	pks, err := s.keys.PublicKeys(ctx)
	if err != nil {
		return KeySet{}, err
	}
	return buildKeySet(s.keys.HostID(), origin, pks)
}

func buildKeySet(hostID, origin string, pks []hostkeys.PublicKey) (KeySet, error) {
	set := KeySet{Keys: make([]JWK, 0, len(pks))}
	for _, pk := range pks {
		j := JWK{Kty: "OKP", Crv: "Ed25519", X: base64.RawURLEncoding.EncodeToString(pk.Key), Kid: pk.ID,
			Alg: "Ed25519", Use: "sig", LsRole: string(pk.Purpose), LsCreatedAt: pk.CreatedAt.UTC().Format(time.RFC3339Nano)}
		if pk.Purpose == hostkeys.Identity {
			j.Kid = hostID
		} else {
			parent := hostID
			j.LsParentKid = &parent
			j.LsEndorsement = &Endorsement{
				Certificate: base64.RawURLEncoding.EncodeToString(pk.Certificate(hostID)),
				Signature:   base64.RawURLEncoding.EncodeToString(pk.Signature)}
		}
		if pk.Purpose == hostkeys.Checkpoint {
			vk, err := VerifierKey(origin, pk.Key)
			if err != nil {
				return KeySet{}, err
			}
			j.LsNoteVerifierKey = &vk
		}
		if pk.RetiredAt != nil {
			r := pk.RetiredAt.UTC().Format(time.RFC3339Nano)
			j.LsRetiredAt = &r
		}
		set.Keys = append(set.Keys, j)
	}
	return set, nil
}

// VerifierKeys returns the signed-note verifier keys of the checkpoint keys in the set,
// current and retired, ready for Verify.
func (k KeySet) VerifierKeys() []string {
	var out []string
	for _, j := range k.Keys {
		if j.LsRole == string(hostkeys.Checkpoint) && j.LsNoteVerifierKey != nil {
			out = append(out, *j.LsNoteVerifierKey)
		}
	}
	return out
}

// ErrNoKey is returned by VerifyReceipt when the set has no receipt key for the receipt's kid.
var ErrNoKey = errors.New("checkpoint: the key set has no receipt key with the receipt's kid")

// ErrWrongHost is returned by VerifyReceipt when the receipt's host_kid is not the identity
// key of the set, or the set has no identity key.
var ErrWrongHost = errors.New("checkpoint: the receipt's host_kid is not the host identity in the key set")

// VerifyReceipt verifies a receipt with the receipt key of the set that its header names by
// kid (retired keys included) and checks that host_kid is the set's identity key (D-94). It
// does not check the endorsement chain (hostkeys.VerifyKey does, with the key rows).
func (k KeySet) VerifyReceipt(jws string) (ledger.Receipt, error) {
	kid, err := ledger.ReceiptKeyID(jws)
	if err != nil {
		return ledger.Receipt{}, err
	}
	var identity string
	var pub ed25519.PublicKey
	for _, j := range k.Keys {
		switch {
		case j.LsRole == string(hostkeys.Identity):
			identity = j.Kid
		case j.LsRole == string(hostkeys.Receipt) && j.Kid == kid && pub == nil:
			x, err := decodeB64URL(j.X)
			if err != nil || len(x) != ed25519.PublicKeySize || j.Kty != "OKP" || j.Crv != "Ed25519" {
				return ledger.Receipt{}, fmt.Errorf("checkpoint: key %s is not an Ed25519 OKP key", kid)
			}
			pub = x
		}
	}
	if pub == nil {
		return ledger.Receipt{}, ErrNoKey
	}
	r, err := ledger.Verify(jws, pub)
	if err != nil {
		return ledger.Receipt{}, err
	}
	if identity == "" || r.HostKID != identity {
		return ledger.Receipt{}, ErrWrongHost
	}
	return r, nil
}

// ErrBadEndorsement is returned by VerifyEndorsement when a key's endorsement is missing, does
// not verify with the set's identity key, or certifies something other than the entry it is on.
var ErrBadEndorsement = errors.New("checkpoint: key endorsement does not verify against the identity key of the set")

// VerifyEndorsement checks every receipt and checkpoint key of the set against the identity
// key of the same set, with nothing else: the endorsement's signature must verify over its
// certificate bytes, and the certificate must name the identity (as host id), the key's role,
// public key and creation time (req_001 D-96). The identity entry itself must have kid equal to
// its own public key (D-94).
func (k KeySet) VerifyEndorsement() error {
	var idJWK *JWK
	identities := 0
	for i := range k.Keys {
		if k.Keys[i].LsRole == string(hostkeys.Identity) {
			idJWK = &k.Keys[i]
			identities++
		}
	}
	if identities != 1 || idJWK.Kid != idJWK.X {
		return ErrBadEndorsement
	}
	idPub, err := decodeB64URL(idJWK.X)
	if err != nil || len(idPub) != ed25519.PublicKeySize {
		return ErrBadEndorsement
	}
	for _, j := range k.Keys {
		if j.LsRole == string(hostkeys.Identity) {
			continue
		}
		if j.LsEndorsement == nil {
			return fmt.Errorf("%w: key %s has none", ErrBadEndorsement, j.Kid)
		}
		if j.LsParentKid == nil || *j.LsParentKid != idJWK.Kid {
			return fmt.Errorf("%w: key %s: ls_parent_kid is not the identity", ErrBadEndorsement, j.Kid)
		}
		cert, err1 := decodeB64URL(j.LsEndorsement.Certificate)
		sig, err2 := decodeB64URL(j.LsEndorsement.Signature)
		created, err3 := time.Parse(time.RFC3339Nano, j.LsCreatedAt)
		if err1 != nil || err2 != nil || err3 != nil || !ed25519.Verify(idPub, cert, sig) {
			return fmt.Errorf("%w: key %s", ErrBadEndorsement, j.Kid)
		}
		want := "local-server/key-certificate/v1\n" + idJWK.Kid + "\n" + j.LsRole + "\n" + j.X + "\n" + strconv.FormatInt(created.UnixMilli(), 10)
		if string(cert) != want {
			return fmt.Errorf("%w: key %s: the certificate names another key", ErrBadEndorsement, j.Kid)
		}
	}
	return nil
}

// decodeB64URL decodes unpadded base64url strictly. The decoder skips CR and LF (see ParseText
// and PLT-B-024), so the canonical re-encoding must equal the input too.
func decodeB64URL(s string) ([]byte, error) {
	b, err := base64.RawURLEncoding.Strict().DecodeString(s)
	if err != nil {
		return nil, err
	}
	if base64.RawURLEncoding.EncodeToString(b) != s {
		return nil, errors.New("checkpoint: base64url is not canonical")
	}
	return b, nil
}
