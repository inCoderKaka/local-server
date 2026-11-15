package admin

import (
	"context"

	ht "github.com/ogen-go/ogen/http"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/api/ignored"
	"github.com/inCoderKaka/local-server/internal/requests"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// The resource request operations are thin: package requests owns the rules (req_001 D-38, D-39
// F3, D-157) and the mapping onto the generated types. Approving makes the new grant version
// through the grant operations (admin/grants), inside the unit of work that approves the request.
// The text of a request or a comment is never logged. With no Config.Requests every operation
// answers 501, as it did before the module existed.

func (a *Admin) requests() (*requests.Service, error) {
	if a.cfg.Requests == nil {
		return nil, ht.ErrNotImplemented
	}
	return a.cfg.Requests, nil
}

// AdminListResourceRequests implements GET /admin/v1/resource-requests: newest first, without
// comments. The admin UI's count of pending requests asks for status=open. The page size is the
// requests.page_size setting.
func (a *Admin) AdminListResourceRequests(ctx context.Context, params api.AdminListResourceRequestsParams) (*api.ResourceRequestList, error) {
	svc, err := a.requests()
	if err != nil {
		return nil, err
	}
	q := requests.QueryOut(params.Status, params.Cursor, params.Limit)
	if id, ok := params.ConsumerID.Get(); ok {
		q.ConsumerID = id.String()
	}
	page, err := svc.List(ctx, q)
	if err != nil {
		return nil, err
	}
	return requests.PageOut(page)
}

// AdminGetResourceRequest implements GET /admin/v1/resource-requests/{id}: with its comments.
func (a *Admin) AdminGetResourceRequest(ctx context.Context, params api.AdminGetResourceRequestParams) (*api.ResourceRequest, error) {
	svc, err := a.requests()
	if err != nil {
		return nil, err
	}
	q, err := svc.Get(ctx, "", params.ResourceRequestID.String())
	if err != nil {
		return nil, err
	}
	return requests.Out(q)
}

// AdminCommentOnResourceRequest implements POST /admin/v1/resource-requests/{id}/comments: 201 with
// the request. A final request is 409.
func (a *Admin) AdminCommentOnResourceRequest(ctx context.Context, req *api.CommentCreate, params api.AdminCommentOnResourceRequestParams) (*api.ResourceRequest, error) {
	svc, err := a.requests()
	if err != nil {
		return nil, err
	}
	q, err := svc.Comment(ctx, "", PrincipalIDOf(ctx), params.ResourceRequestID.String(), req.Body)
	if err != nil {
		return nil, err
	}
	return requests.Out(q)
}

// AdminRejectResourceRequest implements POST /admin/v1/resource-requests/{id}/reject. A final
// request is 409.
func (a *Admin) AdminRejectResourceRequest(ctx context.Context, req *api.ResourceRequestRejection, params api.AdminRejectResourceRequestParams) (*api.ResourceRequest, error) {
	svc, err := a.requests()
	if err != nil {
		return nil, err
	}
	q, err := svc.Reject(ctx, PrincipalIDOf(ctx), params.ResourceRequestID.String(), req.Comment.Value)
	if err != nil {
		return nil, err
	}
	return requests.Out(q)
}

// AdminApproveResourceRequest implements POST /admin/v1/resource-requests/{id}/approve: in one
// unit of work it makes the new grant version from limits and makes it current, approves the
// request, and writes the audit rows, the grant.changed and resource_request.decided events and
// the sync changes (req_001 D-39 F3). A final request is 409, checked before the limits. An
// unknown field of limits is ignored and named in warnings, after the warnings of the grant
// operation (req_001 D-105).
func (a *Admin) AdminApproveResourceRequest(ctx context.Context, req *api.ResourceRequestApproval, params api.AdminApproveResourceRequestParams) (*api.ResourceRequest, error) {
	svc, err := a.requests()
	if err != nil {
		return nil, err
	}
	id, actor := params.ResourceRequestID.String(), PrincipalIDOf(ctx)
	cur, err := svc.Get(ctx, "", id)
	if err != nil {
		return nil, err
	}
	if cur.Status != requests.StatusOpen {
		return nil, requests.ErrNotOpen
	}
	prep, err := a.grs.Prepare(ctx, grantLimitsIn(&req.Limits))
	if err != nil {
		return nil, err
	}
	q, err := svc.Approve(ctx, actor, id, req.Comment.Value, func(tx *storage.Tx, consumerID, reason string) (string, error) {
		v, err := prep.Apply(tx, actor, consumerID, reason)
		return v.ID, err
	})
	if err != nil {
		return nil, err
	}
	a.cfg.Auth.Invalidate() // as SetGrant does once the version has committed
	out, err := requests.Out(q)
	if err != nil {
		return nil, err
	}
	out.Warnings = ignored.With(ctx, prep.Warnings)
	return out, nil
}
