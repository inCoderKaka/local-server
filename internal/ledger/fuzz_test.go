package ledger

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"testing"
)

// fuzzKey is a fixed key so seeds in testdata/fuzz stay valid.
func fuzzKey() ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed([]byte("local-server fuzz seed 000000000"))
}

func fuzzJWS(header string, r Receipt) string {
	enc := base64.RawURLEncoding
	payload, _ := json.Marshal(r)
	in := enc.EncodeToString([]byte(header)) + "." + enc.EncodeToString(payload)
	return in + "." + enc.EncodeToString(ed25519.Sign(fuzzKey(), []byte(in)))
}

const fuzzHeader = `{"alg":"Ed25519","kid":"k1","typ":"ls-receipt+jws"}`

func fuzzReceipt() Receipt {
	n, size := int64(4), int64(5)
	return Receipt{V: 1, Metering: Metered, UsageID: "44444444-4444-4444-8444-444444444444", HostKID: "h",
		ConsumerID: "c", AccessKeyID: "a", GrantVersionID: "g", Operation: OpChatCompletions, Model: "m",
		Outcome: OutcomeCompleted, StartedAt: "2026-10-31T12:00:00.000Z", FinishedAt: "2026-10-31T12:00:01.000Z",
		LeafIndex: &n, TreeSize: &size}
}

// FuzzVerifyReceipt feeds arbitrary text to the receipt parser and verifier (D-60). It must
// never panic, and a receipt it accepts must have the shape the signer guarantees.
func FuzzVerifyReceipt(f *testing.F) {
	f.Add(fuzzJWS(fuzzHeader, fuzzReceipt()))
	un := fuzzReceipt()
	un.Metering, un.LeafIndex, un.TreeSize = Unmetered, nil, nil
	f.Add(fuzzJWS(fuzzHeader, un))
	f.Add(fuzzJWS(`{"alg":"none","kid":"k1","typ":"ls-receipt+jws"}`, fuzzReceipt()))
	f.Add("a.b.c")
	f.Add("..")
	f.Add("")
	pub := fuzzKey().Public().(ed25519.PublicKey)
	f.Fuzz(func(t *testing.T, jws string) {
		_, _ = ReceiptKeyID(jws)
		r, err := Verify(jws, pub)
		if err != nil {
			return
		}
		switch r.Metering {
		case Metered:
			if r.LeafIndex == nil || r.TreeSize == nil || *r.TreeSize != *r.LeafIndex+1 {
				t.Fatalf("accepted a metered receipt with bad leaf fields: %+v", r)
			}
		case Unmetered:
			if r.LeafIndex != nil || r.TreeSize != nil {
				t.Fatalf("accepted an unmetered receipt with leaf fields: %+v", r)
			}
		default:
			t.Fatalf("accepted metering %q", r.Metering)
		}
	})
}
