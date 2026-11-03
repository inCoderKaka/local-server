package credential

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTokenIsRandomAndStoredAsAHash(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewToken()
	if a == b {
		t.Fatal("two tokens are equal")
	}
	if len(a) != 43 || strings.ContainsAny(a, "+/=") {
		t.Fatalf("token %q is not 32 bytes of unpadded base64url", a)
	}
	h := HashToken(a)
	if len(h) != 64 || strings.Contains(h, a) || h != HashToken(a) || h == HashToken(b) {
		t.Fatalf("hash %q", h)
	}
}

func parseSetCookie(t *testing.T, v string) *http.Cookie {
	t.Helper()
	c, err := http.ParseSetCookie(v)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSessionCookieAttributes(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	raw := SessionCookie(AdminSessionCookie, "tok", now.Add(12*time.Hour), now)
	c := parseSetCookie(t, raw)
	if c.Name != "ls_admin_session" || c.Value != "tok" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Fatalf("cookie = %+v (%s)", c, raw)
	}
	if c.MaxAge != 12*3600 || !c.Expires.Equal(now.Add(12*time.Hour)) {
		t.Fatalf("lifetime: max-age %d, expires %v", c.MaxAge, c.Expires)
	}
	if gone := parseSetCookie(t, SessionCookie("x", "tok", now, now)); gone.MaxAge >= 0 {
		t.Errorf("an expired cookie has max-age %d", gone.MaxAge)
	}
}

func TestClearCookieMatchesAndRemoves(t *testing.T) {
	c := parseSetCookie(t, ClearCookie(ConsumerSessionCookie))
	if c.Name != "ls_session" || c.Value != "" || c.MaxAge >= 0 || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Fatalf("clear cookie = %+v", c)
	}
}
