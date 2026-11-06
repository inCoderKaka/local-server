package auth_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/auth"
)

const goodInvite = "ls_invite_0123456789ab_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestInviteTokenRoundTrip(t *testing.T) {
	prefix, secret, err := auth.NewInviteToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(prefix) != 12 || len(secret) != 64 || strings.ToLower(prefix+secret) != prefix+secret {
		t.Fatalf("prefix %q secret length %d", prefix, len(secret))
	}
	tok := auth.FormatInviteToken(prefix, secret)
	if want := "ls_invite_" + prefix + "_" + secret; tok != want {
		t.Fatalf("token = %q, want %q", tok, want)
	}
	p, err := auth.ParseInviteToken(tok)
	if err != nil || p.Prefix != prefix || p.Secret.Reveal() != secret {
		t.Fatalf("parsed %+v, %v", p, err)
	}
	other, _, err := auth.NewInviteToken()
	if err != nil || other == prefix {
		t.Fatalf("two tokens share the prefix %q (%v)", prefix, err)
	}
}

func TestInviteSecretMatches(t *testing.T) {
	_, secret, _ := auth.NewInviteToken()
	hash := auth.HashSecret(secret)
	if !auth.InviteSecretMatches(secret, hash) {
		t.Error("the right secret does not match")
	}
	if auth.InviteSecretMatches(secret+"0", hash) || auth.InviteSecretMatches("", hash) || auth.InviteSecretMatches(secret, "") {
		t.Error("a wrong secret or hash matches")
	}
}

func TestParseInviteTokenRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"", "ls_invite_", goodInvite + "x", goodInvite[:len(goodInvite)-1], " " + goodInvite,
		strings.Replace(goodInvite, "ls_invite_", "ls_invitf_", 1),
		strings.Replace(goodInvite, "ls_invite_", "LS_INVITE_", 1),
		"ls_invite_0123456789AB_" + strings.Repeat("a", 64),
		"ls_invite_0123456789ab-" + strings.Repeat("a", 64),
		"ls_invite_0123456789ab_" + strings.Repeat("g", 64),
		strings.Replace(goodKey, "ls_", "ls_invite_", 1)[:len(goodInvite)-1] + "\n",
		goodKey, "Bearer " + goodInvite,
	} {
		_, err := auth.ParseInviteToken(bad)
		if err != auth.ErrMalformedInviteToken {
			t.Errorf("ParseInviteToken(%q) error = %v, want ErrMalformedInviteToken", bad, err)
		}
	}
	// An access key is not an invite token and an invite token is not an access key.
	if _, err := auth.ParseKey(goodInvite); err == nil {
		t.Error("ParseKey accepted an invite token")
	}
}

func TestInviteTokenSecretIsRedacted(t *testing.T) {
	p, err := auth.ParseInviteToken(goodInvite)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{fmt.Sprint(p), fmt.Sprintf("%v %+v %#v %s", p.Secret, p, p, p.Secret)} {
		if strings.Contains(s, "0123456789abcdef") {
			t.Errorf("printed secret: %s", s)
		}
	}
	if _, err := auth.ParseInviteToken("ls_invite_0123456789ab_" + strings.Repeat("g", 64)); strings.Contains(fmt.Sprint(err), "0123456789ab") {
		t.Errorf("error echoes the input: %v", err)
	}
}

func FuzzParseInviteToken(f *testing.F) {
	for _, s := range []string{goodInvite, goodKey, "", "ls_invite_", goodInvite + "\x00"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p, err := auth.ParseInviteToken(s)
		if err != nil {
			if err != auth.ErrMalformedInviteToken {
				t.Fatalf("error = %v, want the fixed ErrMalformedInviteToken", err)
			}
			return
		}
		if auth.FormatInviteToken(p.Prefix, p.Secret.Reveal()) != s {
			t.Fatalf("accepted %q but it is not a well-formed invite token", s)
		}
	})
}
