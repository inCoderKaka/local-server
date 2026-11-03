package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/go-faster/jx"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// redacted replaces the audited value of a setting whose key names a secret.
const redacted = "[redacted]"

// AdminGetSettings implements GET /admin/v1/settings: the settings the host owns, by key.
func (a *Admin) AdminGetSettings(ctx context.Context) (*api.SettingList, error) {
	return a.settingList(ctx)
}

func (a *Admin) settingList(ctx context.Context) (*api.SettingList, error) {
	out := &api.SettingList{Data: []api.Setting{}}
	for _, d := range a.cfg.Settings.Definitions() {
		if d.Owner != settings.Host || d.Scoped {
			continue
		}
		v, err := a.cfg.Settings.Get(ctx, d.Key, "")
		if err != nil {
			return nil, err
		}
		cur, err := jsonValue(d.Kind, v)
		if err != nil {
			return nil, fmt.Errorf("admin: setting %s: %w", d.Key, err)
		}
		def, err := jsonValue(d.Kind, d.Default)
		if err != nil {
			return nil, fmt.Errorf("admin: setting %s default: %w", d.Key, err)
		}
		s := api.Setting{Key: d.Key, Owner: api.SettingOwner(d.Owner), Value: cur, DefaultValue: def}
		if d.Description != "" {
			s.Description = api.NewOptString(d.Description)
		}
		out.Data = append(out.Data, s)
	}
	return out, nil
}

// jsonValue is the JSON form of a stored text value: a number for an int, true or false for a
// bool, a string for a duration or a string.
func jsonValue(k settings.Kind, v string) (jx.Raw, error) {
	switch k {
	case settings.Int:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, err
		}
		return jx.Raw(strconv.FormatInt(n, 10)), nil
	case settings.Bool:
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, err
		}
		return jx.Raw(strconv.FormatBool(b)), nil
	}
	b, err := json.Marshal(v)
	return jx.Raw(b), err
}

// textValue converts a JSON value to the text form the settings store takes. A value of another
// JSON type than the setting's is an error.
func textValue(k settings.Kind, raw jx.Raw) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", err
	}
	if dec.More() {
		return "", errors.New("trailing data")
	}
	switch x := v.(type) {
	case json.Number:
		if k == settings.Int {
			n, err := strconv.ParseInt(x.String(), 10, 64)
			if err != nil {
				return "", err
			}
			return strconv.FormatInt(n, 10), nil
		}
	case bool:
		if k == settings.Bool {
			return strconv.FormatBool(x), nil
		}
	case string:
		if k == settings.String || k == settings.Duration {
			return x, nil
		}
	}
	return "", errors.New("wrong JSON type")
}

// AdminUpdateSettings implements PATCH /admin/v1/settings. The request is checked whole before
// anything is written: a key that does not exist is 400, one the host does not own is 403, a value
// of the wrong JSON type or one the owning module refuses is 400; any of them applies nothing.
// Every effective change is written with its setting.changed audit row (actor, key, old and new
// value) in one unit of work (req_001 D-07, D-54).
func (a *Admin) AdminUpdateSettings(ctx context.Context, req *api.SettingsUpdate) (*api.SettingList, error) {
	if len(req.Values) == 0 {
		return nil, badRequest("values", "At least one setting is needed.")
	}
	keys := make([]string, 0, len(req.Values))
	for k := range req.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	updates := make([]settings.Update, 0, len(keys))
	for _, k := range keys {
		d, ok := a.cfg.Settings.Definition(k)
		if !ok {
			param := ""
			if safeName(k) {
				param = k
			}
			return nil, newErr(http.StatusBadRequest, typeInvalidRequest, "unknown_setting", param, "No setting has this key.")
		}
		if d.Owner != settings.Host {
			return nil, notOwned(d.Key)
		}
		if d.Scoped {
			return nil, badRequest(d.Key, "Setting "+d.Key+" has one value per scope and cannot be set here.")
		}
		raw := req.Values[k]
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			updates = append(updates, settings.Update{Key: k, Reset: true})
			continue
		}
		text, err := textValue(d.Kind, raw)
		if err != nil {
			return nil, badRequest(d.Key, "The value of setting "+d.Key+" must be a JSON "+kindWord(d.Kind)+".")
		}
		updates = append(updates, settings.Update{Key: k, Value: text})
	}
	actor := PrincipalIDOf(ctx)
	_, err := a.cfg.Settings.Apply(ctx, settings.Host, updates, func(tx *storage.Tx, changes []settings.Change) error {
		for _, c := range changes {
			oldV, newV := auditValue(c.Key, c.Old), auditValue(c.Key, c.New)
			ev := audit.Event{Action: ActionSettingChanged, ActorID: actor,
				Detail: map[string]string{"key": c.Key, "old_value": oldV, "new_value": newV}}
			if err := a.record(tx, ev); err != nil {
				return err
			}
		}
		return nil
	})
	var ue *settings.UpdateError
	if errors.As(err, &ue) {
		switch {
		case errors.Is(err, settings.ErrNotOwner):
			return nil, notOwned(ue.Key)
		case errors.Is(err, settings.ErrRejected):
			return nil, badRequest(ue.Key, "The host refuses this value for setting "+ue.Key+".")
		default:
			return nil, badRequest(ue.Key, "The value of setting "+ue.Key+" is not valid.")
		}
	}
	if err != nil {
		return nil, err
	}
	return a.settingList(ctx)
}

func notOwned(key string) *apiError {
	return forbidden("Setting " + key + " is not owned by the host, so the host cannot change it.")
}

func kindWord(k settings.Kind) string {
	switch k {
	case settings.Int:
		return "whole number"
	case settings.Bool:
		return "boolean (true or false)"
	case settings.Duration:
		return "string such as 90m or 24h"
	}
	return "string"
}

// auditValue is a setting value as the audit trail keeps it: never a secret (req_001 D-31, D-40).
// A value that is a URL with a password in it (the Ollama address may carry one) keeps its
// shape and loses the password.
func auditValue(key, v string) string {
	if secretKey(key) {
		return redacted
	}
	if strings.Contains(v, "://") {
		if u, err := url.Parse(v); err == nil && u.User != nil {
			return u.Redacted()
		}
	}
	return v
}

// secretKey reports whether a setting key names a secret, so its value stays out of the audit
// trail (req_001 D-31, D-40). No setting does today.
func secretKey(key string) bool {
	k := strings.ToLower(key)
	for _, w := range []string{"secret", "token", "private", "apikey", "api_key"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}
