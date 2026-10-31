package hostkeys

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
)

// KeySize is the size in bytes of the master key, the host data key and every AEAD key here.
const KeySize = 32

// ErrOpen is returned by Unseal when the key, the associated data or the ciphertext is wrong.
// It never says which, and never includes any secret.
var ErrOpen = errors.New("hostkeys: cannot unseal (wrong key, wrong context or damaged data)")

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("hostkeys: key must be %d bytes", KeySize)
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// Seal encrypts and authenticates plaintext with key (AES-256-GCM, random nonce). aad is
// associated data that Unseal must be given again; use it to bind the ciphertext to what it
// belongs to, for example a table and row id. The result is nonce || ciphertext || tag.
func Seal(key, plaintext, aad []byte) ([]byte, error) {
	a, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.NonceSize(), a.NonceSize()+len(plaintext)+a.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return a.Seal(nonce, nonce, plaintext, aad), nil
}

// Unseal reverses Seal.
func Unseal(key, sealed, aad []byte) ([]byte, error) {
	a, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < a.NonceSize()+a.Overhead() {
		return nil, ErrOpen
	}
	pt, err := a.Open(nil, sealed[:a.NonceSize()], sealed[a.NonceSize():], aad)
	if err != nil {
		return nil, ErrOpen
	}
	return pt, nil
}

func randomKey() ([]byte, error) {
	k := make([]byte, KeySize)
	_, err := rand.Read(k)
	return k, err
}

var codeEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newRecoveryCode returns a random recovery code as secret bytes and as text: 52 base32
// characters in groups of four.
func newRecoveryCode() ([]byte, string, error) {
	raw, err := randomKey()
	if err != nil {
		return nil, "", err
	}
	s := codeEncoding.EncodeToString(raw)
	var parts []string
	for len(s) > 0 {
		n := min(4, len(s))
		parts = append(parts, s[:n])
		s = s[n:]
	}
	return raw, strings.Join(parts, "-"), nil
}

// parseRecoveryCode accepts the text form with or without dashes, spaces and lower case.
func parseRecoveryCode(code string) ([]byte, error) {
	clean := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
	raw, err := codeEncoding.DecodeString(clean)
	if err != nil || len(raw) != KeySize {
		return nil, ErrBadRecoveryCode
	}
	return raw, nil
}

// recoveryWrapKey derives the key that wraps the host data key from the recovery code's
// secret bytes. The code is 256 bits of randomness, so a plain HKDF (no slow hash) suffices.
func recoveryWrapKey(raw []byte) ([]byte, error) {
	return hkdf.Key(sha256.New, raw, nil, "local-server/host-data-key-recovery/v1", KeySize)
}

// ErrBadRecoveryCode is returned when a recovery code is malformed or does not match.
var ErrBadRecoveryCode = errors.New("hostkeys: recovery code is not valid for this host")
