package consumerauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	ht "github.com/ogen-go/ogen/http"
	"github.com/ogen-go/ogen/ogenerrors"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/api/ignored"
	"github.com/inCoderKaka/local-server/internal/auth/credential"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/telemetry"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// pageAPI is the ogen server's handler on the consumer page listener: the account routes the
// consumer page uses, with the session cookie, passkey routes included. Every other operation
// stays what api.UnimplementedHandler makes it (501).
type pageAPI struct {
	api.UnimplementedHandler
	s *Service
}

var (
	_ api.Handler         = (*pageAPI)(nil)
	_ api.SecurityHandler = (*pageAPI)(nil)
)

func (s *Service) buildPage() error {
	p := &pageAPI{s: s}
	srv, err := api.NewServer(p, p,
		api.WithMiddleware(ignored.Only("createResourceRequest")), // names the ask's ignored fields (req_001 D-105)
		api.WithErrorHandler(s.errorHandler),
		api.WithNotFound(s.notFound),
		api.WithMethodNotAllowed(s.methodNotAllowed),
	)
	if err != nil {
		return err
	}
	s.srv = srv
	return nil
}

// Handler returns the consumer page listener's handler (req_001 D-10, D-148): a request id on
// every response, the credential guard (Host must be loopback or one of the host's names; a
// state-changing request that carries the session cookie must carry a same-origin Origin), then
// the /v1 account routes through their own ogen server with the request body cap, then the page
// (Config.UI) for GET and HEAD of the other paths, and a plain 404 for everything else. A panic
// in a handler is answered 500 and logged by type only. The consumer API listener never serves
// these routes with a cookie: it has its own server (internal/gateway).
func (s *Service) Handler() http.Handler {
	guard := credential.Guard(credential.GuardConfig{
		Names:          s.hostNames,
		SessionCookies: []string{credential.ConsumerSessionCookie},
		Reject:         s.reject,
	})
	return telemetry.Middleware(s.recoverPanics(guard(http.HandlerFunc(s.serve))))
}

func (s *Service) hostNames(ctx context.Context) ([]string, error) {
	names, err := s.cfg.HostNames(ctx)
	if err != nil {
		s.log.ErrorContext(ctx, "consumerauth: read the host's names", "err", err)
	}
	return names, err
}

// reject answers a request the guard refused. The reason is a fixed code; the Host or Origin that
// caused it is neither answered nor logged.
func (s *Service) reject(w http.ResponseWriter, r *http.Request, why credential.Reason) {
	if why == credential.ReasonHostUnavailable {
		s.log.ErrorContext(r.Context(), "consumerauth: guard could not read the accepted host names", "reason", string(why))
		writeError(w, internalError())
		return
	}
	s.log.InfoContext(r.Context(), "consumerauth: request refused by the guard", "reason", string(why))
	msg := "This listener answers only to the host's own name."
	switch why {
	case credential.ReasonForeignOrigin, credential.ReasonOriginRequired:
		msg = "A state-changing request that carries a session must come from this page's own origin."
	}
	writeError(w, forbidden("", msg))
}

func (s *Service) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				// A panic value can hold request bytes; log its type only.
				s.log.ErrorContext(r.Context(), "consumerauth: handler panic", "panic_type", fmt.Sprintf("%T", p))
				writeError(w, internalError())
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// onAPIPath reports whether a request path belongs to the /v1 API. Both the decoded and the
// escaped form must be under /v1/, so no encoding reaches another route.
func onAPIPath(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/v1/") {
		return false
	}
	return r.URL.RawPath == "" || strings.HasPrefix(r.URL.RawPath, "/v1/")
}

// reservedPath reports whether a path is reserved for an API in its decoded or escaped form, so
// such a path never gets the page.
func reservedPath(r *http.Request) bool {
	for _, p := range []string{r.URL.Path, r.URL.RawPath} {
		if p == "" {
			continue
		}
		lp := strings.ToLower(p)
		for _, root := range []string{"/v1", "/admin", "/metrics"} {
			if lp == root || strings.HasPrefix(lp, root+"/") || strings.HasPrefix(lp, root+"%2f") {
				return true
			}
		}
	}
	return false
}

func (s *Service) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case onAPIPath(r):
		if err := s.cfg.LimitBody(r.Context(), r); err != nil {
			e, internal := classify(err)
			if internal {
				s.log.ErrorContext(r.Context(), "consumerauth: request failed", "err", err)
			}
			writeError(w, e)
			return
		}
		ctx := context.WithValue(r.Context(), remoteKey{}, r.RemoteAddr)
		ctx = context.WithValue(ctx, pageHostKey{}, pageHost{host: r.Host, tls: r.TLS != nil})
		r = r.WithContext(ctx)
		s.srv.ServeHTTP(w, r)
	case s.cfg.UI != nil && (r.Method == http.MethodGet || r.Method == http.MethodHead) && !reservedPath(r):
		s.cfg.UI.ServeHTTP(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Service) notFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, notFoundErr("No such endpoint."))
}

func (s *Service) methodNotAllowed(w http.ResponseWriter, _ *http.Request, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, newErr(http.StatusMethodNotAllowed, typeInvalidRequest, "", "", "This endpoint does not accept this method."))
}

// errorHandler answers what ogen sends it directly: request decoding errors, the not-implemented
// error of an operation no ticket has built yet, and a response it could not write. A refusal is
// logged with its status only, never the error text: ogen's messages can quote the request.
func (s *Service) errorHandler(ctx context.Context, w http.ResponseWriter, _ *http.Request, err error) {
	var de *ogenerrors.DecodeRequestError
	var dp *ogenerrors.DecodeParamsError
	var mb *http.MaxBytesError
	var e *Error
	switch {
	case errors.Is(err, ht.ErrNotImplemented):
		e = newErr(http.StatusNotImplemented, typeServer, "", "", "This endpoint is not available on this host yet.")
	case errors.As(err, &de), errors.As(err, &dp), errors.As(err, &mb), errors.Is(err, limits.ErrTooLarge):
		e = decodeError(err)
		s.log.InfoContext(ctx, "consumerauth: request refused at decoding", "status", e.Status)
	default:
		s.log.WarnContext(ctx, "consumerauth: response could not be written")
		e = internalError()
	}
	writeError(w, e)
}

// NewError implements api.Handler: it maps every error a handler or the security handler returns
// to the envelope. Expected refusals are not logged; an internal failure is, with the error text
// of the modules (which never holds a password, a token, a hash or content, req_001 D-31).
func (p *pageAPI) NewError(ctx context.Context, err error) *api.ErrorStatusCodeWithHeaders {
	e, internal := classify(err)
	if internal {
		p.s.log.ErrorContext(ctx, "consumerauth: request failed", "err", err)
	}
	return e.response()
}

// HandleAccessKeyAuth never authenticates on the page listener: an access key belongs to the
// consumer API, so a key sent here is a 401 (req_001 D-148).
func (p *pageAPI) HandleAccessKeyAuth(context.Context, api.OperationName, api.AccessKeyAuth) (context.Context, error) {
	return nil, ogenerrors.ErrSkipServerSecurity
}

// HandleAdminSessionAuth never authenticates: admin operations are not served here.
func (p *pageAPI) HandleAdminSessionAuth(context.Context, api.OperationName, api.AdminSessionAuth) (context.Context, error) {
	return nil, ogenerrors.ErrSkipServerSecurity
}

// HandleConsumerSessionAuth checks the ls_session cookie and puts the caller in the context
// (see Service.ResolveSession).
func (p *pageAPI) HandleConsumerSessionAuth(ctx context.Context, _ api.OperationName, t api.ConsumerSessionAuth) (context.Context, error) {
	c, err := p.s.ResolveSession(ctx, t.APIKey)
	if err != nil {
		return nil, err
	}
	return WithCaller(ctx, c), nil
}

func (p *pageAPI) caller(ctx context.Context) (Caller, error) {
	c, ok := CallerOf(ctx)
	if !ok {
		return Caller{}, errors.New("consumerauth: request reached a handler without authentication")
	}
	return c, nil
}

// RedeemInviteWithPassword implements POST /v1/invite-redemptions/password.
func (p *pageAPI) RedeemInviteWithPassword(ctx context.Context, req *api.InviteRedemptionPassword) (*api.ConsumerHeaders, error) {
	in := Redemption{Token: req.InviteToken, DisplayName: req.DisplayName, Login: req.Username, Secret: req.Password,
		Terms: req.Consent.TermsVersion, Privacy: req.Consent.PrivacyVersion}
	if v, ok := req.Email.Get(); ok {
		in.Email = &v
	}
	acct, cookie, err := p.s.RedeemInvite(ctx, identity.CredentialPassword, in)
	if err != nil {
		return nil, err
	}
	return p.consumerHeaders(acct, cookie)
}

// CreateConsumerSessionWithPassword implements POST /v1/session/password.
func (p *pageAPI) CreateConsumerSessionWithPassword(ctx context.Context, req *api.PasswordLogin) (*api.ConsumerHeaders, error) {
	acct, cookie, err := p.s.Login(ctx, identity.CredentialPassword, req.Username, req.Password, remoteOf(ctx))
	if err != nil {
		return nil, err
	}
	return p.consumerHeaders(acct, cookie)
}

func (p *pageAPI) consumerHeaders(acct identity.ConsumerAccount, cookie string) (*api.ConsumerHeaders, error) {
	body, err := consumerOut(acct)
	if err != nil {
		return nil, err
	}
	return &api.ConsumerHeaders{SetCookie: api.NewOptString(cookie), Response: *body}, nil
}

// DeleteConsumerSession implements DELETE /v1/session.
func (p *pageAPI) DeleteConsumerSession(ctx context.Context) (*api.DeleteConsumerSessionNoContent, error) {
	c, err := p.caller(ctx)
	if err != nil {
		return nil, err
	}
	if err := p.s.Logout(ctx, c); err != nil {
		return nil, err
	}
	return &api.DeleteConsumerSessionNoContent{SetCookie: api.NewOptString(credential.ClearCookie(credential.ConsumerSessionCookie))}, nil
}

// GetMe implements GET /v1/me.
func (p *pageAPI) GetMe(ctx context.Context) (*api.Consumer, error) {
	c, err := p.caller(ctx)
	if err != nil {
		return nil, err
	}
	return p.s.Me(ctx, c)
}

// GetMyLimits implements GET /v1/me/limits.
func (p *pageAPI) GetMyLimits(ctx context.Context) (*api.GrantVersion, error) {
	c, err := p.caller(ctx)
	if err != nil {
		return nil, err
	}
	return p.s.Limits(ctx, c)
}

// ListMyAccessKeys implements GET /v1/me/access-keys.
func (p *pageAPI) ListMyAccessKeys(ctx context.Context) (*api.AccessKeyList, error) {
	c, err := p.caller(ctx)
	if err != nil {
		return nil, err
	}
	return p.s.AccessKeys(ctx, c)
}

// GetMySettings implements GET /v1/me/settings.
func (p *pageAPI) GetMySettings(ctx context.Context) (*api.SettingList, error) {
	c, err := p.caller(ctx)
	if err != nil {
		return nil, err
	}
	return p.s.Settings(ctx, c)
}

// UpdateMySettings implements PATCH /v1/me/settings.
func (p *pageAPI) UpdateMySettings(ctx context.Context, req *api.SettingsUpdate) (*api.SettingList, error) {
	c, err := p.caller(ctx)
	if err != nil {
		return nil, err
	}
	return p.s.UpdateSettings(ctx, c, req)
}

// SetMyPassword implements PUT /v1/me/password.
func (p *pageAPI) SetMyPassword(ctx context.Context, req *api.PasswordChange) error {
	c, err := p.caller(ctx)
	if err != nil {
		return err
	}
	var cur *string
	if v, ok := req.CurrentPassword.Get(); ok {
		cur = &v
	}
	return p.s.SetPassword(ctx, c, cur, req.NewPassword)
}

// GetUsage implements GET /v1/usage with a session: the same body as the key path, for the
// caller's own consumer only (usage.ConsumerRollups).
func (p *pageAPI) GetUsage(ctx context.Context, params api.GetUsageParams) (*api.UsageRollupList, error) {
	c, err := p.caller(ctx)
	if err != nil {
		return nil, err
	}
	return usage.ConsumerRollups(ctx, p.s.cfg.DB.Reader(), p.s.cfg.Settings, c.ConsumerID, params)
}

// ListResourceRequests implements GET /v1/resource-requests with a session.
func (p *pageAPI) ListResourceRequests(ctx context.Context, params api.ListResourceRequestsParams) (*api.ResourceRequestList, error) {
	c, err := p.caller(ctx)
	if err != nil {
		return nil, err
	}
	return p.s.ListResourceRequests(ctx, c, params)
}

// CreateResourceRequest implements POST /v1/resource-requests with a session.
func (p *pageAPI) CreateResourceRequest(ctx context.Context, req *api.ResourceRequestCreate) (*api.ResourceRequest, error) {
	c, err := p.caller(ctx)
	if err != nil {
		return nil, err
	}
	return p.s.CreateResourceRequest(ctx, c, req)
}

// GetResourceRequest implements GET /v1/resource-requests/{id} with a session.
func (p *pageAPI) GetResourceRequest(ctx context.Context, params api.GetResourceRequestParams) (*api.ResourceRequest, error) {
	c, err := p.caller(ctx)
	if err != nil {
		return nil, err
	}
	return p.s.GetResourceRequest(ctx, c, params)
}

// CommentOnResourceRequest implements POST /v1/resource-requests/{id}/comments with a session.
func (p *pageAPI) CommentOnResourceRequest(ctx context.Context, req *api.CommentCreate, params api.CommentOnResourceRequestParams) (*api.ResourceRequest, error) {
	c, err := p.caller(ctx)
	if err != nil {
		return nil, err
	}
	return p.s.CommentOnResourceRequest(ctx, c, req, params)
}

// WithdrawResourceRequest implements POST /v1/resource-requests/{id}/withdraw with a session.
func (p *pageAPI) WithdrawResourceRequest(ctx context.Context, params api.WithdrawResourceRequestParams) (*api.ResourceRequest, error) {
	c, err := p.caller(ctx)
	if err != nil {
		return nil, err
	}
	return p.s.WithdrawResourceRequest(ctx, c, params)
}
