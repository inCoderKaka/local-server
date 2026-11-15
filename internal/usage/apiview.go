package usage

import (
	"context"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// ConsumerRollups answers GET /v1/usage for one consumer: its own rollups per access key and
// model, oldest bucket first, in the API's shape (req_001 D-27, D-104). The consumer filter is
// always consumerID, the authenticated caller's own consumer, so one consumer never sees
// another's usage; the access key and model parameters only narrow within it. Both ways of
// calling /v1/usage (an access key on the consumer API, a session on the consumer page) use this
// one function, so their bodies cannot differ. The page size is the usage.page_size setting
// (req_001 D-38). A bad cursor or granularity returns ErrInvalidCursor or ErrInvalidGranularity.
func ConsumerRollups(ctx context.Context, rd *storage.Reader, st *settings.Store, consumerID string, params api.GetUsageParams) (*api.UsageRollupList, error) {
	q := RollupQuery{Granularity: Granularity(params.Granularity), ConsumerID: consumerID}
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
	page, err := QueryRollups(ctx, rd, st, q)
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
