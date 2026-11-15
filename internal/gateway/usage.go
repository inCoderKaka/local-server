package gateway

import (
	"context"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// GetUsage implements GET /v1/usage: the caller's own usage rollups per access key and model,
// oldest bucket first (req_001 D-27, D-104). The consumer filter is always the authenticated
// caller's consumer, so one consumer never sees another's usage; access_key_id and model only
// narrow within it. The query and the mapping are usage.ConsumerRollups, which the consumer
// page's session path calls too, so both bodies are the same. The page size is the
// usage.page_size setting (req_001 D-38).
func (g *Gateway) GetUsage(ctx context.Context, params api.GetUsageParams) (*api.UsageRollupList, error) {
	id, err := g.caller(ctx)
	if err != nil {
		return nil, err
	}
	return usage.ConsumerRollups(ctx, g.cfg.DB.Reader(), g.cfg.Settings, id.ConsumerID, params)
}
