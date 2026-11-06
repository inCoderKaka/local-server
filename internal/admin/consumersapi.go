package admin

import (
	"context"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/identity"
)

// The consumer operations are thin: package consumers owns the rules (req_001 D-25, D-40, D-54),
// this file maps its types onto the generated ones. Every write names the logged-in admin as the
// actor of its audit row.

// AdminListConsumers implements GET /admin/v1/consumers: newest first, deleted consumers only
// when asked for by status. The page size is the admin.list_page_size setting (req_001 D-38).
func (a *Admin) AdminListConsumers(ctx context.Context, params api.AdminListConsumersParams) (*api.ConsumerList, error) {
	q := consumers.ListQuery{Cursor: params.Cursor.Value}
	if v, ok := params.Status.Get(); ok {
		q.Status = string(v)
	}
	if v, ok := params.Limit.Get(); ok {
		q.Limit = &v
	}
	page, err := a.cons.ListConsumers(ctx, q)
	if err != nil {
		return nil, err
	}
	out := &api.ConsumerList{Data: make([]api.Consumer, 0, len(page.Items))}
	for _, c := range page.Items {
		v, err := toConsumer(c)
		if err != nil {
			return nil, err
		}
		out.Data = append(out.Data, v)
	}
	out.NextCursor = nextCursor(page.NextCursor)
	return out, nil
}

// AdminCreateConsumer implements POST /admin/v1/consumers: a consumer without a login.
func (a *Admin) AdminCreateConsumer(ctx context.Context, req *api.ConsumerCreate) (*api.Consumer, error) {
	in := consumers.NewConsumer{Kind: string(req.Kind), DisplayName: req.DisplayName}
	if v, ok := req.Email.Get(); ok {
		in.Email = &v
	}
	c, err := a.cons.CreateConsumer(ctx, PrincipalIDOf(ctx), in)
	if err != nil {
		return nil, err
	}
	return consumerOut(c)
}

// AdminGetConsumer implements GET /admin/v1/consumers/{consumer_id}.
func (a *Admin) AdminGetConsumer(ctx context.Context, params api.AdminGetConsumerParams) (*api.Consumer, error) {
	c, err := a.cons.GetConsumer(ctx, params.ConsumerID.String())
	if err != nil {
		return nil, err
	}
	return consumerOut(c)
}

// AdminUpdateConsumer implements PATCH /admin/v1/consumers/{consumer_id}.
func (a *Admin) AdminUpdateConsumer(ctx context.Context, req *api.ConsumerUpdate, params api.AdminUpdateConsumerParams) (*api.Consumer, error) {
	var ch consumers.ConsumerChange
	if v, ok := req.DisplayName.Get(); ok {
		ch.DisplayName = consumers.Value(v)
	}
	ch.Email = nilString(req.Email)
	c, err := a.cons.UpdateConsumer(ctx, PrincipalIDOf(ctx), params.ConsumerID.String(), ch)
	if err != nil {
		return nil, err
	}
	return consumerOut(c)
}

// AdminDeleteConsumer implements DELETE /admin/v1/consumers/{consumer_id}: 204.
func (a *Admin) AdminDeleteConsumer(ctx context.Context, params api.AdminDeleteConsumerParams) error {
	return a.cons.DeleteConsumer(ctx, PrincipalIDOf(ctx), params.ConsumerID.String())
}

// AdminSuspendConsumer implements POST /admin/v1/consumers/{consumer_id}/suspend.
func (a *Admin) AdminSuspendConsumer(ctx context.Context, params api.AdminSuspendConsumerParams) (*api.Consumer, error) {
	c, err := a.cons.SuspendConsumer(ctx, PrincipalIDOf(ctx), params.ConsumerID.String())
	if err != nil {
		return nil, err
	}
	return consumerOut(c)
}

// AdminReactivateConsumer implements POST /admin/v1/consumers/{consumer_id}/reactivate. A
// deleted consumer is 409.
func (a *Admin) AdminReactivateConsumer(ctx context.Context, params api.AdminReactivateConsumerParams) (*api.Consumer, error) {
	c, err := a.cons.ReactivateConsumer(ctx, PrincipalIDOf(ctx), params.ConsumerID.String())
	if err != nil {
		return nil, err
	}
	return consumerOut(c)
}

func consumerOut(c identity.ConsumerAccount) (*api.Consumer, error) {
	v, err := toConsumer(c)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func toConsumer(c identity.ConsumerAccount) (api.Consumer, error) {
	id, err := uuid.Parse(c.ID)
	if err != nil {
		return api.Consumer{}, err
	}
	out := api.Consumer{ID: id, Kind: api.ConsumerKind(c.Kind), Status: api.ConsumerStatus(c.Status),
		DisplayName: optNil(c.DisplayName), Username: optNil(c.Username), Email: optNil(c.Email),
		HasLogin: c.HasLogin, CreatedAt: c.CreatedAt}
	if c.Status == identity.StatusDeleted {
		out.DeletedAt = api.NewOptNilDateTime(c.StatusChangedAt)
	} else {
		out.DeletedAt.SetToNull()
	}
	return out, nil
}

// optNil is a string that is null when absent.
func optNil(s *string) api.OptNilString {
	if s == nil {
		var o api.OptNilString
		o.SetToNull()
		return o
	}
	return api.NewOptNilString(*s)
}

// nilString reads an optional nullable string of a request as a change.
func nilString(o api.OptNilString) consumers.Field[string] {
	switch {
	case !o.Set:
		return consumers.Field[string]{}
	case o.Null:
		return consumers.Null[string]()
	}
	return consumers.Value(o.Value)
}

// nextCursor is the next_cursor of a page: null on the last one.
func nextCursor(c string) api.NilNextCursor {
	if c == "" {
		var n api.NilNextCursor
		n.SetToNull()
		return n
	}
	return api.NewNilNextCursor(api.NextCursor(c))
}
