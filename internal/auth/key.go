package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
)

// The access key format is "ls_<prefix>_<secret>": a fixed scheme, a lowercase hex prefix that
// is stored and shown, and a lowercase hex secret that is shown once and never stored (D-25).
// The lengths are part of the format, not tuning values.
const (
	keyScheme    = "ls"
	prefixLen    = 12 // hex characters, 48 bits: a lookup handle, not a secret
	secretLen    = 64 // hex characters, 256 bits
	bearerScheme = "Bearer "
)

// ErrMalformedKey is returned when an Authorization header does not hold a well-formed key.
var ErrMalformedKey = errors.New("auth: malformed access key")

// Parsed is the prefix and secret read from an Authorization header value.
type Parsed struct {
	Prefix string
	Secret string
}

// FormatKey joins a prefix and a secret into the key text a consumer receives.
func FormatKey(prefix, secret string) string { return keyScheme + "_" + prefix + "_" + secret }

// ParseHeader reads the value of an Authorization header, "Bearer ls_<prefix>_<secret>". It is
// strict: the scheme word is case-insensitive (RFC 9110), everything else must match exactly.
// It never returns any part of the input in its error.
func ParseHeader(h string) (Parsed, error) {
	if len(h) != len(bearerScheme)+len(keyScheme)+1+prefixLen+1+secretLen {
		return Parsed{}, ErrMalformedKey
	}
	if !strings.EqualFold(h[:len(bearerScheme)], bearerScheme) {
		return Parsed{}, ErrMalformedKey
	}
	return ParseKey(h[len(bearerScheme):])
}

// ParseKey reads key text "ls_<prefix>_<secret>".
func ParseKey(k string) (Parsed, error) {
	if len(k) != len(keyScheme)+1+prefixLen+1+secretLen || !strings.HasPrefix(k, keyScheme+"_") {
		return Parsed{}, ErrMalformedKey
	}
	rest := k[len(keyScheme)+1:]
	prefix, secret := rest[:prefixLen], rest[prefixLen+1:]
	if rest[prefixLen] != '_' || !isLowerHex(prefix) || !isLowerHex(secret) {
		return Parsed{}, ErrMalformedKey
	}
	return Parsed{Prefix: prefix, Secret: secret}, nil
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// newKey makes a fresh prefix and secret from the system's random source.
func newKey() (prefix, secret string, err error) {
	var b [(prefixLen + secretLen) / 2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", err
	}
	h := hex.EncodeToString(b[:])
	return h[:prefixLen], h[prefixLen:], nil
}

// HashSecret is the stored form of a secret. A fast hash is enough because the secret is 256
// random bits, so there is nothing to guess (D-25).
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// secretMatches compares a presented secret with a stored hash in constant time.
func secretMatches(secret, storedHash string) bool {
	return subtle.ConstantTimeCompare([]byte(HashSecret(secret)), []byte(storedHash)) == 1
}
