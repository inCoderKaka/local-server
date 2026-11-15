package consumerauth

import (
	"context"
	"errors"
	"time"

	"github.com/inCoderKaka/local-server/internal/auth/credential"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// maxTokenLen: a cookie value longer than this cannot be a session token.
const maxTokenLen = 256

// startSession adds a session row for principalID inside tx and returns the Set-Cookie value that
// starts it. The token is shown only here; the row holds its hash. The lifetime is the
// consumer.session_ttl setting, read now (req_001 D-38).
func (s *Service) startSession(ctx context.Context, tx *storage.Tx, principalID string) (cookie string, err error) {
	ttl, err := s.cfg.Settings.Duration(ctx, KeySessionTTL, "")
	if err != nil {
		return "", err
	}
	token, err := credential.NewToken()
	if err != nil {
		return "", err
	}
	now := s.now()
	expires := now.Add(ttl)
	if _, err := s.cfg.Identity.Sessions.Create(tx, principalID, credential.HashToken(token), expires); err != nil {
		return "", err
	}
	return credential.SessionCookie(credential.ConsumerSessionCookie, token, expires, now), nil
}

// ResolveSession returns the consumer behind a session cookie value. It refuses, with the same
// 401, a token that names no session, a session that was revoked or has expired, and a session
// of a principal that is not an active consumer (suspended or deleted), so a consumer the host
// suspended or deleted is out at the next request, not at the end of its session.
func (s *Service) ResolveSession(ctx context.Context, token string) (Caller, error) {
	if token == "" || len(token) > maxTokenLen {
		return Caller{}, unauthenticated()
	}
	rd := s.cfg.DB.Reader()
	ir := s.cfg.Identity
	sess, err := ir.Sessions.GetByTokenHash(ctx, rd, credential.HashToken(token))
	if isNotFound(err) {
		return Caller{}, unauthenticated()
	}
	if err != nil {
		return Caller{}, err
	}
	if sess.RevokedAt != nil || !s.now().Before(sess.ExpiresAt) {
		return Caller{}, unauthenticated()
	}
	p, err := ir.Principals.Get(ctx, rd, sess.PrincipalID)
	if isNotFound(err) {
		return Caller{}, unauthenticated()
	}
	if err != nil {
		return Caller{}, err
	}
	if p.Kind != PrincipalKind || p.Status != identity.StatusActive {
		return Caller{}, unauthenticated()
	}
	c, err := ir.Consumers.GetByPrincipal(ctx, rd, p.ID)
	if isNotFound(err) {
		return Caller{}, unauthenticated()
	}
	if err != nil {
		return Caller{}, err
	}
	return Caller{ConsumerID: c.ID, PrincipalID: p.ID, SessionID: sess.ID}, nil
}

// Login checks a login with the named adapter and starts a session. It returns the account and
// the Set-Cookie value. A wrong answer is ErrInvalidLogin, a lockout a *LockedOutError.
func (s *Service) Login(ctx context.Context, kind, login, secret, remoteAddr string) (identity.ConsumerAccount, string, error) {
	ad, ok := s.adapters[kind]
	if !ok {
		return identity.ConsumerAccount{}, "", errors.New("consumerauth: no adapter for this credential kind")
	}
	v, err := ad.Authenticate(ctx, login, secret, remoteAddr)
	if err != nil {
		return identity.ConsumerAccount{}, "", err
	}
	rd := s.cfg.DB.Reader()
	cons, err := s.cfg.Identity.Consumers.GetByPrincipal(ctx, rd, v.PrincipalID)
	if err != nil {
		return identity.ConsumerAccount{}, "", err
	}
	acct, err := s.cfg.Identity.Consumers.GetAccount(ctx, rd, cons.ID)
	if err != nil {
		return identity.ConsumerAccount{}, "", err
	}
	var cookie string
	err = s.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
		ir := s.cfg.Identity
		if err := ir.Credentials.MarkUsed(tx, v.CredentialID); err != nil {
			return err
		}
		if _, err := ir.Sessions.DeleteExpired(tx, s.now()); err != nil {
			return err
		}
		cookie, err = s.startSession(ctx, tx, v.PrincipalID)
		return err
	})
	if err != nil {
		return identity.ConsumerAccount{}, "", err
	}
	return acct, cookie, nil
}

// Logout ends the caller's session. A session that another request already ended is not an
// error: the cookie is cleared all the same.
func (s *Service) Logout(ctx context.Context, c Caller) error {
	if c.SessionID == "" {
		return unauthenticated()
	}
	return s.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
		if err := s.cfg.Identity.Sessions.Revoke(tx, c.SessionID); err != nil && !isNotFound(err) {
			return err
		}
		return nil
	})
}

// revokeOthers ends every live session of principalID except keep, inside tx.
func (s *Service) revokeOthers(tx *storage.Tx, principalID, keep string, now time.Time) error {
	ir := s.cfg.Identity
	sessions, err := ir.Sessions.ListByPrincipalTx(tx, principalID)
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		if sess.ID == keep || sess.RevokedAt != nil || !now.Before(sess.ExpiresAt) {
			continue
		}
		if err := ir.Sessions.Revoke(tx, sess.ID); err != nil && !isNotFound(err) {
			return err
		}
	}
	return nil
}
