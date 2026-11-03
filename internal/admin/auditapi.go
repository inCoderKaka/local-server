package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-faster/jx"
	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/identity"
)

// AdminListAuditEvents implements GET /admin/v1/audit-events: one page of the audit trail, newest
// first (req_001 D-07). The page size is the audit.list_page_size setting (req_001 D-38). The
// actor is resolved to its principal kind and display name; the target to the kind of the
// principal it was done to.
func (a *Admin) AdminListAuditEvents(ctx context.Context, params api.AdminListAuditEventsParams) (*api.AuditEventList, error) {
	f := audit.Filter{}
	if v, ok := params.From.Get(); ok {
		f.Since = v
	}
	if v, ok := params.To.Get(); ok {
		f.Until = v
	}
	if v, ok := params.Action.Get(); ok {
		f.Action = v
	}
	if v, ok := params.Cursor.Get(); ok {
		f.Cursor = v
	}
	if v, ok := params.Limit.Get(); ok {
		f.Limit = v
	}
	rd := a.cfg.DB.Reader()
	page, err := a.cfg.Audit.List(ctx, rd, f)
	if errors.Is(err, audit.ErrInvalidEvent) && f.Limit < 0 {
		return nil, badRequest("limit", "The limit must be at least 1.")
	}
	if err != nil {
		return nil, err
	}
	refs := map[string]*principalInfo{}
	out := &api.AuditEventList{Data: make([]api.AuditEvent, 0, len(page.Events))}
	for _, s := range page.Events {
		ev, err := a.auditEvent(ctx, s, refs)
		if err != nil {
			return nil, err
		}
		out.Data = append(out.Data, ev)
	}
	if page.NextCursor == "" {
		out.NextCursor.SetToNull()
	} else {
		out.NextCursor = api.NewNilNextCursor(api.NextCursor(page.NextCursor))
	}
	return out, nil
}

// principalInfo is what the audit list shows of a principal; nil in the cache means gone.
type principalInfo struct {
	kind        string
	displayName string
}

func (a *Admin) principal(ctx context.Context, cache map[string]*principalInfo, id string) (*principalInfo, error) {
	if p, ok := cache[id]; ok {
		return p, nil
	}
	rd := a.cfg.DB.Reader()
	ir := a.cfg.Identity
	p, err := ir.Principals.Get(ctx, rd, id)
	if errors.Is(err, identity.ErrNotFound) {
		cache[id] = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info := &principalInfo{kind: p.Kind}
	switch p.Kind {
	case PrincipalKind:
		au, err := ir.AdminUsers.GetByPrincipal(ctx, rd, id)
		if err != nil && !errors.Is(err, identity.ErrNotFound) {
			return nil, err
		}
		if err == nil {
			info.displayName = au.Username
			if au.DisplayName != nil && *au.DisplayName != "" {
				info.displayName = *au.DisplayName
			}
		}
	case "consumer":
		c, err := ir.Consumers.GetByPrincipal(ctx, rd, id)
		if err != nil && !errors.Is(err, identity.ErrNotFound) {
			return nil, err
		}
		if err == nil && c.DisplayName != nil {
			info.displayName = *c.DisplayName
		}
	}
	cache[id] = info
	return info, nil
}

func (a *Admin) auditEvent(ctx context.Context, s audit.Stored, cache map[string]*principalInfo) (api.AuditEvent, error) {
	id, err := uuid.Parse(s.ID)
	if err != nil {
		return api.AuditEvent{}, err
	}
	ev := api.AuditEvent{ID: id, OccurredAt: s.CreatedAt.UTC(), Action: s.Action}
	ev.Actor.SetToNull()
	ev.TargetKind.SetToNull()
	ev.TargetID.SetToNull()
	if s.ActorID != "" {
		p, err := a.principal(ctx, cache, s.ActorID)
		if err != nil {
			return api.AuditEvent{}, err
		}
		pid, perr := uuid.Parse(s.ActorID)
		if p != nil && perr == nil && knownKind(p.kind) {
			ref := api.PrincipalRef{PrincipalID: pid, Kind: api.PrincipalRefKind(p.kind)}
			if p.displayName != "" {
				ref.DisplayName = api.NewOptNilString(p.displayName)
			} else {
				ref.DisplayName.SetToNull()
			}
			ev.Actor = api.NewOptNilPrincipalRef(ref)
		}
	}
	if s.TargetID != "" {
		ev.TargetID = api.NewOptNilString(s.TargetID)
		if p, err := a.principal(ctx, cache, s.TargetID); err != nil {
			return api.AuditEvent{}, err
		} else if p != nil {
			ev.TargetKind = api.NewOptNilString(p.kind)
		}
	}
	var details map[string]json.RawMessage
	if err := json.Unmarshal(s.Detail, &details); err != nil {
		return api.AuditEvent{}, fmt.Errorf("admin: audit event %s detail: %w", s.ID, err)
	}
	ev.Details = make(api.AuditEventDetails, len(details))
	for k, v := range details {
		ev.Details[k] = jx.Raw(v)
	}
	return ev, nil
}

func knownKind(k string) bool {
	for _, v := range api.PrincipalRefKind("").AllValues() {
		if string(v) == k {
			return true
		}
	}
	return false
}
