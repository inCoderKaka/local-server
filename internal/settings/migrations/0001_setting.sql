-- settings module, migration 1: stored overrides of runtime settings (D-38, D-54, D-70).
-- A setting's type, default, owner and description live in code, where the owning module
-- registers them; a row exists only once the owner has changed the value.

CREATE TABLE hst_setting (
  key        TEXT NOT NULL,
  scope      TEXT NOT NULL DEFAULT '',
  value      TEXT NOT NULL,
  updated_by TEXT NOT NULL CHECK (updated_by IN ('platform_admin', 'host', 'consumer')),
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (key, scope)
) STRICT;
--@table hst_setting classification=internal billable=false | One runtime setting value that its owner changed from the default; the rest use the default registered in code.
--@column hst_setting.key classification=internal billable=false | Dotted setting name, for example retention.interval.
--@column hst_setting.scope classification=internal billable=false | What the value applies to, for example a consumer id for a consumer-owned setting; empty for a setting with one value.
--@column hst_setting.value classification=internal billable=false | The value as text; its type is the one registered for the key.
--@column hst_setting.updated_by classification=internal billable=false | Party that set it: platform_admin, host or consumer.
--@column hst_setting.updated_at classification=internal billable=false | When it was set, Unix milliseconds UTC.
