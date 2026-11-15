package consumerauth

import (
	"context"
	"errors"

	ht "github.com/ogen-go/ogen/http"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/api/ignored"
	"github.com/inCoderKaka/local-server/internal/requests"
)

// The methods below are the consumer's own resource request routes (/v1/resource-requests). Each
// takes the Caller and touches only that consumer's requests, never a consumer id from the
// request: another consumer's request is not found, the same answer as one that does not exist.
// The consumer page calls them with the session's Caller and the consumer API with the Caller of
// an access key, because the spec lets these routes take a key or a session (req_001 D-39, D-148).
// With no Config.Requests they answer 501, as they did before the requests module existed.
// Nothing of a request's or a comment's text is logged.

func (s *Service) requests() (*requests.Service, error) {
	if s.cfg.Requests == nil {
		return nil, ht.ErrNotImplemented
	}
	return s.cfg.Requests, nil
}

// requestsErr turns a refusal of the requests module into this package's *Error, so both listeners
// answer it the same way. Any other error passes through.
func requestsErr(err error) error {
	var re *requests.Error
	if !errors.As(err, &re) {
		return err
	}
	switch re.Kind {
	case requests.KindNotFound:
		return notFoundErr(re.Message)
	case requests.KindConflict:
		return conflict(re.Message)
	}
	param := re.Param
	if !safeName(param) {
		param = ""
	}
	return badRequest(param, re.Message)
}

// ListResourceRequests implements GET /v1/resource-requests: the caller's requests, newest first,
// without comments.
func (s *Service) ListResourceRequests(ctx context.Context, c Caller, params api.ListResourceRequestsParams) (*api.ResourceRequestList, error) {
	svc, err := s.requests()
	if err != nil {
		return nil, err
	}
	q := requests.QueryOut(params.Status, params.Cursor, params.Limit)
	q.ConsumerID = c.ConsumerID
	page, err := svc.List(ctx, q)
	if err != nil {
		return nil, requestsErr(err)
	}
	return requests.PageOut(page)
}

// CreateResourceRequest implements POST /v1/resource-requests. A field of the ask that the host
// does not know is ignored and named in warnings (req_001 D-105).
func (s *Service) CreateResourceRequest(ctx context.Context, c Caller, req *api.ResourceRequestCreate) (*api.ResourceRequest, error) {
	svc, err := s.requests()
	if err != nil {
		return nil, err
	}
	ask, err := requests.AskIn(req.Ask)
	if err != nil {
		return nil, err
	}
	q, err := svc.Create(ctx, c.ConsumerID, c.PrincipalID, requests.NewRequest{Message: req.Message, Ask: ask})
	if err != nil {
		return nil, requestsErr(err)
	}
	out, err := requests.Out(q)
	if err != nil {
		return nil, err
	}
	out.Warnings = ignored.With(ctx, nil)
	return out, nil
}

// GetResourceRequest implements GET /v1/resource-requests/{id}: with its comments.
func (s *Service) GetResourceRequest(ctx context.Context, c Caller, params api.GetResourceRequestParams) (*api.ResourceRequest, error) {
	svc, err := s.requests()
	if err != nil {
		return nil, err
	}
	q, err := svc.Get(ctx, c.ConsumerID, params.ResourceRequestID.String())
	if err != nil {
		return nil, requestsErr(err)
	}
	return requests.Out(q)
}

// CommentOnResourceRequest implements POST /v1/resource-requests/{id}/comments: 201 with the
// request. A final request is 409.
func (s *Service) CommentOnResourceRequest(ctx context.Context, c Caller, req *api.CommentCreate, params api.CommentOnResourceRequestParams) (*api.ResourceRequest, error) {
	svc, err := s.requests()
	if err != nil {
		return nil, err
	}
	q, err := svc.Comment(ctx, c.ConsumerID, c.PrincipalID, params.ResourceRequestID.String(), req.Body)
	if err != nil {
		return nil, requestsErr(err)
	}
	return requests.Out(q)
}

// WithdrawResourceRequest implements POST /v1/resource-requests/{id}/withdraw. A final request is
// 409.
func (s *Service) WithdrawResourceRequest(ctx context.Context, c Caller, params api.WithdrawResourceRequestParams) (*api.ResourceRequest, error) {
	svc, err := s.requests()
	if err != nil {
		return nil, err
	}
	q, err := svc.Withdraw(ctx, c.ConsumerID, c.PrincipalID, params.ResourceRequestID.String())
	if err != nil {
		return nil, requestsErr(err)
	}
	return requests.Out(q)
}
