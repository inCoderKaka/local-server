package admin

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/grants"
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
// A consumer that may not set quota windows on its own keys is 403 (req_001 D-107). An unknown
// request field is ignored and named in warnings, after the warnings of the service (req_001 D-105).
func (a *Admin) AdminIssueAccessKey(ctx context.Context, req *api.AccessKeyCreate, params api.AdminIssueAccessKeyParams) (api.AdminIssueAccessKeyRes, error) {
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
	k.Warnings = withIgnored(ctx, k.Warnings)
	return &api.AccessKeyIssued{AccessKey: k, Secret: is.Secret.Reveal()}, nil
}

// AdminUpdateAccessKey implements PATCH /admin/v1/access-keys/{access_key_id}. A revoked or
// expired key is 409, and a narrowing the consumer may not set is 403. An unknown request field is
// ignored and named in warnings, after the warnings of the service (req_001 D-105).
func (a *Admin) AdminUpdateAccessKey(ctx context.Context, req *api.AccessKeyUpdate, params api.AdminUpdateAccessKeyParams) (api.AdminUpdateAccessKeyRes, error) {
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
	v, err := toAccessKey(k)
	if err != nil {
		return nil, err
	}
	v.Warnings = withIgnored(ctx, v.Warnings)
	return &v, nil
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
		BoundNode: optNil(k.BoundNode), CreatedAt: k.CreatedAt, ExpiresAt: optNilTime(k.ExpiresAt), RevokedAt: optNilTime(k.RevokedAt),
		Warnings: k.Warnings}
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

// narrowingIn reads the narrowing of a request. The deprecated quota given as null, or without a
// number, adds no quota. Both quotas and the deprecated quota may be given; the service decides
// which wins and warns (req_001 D-107).
func narrowingIn(n api.AccessKeyNarrowing) *consumers.Narrowing {
	out := &consumers.Narrowing{Models: n.Models, Quotas: quotaEntriesIn(n.Quotas)}
	if q, ok := n.Quota.Get(); ok {
		out.Quota = &consumers.Quota{Tokens: optInt64(q.Tokens), Requests: optInt64(q.Requests)}
	}
	out.ContextLimit = optInt(n.ContextLimit)
	out.MaxOutputTokens = optInt(n.MaxOutputTokens)
	out.Concurrency = optInt(n.Concurrency)
	return out
}

// quotaEntriesIn reads the quota entries of a request. A window too long to hold as a duration
// becomes the longest whole-second one, which the host's limits.quota_window_max refuses.
func quotaEntriesIn(in []api.QuotaEntry) []grants.QuotaEntry {
	if len(in) == 0 {
		return nil
	}
	out := make([]grants.QuotaEntry, len(in))
	for i, e := range in {
		secs := e.WindowSeconds
		if secs > math.MaxInt64/int64(time.Second) {
			secs = math.MaxInt64 / int64(time.Second)
		}
		out[i] = grants.QuotaEntry{Metric: string(e.Metric), Limit: e.Limit, Window: time.Duration(secs) * time.Second}
	}
	return out
}

func quotaEntriesOut(in []grants.QuotaEntry) []api.QuotaEntry {
	if len(in) == 0 {
		return nil
	}
	out := make([]api.QuotaEntry, len(in))
	for i, e := range in {
		out[i] = api.QuotaEntry{Metric: api.QuotaEntryMetric(e.Metric), Limit: e.Limit, WindowSeconds: int64(e.Window / time.Second)}
	}
	return out
}

func narrowingOut(n consumers.Narrowing) api.AccessKeyNarrowing {
	out := api.AccessKeyNarrowing{Models: n.Models, Quotas: quotaEntriesOut(n.Quotas)}
	if n.Quota != nil {
		q := api.Quota{}
		if n.Quota.Tokens != nil {
			q.Tokens = api.NewOptInt64(*n.Quota.Tokens)
		}
		if n.Quota.Requests != nil {
			q.Requests = api.NewOptInt64(*n.Quota.Requests)
		}
		out.Quota = api.NewOptNilQuota(q)
	} else {
		out.Quota.SetToNull()
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
