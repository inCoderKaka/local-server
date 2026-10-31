package hostkeys

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
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

// recoveryCodeOf returns the recovery code for the host data key: its 52 base32 characters in
// groups of four (D-41).
func recoveryCodeOf(dataKey []byte) string {
	s := codeEncoding.EncodeToString(dataKey)
	var parts []string
	for len(s) > 0 {
		n := min(4, len(s))
		parts = append(parts, s[:n])
		s = s[n:]
	}
	return strings.Join(parts, "-")
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

// ErrBadRecoveryCode is returned when a recovery code is malformed or does not match.
var ErrBadRecoveryCode = errors.New("hostkeys: recovery code is malformed")
