package grants

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// defaultEngineName is the engine adapter named in a HostModel when the engine does not name
// itself (an engine may have a `Name() string` method).
const defaultEngineName = "ollama"

// errPreloadChanged is returned from inside UpdateModel's unit of work when scheduler.preload_models
// is no longer the list the new one was built from. The unit rolls back and UpdateModel tries again
// from a fresh read.
var errPreloadChanged = errors.New("admin grants: preload list changed")

// HostModel is one engine model and how the host shares it.
type HostModel struct {
	ID     string
	Name   string
	Engine string
	// Allowed is whether a new grant may include the model.
	Allowed bool
	// Preload is whether the model is in scheduler.preload_models.
	Preload bool
	// KeepAliveSeconds nil means the host's default keep-alive. It is stored and shown; the
	// scheduler still uses its global scheduler.keep_alive.
	KeepAliveSeconds *int64
	Loaded           bool
	// SizeBytes is the model's memory use while loaded, otherwise its size on disk; nil if unknown.
	SizeBytes *int64
}

// ModelChange is a change to a model; a field left out stays as it is.
type ModelChange struct {
	Allowed          *bool
	Preload          *bool
	KeepAliveSeconds consumers.Field[int64]
}

func (s *Service) engineName() string {
	if n, ok := s.cfg.Engine.(interface{ Name() string }); ok && n.Name() != "" {
		return n.Name()
	}
	return defaultEngineName
}

// syncModels asks the engine for its models and stores the ones the host has not seen.
func (s *Service) syncModels(ctx context.Context) ([]engine.Model, error) {
	installed, err := s.cfg.Engine.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	return installed, s.storeModels(ctx, installed)
}

// storeModels adds a grt_model row for each installed model that has none. A new model is
// allowed only when admin.model_default_allowed is on (the host opts each model in).
func (s *Service) storeModels(ctx context.Context, installed []engine.Model) error {
	allowed, err := s.cfg.Settings.Bool(ctx, KeyModelDefaultAllowed, "")
	if err != nil {
		return err
	}
	rd := s.cfg.DB.Reader()
	var missing []string
	for _, m := range installed {
		if _, err := s.cfg.Grants.Models.GetByName(ctx, rd, m.Name); isNotFound(err) {
			missing = append(missing, m.Name)
		} else if err != nil {
			return err
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return s.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
		for _, name := range missing {
			if _, err := s.cfg.Grants.Models.GetByNameTx(tx, name); err == nil {
				continue // stored by a request that ran at the same time
			} else if !isNotFound(err) {
				return err
			}
			if _, err := s.cfg.Grants.Models.CreateWithAllowed(tx, name, allowed); err != nil {
				return err
			}
		}
		return nil
	})
}

// parsePreload reads a scheduler.preload_models value as the scheduler does: a comma-separated
// list, blanks dropped.
func parsePreload(raw string) []string {
	var out []string
	for _, m := range strings.Split(raw, ",") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

// preloadSet reads scheduler.preload_models as the scheduler does: a comma-separated list,
// blanks dropped.
func (s *Service) preloadList(ctx context.Context) ([]string, error) {
	raw, err := s.cfg.Settings.String(ctx, scheduler.KeyPreloadModels, "")
	if err != nil {
		return nil, err
	}
	return parsePreload(raw), nil
}

// ListModels returns the engine's models with the host's configuration, by name, adding the ones
// the host has not seen. When the engine cannot be asked, it returns the stored models, none of
// them loaded.
func (s *Service) ListModels(ctx context.Context) ([]HostModel, error) {
	installed, ierr := s.cfg.Engine.ListModels(ctx)
	if ierr != nil {
		s.log.WarnContext(ctx, "admin grants: could not list the engine's models", "err", ierr)
	} else if err := s.storeModels(ctx, installed); err != nil {
		return nil, err
	}
	rows, err := s.cfg.Grants.Models.List(ctx, s.cfg.DB.Reader(), false)
	if err != nil {
		return nil, err
	}
	size := map[string]int64{}
	for _, m := range installed {
		size[m.Name] = m.SizeBytes
	}
	loaded := map[string]engine.LoadedModel{}
	if lm, lerr := s.cfg.Engine.Loaded(ctx); lerr != nil {
		s.log.WarnContext(ctx, "admin grants: could not list the loaded models", "err", lerr)
	} else {
		for _, m := range lm {
			loaded[m.Name] = m
		}
	}
	pre, err := s.preloadList(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]HostModel, 0, len(rows))
	for _, r := range rows {
		sz, installedHere := size[r.Name]
		if ierr == nil && !installedHere {
			continue // the engine no longer has it
		}
		out = append(out, s.hostModel(r, sz, installedHere, loaded, pre))
	}
	return out, nil
}

func (s *Service) hostModel(r grants.Model, installedSize int64, installed bool, loaded map[string]engine.LoadedModel, pre []string) HostModel {
	m := HostModel{ID: r.ID, Name: r.Name, Engine: s.engineName(), Allowed: r.Allowed, Preload: slices.Contains(pre, r.Name), KeepAliveSeconds: r.KeepAliveSeconds}
	if l, ok := loaded[r.Name]; ok {
		m.Loaded = true
		if l.SizeBytes > 0 {
			v := l.SizeBytes
			m.SizeBytes = &v
		}
	}
	if m.SizeBytes == nil && installed && installedSize > 0 {
		v := installedSize
		m.SizeBytes = &v
	}
	return m
}

// UpdateModel changes a model's configuration and audits it (model.updated: the model's id and
// name and the names of the fields that changed). Preload rewrites scheduler.preload_models in
// the same unit of work; the scheduler applies it when it changes (req_001 D-22). When the list
// changes under it, it reads again and retries, so two simultaneous edits both land. A model that
// does not exist or is retired is not found.
func (s *Service) UpdateModel(ctx context.Context, actor, id string, ch ModelChange) (HostModel, error) {
	if ch.KeepAliveSeconds.Set && !ch.KeepAliveSeconds.Null && ch.KeepAliveSeconds.Value < 0 {
		return HostModel{}, invalid("keep_alive_seconds", "The value is below the lowest allowed.")
	}
	for {
		cur, err := s.cfg.Grants.Models.Get(ctx, s.cfg.DB.Reader(), id)
		if isNotFound(err) || (err == nil && cur.RetiredAt != nil) {
			return HostModel{}, errNoModel
		}
		if err != nil {
			return HostModel{}, err
		}
		pre, err := s.preloadList(ctx)
		if err != nil {
			return HostModel{}, err
		}
		var changed []string
		allowed, keep := cur.Allowed, cur.KeepAliveSeconds
		if ch.Allowed != nil && *ch.Allowed != allowed {
			allowed = *ch.Allowed
			changed = append(changed, "allowed")
		}
		if ch.KeepAliveSeconds.Set {
			var next *int64
			if !ch.KeepAliveSeconds.Null {
				v := ch.KeepAliveSeconds.Value
				next = &v
			}
			if (next == nil) != (keep == nil) || (next != nil && *next != *keep) {
				keep = next
				changed = append(changed, "keep_alive_seconds")
			}
		}
		var updates []settings.Update
		if ch.Preload != nil && *ch.Preload != slices.Contains(pre, cur.Name) {
			next := slices.Clone(pre)
			if *ch.Preload {
				if strings.Contains(cur.Name, ",") {
					return HostModel{}, invalid("preload", "A model whose name has a comma cannot be preloaded.")
				}
				next = append(next, cur.Name)
			} else {
				next = slices.DeleteFunc(next, func(n string) bool { return n == cur.Name })
			}
			updates = append(updates, settings.Update{Key: scheduler.KeyPreloadModels, Value: strings.Join(next, ",")})
			changed = append(changed, "preload")
		}
		if len(changed) == 0 {
			return s.oneModel(ctx, id)
		}
		_, err = s.cfg.Settings.Apply(ctx, settings.Host, updates, func(tx *storage.Tx, changes []settings.Change) error {
			// What the request leaves out keeps its value as of this transaction, not the earlier read,
			// so an edit does not undo a concurrent one.
			head, err := s.cfg.Grants.Models.GetTx(tx, id)
			if err != nil {
				return err
			}
			if head.RetiredAt != nil {
				return grants.ErrNotFound
			}
			allowed, keep := head.Allowed, head.KeepAliveSeconds
			if ch.Allowed != nil {
				allowed = *ch.Allowed
			}
			if ch.KeepAliveSeconds.Set {
				keep = nil
				if !ch.KeepAliveSeconds.Null {
					v := ch.KeepAliveSeconds.Value
					keep = &v
				}
			}
			var fields []string
			if allowed != head.Allowed {
				fields = append(fields, "allowed")
			}
			if (keep == nil) != (head.KeepAliveSeconds == nil) || (keep != nil && *keep != *head.KeepAliveSeconds) {
				fields = append(fields, "keep_alive_seconds")
			}
			if len(fields) > 0 {
				if err := s.cfg.Grants.Models.SetConfig(tx, id, allowed, keep); err != nil {
					return err
				}
			}
			if len(updates) > 0 {
				// The new list was built from the list read before the transaction. If the stored list
				// is not that one, someone else changed it; the whole unit rolls back and the caller reads again.
				for _, c := range changes {
					if c.Key != scheduler.KeyPreloadModels {
						continue
					}
					if !slices.Equal(parsePreload(c.Old), pre) {
						return errPreloadChanged
					}
					fields = append(fields, "preload")
				}
			}
			if len(fields) == 0 {
				return nil
			}
			return s.record(tx, audit.Event{Action: ActionModelUpdated, ActorID: actor,
				Detail: map[string]any{"model_id": id, "model": cur.Name, "changed": fields}})
		})
		if errors.Is(err, errPreloadChanged) {
			if ctx.Err() != nil {
				return HostModel{}, ctx.Err()
			}
			continue // the list changed under this pass: read it again
		}
		if isNotFound(err) {
			return HostModel{}, errNoModel
		}
		if err != nil {
			return HostModel{}, wrap("update model", err)
		}
		break
	}
	return s.oneModel(ctx, id)
}

// oneModel returns the shown form of one stored model.
func (s *Service) oneModel(ctx context.Context, id string) (HostModel, error) {
	r, err := s.cfg.Grants.Models.Get(ctx, s.cfg.DB.Reader(), id)
	if isNotFound(err) {
		return HostModel{}, errNoModel
	}
	if err != nil {
		return HostModel{}, err
	}
	pre, err := s.preloadList(ctx)
	if err != nil {
		return HostModel{}, err
	}
	loaded := map[string]engine.LoadedModel{}
	if lm, lerr := s.cfg.Engine.Loaded(ctx); lerr == nil {
		for _, m := range lm {
			loaded[m.Name] = m
		}
	}
	var size int64
	installed := false
	if im, ierr := s.cfg.Engine.ListModels(ctx); ierr == nil {
		for _, m := range im {
			if m.Name == r.Name {
				size, installed = m.SizeBytes, true
			}
		}
	}
	return s.hostModel(r, size, installed, loaded, pre), nil
}
