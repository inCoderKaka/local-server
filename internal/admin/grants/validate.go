package grants

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/limits"
)

// newVersion checks the terms of a grant and returns them in their stored form, with the
// warnings to hand back. Every refusal names the request field at fault; nothing of the request
// is copied into the text (req_001 D-31).
func (s *Service) newVersion(ctx context.Context, in Limits) (grants.NewVersion, []string, error) {
	var nv grants.NewVersion
	if in.Concurrency < 1 {
		return nv, nil, invalid("limits.concurrency", "The value is below the lowest allowed.")
	}
	nv.MaxConcurrency = &in.Concurrency
	switch in.OnContextOverflow {
	case grants.OverflowRefuse, grants.OverflowTruncate:
		nv.OnContextOverflow = in.OnContextOverflow
	default:
		return nv, nil, invalid("limits.on_context_overflow", "The value must be refuse or truncate.")
	}
	if in.ExpiresAt != nil {
		if !in.ExpiresAt.After(s.now()) {
			return nv, nil, invalid("limits.expires_at", "The time must be in the future.")
		}
		t := in.ExpiresAt.UTC()
		nv.ExpiresAt = &t
	}
	if in.AllowedProperties != nil {
		for _, p := range in.AllowedProperties {
			if strings.TrimSpace(p) == "" {
				return nv, nil, invalid("limits.allowed_request_properties", "A property name must not be empty.")
			}
		}
		b, err := json.Marshal(in.AllowedProperties)
		if err != nil {
			return nv, nil, err
		}
		text := string(b)
		nv.AllowedProperties = &text
	}
	quotas, warnings, err := s.quotaEntries(ctx, in)
	if err != nil {
		return nv, nil, err
	}
	nv.Quotas = quotas
	if nv.Models, err = s.versionModels(ctx, in.Models); err != nil {
		return nv, nil, err
	}
	return nv, warnings, nil
}

// quotaEntries reads the quotas of a request: quotas when given, otherwise the deprecated quota
// expanded to entries with the host's legacy window (req_001 D-107). When both are given quotas
// wins and a warning says so.
func (s *Service) quotaEntries(ctx context.Context, in Limits) ([]grants.QuotaEntry, []string, error) {
	var warnings []string
	hasLegacy := in.Quota != nil && (in.Quota.Tokens != nil || in.Quota.Requests != nil)
	entries := slices.Clone(in.Quotas)
	switch {
	case hasLegacy && len(entries) > 0:
		warnings = append(warnings, consumers.QuotaWarning)
	case hasLegacy:
		for _, l := range []struct {
			param  string
			metric string
			limit  *int64
		}{{"limits.quota.tokens", grants.MetricTokens, in.Quota.Tokens}, {"limits.quota.requests", grants.MetricRequests, in.Quota.Requests}} {
			if l.limit == nil {
				continue
			}
			if *l.limit < 0 {
				return nil, nil, invalid(l.param, "The value is below the lowest allowed.")
			}
			w, err := limits.LegacyWindow(ctx, s.cfg.Settings)
			if err != nil {
				return nil, nil, err
			}
			entries = append(entries, grants.QuotaEntry{Metric: l.metric, Limit: *l.limit, Window: w})
		}
	}
	if p := grants.ValidateQuotas(in.Quotas); p != nil {
		return nil, nil, invalid("limits.quotas."+strconv.Itoa(p.Index)+"."+p.Field, p.Reason)
	}
	for i, q := range in.Quotas {
		if err := limits.CheckWindow(ctx, s.cfg.Settings, q.Window); err != nil {
			var we *limits.WindowError
			if errors.As(err, &we) {
				return nil, nil, invalid("limits.quotas."+strconv.Itoa(i)+".window_seconds", we.Reason)
			}
			return nil, nil, err
		}
	}
	grants.SortQuotas(entries)
	return entries, warnings, nil
}

// versionModels resolves the models of a request, by engine name, to grt_model rows. A name the
// host has not stored yet is looked for in the engine once; a model that is still unknown,
// retired or not opened for sharing is refused. Closing a model never edits a version that
// already holds it: only new grants are checked here.
func (s *Service) versionModels(ctx context.Context, in []ModelLimit) ([]grants.VersionModel, error) {
	var out []grants.VersionModel
	seen := map[string]bool{}
	synced := false
	for i, m := range in {
		param := "limits.models." + strconv.Itoa(i)
		switch {
		case strings.TrimSpace(m.Model) == "":
			return nil, invalid(param+".model", "A model name must not be empty.")
		case seen[m.Model]:
			return nil, invalid(param+".model", "The model is listed twice.")
		case m.ContextLimit < 1:
			return nil, invalid(param+".context_limit", "The value is below the lowest allowed.")
		case m.MaxOutputTokens < 1:
			return nil, invalid(param+".max_output_tokens", "The value is below the lowest allowed.")
		}
		seen[m.Model] = true
		row, err := s.cfg.Grants.Models.GetByName(ctx, s.cfg.DB.Reader(), m.Model)
		if isNotFound(err) && !synced {
			synced = true
			if _, serr := s.syncModels(ctx); serr != nil {
				s.log.WarnContext(ctx, "admin grants: could not list the engine's models", "err", serr)
			}
			row, err = s.cfg.Grants.Models.GetByName(ctx, s.cfg.DB.Reader(), m.Model)
		}
		switch {
		case isNotFound(err):
			return nil, invalid(param+".model", "The host does not know this model.")
		case err != nil:
			return nil, err
		case row.RetiredAt != nil:
			return nil, invalid(param+".model", "This model is retired.")
		case !row.Allowed:
			return nil, invalid(param+".model", "The host has not opened this model for sharing.")
		}
		cl, mo := m.ContextLimit, m.MaxOutputTokens
		out = append(out, grants.VersionModel{ModelID: row.ID, ContextCap: &cl, MaxOutputTokens: &mo})
	}
	return out, nil
}
