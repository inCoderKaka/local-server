-- grants module, migration 1: grants, their immutable versions, the models a version allows and
-- the policy rules (D-25, D-44, D-48). These tables reference idn_consumer and idn_principal, so
-- the identity module's migrations must run first: register identity before grants with storage.
-- A grant version is never changed or deleted (use records and receipts refer to it forever), so
-- triggers refuse UPDATE and DELETE on grt_version and on grt_version_model.

CREATE TABLE grt_model (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL,
  retired_at INTEGER
) STRICT;
--@table grt_model classification=internal billable=false | A model name a grant can allow, as the engine names it.
--@column grt_model.id classification=internal billable=false | Model id, UUIDv7.
--@column grt_model.name classification=internal billable=false | Model name in the engine, unique.
--@column grt_model.created_at classification=internal billable=false | When the model was added, Unix milliseconds UTC.
--@column grt_model.retired_at classification=internal billable=false | When it was retired (soft delete), Unix milliseconds UTC; empty while in use.

CREATE TABLE grt_grant (
  id                 TEXT PRIMARY KEY,
  consumer_id        TEXT NOT NULL UNIQUE REFERENCES idn_consumer(id) ON DELETE RESTRICT,
  current_version_id TEXT REFERENCES grt_version(id) ON DELETE RESTRICT,
  created_at         INTEGER NOT NULL,
  updated_at         INTEGER NOT NULL
) STRICT;
--@table grt_grant classification=internal billable=true | The grant of one consumer: what it may use. The terms live in immutable versions.
--@column grt_grant.id classification=internal billable=false | Grant id, UUIDv7.
--@column grt_grant.consumer_id classification=internal billable=false | Consumer the grant belongs to, one grant per consumer.
--@column grt_grant.current_version_id classification=internal billable=false | The live version; empty until the first version is made.
--@column grt_grant.created_at classification=internal billable=false | When the grant was created, Unix milliseconds UTC.
--@column grt_grant.updated_at classification=internal billable=false | When the live version last changed, Unix milliseconds UTC.

CREATE TABLE grt_version (
  id                  TEXT PRIMARY KEY,
  grant_id            TEXT NOT NULL REFERENCES grt_grant(id) ON DELETE RESTRICT,
  version_no          INTEGER NOT NULL CHECK (version_no >= 1),
  quota_tokens_per_day   INTEGER CHECK (quota_tokens_per_day IS NULL OR quota_tokens_per_day >= 0),
  quota_requests_per_day INTEGER CHECK (quota_requests_per_day IS NULL OR quota_requests_per_day >= 0),
  max_concurrency     INTEGER CHECK (max_concurrency IS NULL OR max_concurrency >= 1),
  expires_at          INTEGER,
  allowed_properties  TEXT,
  on_context_overflow TEXT NOT NULL DEFAULT 'refuse' CHECK (on_context_overflow IN ('refuse', 'truncate')),
  reason              TEXT,
  created_by          TEXT REFERENCES idn_principal(id) ON DELETE RESTRICT,
  created_at          INTEGER NOT NULL,
  UNIQUE (grant_id, version_no)
) STRICT;
--@table grt_version classification=internal billable=true | One immutable version of a grant: quotas, concurrency, expiry, allowed request properties and the context-overflow behaviour. A change makes a new version.
--@column grt_version.id classification=internal billable=false | Version id, UUIDv7.
--@column grt_version.grant_id classification=internal billable=false | Grant the version belongs to.
--@column grt_version.version_no classification=internal billable=false | 1, 2, 3 ... within the grant.
--@column grt_version.quota_tokens_per_day classification=internal billable=true | Most tokens per day; empty means no limit.
--@column grt_version.quota_requests_per_day classification=internal billable=true | Most requests per day; empty means no limit.
--@column grt_version.max_concurrency classification=internal billable=false | Most requests at the same time; empty means the host default.
--@column grt_version.expires_at classification=internal billable=false | When the grant stops working, Unix milliseconds UTC; empty means never.
--@column grt_version.allowed_properties classification=internal billable=false | JSON list of the request properties or tags the grant allows; empty means any.
--@column grt_version.on_context_overflow classification=internal billable=false | refuse (default) or truncate: what happens when a prompt is longer than the context cap (D-48).
--@column grt_version.reason classification=internal billable=false | Why the version was made, for example the approved resource request.
--@column grt_version.created_by classification=internal billable=false | Principal that made the version; empty if the system did.
--@column grt_version.created_at classification=internal billable=false | When the version was made, Unix milliseconds UTC.

CREATE TRIGGER grt_version_no_update BEFORE UPDATE ON grt_version
BEGIN SELECT RAISE(ABORT, 'grt_version is immutable'); END;
CREATE TRIGGER grt_version_no_delete BEFORE DELETE ON grt_version
BEGIN SELECT RAISE(ABORT, 'grt_version is immutable'); END;

CREATE TABLE grt_version_model (
  version_id        TEXT NOT NULL REFERENCES grt_version(id) ON DELETE CASCADE,
  model_id          TEXT NOT NULL REFERENCES grt_model(id) ON DELETE RESTRICT,
  context_cap       INTEGER CHECK (context_cap IS NULL OR context_cap >= 1),
  max_output_tokens INTEGER CHECK (max_output_tokens IS NULL OR max_output_tokens >= 1),
  PRIMARY KEY (version_id, model_id)
) STRICT;
--@table grt_version_model classification=internal billable=false | A model a grant version allows, with its per-model caps.
--@column grt_version_model.version_id classification=internal billable=false | Grant version.
--@column grt_version_model.model_id classification=internal billable=false | Allowed model.
--@column grt_version_model.context_cap classification=internal billable=false | Most context tokens for this model; empty means the host default.
--@column grt_version_model.max_output_tokens classification=internal billable=false | Most output tokens for this model; empty means the host default.

CREATE TRIGGER grt_version_model_no_update BEFORE UPDATE ON grt_version_model
BEGIN SELECT RAISE(ABORT, 'grt_version_model is immutable'); END;
CREATE TRIGGER grt_version_model_no_delete BEFORE DELETE ON grt_version_model
BEGIN SELECT RAISE(ABORT, 'grt_version_model is immutable'); END;

CREATE TABLE grt_policy_rule (
  id             TEXT PRIMARY KEY,
  grant_id       TEXT REFERENCES grt_grant(id) ON DELETE CASCADE,
  name           TEXT NOT NULL,
  position       INTEGER NOT NULL DEFAULT 0,
  format_version INTEGER NOT NULL CHECK (format_version >= 1),
  body           TEXT NOT NULL,
  enabled        INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  created_at     INTEGER NOT NULL,
  updated_at     INTEGER NOT NULL
) STRICT;
--@table grt_policy_rule classification=internal billable=false | A policy rule in the versioned declarative rule format; scoped to one grant or global.
--@column grt_policy_rule.id classification=internal billable=false | Rule id, UUIDv7.
--@column grt_policy_rule.grant_id classification=internal billable=false | Grant the rule is scoped to; empty means it applies to every grant.
--@column grt_policy_rule.name classification=internal billable=false | Short name the host gave the rule.
--@column grt_policy_rule.position classification=internal billable=false | Evaluation order, lowest first.
--@column grt_policy_rule.format_version classification=internal billable=false | Version of the rule format that body is written in.
--@column grt_policy_rule.body classification=internal billable=false | The rule as JSON: request properties, time windows, priority class, caps.
--@column grt_policy_rule.enabled classification=internal billable=false | 1 when the rule is active, 0 when switched off.
--@column grt_policy_rule.created_at classification=internal billable=false | When the rule was created, Unix milliseconds UTC.
--@column grt_policy_rule.updated_at classification=internal billable=false | When the rule last changed, Unix milliseconds UTC.
