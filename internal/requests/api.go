package requests

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/api"
)

// This file maps the requests of this package onto the API's generated types, once, for the two
// listeners that serve them: the consumer routes (internal/consumerauth, internal/gateway) and
// the admin routes (internal/admin).

// AskIn returns the ask a consumer sent as the JSON this package stores, or nil when the request
// has none. Only the fields of the spec's ResourceRequestAsk survive; the unknown ones are named
// in the answer's warnings by internal/api/ignored.
func AskIn(ask api.OptResourceRequestAsk) (json.RawMessage, error) {
	v, ok := ask.Get()
	if !ok {
		return nil, nil
	}
	b, err := v.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// Out returns a request in the API's shape. Its comments are included, as an empty list when
// there are none, when q.Comments is not nil; a list entry has none. A final request has a null
// expires_at.
func Out(q Request) (*api.ResourceRequest, error) {
	id, err := uuid.Parse(q.ID)
	if err != nil {
		return nil, err
	}
	cid, err := uuid.Parse(q.ConsumerID)
	if err != nil {
		return nil, err
	}
	out := &api.ResourceRequest{ID: id, ConsumerID: cid, Status: api.ResourceRequestStatus(q.Status), Message: q.Message, CreatedAt: q.CreatedAt.UTC()}
	if len(q.Ask) > 0 {
		var a api.ResourceRequestAsk
		if err := a.UnmarshalJSON(q.Ask); err != nil {
			return nil, err
		}
		out.Ask = api.NewOptResourceRequestAsk(a)
	}
	if q.ExpiresAt != nil {
		out.ExpiresAt = api.NewOptNilDateTime(q.ExpiresAt.UTC())
	} else {
		out.ExpiresAt.SetToNull()
	}
	if q.DecidedAt != nil {
		out.DecidedAt = api.NewOptNilDateTime(q.DecidedAt.UTC())
	} else {
		out.DecidedAt.SetToNull()
	}
	if q.ResultingVersionID != nil {
		vid, err := uuid.Parse(*q.ResultingVersionID)
		if err != nil {
			return nil, err
		}
		out.ResultingGrantVersionID = api.NewOptNilUUID(vid)
	} else {
		out.ResultingGrantVersionID.SetToNull()
	}
	if q.Comments != nil {
		out.Comments = make([]api.Comment, 0, len(q.Comments))
		for _, c := range q.Comments {
			pid, err := uuid.Parse(c.Author.PrincipalID)
			if err != nil {
				return nil, err
			}
			ref := api.PrincipalRef{PrincipalID: pid, Kind: api.PrincipalRefKind(c.Author.Kind)}
			if c.Author.DisplayName != nil {
				ref.DisplayName = api.NewOptNilString(*c.Author.DisplayName)
			} else {
				ref.DisplayName.SetToNull()
			}
			xid, err := uuid.Parse(c.ID)
			if err != nil {
				return nil, err
			}
			out.Comments = append(out.Comments, api.Comment{ID: xid, Author: ref, Body: c.Body, CreatedAt: c.CreatedAt.UTC()})
		}
	}
	return out, nil
}

// PageOut returns a page in the API's shape.
func PageOut(p Page) (*api.ResourceRequestList, error) {
	out := &api.ResourceRequestList{Data: make([]api.ResourceRequest, 0, len(p.Items))}
	for _, q := range p.Items {
		o, err := Out(q)
		if err != nil {
			return nil, err
		}
		out.Data = append(out.Data, *o)
	}
	if p.NextCursor == "" {
		out.NextCursor.SetToNull()
	} else {
		out.NextCursor = api.NewNilNextCursor(api.NextCursor(p.NextCursor))
	}
	return out, nil
}

// QueryOut returns the list query of the API's status filter and paging parameters.
func QueryOut(status api.OptResourceRequestStatus, cursor api.OptString, limit api.OptInt) ListQuery {
	q := ListQuery{Cursor: cursor.Value}
	if s, ok := status.Get(); ok {
		q.Status = Status(s)
	}
	if v, ok := limit.Get(); ok {
		q.Limit = &v
	}
	return q
}
