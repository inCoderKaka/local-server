package consumers

import (
	"context"
	"net/mail"
	"strings"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// NewConsumer is a consumer to create. The consumer has no login: a person gets one by redeeming
// an invite.
type NewConsumer struct {
	Kind        string // identity.ConsumerIndividual or identity.ConsumerApplication
	DisplayName string
	Email       *string
}

// ConsumerChange is a change to a consumer; a field left out stays as it is.
type ConsumerChange struct {
	DisplayName Field[string]
	Email       Field[string]
}

// ConsumerPage is one page of consumers, newest first.
type ConsumerPage struct {
	Items      []identity.ConsumerAccount
	NextCursor string // empty on the last page
}

func validKind(k string) bool {
	return k == identity.ConsumerIndividual || k == identity.ConsumerApplication
}

// cleanName trims a display name and refuses a blank one.
func cleanName(param, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", invalid(param, "The value must not be empty.")
	}
	return v, nil
}

// cleanEmail accepts one bare address, as net/mail reads it, and returns it trimmed.
func cleanEmail(v string) (string, error) {
	v = strings.TrimSpace(v)
	a, err := mail.ParseAddress(v)
	if err != nil || a.Address != v {
		return "", invalid("email", "The email address is not valid.")
	}
	return v, nil
}

// CreateConsumer creates a key-only consumer: its principal and consumer row, audited in the same
// unit of work (req_001 D-25, D-13). actor is the admin's principal id.
func (s *Service) CreateConsumer(ctx context.Context, actor string, in NewConsumer) (identity.ConsumerAccount, error) {
	if !validKind(in.Kind) {
		return identity.ConsumerAccount{}, invalid("kind", "The kind must be individual or application.")
	}
	name, err := cleanName("display_name", in.DisplayName)
	if err != nil {
		return identity.ConsumerAccount{}, err
	}
	var email *string
	if in.Email != nil {
		e, err := cleanEmail(*in.Email)
		if err != nil {
			return identity.ConsumerAccount{}, err
		}
		email = &e
	}
	var out identity.ConsumerAccount
	err = s.write(ctx, func(tx *storage.Tx) error {
		ir := s.cfg.Identity
		p, err := ir.Principals.Create(tx, principalKindConsumer, nil, nil)
		if err != nil {
			return err
		}
		c, err := ir.Consumers.Create(tx, p.ID, in.Kind, &name, email)
		if err != nil {
			return err
		}
		if err := s.record(tx, audit.Event{Action: ActionConsumerCreated, ActorID: actor, TargetID: p.ID,
			Detail: map[string]any{"consumer_id": c.ID, "kind": in.Kind}}); err != nil {
			return err
		}
		out, err = ir.Consumers.GetAccountTx(tx, c.ID)
		return err
	})
	return out, err
}

// GetConsumer returns one consumer, deleted or not.
func (s *Service) GetConsumer(ctx context.Context, id string) (identity.ConsumerAccount, error) {
	a, err := s.cfg.Identity.Consumers.GetAccount(ctx, s.cfg.DB.Reader(), id)
	if isNotFound(err) {
		return a, errNoConsumer
	}
	return a, err
}

// ListConsumers returns one page, newest first. A deleted consumer is listed only when the query
// asks for the status deleted.
func (s *Service) ListConsumers(ctx context.Context, q ListQuery) (ConsumerPage, error) {
	switch q.Status {
	case "", identity.StatusActive, identity.StatusSuspended, identity.StatusDeleted:
	default:
		return ConsumerPage{}, invalid("status", "The status must be active, suspended or deleted.")
	}
	n, err := s.pageSize(ctx, q.Limit)
	if err != nil {
		return ConsumerPage{}, err
	}
	after, err := decodeCursor(q.Cursor)
	if err != nil {
		return ConsumerPage{}, err
	}
	rows, err := s.cfg.Identity.Consumers.ListAccounts(ctx, s.cfg.DB.Reader(), q.Status, after, n+1)
	if err != nil {
		return ConsumerPage{}, err
	}
	p := ConsumerPage{Items: rows}
	if len(rows) > n {
		p.Items = rows[:n]
		last := p.Items[n-1]
		p.NextCursor = encodeCursor(identity.PageKey{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return p, nil
}

// consumerTx reads a consumer inside the unit of work.
func (s *Service) consumerTx(tx *storage.Tx, id string) (identity.ConsumerAccount, error) {
	a, err := s.cfg.Identity.Consumers.GetAccountTx(tx, id)
	if isNotFound(err) {
		return a, errNoConsumer
	}
	return a, err
}

// UpdateConsumer changes the display name or email. A deleted consumer is refused: its personal
// fields were erased and are not filled again (req_001 D-40, D-54). A change that leaves a field
// as it is writes nothing.
func (s *Service) UpdateConsumer(ctx context.Context, actor, id string, ch ConsumerChange) (identity.ConsumerAccount, error) {
	var name *string
	if ch.DisplayName.Set {
		if ch.DisplayName.Null {
			return identity.ConsumerAccount{}, invalid("display_name", "The display name cannot be removed.")
		}
		v, err := cleanName("display_name", ch.DisplayName.Value)
		if err != nil {
			return identity.ConsumerAccount{}, err
		}
		name = &v
	}
	var email *string
	if ch.Email.Set && !ch.Email.Null {
		v, err := cleanEmail(ch.Email.Value)
		if err != nil {
			return identity.ConsumerAccount{}, err
		}
		email = &v
	}
	var out identity.ConsumerAccount
	err := s.write(ctx, func(tx *storage.Tx) error {
		a, err := s.consumerTx(tx, id)
		if err != nil {
			return err
		}
		out = a
		if a.Status == identity.StatusDeleted {
			return conflict("A deleted consumer cannot be changed.")
		}
		newName, newEmail := a.DisplayName, a.Email
		var fields []string
		if name != nil && !samePtr(a.DisplayName, name) {
			newName = name
			fields = append(fields, "display_name")
		}
		if ch.Email.Set && !samePtr(a.Email, email) {
			newEmail = email
			fields = append(fields, "email")
		}
		if len(fields) == 0 {
			return nil
		}
		if err := s.cfg.Identity.Consumers.Update(tx, id, newName, newEmail); err != nil {
			return err
		}
		// Field names only: the values are personal data and the audit trail is kept forever.
		if err := s.record(tx, audit.Event{Action: ActionConsumerUpdated, ActorID: actor, TargetID: a.PrincipalID,
			Detail: map[string]any{"consumer_id": id, "fields": fields}}); err != nil {
			return err
		}
		out, err = s.consumerTx(tx, id)
		return err
	})
	return out, err
}

func samePtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// SuspendConsumer blocks the consumer's keys until it is reactivated. A suspended consumer stays
// suspended and writes nothing; a deleted one cannot be suspended.
func (s *Service) SuspendConsumer(ctx context.Context, actor, id string) (identity.ConsumerAccount, error) {
	return s.setStatus(ctx, actor, id, identity.StatusActive, identity.StatusSuspended, ActionConsumerSuspended, "A deleted consumer cannot be suspended.")
}

// ReactivateConsumer ends a suspension. A deleted consumer is never reactivated (req_001 D-54):
// that is a conflict. An active consumer stays active and writes nothing.
func (s *Service) ReactivateConsumer(ctx context.Context, actor, id string) (identity.ConsumerAccount, error) {
	return s.setStatus(ctx, actor, id, identity.StatusSuspended, identity.StatusActive, ActionConsumerReactivated, "A deleted consumer cannot be reactivated.")
}

func (s *Service) setStatus(ctx context.Context, actor, id, from, to, action, deletedMsg string) (identity.ConsumerAccount, error) {
	var out identity.ConsumerAccount
	changed := false
	err := s.write(ctx, func(tx *storage.Tx) error {
		a, err := s.consumerTx(tx, id)
		if err != nil {
			return err
		}
		out = a
		switch a.Status {
		case identity.StatusDeleted:
			return conflict(deletedMsg)
		case to:
			return nil
		}
		if err := s.cfg.Identity.Principals.SetStatus(tx, a.PrincipalID, to); err != nil {
			return err
		}
		if err := s.record(tx, audit.Event{Action: action, ActorID: actor, TargetID: a.PrincipalID,
			Detail: map[string]any{"consumer_id": id, "from": from, "to": to}}); err != nil {
			return err
		}
		changed = true
		out, err = s.consumerTx(tx, id)
		return err
	})
	if err != nil {
		return out, err
	}
	if changed {
		s.cfg.Auth.Invalidate() // the key cache clears itself only on access_key.revoked and grant.changed
	}
	return out, nil
}

// DeleteConsumer soft-deletes a consumer in one unit of work (req_001 D-40, D-54): every key that
// is not revoked is revoked through the auth service, the key names, the display name, the email,
// the login credentials and the notes of the invites the consumer redeemed are erased, the
// sessions end, and the principal becomes deleted for good. The consumer row, the usage records
// and the ledger leaves stay. A consumer that is already deleted stays so and writes nothing.
func (s *Service) DeleteConsumer(ctx context.Context, actor, id string) error {
	changed := false
	err := s.write(ctx, func(tx *storage.Tx) error {
		a, err := s.consumerTx(tx, id)
		if err != nil {
			return err
		}
		if a.Status == identity.StatusDeleted {
			return nil
		}
		ir := s.cfg.Identity
		keys, err := ir.AccessKeys.ListByConsumerTx(tx, id)
		if err != nil {
			return err
		}
		revoked := 0
		for _, k := range keys {
			if k.RevokedAt == nil {
				if err := s.cfg.Auth.Revoke(tx, k.ID, actor); err != nil {
					return err
				}
				revoked++
			}
			if k.Label != nil { // the name the consumer gave a key is personal data
				if err := ir.AccessKeys.Update(tx, k.ID, nil, k.NarrowingJSON, k.BoundNode, k.ExpiresAt); err != nil {
					return err
				}
			}
		}
		sessions, err := ir.Sessions.ListByPrincipalTx(tx, a.PrincipalID)
		if err != nil {
			return err
		}
		for _, ss := range sessions {
			if ss.RevokedAt == nil {
				if err := ir.Sessions.Revoke(tx, ss.ID); err != nil {
					return err
				}
			}
		}
		if _, err := ir.Credentials.DeleteByPrincipal(tx, a.PrincipalID); err != nil {
			return err
		}
		if _, err := ir.Invites.EraseNotesRedeemedBy(tx, id); err != nil {
			return err
		}
		if err := ir.Consumers.SoftDelete(tx, id); err != nil {
			return err
		}
		changed = true
		return s.record(tx, audit.Event{Action: ActionConsumerDeleted, ActorID: actor, TargetID: a.PrincipalID,
			Detail: map[string]any{"consumer_id": id, "revoked_keys": revoked}})
	})
	if err == nil && changed {
		s.cfg.Auth.Invalidate()
	}
	return err
}
