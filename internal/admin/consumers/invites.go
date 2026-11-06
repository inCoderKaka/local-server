package consumers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// Invite states, derived from the invite row and the clock (req_001 D-38).
const (
	InvitePending  = "pending"
	InviteRedeemed = "redeemed"
	InviteRevoked  = "revoked"
	InviteExpired  = "expired"
)

// invitePath is the consumer web page's route that takes the token in the URL fragment, so the
// token never reaches a server log (req_001 D-13).
const invitePath = "/invite#"

// Invite is an invite as the admin sees it. It holds no token and no hash.
type Invite struct {
	ID                 string
	Status             string
	Note               *string
	CreatedAt          time.Time
	ExpiresAt          time.Time
	RedeemedConsumerID *string
}

// NewInvite is an invite to create. A nil ExpiresAt means now plus the invites.ttl setting.
type NewInvite struct {
	Note      *string
	ExpiresAt *time.Time
}

// IssuedInvite is a new invite with its token and its link. Both are redacted in any log line or
// error; the one caller that answers the request calls Reveal.
type IssuedInvite struct {
	Invite Invite
	Token  telemetry.Redacted
	Link   telemetry.Redacted
}

// InvitePage is one page of invites, newest first.
type InvitePage struct {
	Items      []Invite
	NextCursor string // empty on the last page
}

func inviteView(i identity.Invite, now time.Time) Invite {
	st := InvitePending
	switch {
	case i.RedeemedAt != nil:
		st = InviteRedeemed
	case i.RevokedAt != nil:
		st = InviteRevoked
	case !now.Before(i.ExpiresAt):
		st = InviteExpired
	}
	return Invite{ID: i.ID, Status: st, Note: i.Note, CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt, RedeemedConsumerID: i.RedeemedConsumerID}
}

// ListInvites returns one page of invites, newest first. The page never holds a token: only a
// hash is stored.
func (s *Service) ListInvites(ctx context.Context, q ListQuery) (InvitePage, error) {
	n, err := s.pageSize(ctx, q.Limit)
	if err != nil {
		return InvitePage{}, err
	}
	after, err := decodeCursor(q.Cursor)
	if err != nil {
		return InvitePage{}, err
	}
	rows, err := s.cfg.Identity.Invites.ListPage(ctx, s.cfg.DB.Reader(), after, n+1)
	if err != nil {
		return InvitePage{}, err
	}
	p := InvitePage{}
	if len(rows) > n {
		rows = rows[:n]
		last := rows[n-1]
		p.NextCursor = encodeCursor(identity.PageKey{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	now := s.now()
	p.Items = make([]Invite, 0, len(rows))
	for _, r := range rows {
		p.Items = append(p.Items, inviteView(r, now))
	}
	return p, nil
}

// CreateInvite creates a one-time sign-up invite for the admin actor. Without an expiry the
// invites.ttl setting applies, read now, so a change applies to the next invite (req_001 D-13,
// D-38). Only the prefix and a hash of the token are stored, in one unit of work with the audit
// row; the token and the link are in the result and nowhere else. The audit row holds no note.
func (s *Service) CreateInvite(ctx context.Context, actor string, in NewInvite) (IssuedInvite, error) {
	if actor == "" {
		return IssuedInvite{}, errors.New("consumers: an invite needs the admin who creates it")
	}
	now := s.now()
	var expires time.Time
	if in.ExpiresAt != nil {
		if !in.ExpiresAt.After(now) {
			return IssuedInvite{}, invalid("expires_at", "The expiry must be in the future.")
		}
		expires = *in.ExpiresAt
	} else {
		ttl, err := s.cfg.Settings.Duration(ctx, KeyInviteTTL, "")
		if err != nil {
			return IssuedInvite{}, err
		}
		expires = now.Add(ttl)
	}
	base, err := s.cfg.Settings.String(ctx, KeyInviteLinkBaseURL, "")
	if err != nil {
		return IssuedInvite{}, err
	}
	var note *string
	if in.Note != nil && strings.TrimSpace(*in.Note) != "" {
		note = in.Note
	}
	prefix, secret, err := auth.NewInviteToken()
	if err != nil {
		return IssuedInvite{}, wrap("generate invite token", err)
	}
	token := auth.FormatInviteToken(prefix, secret)
	var out IssuedInvite
	err = s.write(ctx, func(tx *storage.Tx) error {
		inv, err := s.cfg.Identity.Invites.Create(tx, prefix, auth.HashSecret(secret), actor, note, expires)
		if err != nil {
			return err
		}
		if err := s.record(tx, audit.Event{Action: ActionInviteCreated, ActorID: actor,
			Detail: map[string]any{"invite_id": inv.ID, "prefix": prefix, "expires_at": inv.ExpiresAt.UTC().Format(time.RFC3339)}}); err != nil {
			return err
		}
		out = IssuedInvite{Invite: inviteView(inv, now), Token: telemetry.Redact(token),
			Link: telemetry.Redact(strings.TrimRight(base, "/") + invitePath + token)}
		return nil
	})
	if err != nil {
		return IssuedInvite{}, err
	}
	return out, nil
}

// RevokeInvite ends a pending invite. An invite that is redeemed, revoked or expired is final
// and returns a conflict (req_001 D-54).
func (s *Service) RevokeInvite(ctx context.Context, actor, id string) (Invite, error) {
	var out Invite
	err := s.write(ctx, func(tx *storage.Tx) error {
		inv, err := s.cfg.Identity.Invites.GetTx(tx, id)
		if isNotFound(err) {
			return errNoInvite
		}
		if err != nil {
			return err
		}
		if st := inviteView(inv, s.now()).Status; st != InvitePending {
			return conflict("Only a pending invite can be revoked; this one is " + st + ".")
		}
		if err := s.cfg.Identity.Invites.Revoke(tx, id); err != nil {
			return err
		}
		if err := s.record(tx, audit.Event{Action: ActionInviteRevoked, ActorID: actor,
			Detail: map[string]any{"invite_id": id, "prefix": inv.Prefix}}); err != nil {
			return err
		}
		inv, err = s.cfg.Identity.Invites.GetTx(tx, id)
		out = inviteView(inv, s.now())
		return err
	})
	return out, err
}
