package gateway

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// GetUsage implements GET /v1/usage: the caller's own usage rollups per access key and model,
// oldest bucket first (req_001 D-27, D-104). The consumer filter is always the authenticated
// caller's consumer, so one consumer never sees another's usage; access_key_id and model only
// narrow within it. The page size is the usage.page_size setting (req_001 D-38).
func (g *Gateway) GetUsage(ctx context.Context, params api.GetUsageParams) (*api.UsageRollupList, error) {
	id := identityOf(ctx)
	if id == nil {
		return nil, errors.New("gateway: request reached a handler without authentication")
	}
	q := usage.RollupQuery{Granularity: usage.Granularity(params.Granularity), ConsumerID: id.ConsumerID}
	if v, ok := params.From.Get(); ok {
		q.From = v
	}
	if v, ok := params.To.Get(); ok {
		q.To = v
	}
	if v, ok := params.Model.Get(); ok {
		q.Model = v
	}
	if v, ok := params.AccessKeyID.Get(); ok {
		q.AccessKeyID = v.String()
	}
	if v, ok := params.Cursor.Get(); ok {
		q.Cursor = v
	}
	if v, ok := params.Limit.Get(); ok {
		q.Limit = v
	}
	page, err := usage.QueryRollups(ctx, g.cfg.DB.Reader(), g.cfg.Settings, q)
	if err != nil {
		return nil, err
	}
	out := &api.UsageRollupList{Data: make([]api.UsageRollup, 0, len(page.Rollups))}
	for _, r := range page.Rollups {
		cid, err := uuid.Parse(r.ConsumerID)
		if err != nil {
			return nil, err
		}
		kid, err := uuid.Parse(r.AccessKeyID)
		if err != nil {
			return nil, err
		}
		out.Data = append(out.Data, api.UsageRollup{BucketStart: r.BucketStart, Granularity: api.UsageGranularity(r.Granularity),
			ConsumerID: cid, AccessKeyID: kid, Model: r.Model, Requests: r.Requests, PromptTokens: r.PromptTokens,
			CompletionTokens: r.CompletionTokens, TotalTokens: r.TotalTokens})
	}
	if page.NextCursor == "" {
		out.NextCursor.SetToNull()
	} else {
		out.NextCursor = api.NewNilNextCursor(api.NextCursor(page.NextCursor))
	}
	return out, nil
}
