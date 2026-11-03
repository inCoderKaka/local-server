package admin

import (
	"context"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// AdminGetUsage implements GET /admin/v1/usage: the usage rollups of every consumer, or of one
// when consumer_id is given, oldest bucket first. It is the same query function the consumer's
// GET /v1/usage uses, with the consumer filter left empty (req_001 D-27, D-104); the page size is
// the usage.page_size setting (req_001 D-38).
func (a *Admin) AdminGetUsage(ctx context.Context, params api.AdminGetUsageParams) (*api.UsageRollupList, error) {
	q := usage.RollupQuery{Granularity: usage.Granularity(params.Granularity)}
	if v, ok := params.ConsumerID.Get(); ok {
		q.ConsumerID = v.String()
	}
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
	page, err := usage.QueryRollups(ctx, a.cfg.DB.Reader(), a.cfg.Settings, q)
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
