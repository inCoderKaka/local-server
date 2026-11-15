package consumerauth

import (
	"context"
	"encoding/json"

	"github.com/go-faster/jx"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/identity"
)

// The passkey routes of the consumer page (req_001 D-13, D-135, D-154). A passkey sign-up, login
// and registration are ceremonies of the PasskeyAdapter; what they have in common with a password
// (the invite redemption, the session) is the Service's, so a passkey-only consumer is created
// and logs in exactly like a consumer with a password.

// BeginPasskeySignup starts redeeming an invite with a passkey. The invite, the profile and the
// consent are checked as for a password sign-up; nothing is created until FinishPasskeySignup.
func (s *Service) BeginPasskeySignup(ctx context.Context, in Redemption) (Ceremony, error) {
	rp, err := relyingPartyOf(ctx)
	if err != nil {
		return Ceremony{}, err
	}
	chk, err := s.checkRedemption(ctx, in)
	if err != nil {
		return Ceremony{}, err
	}
	return s.passkey.BeginSignup(ctx, rp, chk)
}

// FinishPasskeySignup verifies the authenticator's response and creates the consumer, its
// passkey and consent, redeems the invite (checked again) and starts a session, in one unit of
// work. It returns the new account and the Set-Cookie value.
func (s *Service) FinishPasskeySignup(ctx context.Context, ceremonyID string, response []byte) (identity.ConsumerAccount, string, error) {
	rp, err := relyingPartyOf(ctx)
	if err != nil {
		return identity.ConsumerAccount{}, "", err
	}
	enrol, chk, err := s.passkey.FinishSignup(ctx, rp, ceremonyID, response)
	if err != nil {
		return identity.ConsumerAccount{}, "", err
	}
	return s.completeRedemption(ctx, chk, enrol)
}

// BeginPasskeyLogin starts a passkey login.
func (s *Service) BeginPasskeyLogin(ctx context.Context) (Ceremony, error) {
	rp, err := relyingPartyOf(ctx)
	if err != nil {
		return Ceremony{}, err
	}
	return s.passkey.BeginLogin(ctx, rp)
}

// FinishPasskeyLogin verifies the assertion and starts a session for a client at remoteAddr. It
// returns the account and the Set-Cookie value.
func (s *Service) FinishPasskeyLogin(ctx context.Context, ceremonyID string, response []byte, remoteAddr string) (identity.ConsumerAccount, string, error) {
	rp, err := relyingPartyOf(ctx)
	if err != nil {
		return identity.ConsumerAccount{}, "", err
	}
	v, err := s.passkey.FinishLogin(ctx, rp, ceremonyID, response, remoteAddr)
	if err != nil {
		return identity.ConsumerAccount{}, "", err
	}
	return s.startLogin(ctx, v)
}

// BeginPasskeyAdd starts adding a passkey to the logged-in consumer. Session authentication only.
func (s *Service) BeginPasskeyAdd(ctx context.Context, c Caller) (Ceremony, error) {
	if c.SessionID == "" {
		return Ceremony{}, unauthenticated()
	}
	rp, err := relyingPartyOf(ctx)
	if err != nil {
		return Ceremony{}, err
	}
	acct, err := s.cfg.Identity.Consumers.GetAccount(ctx, s.cfg.DB.Reader(), c.ConsumerID)
	if isNotFound(err) {
		return Ceremony{}, unauthenticated()
	}
	if err != nil {
		return Ceremony{}, err
	}
	label := rp.ID
	switch {
	case acct.DisplayName != nil && *acct.DisplayName != "":
		label = *acct.DisplayName
	case acct.Username != nil:
		label = *acct.Username
	}
	return s.passkey.BeginAdd(ctx, rp, c.PrincipalID, label)
}

// FinishPasskeyAdd verifies the authenticator's response and stores the passkey for the
// logged-in consumer.
func (s *Service) FinishPasskeyAdd(ctx context.Context, c Caller, ceremonyID string, response []byte) error {
	if c.SessionID == "" {
		return unauthenticated()
	}
	rp, err := relyingPartyOf(ctx)
	if err != nil {
		return err
	}
	return s.passkey.FinishAdd(ctx, rp, c.PrincipalID, ceremonyID, response)
}

// ceremonyOut is a started ceremony in the API's shape.
func ceremonyOut(c Ceremony) (*api.PasskeyCeremony, error) {
	var opts map[string]json.RawMessage
	if err := json.Unmarshal(c.Options, &opts); err != nil {
		return nil, err
	}
	out := make(api.PasskeyCeremonyOptions, len(opts))
	for k, v := range opts {
		out[k] = jx.Raw(v)
	}
	return &api.PasskeyCeremony{CeremonyID: c.ID, Options: out}, nil
}

// credentialJSON is the authenticator's response as the JSON the WebAuthn library parses.
func credentialJSON(cred api.PasskeyCeremonyFinishCredential) ([]byte, error) {
	m := make(map[string]json.RawMessage, len(cred))
	for k, v := range cred {
		m[k] = json.RawMessage(v)
	}
	return json.Marshal(m)
}

// BeginInviteRedemptionWithPasskey implements POST /v1/invite-redemptions/passkey/begin.
func (p *pageAPI) BeginInviteRedemptionWithPasskey(ctx context.Context, req *api.InviteRedemptionPasskeyBegin) (*api.PasskeyCeremony, error) {
	in := Redemption{Token: req.InviteToken, DisplayName: req.DisplayName,
		Terms: req.Consent.TermsVersion, Privacy: req.Consent.PrivacyVersion}
	if v, ok := req.Email.Get(); ok {
		in.Email = &v
	}
	c, err := p.s.BeginPasskeySignup(ctx, in)
	if err != nil {
		return nil, err
	}
	return ceremonyOut(c)
}

// FinishInviteRedemptionWithPasskey implements POST /v1/invite-redemptions/passkey/finish.
func (p *pageAPI) FinishInviteRedemptionWithPasskey(ctx context.Context, req *api.PasskeyCeremonyFinish) (*api.ConsumerHeaders, error) {
	body, err := credentialJSON(req.Credential)
	if err != nil {
		return nil, err
	}
	acct, cookie, err := p.s.FinishPasskeySignup(ctx, req.CeremonyID, body)
	if err != nil {
		return nil, err
	}
	return p.consumerHeaders(acct, cookie)
}

// BeginConsumerSessionWithPasskey implements POST /v1/session/passkey/begin.
func (p *pageAPI) BeginConsumerSessionWithPasskey(ctx context.Context) (*api.PasskeyCeremony, error) {
	c, err := p.s.BeginPasskeyLogin(ctx)
	if err != nil {
		return nil, err
	}
	return ceremonyOut(c)
}

// FinishConsumerSessionWithPasskey implements POST /v1/session/passkey/finish.
func (p *pageAPI) FinishConsumerSessionWithPasskey(ctx context.Context, req *api.PasskeyCeremonyFinish) (*api.ConsumerHeaders, error) {
	body, err := credentialJSON(req.Credential)
	if err != nil {
		return nil, err
	}
	acct, cookie, err := p.s.FinishPasskeyLogin(ctx, req.CeremonyID, body, remoteOf(ctx))
	if err != nil {
		return nil, err
	}
	return p.consumerHeaders(acct, cookie)
}

// BeginMyPasskeyRegistration implements POST /v1/me/passkeys/begin.
func (p *pageAPI) BeginMyPasskeyRegistration(ctx context.Context) (*api.PasskeyCeremony, error) {
	c, err := p.caller(ctx)
	if err != nil {
		return nil, err
	}
	cer, err := p.s.BeginPasskeyAdd(ctx, c)
	if err != nil {
		return nil, err
	}
	return ceremonyOut(cer)
}

// FinishMyPasskeyRegistration implements POST /v1/me/passkeys/finish.
func (p *pageAPI) FinishMyPasskeyRegistration(ctx context.Context, req *api.PasskeyCeremonyFinish) error {
	c, err := p.caller(ctx)
	if err != nil {
		return err
	}
	body, err := credentialJSON(req.Credential)
	if err != nil {
		return err
	}
	return p.s.FinishPasskeyAdd(ctx, c, req.CeremonyID, body)
}
