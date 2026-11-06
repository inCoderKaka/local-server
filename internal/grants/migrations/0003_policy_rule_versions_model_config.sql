-- grants module, migration 3: policy rules are immutable versions (req_001 D-87) and a model
-- carries the host's sharing configuration (req_001 D-25, D-22).
-- grt_policy_rule stays the rule's head: its id, scope, switch and a copy of the live version, so
-- the gateway reads one row per rule. Every change to the name, order or document stores a new
-- row in grt_policy_rule_version, which is never changed or deleted. A deleted rule keeps its
-- versions: deleted_at marks it (soft delete) and it stops applying at once.
-- The rule body is the host's own rule document, never prompts or responses (D-31).

CREATE TABLE grt_policy_rule_version (
  rule_id        TEXT NOT NULL REFERENCES grt_policy_rule(id) ON DELETE RESTRICT,
  version_no     INTEGER NOT NULL CHECK (version_no >= 1),
  name           TEXT NOT NULL,
  position       INTEGER NOT NULL DEFAULT 0,
  format_version INTEGER NOT NULL CHECK (format_version >= 1),
  body           TEXT NOT NULL,
  created_by     TEXT REFERENCES idn_principal(id) ON DELETE RESTRICT,
  created_at     INTEGER NOT NULL,
  PRIMARY KEY (rule_id, version_no)
) STRICT;
--@table grt_policy_rule_version classification=internal billable=false | One immutable version of a policy rule: its name, order, format version and document. A change makes a new version.
--@column grt_policy_rule_version.rule_id classification=internal billable=false | Rule the version belongs to.
--@column grt_policy_rule_version.version_no classification=internal billable=false | 1, 2, 3 ... within the rule.
--@column grt_policy_rule_version.name classification=internal billable=false | Short name the host gave the rule in this version.
--@column grt_policy_rule_version.position classification=internal billable=false | Evaluation order in this version, lowest first.
--@column grt_policy_rule_version.format_version classification=internal billable=false | Version of the rule format that body is written in, as the policy validator read it.
--@column grt_policy_rule_version.body classification=internal billable=false | The rule document of this version, as JSON.
--@column grt_policy_rule_version.created_by classification=internal billable=false | Principal that stored the version; empty if the system did or the version came from before versions existed.
--@column grt_policy_rule_version.created_at classification=internal billable=false | When the version was stored, Unix milliseconds UTC.

CREATE TRIGGER grt_policy_rule_version_no_update BEFORE UPDATE ON grt_policy_rule_version
BEGIN SELECT RAISE(ABORT, 'grt_policy_rule_version is immutable'); END;
CREATE TRIGGER grt_policy_rule_version_no_delete BEFORE DELETE ON grt_policy_rule_version
BEGIN SELECT RAISE(ABORT, 'grt_policy_rule_version is immutable'); END;

ALTER TABLE grt_policy_rule ADD COLUMN version_no INTEGER NOT NULL DEFAULT 1;
--@column grt_policy_rule.version_no classification=internal billable=false | The live version of the rule in grt_policy_rule_version; name, position, format_version and body above are a copy of it.
ALTER TABLE grt_policy_rule ADD COLUMN deleted_at INTEGER;
--@column grt_policy_rule.deleted_at classification=internal billable=false | When the rule was deleted (soft delete), Unix milliseconds UTC; empty while it applies.

-- Every rule that exists now becomes version 1 of itself.
INSERT INTO grt_policy_rule_version (rule_id, version_no, name, position, format_version, body, created_by, created_at)
SELECT id, 1, name, position, format_version, body, NULL, created_at FROM grt_policy_rule;

ALTER TABLE grt_model ADD COLUMN allowed INTEGER NOT NULL DEFAULT 1 CHECK (allowed IN (0, 1));
--@column grt_model.allowed classification=internal billable=false | 1 when grants may include the model, 0 when the host has not opened it for sharing.
ALTER TABLE grt_model ADD COLUMN keep_alive_seconds INTEGER CHECK (keep_alive_seconds IS NULL OR keep_alive_seconds >= 0);
--@column grt_model.keep_alive_seconds classification=internal billable=false | How long the model stays loaded when idle, in seconds; empty means the host's default keep-alive setting.
