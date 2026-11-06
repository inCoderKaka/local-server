package admin

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/api"
)

// AdminListAccessKeys implements GET /admin/v1/consumers/{consumer_id}/access-keys: by prefix,
// never a secret or a hash.
func (a *Admin) AdminListAccessKeys(ctx context.Context, params api.AdminListAccessKeysParams) (*api.AccessKeyList, error) {
	keys, err := a.cons.ListAccessKeys(ctx, params.ConsumerID.String())
	if err != nil {
		return nil, err
	}
	out := &api.AccessKeyList{Data: make([]api.AccessKey, 0, len(keys))}
	for _, k := range keys {
		v, err := toAccessKey(k)
		if err != nil {
			return nil, err
		}
		out.Data = append(out.Data, v)
	}
	return out, nil
}

// AdminIssueAccessKey implements POST /admin/v1/consumers/{consumer_id}/access-keys: 201 with
// the whole key, which no later answer repeats. This is the one place the secret is revealed.
func (a *Admin) AdminIssueAccessKey(ctx context.Context, req *api.AccessKeyCreate, params api.AdminIssueAccessKeyParams) (*api.AccessKeyIssued, error) {
	var in consumers.NewKey
	if v, ok := req.Name.Get(); ok {
		in.Name = &v
	}
	if v, ok := req.ExpiresAt.Get(); ok {
		in.ExpiresAt = &v
	}
	if v, ok := req.BoundNode.Get(); ok {
		in.BoundNode = &v
	}
	if v, ok := req.Narrowing.Get(); ok {
		in.Narrowing = narrowingIn(v)
	}
	is, err := a.cons.IssueAccessKey(ctx, PrincipalIDOf(ctx), params.ConsumerID.String(), in)
	if err != nil {
		return nil, err
	}
	k, err := toAccessKey(is.Key)
	if err != nil {
		return nil, err
	}
	return &api.AccessKeyIssued{AccessKey: k, Secret: is.Secret.Reveal()}, nil
}

// AdminUpdateAccessKey implements PATCH /admin/v1/access-keys/{access_key_id}. A revoked or
// expired key is 409.
func (a *Admin) AdminUpdateAccessKey(ctx context.Context, req *api.AccessKeyUpdate, params api.AdminUpdateAccessKeyParams) (*api.AccessKey, error) {
	ch := consumers.KeyChange{Name: nilString(req.Name), BoundNode: nilString(req.BoundNode)}
	switch {
	case !req.ExpiresAt.Set:
	case req.ExpiresAt.Null:
		ch.ExpiresAt = consumers.Null[time.Time]()
	default:
		ch.ExpiresAt = consumers.Value(req.ExpiresAt.Value)
	}
	switch {
	case !req.Narrowing.Set:
	case req.Narrowing.Null:
		ch.Narrowing = consumers.Null[consumers.Narrowing]()
	default:
		ch.Narrowing = consumers.Value(*narrowingIn(req.Narrowing.Value))
	}
	k, err := a.cons.UpdateAccessKey(ctx, PrincipalIDOf(ctx), params.AccessKeyID.String(), ch)
	if err != nil {
		return nil, err
	}
	return keyOut(k)
}

// AdminRevokeAccessKey implements POST /admin/v1/access-keys/{access_key_id}/revoke.
func (a *Admin) AdminRevokeAccessKey(ctx context.Context, params api.AdminRevokeAccessKeyParams) (*api.AccessKey, error) {
	k, err := a.cons.RevokeAccessKey(ctx, PrincipalIDOf(ctx), params.AccessKeyID.String())
	if err != nil {
		return nil, err
	}
	return keyOut(k)
}

func keyOut(k consumers.Key) (*api.AccessKey, error) {
	v, err := toAccessKey(k)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func toAccessKey(k consumers.Key) (api.AccessKey, error) {
	id, err := uuid.Parse(k.ID)
	if err != nil {
		return api.AccessKey{}, err
	}
	cid, err := uuid.Parse(k.ConsumerID)
	if err != nil {
		return api.AccessKey{}, err
	}
	out := api.AccessKey{ID: id, ConsumerID: cid, Prefix: k.Prefix, Name: optNil(k.Name), Status: api.AccessKeyStatus(k.Status),
		BoundNode: optNil(k.BoundNode), CreatedAt: k.CreatedAt, ExpiresAt: optNilTime(k.ExpiresAt), RevokedAt: optNilTime(k.RevokedAt)}
	if k.Narrowing == nil {
		out.Narrowing.SetToNull()
	} else {
		out.Narrowing = api.NewOptNilAccessKeyNarrowing(narrowingOut(*k.Narrowing))
	}
	return out, nil
}

func optNilTime(t *time.Time) api.OptNilDateTime {
	if t == nil {
		var o api.OptNilDateTime
		o.SetToNull()
		return o
	}
	return api.NewOptNilDateTime(t.UTC())
}

// narrowingIn reads the narrowing of a request. A quota given as null, or without a number,
// adds no quota.
func narrowingIn(n api.AccessKeyNarrowing) *consumers.Narrowing {
	out := &consumers.Narrowing{Models: n.Models}
	if q, ok := n.Quota.Get(); ok {
		out.Quota = &consumers.Quota{Tokens: optInt64(q.Tokens), Requests: optInt64(q.Requests)}
	}
	out.ContextLimit = optInt(n.ContextLimit)
	out.MaxOutputTokens = optInt(n.MaxOutputTokens)
	out.Concurrency = optInt(n.Concurrency)
	return out
}

func narrowingOut(n consumers.Narrowing) api.AccessKeyNarrowing {
	out := api.AccessKeyNarrowing{Models: n.Models}
	if n.Quota != nil {
		q := api.Quota{}
		if n.Quota.Tokens != nil {
			q.Tokens = api.NewOptInt64(*n.Quota.Tokens)
		}
		if n.Quota.Requests != nil {
			q.Requests = api.NewOptInt64(*n.Quota.Requests)
		}
		out.Quota = api.NewOptNilQuota(q)
	}
	if n.ContextLimit != nil {
		out.ContextLimit = api.NewOptInt(int(*n.ContextLimit))
	}
	if n.MaxOutputTokens != nil {
		out.MaxOutputTokens = api.NewOptInt(int(*n.MaxOutputTokens))
	}
	if n.Concurrency != nil {
		out.Concurrency = api.NewOptInt(int(*n.Concurrency))
	}
	return out
}

func optInt64(o api.OptInt64) *int64 {
	if v, ok := o.Get(); ok {
		return &v
	}
	return nil
}

func optInt(o api.OptInt) *int64 {
	if v, ok := o.Get(); ok {
		x := int64(v)
		return &x
	}
	return nil
}
