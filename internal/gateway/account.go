package gateway

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/consumerauth"
)

// Account is the part of consumerauth.Service the gateway serves the /v1/me and /v1/resource-requests routes with: the
// routes the spec lets a consumer call with an access key as well as with a session (req_001
// D-39). The gateway never takes a session or a cookie (req_001 D-148); it calls these with the
// Caller of the authenticated key, so a key and a session see the same account through one set
// of methods. The password, session and sign-up routes are not here: they are the consumer
// page's alone.
type Account interface {
	Me(ctx context.Context, c consumerauth.Caller) (*api.Consumer, error)
	Limits(ctx context.Context, c consumerauth.Caller) (*api.GrantVersion, error)
	AccessKeys(ctx context.Context, c consumerauth.Caller) (*api.AccessKeyList, error)
	Settings(ctx context.Context, c consumerauth.Caller) (*api.SettingList, error)
	UpdateSettings(ctx context.Context, c consumerauth.Caller, req *api.SettingsUpdate) (*api.SettingList, error)
	// The resource request routes, which also take a key or a session (req_001 D-39). A consumer
	// sees only its own requests.
	ListResourceRequests(ctx context.Context, c consumerauth.Caller, params api.ListResourceRequestsParams) (*api.ResourceRequestList, error)
	CreateResourceRequest(ctx context.Context, c consumerauth.Caller, req *api.ResourceRequestCreate) (*api.ResourceRequest, error)
	GetResourceRequest(ctx context.Context, c consumerauth.Caller, params api.GetResourceRequestParams) (*api.ResourceRequest, error)
	CommentOnResourceRequest(ctx context.Context, c consumerauth.Caller, req *api.CommentCreate, params api.CommentOnResourceRequestParams) (*api.ResourceRequest, error)
	WithdrawResourceRequest(ctx context.Context, c consumerauth.Caller, params api.WithdrawResourceRequestParams) (*api.ResourceRequest, error)
}

// account returns the consumer the authenticated key stands for. With no Config.Account the
// routes answer 501, as they did before the accounts module existed.
func (g *Gateway) account(ctx context.Context) (Account, consumerauth.Caller, error) {
	if g.cfg.Account == nil {
		return nil, consumerauth.Caller{}, ht.ErrNotImplemented
	}
	id, err := g.caller(ctx)
	if err != nil {
		return nil, consumerauth.Caller{}, err
	}
	return g.cfg.Account, consumerauth.Caller{ConsumerID: id.ConsumerID, PrincipalID: id.PrincipalID, KeyID: id.KeyID}, nil
}

// GetMe implements GET /v1/me with an access key.
func (g *Gateway) GetMe(ctx context.Context) (*api.Consumer, error) {
	a, c, err := g.account(ctx)
	if err != nil {
		return nil, err
	}
	return a.Me(ctx, c)
}

// GetMyLimits implements GET /v1/me/limits with an access key.
func (g *Gateway) GetMyLimits(ctx context.Context) (*api.GrantVersion, error) {
	a, c, err := g.account(ctx)
	if err != nil {
		return nil, err
	}
	return a.Limits(ctx, c)
}

// ListMyAccessKeys implements GET /v1/me/access-keys with an access key: prefixes only.
func (g *Gateway) ListMyAccessKeys(ctx context.Context) (*api.AccessKeyList, error) {
	a, c, err := g.account(ctx)
	if err != nil {
		return nil, err
	}
	return a.AccessKeys(ctx, c)
}

// GetMySettings implements GET /v1/me/settings with an access key.
func (g *Gateway) GetMySettings(ctx context.Context) (*api.SettingList, error) {
	a, c, err := g.account(ctx)
	if err != nil {
		return nil, err
	}
	return a.Settings(ctx, c)
}

// UpdateMySettings implements PATCH /v1/me/settings with an access key.
func (g *Gateway) UpdateMySettings(ctx context.Context, req *api.SettingsUpdate) (*api.SettingList, error) {
	a, c, err := g.account(ctx)
	if err != nil {
		return nil, err
	}
	return a.UpdateSettings(ctx, c, req)
}

// ListResourceRequests implements GET /v1/resource-requests with an access key.
func (g *Gateway) ListResourceRequests(ctx context.Context, params api.ListResourceRequestsParams) (*api.ResourceRequestList, error) {
	a, c, err := g.account(ctx)
	if err != nil {
		return nil, err
	}
	return a.ListResourceRequests(ctx, c, params)
}

// CreateResourceRequest implements POST /v1/resource-requests with an access key.
func (g *Gateway) CreateResourceRequest(ctx context.Context, req *api.ResourceRequestCreate) (*api.ResourceRequest, error) {
	a, c, err := g.account(ctx)
	if err != nil {
		return nil, err
	}
	return a.CreateResourceRequest(ctx, c, req)
}

// GetResourceRequest implements GET /v1/resource-requests/{id} with an access key.
func (g *Gateway) GetResourceRequest(ctx context.Context, params api.GetResourceRequestParams) (*api.ResourceRequest, error) {
	a, c, err := g.account(ctx)
	if err != nil {
		return nil, err
	}
	return a.GetResourceRequest(ctx, c, params)
}

// CommentOnResourceRequest implements POST /v1/resource-requests/{id}/comments with an access key.
func (g *Gateway) CommentOnResourceRequest(ctx context.Context, req *api.CommentCreate, params api.CommentOnResourceRequestParams) (*api.ResourceRequest, error) {
	a, c, err := g.account(ctx)
	if err != nil {
		return nil, err
	}
	return a.CommentOnResourceRequest(ctx, c, req, params)
}

// WithdrawResourceRequest implements POST /v1/resource-requests/{id}/withdraw with an access key.
func (g *Gateway) WithdrawResourceRequest(ctx context.Context, params api.WithdrawResourceRequestParams) (*api.ResourceRequest, error) {
	a, c, err := g.account(ctx)
	if err != nil {
		return nil, err
	}
	return a.WithdrawResourceRequest(ctx, c, params)
}
