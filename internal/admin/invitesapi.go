package admin

import (
	"context"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/api"
)

// AdminListInvites implements GET /admin/v1/invites: newest first, never a token.
func (a *Admin) AdminListInvites(ctx context.Context, params api.AdminListInvitesParams) (*api.InviteList, error) {
	q := consumers.ListQuery{Cursor: params.Cursor.Value}
	if v, ok := params.Limit.Get(); ok {
		q.Limit = &v
	}
	page, err := a.cons.ListInvites(ctx, q)
	if err != nil {
		return nil, err
	}
	out := &api.InviteList{Data: make([]api.Invite, 0, len(page.Items))}
	for _, i := range page.Items {
		v, err := toInvite(i)
		if err != nil {
			return nil, err
		}
		out.Data = append(out.Data, v)
	}
	out.NextCursor = nextCursor(page.NextCursor)
	return out, nil
}

// AdminCreateInvite implements POST /admin/v1/invites: 201 with the token and the link, which no
// later answer repeats. This is the one place they are revealed.
func (a *Admin) AdminCreateInvite(ctx context.Context, req *api.InviteCreate) (*api.InviteIssued, error) {
	var in consumers.NewInvite
	if v, ok := req.Note.Get(); ok {
		in.Note = &v
	}
	if v, ok := req.ExpiresAt.Get(); ok {
		in.ExpiresAt = &v
	}
	is, err := a.cons.CreateInvite(ctx, PrincipalIDOf(ctx), in)
	if err != nil {
		return nil, err
	}
	inv, err := toInvite(is.Invite)
	if err != nil {
		return nil, err
	}
	inv.Warnings = withIgnored(ctx, nil)
	return &api.InviteIssued{Invite: inv, Token: is.Token.Reveal(), Link: is.Link.Reveal()}, nil
}

// AdminRevokeInvite implements POST /admin/v1/invites/{invite_id}/revoke. An invite that is not
// pending is 409.
func (a *Admin) AdminRevokeInvite(ctx context.Context, params api.AdminRevokeInviteParams) (*api.Invite, error) {
	i, err := a.cons.RevokeInvite(ctx, PrincipalIDOf(ctx), params.InviteID.String())
	if err != nil {
		return nil, err
	}
	v, err := toInvite(i)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func toInvite(i consumers.Invite) (api.Invite, error) {
	id, err := uuid.Parse(i.ID)
	if err != nil {
		return api.Invite{}, err
	}
	out := api.Invite{ID: id, Status: api.InviteStatus(i.Status), Note: optNil(i.Note), CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt}
	if i.RedeemedConsumerID == nil {
		out.RedeemedConsumerID.SetToNull()
	} else {
		cid, err := uuid.Parse(*i.RedeemedConsumerID)
		if err != nil {
			return api.Invite{}, err
		}
		out.RedeemedConsumerID = api.NewOptNilUUID(cid)
	}
	return out, nil
}
