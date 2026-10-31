package ledger

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/inCoderKaka/local-server/internal/hostkeys"
)

// The JWS protected header values of a receipt (req_001 D-27).
const (
	ReceiptAlg = "Ed25519"
	ReceiptTyp = "ls-receipt+jws"
)

// The values of Receipt.Metering.
const (
	Metered   = "metered"
	Unmetered = "unmetered"
)

// Receipt is the payload of a signed usage receipt: the UsageReceipt schema of
// api/openapi.yaml (api has not generated a type for it yet, PLT-F-052; a golden test keeps
// this type and the schema together). Timestamps are RFC 3339 UTC with milliseconds.
type Receipt struct {
	V                int     `json:"v"`
	Metering         string  `json:"metering"`
	UsageID          string  `json:"usage_id"`
	HostKID          string  `json:"host_kid"`
	ConsumerID       string  `json:"consumer_id"`
	AccessKeyID      string  `json:"access_key_id"`
	GrantVersionID   string  `json:"grant_version_id"`
	JobID            *string `json:"job_id,omitempty"`
	Operation        string  `json:"operation"`
	Model            string  `json:"model"`
	Outcome          string  `json:"outcome"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	StartedAt        string  `json:"started_at"`
	FinishedAt       string  `json:"finished_at"`
	// LeafIndex and TreeSize are set on a metered receipt only (D-51, D-52). TreeSize is the
	// size of the log once this leaf was appended, LeafIndex + 1; there is no checkpoint.
	LeafIndex *int64 `json:"leaf_index,omitempty"`
	TreeSize  *int64 `json:"tree_size,omitempty"`
}

// Signed is a receipt after signing.
type Signed struct {
	// JWS is the compact serialization, what goes into the LS-Receipt header.
	JWS string
	// KeyID is the receipt key that signed it (the header's kid).
	KeyID string
	// Signature is the raw 64-byte Ed25519 signature, stored on use_record (D-40).
	Signature []byte
}

// Signer is the receipt-signing key custody; *hostkeys.Keys implements it.
type Signer interface {
	SignWithKeyID(p hostkeys.Purpose, build func(keyID string) []byte) (keyID string, sig []byte, err error)
	HostID() string
}

func (u Usage) receipt(hostKID string) Receipt {
	r := Receipt{V: LeafVersion, UsageID: u.UsageID, HostKID: hostKID, ConsumerID: u.ConsumerID,
		AccessKeyID: u.AccessKeyID, GrantVersionID: u.GrantVersionID, Operation: u.Operation, Model: u.Model,
		Outcome: u.Outcome, PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens,
		TotalTokens: u.TotalTokens(), StartedAt: u.StartedAt.Format(timeLayout), FinishedAt: u.FinishedAt.Format(timeLayout)}
	if u.JobID != "" {
		j := u.JobID
		r.JobID = &j
	}
	return r
}

// SignReceipt signs r after setting V and HostKID, and checks that the metered and
// unmetered forms hold exactly their own fields (D-51, D-52). Use Record
// for a metered call: it signs inside the unit of work that fixes the leaf. Calling this with
// leaf fields for a leaf that is not in the ledger would make a false receipt.
func (l *Ledger) SignReceipt(r Receipt) (Signed, error) {
	r.V, r.HostKID = LeafVersion, l.signer.HostID()
	switch r.Metering {
	case Metered:
		if r.LeafIndex == nil || r.TreeSize == nil || *r.TreeSize != *r.LeafIndex+1 {
			return Signed{}, errors.New("ledger: a metered receipt needs leaf_index and tree_size = leaf_index + 1")
		}
	case Unmetered:
		if r.LeafIndex != nil || r.TreeSize != nil {
			return Signed{}, errors.New("ledger: an unmetered receipt has no leaf_index or tree_size")
		}
	default:
		return Signed{}, fmt.Errorf("ledger: unknown metering %q", r.Metering)
	}
	payload, err := json.Marshal(r)
	if err != nil {
		return Signed{}, err
	}
	enc := base64.RawURLEncoding
	// The header names the key that signs, so the signer hands the key id to build the signed
	// input from: the id and the key are taken together, however the keys rotate (PLT-B-022).
	var input string
	var buildErr error
	kid, sig, err := l.signer.SignWithKeyID(hostkeys.Receipt, func(kid string) []byte {
		kidJSON, err := json.Marshal(kid)
		if err != nil {
			buildErr = err
			return nil
		}
		header := `{"alg":"` + ReceiptAlg + `","kid":` + string(kidJSON) + `,"typ":"` + ReceiptTyp + `"}`
		input = enc.EncodeToString([]byte(header)) + "." + enc.EncodeToString(payload)
		return []byte(input)
	})
	if err != nil {
		return Signed{}, err
	}
	if buildErr != nil {
		return Signed{}, buildErr
	}
	return Signed{JWS: input + "." + enc.EncodeToString(sig), KeyID: kid, Signature: sig}, nil
}

// SignUnmetered signs the receipt for a call that was answered but could not be recorded
// (D-51): the engine's token counts, no leaf_index and no tree_size. It touches no table.
func (l *Ledger) SignUnmetered(u Usage) (Signed, error) {
	u, err := u.normalized()
	if err != nil {
		return Signed{}, err
	}
	r := u.receipt(l.signer.HostID())
	r.Metering = Unmetered
	return l.SignReceipt(r)
}

// Errors returned by Verify.
var (
	ErrMalformedReceipt = errors.New("ledger: malformed receipt")
	ErrBadSignature     = errors.New("ledger: receipt signature does not verify")
)

type header struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

func parseHeader(jws string) (parts []string, h header, err error) {
	parts = strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil, h, fmt.Errorf("%w: want 3 parts", ErrMalformedReceipt)
	}
	raw, err := decodeB64(parts[0])
	if err != nil {
		return nil, h, fmt.Errorf("%w: header: %v", ErrMalformedReceipt, err)
	}
	if err := strictJSON(raw, &h); err != nil {
		return nil, h, fmt.Errorf("%w: header: %v", ErrMalformedReceipt, err)
	}
	if h.Alg != ReceiptAlg || h.Typ != ReceiptTyp || h.Kid == "" {
		return nil, h, fmt.Errorf("%w: header must be alg %s, typ %s and name a kid", ErrMalformedReceipt, ReceiptAlg, ReceiptTyp)
	}
	return parts, h, nil
}

// decodeB64 decodes unpadded base64url strictly. The decoder skips CR and LF, so a segment is
// accepted only if it is the canonical encoding of what it decodes to (PLT-B-024).
func decodeB64(s string) ([]byte, error) {
	b, err := base64.RawURLEncoding.Strict().DecodeString(s)
	if err != nil {
		return nil, err
	}
	if base64.RawURLEncoding.EncodeToString(b) != s {
		return nil, errors.New("not canonical base64url")
	}
	return b, nil
}

func strictJSON(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.More() {
		return errors.New("trailing data")
	}
	return nil
}

// ReceiptKeyID returns the kid in the receipt's protected header, so a verifier can pick the
// matching key from /v1/keys. It does not verify anything.
func ReceiptKeyID(jws string) (string, error) {
	_, h, err := parseHeader(jws)
	return h.Kid, err
}

// Verify checks a receipt's signature with the receipt public key named by its kid and returns
// the payload. It also checks the shape the signer guarantees: version 1, and leaf_index and
// tree_size present, with tree_size = leaf_index + 1, on a metered receipt and absent on an
// unmetered one. It does not check the key chains to the host (hostkeys.VerifyKey does).
func Verify(jws string, key ed25519.PublicKey) (Receipt, error) {
	var r Receipt
	if len(key) != ed25519.PublicKeySize {
		return r, errors.New("ledger: not an Ed25519 public key")
	}
	parts, _, err := parseHeader(jws)
	if err != nil {
		return r, err
	}
	sig, err := decodeB64(parts[2])
	if err != nil {
		return r, fmt.Errorf("%w: signature: %v", ErrMalformedReceipt, err)
	}
	if !ed25519.Verify(key, []byte(parts[0]+"."+parts[1]), sig) {
		return r, ErrBadSignature
	}
	raw, err := decodeB64(parts[1])
	if err != nil {
		return r, fmt.Errorf("%w: payload: %v", ErrMalformedReceipt, err)
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return Receipt{}, fmt.Errorf("%w: payload: %v", ErrMalformedReceipt, err)
	}
	if r.V != LeafVersion {
		return Receipt{}, fmt.Errorf("%w: unsupported version %d", ErrMalformedReceipt, r.V)
	}
	switch {
	case r.Metering == Metered && r.LeafIndex != nil && r.TreeSize != nil && *r.TreeSize == *r.LeafIndex+1 && *r.LeafIndex >= 0:
	case r.Metering == Unmetered && r.LeafIndex == nil && r.TreeSize == nil:
	default:
		return Receipt{}, fmt.Errorf("%w: metering %q does not match leaf_index and tree_size", ErrMalformedReceipt, r.Metering)
	}
	return r, nil
}
