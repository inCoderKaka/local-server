// Package settings is the settings module (docs/modules/settings.md): the settings owner
// model and the retention registry (D-38, D-54, D-40).
//
// # Settings owner model
//
// Every time, retention, TTL, retry and threshold value is a setting. The module that
// needs one registers a Def (key, type, default, owner, description) with Store.Register;
// the definition lives in code and only values changed by the owner are stored, in
// hst_setting. Store.Get reads the stored value or the default. Store.Set succeeds only
// when the calling party is the setting's owner (platform admin, host or consumer); there
// is no bounds hierarchy, so nothing else restricts the value (D-54) beyond it parsing as
// the registered type.
//
// A Def with Scoped set has one value per scope (a consumer id for a consumer-owned
// setting); one without has a single value and an empty scope.
//
// # Retention registry
//
// Store.RegisterRetention adds a Rule: a table, its time column (Unix milliseconds UTC),
// the key of a duration setting holding how long rows are kept, and an optional extra
// condition. Store.PruneOnce deletes the expired rows of every rule through
// storage.DB.Write, reading each retention at that moment, so a changed setting applies
// on the next run. Store.Run calls PruneOnce on a ticker whose interval is itself the
// setting retention.interval. A retention of zero or less keeps rows forever.
//
// Setting values are configuration, never prompts or responses (D-31, D-40).
package settings
