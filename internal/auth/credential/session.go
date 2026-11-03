package credential

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

// Names of the session cookies (api/openapi.yaml securitySchemes). A surface passes its own name.
const (
	AdminSessionCookie    = "ls_admin_session"
	ConsumerSessionCookie = "ls_session"
)

// tokenBytes is the size of a session token before encoding: 256 bits from crypto/rand.
const tokenBytes = 32

// NewToken returns a new session token: 32 random bytes, base64url without padding. It is the
// value of the session cookie and is never stored.
func NewToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("credential: read random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns the form of a token that is stored (idn_session.token_hash): the SHA-256 of
// the token in lowercase hex. A token is 256 random bits, so a fast hash is right; a stolen
// database does not give the cookie value back.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// SessionCookie returns the Set-Cookie value that starts a session: HttpOnly, Secure,
// SameSite=Strict, Path=/, living until expires. now is the current time; an expiry at or before
// it gives a cookie that is already gone.
func SessionCookie(name, token string, expires, now time.Time) string {
	maxAge := int(expires.Sub(now) / time.Second)
	if maxAge <= 0 {
		maxAge = -1
	}
	return (&http.Cookie{Name: name, Value: token, Path: "/", Expires: expires.UTC(), MaxAge: maxAge,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode}).String()
}

// ClearCookie returns the Set-Cookie value that removes the session cookie, with the same
// attributes so the browser matches it.
func ClearCookie(name string) string {
	return (&http.Cookie{Name: name, Value: "", Path: "/", Expires: time.Unix(0, 0).UTC(), MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode}).String()
}
