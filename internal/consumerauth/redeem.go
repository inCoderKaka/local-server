package consumerauth

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"

	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// The policy versions in force, and the purposes of the consent rows written for them
// (req_001 D-44, D-45). The versions are the `**Version:**` lines of docs/legal/terms-of-use.md
// and docs/legal/privacy-consent.md: when a text changes, its version and this constant change
// together. A sign-up that does not accept exactly the version in force is refused, so no
// consent row ever names a version the person was not shown as current.
const (
	TermsVersion   = "terms-2026-10-v1"
	PrivacyVersion = "privacy-2026-10-v1"
	PurposeTerms   = "terms"
	PurposePrivacy = "privacy"
)

// consentEvidence is stored with each sign-up consent row. It holds no address, no browser
// identification and nothing personal (req_001 D-40).
const consentEvidence = `{"via":"invite_redemption"}`

// Redemption is a sign-up from an invite: the invite token, the profile, the login of the
// adapter Kind and the policy versions the person accepted.
type Redemption struct {
	Token       string
	DisplayName string
	Email       *string
	Login       string
	Secret      string
	Terms       string
	Privacy     string
}

// dummyInviteHash is compared against when an invite prefix is unknown, so a known and an
// unknown prefix cost the same.
var dummyInviteHash = auth.HashSecret("no such invite")

func errInviteInvalid() *Error { return notFoundErr("This invite link is not valid.") }

func errInviteGone() *Error {
	return newErr(http.StatusGone, typeInvalidRequest, "", "", "This invite link was already used, was withdrawn by the host or has expired. Ask the host for a new one.")
}

// inviteLive reports whether an invite can still be redeemed.
func (s *Service) inviteLive(i identity.Invite) bool {
	return i.RedeemedAt == nil && i.RevokedAt == nil && s.now().Before(i.ExpiresAt)
}

// cleanEmail accepts one bare address, as net/mail reads it, and returns it trimmed.
func cleanEmail(v string) (string, error) {
	v = strings.TrimSpace(v)
	a, err := mail.ParseAddress(v)
	if err != nil || a.Address != v {
		return "", badRequest("email", "The email address is not valid.")
	}
	return v, nil
}

// RedeemInvite redeems an invite with the login of the named adapter (req_001 D-13): it checks
// the token, creates the principal, the consumer, the credential, marks the invite redeemed,
// records consent to the terms and privacy versions in force, and starts a session, in one unit
// of work. The single writer orders concurrent redemptions of one invite, so exactly one wins and
// the others get 410. A refusal before the unit (a token that is not valid or no longer live, a
// field that is not acceptable, a policy version that is not the one in force) spends no hash. It
// returns the new account and the Set-Cookie value.
//
// The new consumer is an individual with billing mode none and no grant: the host gives it a
// grant and keys in the admin UI, exactly as for a consumer it created itself. The invite carries
// nothing else (req_001 D-25, D-107).
func (s *Service) RedeemInvite(ctx context.Context, kind string, in Redemption) (identity.ConsumerAccount, string, error) {
	ad, ok := s.adapters[kind]
	if !ok {
		return identity.ConsumerAccount{}, "", errors.New("consumerauth: no adapter for this credential kind")
	}
	chk, err := s.checkRedemption(ctx, in)
	if err != nil {
		return identity.ConsumerAccount{}, "", err
	}
	enrol, err := ad.Prepare(ctx, in.Login, in.Secret)
	if err != nil {
		return identity.ConsumerAccount{}, "", err
	}
	return s.completeRedemption(ctx, chk, enrol)
}

// checkedRedemption is a sign-up that passed every check that needs no credential: the invite it
// redeems and the profile and consent it carries.
type checkedRedemption struct {
	InviteID string
	Name     string
	Email    *string
}

// checkRedemption checks the token, the invite, the profile and the policy versions of in, in
// that order, and spends nothing. A passkey sign-up runs it when the ceremony begins, and the
// invite is checked again in the unit of work that finishes it.
func (s *Service) checkRedemption(ctx context.Context, in Redemption) (checkedRedemption, error) {
	parsed, err := auth.ParseInviteToken(in.Token)
	if err != nil {
		return checkedRedemption{}, errInviteInvalid()
	}
	inv, err := s.cfg.Identity.Invites.GetByPrefix(ctx, s.cfg.DB.Reader(), parsed.Prefix)
	known, stored := true, dummyInviteHash
	switch {
	case isNotFound(err):
		known = false
	case err != nil:
		return checkedRedemption{}, err
	default:
		stored = inv.TokenHash
	}
	if !auth.InviteSecretMatches(parsed.Secret.Reveal(), stored) || !known {
		return checkedRedemption{}, errInviteInvalid()
	}
	if !s.inviteLive(inv) {
		return checkedRedemption{}, errInviteGone()
	}

	name := strings.TrimSpace(in.DisplayName)
	if name == "" {
		return checkedRedemption{}, badRequest("display_name", "The name must not be empty.")
	}
	var email *string
	if in.Email != nil {
		e, err := cleanEmail(*in.Email)
		if err != nil {
			return checkedRedemption{}, err
		}
		email = &e
	}
	if in.Terms != TermsVersion {
		return checkedRedemption{}, badRequest("consent.terms_version", "Accept the current Terms of Use ("+TermsVersion+") to sign up.")
	}
	if in.Privacy != PrivacyVersion {
		return checkedRedemption{}, badRequest("consent.privacy_version", "Accept the current Privacy and consent notice ("+PrivacyVersion+") to sign up.")
	}
	return checkedRedemption{InviteID: inv.ID, Name: name, Email: email}, nil
}

// completeRedemption is the unit of work of a sign-up: it checks the invite again, creates the
// consumer, stores the credential of enrol, redeems the invite, records consent and starts a
// session. A password sign-up and a passkey sign-up end here, so they cannot differ.
func (s *Service) completeRedemption(ctx context.Context, chk checkedRedemption, enrol Enrollment) (identity.ConsumerAccount, string, error) {
	var acct identity.ConsumerAccount
	var cookie string
	err := s.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
		ir := s.cfg.Identity
		cur, err := ir.Invites.GetTx(tx, chk.InviteID)
		if err != nil {
			return err
		}
		if !s.inviteLive(cur) {
			return errInviteGone()
		}
		p, err := ir.Principals.Create(tx, PrincipalKind, nil, nil)
		if err != nil {
			return err
		}
		c, err := ir.Consumers.CreateWithBilling(tx, p.ID, identity.ConsumerIndividual, identity.BillingNone, &chk.Name, chk.Email)
		if err != nil {
			return err
		}
		if err := enrol.Store(tx, p.ID); err != nil {
			return err
		}
		if err := ir.Invites.Redeem(tx, chk.InviteID, c.ID); err != nil {
			if isNotFound(err) {
				return errInviteGone()
			}
			return err
		}
		ev := consentEvidence
		for _, cs := range []struct{ purpose, version string }{{PurposeTerms, TermsVersion}, {PurposePrivacy, PrivacyVersion}} {
			if _, err := ir.Consents.Record(tx, identity.ConsentRef{PrincipalID: p.ID, Purpose: cs.purpose, PolicyVersion: cs.version}, &ev); err != nil {
				return err
			}
		}
		if cookie, err = s.startSession(ctx, tx, p.ID); err != nil {
			return err
		}
		acct, err = ir.Consumers.GetAccountTx(tx, c.ID)
		return err
	})
	if err != nil {
		return identity.ConsumerAccount{}, "", err
	}
	return acct, cookie, nil
}
