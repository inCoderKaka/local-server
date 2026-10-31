package checkpoint

import (
	"crypto/ed25519"
	"strings"
	"testing"

	"golang.org/x/mod/sumdb/tlog"
)

func fuzzKey() ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed([]byte("local-server fuzz seed 000000000"))
}

const fuzzName = "example.org/ledger"

func fuzzNote(text string) string {
	priv := fuzzKey()
	return signedNote(text, fuzzName, priv.Public().(ed25519.PublicKey), ed25519.Sign(priv, []byte(text)))
}

// FuzzParseText feeds arbitrary text to the checkpoint text parser (D-60): no panic, and what
// parses formats back to the same text.
func FuzzParseText(f *testing.F) {
	f.Add(Text{Origin: fuzzName, TreeSize: 7, RootHash: tlog.Hash{1, 2, 3}}.Format())
	f.Add(Text{Origin: fuzzName, TreeSize: 1, Extensions: []string{"x"}}.Format())
	f.Add("")
	f.Add("a\n0\n\n")
	f.Add("a\n01\nAAAA\n")
	f.Fuzz(func(t *testing.T, s string) {
		p, err := ParseText(s)
		if err != nil {
			return
		}
		if got := p.Format(); got != s {
			t.Fatalf("Format(ParseText(%q)) = %q", s, got)
		}
	})
}

// FuzzVerifyNote feeds arbitrary signed-note text to Verify (D-60): no panic, and a note that
// verifies has the origin of the key name and parses.
func FuzzVerifyNote(f *testing.F) {
	vk, err := VerifierKey(fuzzName, fuzzKey().Public().(ed25519.PublicKey))
	if err != nil {
		f.Fatal(err)
	}
	valid := fuzzNote(Text{Origin: fuzzName, TreeSize: 3, RootHash: tlog.Hash{9}}.Format())
	f.Add(valid)
	f.Add(strings.Replace(valid, "\n3\n", "\n4\n", 1))
	f.Add(fuzzNote(Text{Origin: "other", TreeSize: 3}.Format()))
	f.Add("")
	f.Add("\n\n— x y\n")
	f.Add(valid + valid)
	f.Fuzz(func(t *testing.T, s string) {
		v, err := Verify(s, vk)
		if err != nil {
			return
		}
		if v.Origin != fuzzName || v.KeyName != fuzzName || v.TreeSize < 0 {
			t.Fatalf("accepted %+v", v)
		}
	})
}
