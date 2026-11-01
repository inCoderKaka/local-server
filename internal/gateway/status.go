package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/scheduler"
)

// modelOwner is the owned_by of every model in GET /v1/models. The engine port does not report
// who publishes a model.
const modelOwner = "local-server"

// grantedModel is a model the caller may use: the catalogue entry with the key-narrowed limits.
type grantedModel struct {
	name    string
	created time.Time
	limit   auth.ModelLimit
}

// granted returns the models in the caller's effective limits (the grant narrowed by the key,
// already applied by auth), by name. A retired model is no longer granted.
func (g *Gateway) granted(ctx context.Context, id *auth.Identity) ([]grantedModel, error) {
	models, err := g.cfg.Grants.Models.List(ctx, g.cfg.DB.Reader(), false)
	if err != nil {
		return nil, fmt.Errorf("gateway: read models: %w", err)
	}
	var out []grantedModel
	for _, m := range models {
		for _, l := range id.Limits.Models {
			if l.ModelID == m.ID {
				out = append(out, grantedModel{name: m.Name, created: m.CreatedAt, limit: l})
				break
			}
		}
	}
	return out, nil
}

func (g *Gateway) caller(ctx context.Context) (*auth.Identity, error) {
	id := identityOf(ctx)
	if id == nil {
		return nil, errors.New("gateway: request reached a handler without authentication")
	}
	return id, nil
}

// ListModels implements GET /v1/models: the OpenAI model list, filtered to the models the
// caller's grant and key allow (req_001 D-03, D-05). It lists the host's model catalogue, not
// what the engine has installed, so it answers while the engine is down.
func (g *Gateway) ListModels(ctx context.Context) (*api.ModelList, error) {
	id, err := g.caller(ctx)
	if err != nil {
		return nil, err
	}
	ms, err := g.granted(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &api.ModelList{Object: "list", Data: make([]api.Model, 0, len(ms))}
	for _, m := range ms {
		out.Data = append(out.Data, api.Model{ID: m.name, Object: "model", Created: m.created.Unix(), OwnedBy: modelOwner})
	}
	return out, nil
}

// GetHealth implements GET /v1/health (req_001 D-05): sharing state, engine availability, loaded
// models and queue depth. It needs the same credential as every consumer route (D-24), so
// public mode leaks nothing to anonymous callers. Loaded models are limited to those the caller
// may use, so a caller never learns of a model outside its grant. Queue depth counts the
// scheduler's waiting synchronous requests by class; queued jobs join when the job worker lands.
func (g *Gateway) GetHealth(ctx context.Context) (*api.HostHealth, error) {
	id, err := g.caller(ctx)
	if err != nil {
		return nil, err
	}
	now := g.now()
	state, err := g.cfg.Sharing.Effective(ctx)
	if err != nil && !errors.Is(err, host.ErrInvalidSchedule) {
		return nil, fmt.Errorf("gateway: sharing state: %w", err)
	}
	out := &api.HostHealth{SharingState: api.SharingState(state), CheckedAt: now.UTC(), LoadedModels: []string{}}
	switch state {
	case host.Quiet:
		sch, err := host.LoadSchedule(ctx, g.cfg.Settings)
		if err == nil {
			if end := sch.NextEnd(now); !end.IsZero() {
				out.SharingResumesAt = api.NewOptNilDateTime(end.UTC())
			}
		}
	}
	if !out.SharingResumesAt.Set {
		out.SharingResumesAt.SetToNull()
	}
	st := g.cfg.Scheduler.Stats()
	out.QueueDepth = api.HostHealthQueueDepth{Interactive: st.QueuedInteractive, Background: st.QueuedBackground}

	timeout, err := g.cfg.Settings.Duration(ctx, KeyHealthEngineTimeout, "")
	if err != nil {
		return nil, fmt.Errorf("gateway: read settings: %w", err)
	}
	ectx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	loaded, lerr := g.cfg.Engine.Loaded(ectx)
	if lerr != nil {
		if ctx.Err() == nil {
			g.log.WarnContext(ctx, "gateway: model engine did not answer the health check", "err", lerr)
		}
		out.EngineState = api.HostHealthEngineStateUnavailable
		return out, nil
	}
	out.EngineState = api.HostHealthEngineStateAvailable
	ms, err := g.granted(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		if slices.ContainsFunc(loaded, func(l engine.LoadedModel) bool { return sameModel(m.name, l.Name) }) {
			out.LoadedModels = append(out.LoadedModels, m.name)
		}
	}
	return out, nil
}

// withTag returns a model name with its tag, adding Ollama's default tag when it has none. A colon
// before the last slash is a registry port (host:5000/ns/model), not a tag.
func withTag(n string) string {
	if strings.LastIndex(n, ":") > strings.LastIndex(n, "/") {
		return n
	}
	return n + ":latest"
}

// sameModel reports whether two names are the same model: the engine reports tagged names
// (llama3.2:latest), the catalogue holds whatever the host entered.
func sameModel(a, b string) bool { return withTag(a) == withTag(b) }

// GetCapabilities implements GET /v1/capabilities: the caller's models, context and output
// limits, concurrency, allowed request properties and remaining quota, after the key's
// narrowing (req_001 D-05, D-25, D-98). A model without a cap shows the host's default (the
// value the gateway would apply). Policy rules are not applied: they depend on the request's
// properties and the time. The benchmark fields stay reserved and null (D-45).
func (g *Gateway) GetCapabilities(ctx context.Context) (*api.Capabilities, error) {
	id, err := g.caller(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := g.loadConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("gateway: read settings: %w", err)
	}
	ms, err := g.granted(ctx, id)
	if err != nil {
		return nil, err
	}
	cid, err := uuid.Parse(id.ConsumerID)
	if err != nil {
		return nil, fmt.Errorf("gateway: consumer id: %w", err)
	}
	gvid, err := uuid.Parse(id.GrantVersionID)
	if err != nil {
		return nil, fmt.Errorf("gateway: grant version id: %w", err)
	}
	out := &api.Capabilities{ConsumerID: cid, GrantVersionID: gvid, Models: make([]api.GrantModelLimit, 0, len(ms)),
		OnContextOverflow: api.ContextOverflowRefuse, Quotas: []api.QuotaStatus{}}
	if id.Limits.OnContextOverflow == string(api.ContextOverflowTruncate) {
		out.OnContextOverflow = api.ContextOverflowTruncate
	}
	for _, m := range ms {
		ctxLimit, outLimit := cfg.defaultContext, cfg.maxTokensCeiling
		if c := m.limit.ContextCap; c != nil && *c > 0 {
			ctxLimit = *c
		}
		if c := m.limit.MaxOutputTokens; c != nil && *c > 0 {
			outLimit = *c
		}
		out.Models = append(out.Models, api.GrantModelLimit{Model: m.name, ContextLimit: clampInt(ctxLimit), MaxOutputTokens: clampInt(outLimit)})
	}
	conc, err := g.cfg.Settings.Int(ctx, scheduler.KeyGrantConcurrency, "")
	if err != nil {
		return nil, fmt.Errorf("gateway: read settings: %w", err)
	}
	if mc := id.Limits.MaxConcurrency; mc != nil {
		conc = *mc
	}
	out.Concurrency = clampInt(max(conc, 1))
	if id.Limits.AllowedProperties != nil {
		props := []string{}
		if err := json.Unmarshal([]byte(*id.Limits.AllowedProperties), &props); err != nil || props == nil {
			return nil, fmt.Errorf("gateway: allowed properties of grant version %s are not a JSON array", id.GrantVersionID)
		}
		out.AllowedRequestProperties = props
	}
	if id.KeyID != "" {
		kid, err := uuid.Parse(id.KeyID)
		if err != nil {
			return nil, fmt.Errorf("gateway: access key id: %w", err)
		}
		out.AccessKeyID = api.NewOptNilUUID(kid)
	} else {
		out.AccessKeyID.SetToNull()
	}
	status, err := g.cfg.Limits.StatusOf(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, q := range status.Quotas {
		out.Quotas = append(out.Quotas, quotaStatus(q))
	}
	if r := status.Rate; r != nil {
		out.RateLimit = api.NewOptNilRateLimit(api.RateLimit{Requests: clampInt(r.Capacity), WindowSeconds: clampInt(int64(r.Window / time.Second))})
	} else {
		out.RateLimit.SetToNull()
	}
	if status.ExpiresAt != nil {
		out.ExpiresAt = api.NewOptNilDateTime(status.ExpiresAt.UTC())
	} else {
		out.ExpiresAt.SetToNull()
	}
	out.Benchmark.SetToNull() // reserved (req_001 D-45)
	return out, nil
}

func quotaStatus(q limits.QuotaStatus) api.QuotaStatus {
	scope := api.QuotaStatusScopeGrant
	if q.Scope == limits.ScopeAccessKey {
		scope = api.QuotaStatusScopeAccessKey
	}
	opt := func(v *int64) api.OptInt64 {
		if v == nil {
			return api.OptInt64{}
		}
		return api.NewOptInt64(*v)
	}
	return api.QuotaStatus{Scope: scope, WindowSeconds: clampInt(int64(q.Window / time.Second)), ResetsAt: q.ResetsAt.UTC(),
		Tokens: opt(q.Tokens), UsedTokens: opt(q.UsedTokens), RemainingTokens: opt(q.RemainingTokens),
		Requests: opt(q.Requests), UsedRequests: opt(q.UsedRequests), RemainingRequests: opt(q.RemainingReqs)}
}

// clampInt narrows an int64 limit to the int of the generated schema types.
func clampInt(v int64) int {
	return int(min(v, int64(^uint(0)>>1)))
}
