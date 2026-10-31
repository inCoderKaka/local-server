package identity

import (
	"context"
	"errors"
)

// ConsentRef names the idn_consent record that permits one capture (D-44, D-45).
type ConsentRef struct {
	PrincipalID string
	Purpose     string
	// PolicyVersion is the version of the terms the principal accepted.
	PolicyVersion string
}

// ContentCapture stores prompts or responses. It is defined and off: Phase 1
// has no adapter, and prompts and responses are never stored by default
// (D-31, D-40). An adapter may only ever capture behind a consent record that
// matches the principal and purpose and has not been withdrawn (D-45).
type ContentCapture interface {
	// Capture stores content for the consent's principal and purpose. It must
	// return ErrNoConsent when no matching, unwithdrawn consent record exists.
	Capture(ctx context.Context, consent ConsentRef, content []byte) error
}

// ErrNoConsent is returned by a ContentCapture adapter when no matching
// consent record exists.
var ErrNoConsent = errors.New("identity: no matching consent record for content capture")
