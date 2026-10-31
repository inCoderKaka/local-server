package checkpoint

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/mod/sumdb/note"
	"golang.org/x/mod/sumdb/tlog"
)

// sigTypeEd25519 is the C2SP signed-note signature type of Ed25519.
const sigTypeEd25519 = 0x01

// Errors returned by Parse and Verify.
var (
	ErrMalformedNote = errors.New("checkpoint: malformed checkpoint note")
	ErrBadNote       = errors.New("checkpoint: note signature does not verify with the given keys")
	ErrBadKeyName    = errors.New("checkpoint: key name must be non-empty with no white space or '+'")
)

// Text is the unsigned body of a checkpoint note.
type Text struct {
	Origin   string
	TreeSize int64
	RootHash tlog.Hash
	// Extensions are the optional extra lines after the root hash; this host writes none.
	Extensions []string
}

// Format returns the note text: origin, size, base64 root, each line ending in a newline.
func (t Text) Format() string {
	var b strings.Builder
	b.WriteString(t.Origin + "\n" + strconv.FormatInt(t.TreeSize, 10) + "\n" +
		base64.StdEncoding.EncodeToString(t.RootHash[:]) + "\n")
	for _, e := range t.Extensions {
		b.WriteString(e + "\n")
	}
	return b.String()
}

// ParseText parses a checkpoint note text strictly: a non-empty origin, a canonical decimal
// size, a 32-byte standard-base64 root, and optional non-empty extension lines, each line
// ending in a newline.
func ParseText(s string) (Text, error) {
	var t Text
	if !strings.HasSuffix(s, "\n") {
		return t, fmt.Errorf("%w: text must end in a newline", ErrMalformedNote)
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	if len(lines) < 3 {
		return t, fmt.Errorf("%w: want origin, size and root lines", ErrMalformedNote)
	}
	if lines[0] == "" {
		return t, fmt.Errorf("%w: empty origin", ErrMalformedNote)
	}
	t.Origin = lines[0]
	n, err := strconv.ParseInt(lines[1], 10, 64)
	if err != nil || n < 0 || strconv.FormatInt(n, 10) != lines[1] {
		return Text{}, fmt.Errorf("%w: tree size %q", ErrMalformedNote, lines[1])
	}
	t.TreeSize = n
	raw, err := base64.StdEncoding.Strict().DecodeString(lines[2])
	// The base64 decoder skips CR and LF, so compare the canonical re-encoding too.
	if err != nil || len(raw) != len(t.RootHash) || base64.StdEncoding.EncodeToString(raw) != lines[2] {
		return Text{}, fmt.Errorf("%w: root hash", ErrMalformedNote)
	}
	copy(t.RootHash[:], raw)
	for _, e := range lines[3:] {
		if e == "" {
			return Text{}, fmt.Errorf("%w: empty extension line", ErrMalformedNote)
		}
		t.Extensions = append(t.Extensions, e)
	}
	return t, nil
}

func validName(name string) bool {
	return name != "" && utf8.ValidString(name) && strings.IndexFunc(name, unicode.IsSpace) < 0 && !strings.Contains(name, "+")
}

// KeyHash returns the 4-byte key hash of a signed-note Ed25519 key.
func KeyHash(name string, pub ed25519.PublicKey) [4]byte {
	h := sha256.Sum256(append([]byte(name+"\n"+string([]byte{sigTypeEd25519})), pub...))
	return [4]byte{h[0], h[1], h[2], h[3]}
}

// VerifierKey returns the signed-note verifier key text "<name>+<hash>+<base64 keydata>".
func VerifierKey(name string, pub ed25519.PublicKey) (string, error) {
	if !validName(name) {
		return "", ErrBadKeyName
	}
	if len(pub) != ed25519.PublicKeySize {
		return "", errors.New("checkpoint: not an Ed25519 public key")
	}
	h := KeyHash(name, pub)
	return name + "+" + hex.EncodeToString(h[:]) + "+" + base64.StdEncoding.EncodeToString(append([]byte{sigTypeEd25519}, pub...)), nil
}

// signedNote assembles the signed note from the text and a signature made over it.
func signedNote(text, name string, pub ed25519.PublicKey, sig []byte) string {
	h := KeyHash(name, pub)
	return text + "\n— " + name + " " + base64.StdEncoding.EncodeToString(append(h[:], sig...)) + "\n"
}

// Verified is a checkpoint whose signature verified.
type Verified struct {
	Text
	// KeyName is the name of the key whose signature verified.
	KeyName string
}

// Verify checks a signed checkpoint note against verifier keys (each from VerifierKey or the
// ls_note_verifier_key member of /v1/keys; pass the retired keys too, so old notes verify). It
// returns the parsed text. The note needs a valid signature by a key whose name equals the
// note's origin.
func Verify(signed string, verifierKeys ...string) (Verified, error) {
	var vs []note.Verifier
	seen := map[string]bool{}
	type nameHash struct {
		name string
		hash uint32
	}
	byID := map[nameHash]string{}
	for _, k := range verifierKeys {
		if seen[k] {
			continue
		}
		seen[k] = true
		v, err := note.NewVerifier(k)
		if err != nil {
			return Verified{}, fmt.Errorf("%w: verifier key: %v", ErrBadNote, err)
		}
		id := nameHash{v.Name(), v.KeyHash()}
		if prev, ok := byID[id]; ok && prev != k {
			return Verified{}, fmt.Errorf("%w: conflicting verifier keys %s+%08x", ErrBadNote, v.Name(), v.KeyHash())
		}
		byID[id] = k
		vs = append(vs, v)
	}
	n, err := open(signed, vs)
	if err != nil {
		return Verified{}, err
	}
	t, err := ParseText(n.Text)
	if err != nil {
		return Verified{}, err
	}
	for _, s := range n.Sigs {
		if s.Name == t.Origin {
			return Verified{Text: t, KeyName: s.Name}, nil
		}
	}
	return Verified{}, fmt.Errorf("%w: no verified signature by a key named like the origin %q", ErrBadNote, t.Origin)
}

// open wraps note.Open and sorts its errors into ErrBadNote and ErrMalformedNote.
func open(signed string, vs []note.Verifier) (*note.Note, error) {
	n, err := note.Open([]byte(signed), note.VerifierList(vs...))
	if err != nil {
		var unverified *note.UnverifiedNoteError
		var invalid *note.InvalidSignatureError
		var unknown *note.UnknownVerifierError
		if errors.As(err, &unverified) || errors.As(err, &invalid) || errors.As(err, &unknown) {
			return nil, fmt.Errorf("%w: %v", ErrBadNote, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrMalformedNote, err)
	}
	return n, nil
}
