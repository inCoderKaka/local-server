package admin

import (
	"context"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	admingrants "github.com/inCoderKaka/local-server/internal/admin/grants"
	"github.com/inCoderKaka/local-server/internal/api"
)

// The grant operations are thin: package admin/grants owns the rules (req_001 D-25, D-107), this
// file maps its types onto the generated ones. Every write names the logged-in admin as the actor
// of its audit row.

// AdminGetGrant implements GET /admin/v1/consumers/{consumer_id}/grant.
func (a *Admin) AdminGetGrant(ctx context.Context, params api.AdminGetGrantParams) (*api.GrantVersion, error) {
	v, err := a.grs.GetGrant(ctx, params.ConsumerID.String())
	if err != nil {
		return nil, err
	}
	return grantVersionOut(v)
}

// AdminSetGrant implements PUT /admin/v1/consumers/{consumer_id}/grant: always a new version. An
// unknown request field is ignored and named in warnings, after the warnings of the service
// (req_001 D-105).
func (a *Admin) AdminSetGrant(ctx context.Context, req *api.GrantLimits, params api.AdminSetGrantParams) (*api.GrantVersion, error) {
	v, err := a.grs.SetGrant(ctx, PrincipalIDOf(ctx), params.ConsumerID.String(), grantLimitsIn(req))
	if err != nil {
		return nil, err
	}
	out, err := grantVersionOut(v)
	if err != nil {
		return nil, err
	}
	out.Warnings = withIgnored(ctx, out.Warnings)
	return out, nil
}

// AdminListGrantVersions implements GET /admin/v1/consumers/{consumer_id}/grant/versions: newest
// first. The page size is the admin.list_page_size setting (req_001 D-38).
func (a *Admin) AdminListGrantVersions(ctx context.Context, params api.AdminListGrantVersionsParams) (*api.GrantVersionList, error) {
	q := admingrants.ListQuery{Cursor: params.Cursor.Value}
	if v, ok := params.Limit.Get(); ok {
		q.Limit = &v
	}
	page, err := a.grs.ListGrantVersions(ctx, params.ConsumerID.String(), q)
	if err != nil {
		return nil, err
	}
	out := &api.GrantVersionList{Data: make([]api.GrantVersion, 0, len(page.Items))}
	for _, v := range page.Items {
		o, err := grantVersionOut(v)
		if err != nil {
			return nil, err
		}
		out.Data = append(out.Data, *o)
	}
	out.NextCursor = nextCursor(page.NextCursor)
	return out, nil
}

func grantLimitsIn(r *api.GrantLimits) admingrants.Limits {
	in := admingrants.Limits{Quotas: quotaEntriesIn(r.Quotas), Concurrency: int64(r.Concurrency),
		OnContextOverflow: string(r.OnContextOverflow), AllowedProperties: r.AllowedRequestProperties}
	for _, m := range r.Models {
		in.Models = append(in.Models, admingrants.ModelLimit{Model: m.Model, ContextLimit: int64(m.ContextLimit), MaxOutputTokens: int64(m.MaxOutputTokens)})
	}
	if q, ok := r.Quota.Get(); ok {
		in.Quota = &consumers.Quota{Tokens: optInt64(q.Tokens), Requests: optInt64(q.Requests)}
	}
	if t, ok := r.ExpiresAt.Get(); ok {
		in.ExpiresAt = &t
	}
	return in
}

func grantVersionOut(v admingrants.GrantVersion) (*api.GrantVersion, error) {
	id, err := uuid.Parse(v.ID)
	if err != nil {
		return nil, err
	}
	gid, err := uuid.Parse(v.GrantID)
	if err != nil {
		return nil, err
	}
	cid, err := uuid.Parse(v.ConsumerID)
	if err != nil {
		return nil, err
	}
	l := api.GrantLimits{Models: make([]api.GrantModelLimit, len(v.Limits.Models)), Quotas: quotaEntriesOut(v.Limits.Quotas),
		Concurrency: int(v.Limits.Concurrency), OnContextOverflow: api.ContextOverflow(v.Limits.OnContextOverflow),
		AllowedRequestProperties: v.Limits.AllowedProperties}
	for i, m := range v.Limits.Models {
		l.Models[i] = api.GrantModelLimit{Model: m.Model, ContextLimit: int(m.ContextLimit), MaxOutputTokens: int(m.MaxOutputTokens)}
	}
	if q := v.Limits.Quota; q != nil {
		o := api.Quota{Tokens: optInt64Out(q.Tokens), Requests: optInt64Out(q.Requests)}
		l.Quota = api.NewOptNilQuota(o)
	} else {
		l.Quota.SetToNull()
	}
	if v.Limits.ExpiresAt != nil {
		l.ExpiresAt = api.NewOptNilDateTime(v.Limits.ExpiresAt.UTC())
	} else {
		l.ExpiresAt.SetToNull()
	}
	out := &api.GrantVersion{ID: id, GrantID: gid, ConsumerID: cid, Version: int(v.Version), CreatedAt: v.CreatedAt.UTC(), Limits: l, Warnings: v.Warnings}
	if v.ResourceRequestID != nil {
		rid, err := uuid.Parse(*v.ResourceRequestID)
		if err != nil {
			return nil, err
		}
		out.ResourceRequestID = api.NewOptNilUUID(rid)
	} else {
		out.ResourceRequestID.SetToNull()
	}
	return out, nil
}

func optInt64Out(v *int64) api.OptInt64 {
	if v == nil {
		return api.OptInt64{}
	}
	return api.NewOptInt64(*v)
}
