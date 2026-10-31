package auth_test

import (
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/auth"
)

const goodKey = "ls_0123456789ab_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// FuzzParseHeader fuzzes the Authorization header parser, the one parser of untrusted key input
// (D-60). The seed corpus is in testdata/fuzz/FuzzParseHeader.
func FuzzParseHeader(f *testing.F) {
	for _, s := range []string{
		"Bearer " + goodKey, "bearer " + goodKey, "BEARER " + goodKey, "Bearer  " + goodKey, "Bearer " + goodKey + " ",
		"Bearer " + strings.ToUpper(goodKey), "Bearer ls__", "Bearer", "", goodKey, "Basic " + goodKey,
		"Bearer ls_0123456789ab_" + strings.Repeat("g", 64), "Bearer ls_0123456789ab-" + strings.Repeat("a", 64),
		"Bearer \x00" + goodKey, "Bearer " + goodKey + "\n", strings.Repeat("Bearer ", 500),
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, h string) {
		p, err := auth.ParseHeader(h)
		if err != nil {
			if err != auth.ErrMalformedKey {
				t.Fatalf("error = %v, want the fixed ErrMalformedKey that carries no input", err)
			}
			return
		}
		if len(p.Prefix) != 12 || len(p.Secret.Reveal()) != 64 {
			t.Fatalf("accepted %q with prefix %q secret length %d", h, p.Prefix, len(p.Secret.Reveal()))
		}
		if !strings.EqualFold(h[:7], "Bearer ") || h[7:] != auth.FormatKey(p.Prefix, p.Secret.Reveal()) {
			t.Fatalf("accepted %q but it is not Bearer plus a well-formed key", h)
		}
	})
}

func TestParseHeader(t *testing.T) {
	if p, err := auth.ParseHeader("bEaReR " + goodKey); err != nil || p.Prefix != "0123456789ab" {
		t.Fatalf("%+v %v", p, err)
	}
	for _, bad := range []string{"", "Bearer ", "Bearer " + goodKey + "x", "Bearer " + goodKey[:len(goodKey)-1], "Token " + goodKey,
		"Bearer ls_0123456789AB_" + strings.Repeat("a", 64)} {
		if _, err := auth.ParseHeader(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
