package auth

import (
	"errors"
	"strings"

	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// The invite token format is "ls_invite_<prefix>_<secret>": the same prefix and secret shapes as
// an access key (a lowercase hex prefix that is stored and shown, a lowercase hex secret that is
// shown once and only its hash stored), behind the scheme word "ls_invite" (req_001 D-13, D-25).
// The lengths are part of the format, not tuning values.
const inviteScheme = "ls_invite"

// ErrMalformedInviteToken is returned when text is not a well-formed invite token.
var ErrMalformedInviteToken = errors.New("auth: malformed invite token")

// NewInviteToken makes a fresh prefix and secret for an invite from the system's random source.
// The host stores the prefix and HashSecret(secret) and shows FormatInviteToken(prefix, secret)
// once.
func NewInviteToken() (prefix, secret string, err error) { return newKey() }

// FormatInviteToken joins a prefix and a secret into the token text an invitee receives.
func FormatInviteToken(prefix, secret string) string { return inviteScheme + "_" + prefix + "_" + secret }

// ParseInviteToken reads token text "ls_invite_<prefix>_<secret>". It is strict, and it never
// returns any part of the input in its error. The secret in the result is redacted.
func ParseInviteToken(t string) (Parsed, error) {
	if len(t) != len(inviteScheme)+1+prefixLen+1+secretLen || !strings.HasPrefix(t, inviteScheme+"_") {
		return Parsed{}, ErrMalformedInviteToken
	}
	rest := t[len(inviteScheme)+1:]
	prefix, secret := rest[:prefixLen], rest[prefixLen+1:]
	if rest[prefixLen] != '_' || !isLowerHex(prefix) || !isLowerHex(secret) {
		return Parsed{}, ErrMalformedInviteToken
	}
	return Parsed{Prefix: prefix, Secret: telemetry.Redact(secret)}, nil
}

// InviteSecretMatches reports, in constant time, whether a presented invite secret hashes to the
// stored hash.
func InviteSecretMatches(secret, storedHash string) bool { return secretMatches(secret, storedHash) }
