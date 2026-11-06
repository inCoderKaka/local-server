package admin

import (
	"context"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	admingrants "github.com/inCoderKaka/local-server/internal/admin/grants"
	"github.com/inCoderKaka/local-server/internal/api"
)

// AdminListModels implements GET /admin/v1/models: the engine's models with the host's sharing
// configuration for each.
func (a *Admin) AdminListModels(ctx context.Context) (*api.HostModelList, error) {
	ms, err := a.grs.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	out := &api.HostModelList{Data: make([]api.HostModel, 0, len(ms))}
	for _, m := range ms {
		o, err := hostModelOut(m)
		if err != nil {
			return nil, err
		}
		out.Data = append(out.Data, o)
	}
	return out, nil
}

// AdminUpdateModel implements PATCH /admin/v1/models/{model_id}: fields left out stay as they
// are, and a keep_alive_seconds of null returns the model to the host's default.
func (a *Admin) AdminUpdateModel(ctx context.Context, req *api.HostModelUpdate, params api.AdminUpdateModelParams) (*api.HostModel, error) {
	var ch admingrants.ModelChange
	if v, ok := req.Allowed.Get(); ok {
		ch.Allowed = &v
	}
	if v, ok := req.Preload.Get(); ok {
		ch.Preload = &v
	}
	switch {
	case !req.KeepAliveSeconds.Set:
	case req.KeepAliveSeconds.Null:
		ch.KeepAliveSeconds = consumers.Null[int64]()
	default:
		ch.KeepAliveSeconds = consumers.Value(int64(req.KeepAliveSeconds.Value))
	}
	m, err := a.grs.UpdateModel(ctx, PrincipalIDOf(ctx), params.ModelID.String(), ch)
	if err != nil {
		return nil, err
	}
	o, err := hostModelOut(m)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func hostModelOut(m admingrants.HostModel) (api.HostModel, error) {
	id, err := uuid.Parse(m.ID)
	if err != nil {
		return api.HostModel{}, err
	}
	out := api.HostModel{ID: id, Name: m.Name, Engine: m.Engine, Allowed: m.Allowed, Preload: m.Preload, Loaded: m.Loaded}
	if m.KeepAliveSeconds != nil {
		out.KeepAliveSeconds = api.NewOptNilInt(int(*m.KeepAliveSeconds))
	} else {
		out.KeepAliveSeconds.SetToNull()
	}
	if m.SizeBytes != nil {
		out.SizeBytes = api.NewOptNilInt64(*m.SizeBytes)
	} else {
		out.SizeBytes.SetToNull()
	}
	return out, nil
}
