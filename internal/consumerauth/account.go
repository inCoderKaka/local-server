package consumerauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/go-faster/jx"
	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// The methods below are the consumer's own account routes (/v1/me). Each takes the Caller and
// reads or writes only that consumer's rows, never an id from the request. The consumer page
// calls them with the session's Caller; the consumer API calls them with the Caller of an access
// key, because the spec lets these routes take a key or a session (req_001 D-39, D-148).

func consumerOut(a identity.ConsumerAccount) (*api.Consumer, error) {
	id, err := uuid.Parse(a.ID)
	if err != nil {
		return nil, err
	}
	out := &api.Consumer{ID: id, Kind: api.ConsumerKind(a.Kind), Status: api.ConsumerStatus(a.Status),
		DisplayName: optNil(a.DisplayName), Username: optNil(a.Username), Email: optNil(a.Email),
		HasLogin: a.HasLogin, BillingMode: api.NewOptBillingMode(api.BillingMode(a.BillingMode)), CreatedAt: a.CreatedAt}
	if a.Status == identity.StatusDeleted {
		out.DeletedAt = api.NewOptNilDateTime(a.StatusChangedAt)
	} else {
		out.DeletedAt.SetToNull()
	}
	return out, nil
}

func optNil(s *string) api.OptNilString {
	if s == nil {
		var o api.OptNilString
		o.SetToNull()
		return o
	}
	return api.NewOptNilString(*s)
}

// Me implements GET /v1/me: the caller's consumer.
func (s *Service) Me(ctx context.Context, c Caller) (*api.Consumer, error) {
	a, err := s.cfg.Identity.Consumers.GetAccount(ctx, s.cfg.DB.Reader(), c.ConsumerID)
	if isNotFound(err) {
		return nil, unauthenticated()
	}
	if err != nil {
		return nil, err
	}
	return consumerOut(a)
}

// Limits implements GET /v1/me/limits: the consumer's current grant version, before any key
// narrows it. A consumer the host has not given a grant yet has none, which is a 404 and not an
// empty set of limits, because "no grant" and "a grant that allows nothing" are different.
func (s *Service) Limits(ctx context.Context, c Caller) (*api.GrantVersion, error) {
	g, err := s.cfg.Grants.Grants.GetByConsumer(ctx, s.cfg.DB.Reader(), c.ConsumerID)
	if errors.Is(err, grants.ErrNotFound) || (err == nil && g.CurrentVersionID == nil) {
		return nil, notFoundErr("The host has not given this account any limits yet.")
	}
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(c.ConsumerID)
	if err != nil {
		return nil, err
	}
	return s.cfg.Limits(ctx, id)
}

// AccessKeys implements GET /v1/me/access-keys: the consumer's keys by prefix, never a secret
// or a hash.
func (s *Service) AccessKeys(ctx context.Context, c Caller) (*api.AccessKeyList, error) {
	id, err := uuid.Parse(c.ConsumerID)
	if err != nil {
		return nil, err
	}
	return s.cfg.Keys(ctx, id)
}

// Settings implements GET /v1/me/settings: the settings the consumer owns, one value per
// consumer, with their defaults.
func (s *Service) Settings(ctx context.Context, c Caller) (*api.SettingList, error) {
	out := &api.SettingList{Data: []api.Setting{}}
	for _, d := range s.cfg.Settings.Definitions() {
		if d.Owner != settings.Consumer || !d.Scoped {
			continue
		}
		v, err := s.cfg.Settings.Get(ctx, d.Key, c.ConsumerID)
		if err != nil {
			return nil, err
		}
		cur, err := jsonValue(d.Kind, v)
		if err != nil {
			return nil, fmt.Errorf("consumerauth: setting %s: %w", d.Key, err)
		}
		def, err := jsonValue(d.Kind, d.Default)
		if err != nil {
			return nil, fmt.Errorf("consumerauth: setting %s default: %w", d.Key, err)
		}
		st := api.Setting{Key: d.Key, Owner: api.SettingOwner(d.Owner), Value: cur, DefaultValue: def}
		if d.Description != "" {
			st.Description = api.NewOptString(d.Description)
		}
		out.Data = append(out.Data, st)
	}
	return out, nil
}

// UpdateSettings implements PATCH /v1/me/settings. The request is checked whole before anything
// is written: a key that does not exist is 400, one the consumer does not own (a host-owned
// setting) is 403, a value of the wrong JSON type is 400. A null value returns the setting to its
// default. The scope is always the caller's own consumer id (req_001 D-54). A value that the
// owning module refuses when it is written (a validator) is 400; writes made before it stay, since
// the store writes scoped settings one at a time.
func (s *Service) UpdateSettings(ctx context.Context, c Caller, req *api.SettingsUpdate) (*api.SettingList, error) {
	if len(req.Values) == 0 {
		return nil, badRequest("values", "At least one setting is needed.")
	}
	type change struct {
		key, value string
		reset      bool
	}
	keys := make([]string, 0, len(req.Values))
	for k := range req.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	plan := make([]change, 0, len(keys))
	for _, k := range keys {
		d, ok := s.cfg.Settings.Definition(k)
		if !ok {
			param := ""
			if safeName(k) {
				param = k
			}
			return nil, newErr(http.StatusBadRequest, typeInvalidRequest, "unknown_setting", param, "No setting has this key.")
		}
		if d.Owner != settings.Consumer {
			return nil, forbidden(d.Key, "Setting "+d.Key+" is not owned by you, so you cannot change it.")
		}
		if !d.Scoped {
			return nil, badRequest(d.Key, "Setting "+d.Key+" cannot be set per consumer.")
		}
		raw := req.Values[k]
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			plan = append(plan, change{key: k, reset: true})
			continue
		}
		text, err := textValue(d.Kind, raw)
		if err != nil {
			return nil, badRequest(d.Key, "The value of setting "+d.Key+" must be a JSON "+kindWord(d.Kind)+".")
		}
		plan = append(plan, change{key: k, value: text})
	}
	for _, ch := range plan {
		var err error
		if ch.reset {
			err = s.cfg.Settings.Reset(ctx, settings.Consumer, ch.key, c.ConsumerID)
		} else {
			err = s.cfg.Settings.Set(ctx, settings.Consumer, ch.key, c.ConsumerID, ch.value)
		}
		switch {
		case errors.Is(err, settings.ErrNotOwner):
			return nil, forbidden(ch.key, "Setting "+ch.key+" is not owned by you, so you cannot change it.")
		case errors.Is(err, settings.ErrRejected):
			return nil, badRequest(ch.key, "The host refuses this value for setting "+ch.key+".")
		case errors.Is(err, settings.ErrInvalidValue):
			return nil, badRequest(ch.key, "The value of setting "+ch.key+" is not valid.")
		case err != nil:
			return nil, err
		}
	}
	return s.Settings(ctx, c)
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

// SetPassword implements PUT /v1/me/password: it changes the logged-in consumer's password. The
// current password is required and checked, and the check counts toward the login lockout like a
// failed login (req_001 D-54). Session authentication only: a call made with an access key is
// refused. On success the consumer's other sessions end and the caller's own stays.
//
// A consumer with no password cannot set one here: the request carries no login name, so a
// passkey-only consumer gets the 409 below (see docs/tracks/platform/phase_1/phase-2-parking.md).
func (s *Service) SetPassword(ctx context.Context, c Caller, current *string, newPassword string) error {
	if c.SessionID == "" {
		return unauthenticated()
	}
	creds, err := s.cfg.Identity.Credentials.ListByPrincipal(ctx, s.cfg.DB.Reader(), c.PrincipalID)
	if err != nil {
		return err
	}
	var cred identity.Credential
	found := false
	for _, k := range creds {
		if k.Kind == identity.CredentialPassword && k.RevokedAt == nil {
			cred, found = k, true
			break
		}
	}
	if !found {
		return conflict("This account has no password login to change.")
	}
	if err := s.password.checkPolicy(ctx, newPassword, "new_password"); err != nil {
		return err
	}
	if current == nil || *current == "" {
		return badRequest("current_password", "The current password is required.")
	}
	if err := s.password.CheckCurrent(ctx, cred, *current, remoteOf(ctx)); err != nil {
		if errors.Is(err, ErrInvalidLogin) {
			return badRequest("current_password", "The current password is wrong.")
		}
		return err
	}
	hash, err := s.password.hasher.Hash(ctx, newPassword)
	if err != nil {
		return err
	}
	return s.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
		if err := s.cfg.Identity.Credentials.UpdateSecret(tx, cred.ID, hash); err != nil {
			return err
		}
		return s.revokeOthers(tx, c.PrincipalID, c.SessionID, s.now())
	})
}
