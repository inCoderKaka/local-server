-- storage module, migration 1: the migration bookkeeping and the data catalog (D-28, D-44).
-- Every table and column carries a plain-language comment, a data classification and a
-- billable flag in a "--@" line; storage collects them into ops_data_catalog at startup.

CREATE TABLE ops_schema_migrations (
  module      TEXT NOT NULL,
  version     INTEGER NOT NULL,
  name        TEXT NOT NULL,
  checksum    TEXT NOT NULL,
  applied_at  INTEGER NOT NULL,
  duration_ms INTEGER NOT NULL,
  PRIMARY KEY (module, version)
) STRICT;
--@table ops_schema_migrations classification=internal billable=false | Which migration of which module has been applied, with the checksum of its file at that time.
--@column ops_schema_migrations.module classification=internal billable=false | Name of the module that owns the migration.
--@column ops_schema_migrations.version classification=internal billable=false | Migration number inside the module, increasing.
--@column ops_schema_migrations.name classification=internal billable=false | Short name taken from the migration file name.
--@column ops_schema_migrations.checksum classification=internal billable=false | SHA-256 of the migration file as applied; a changed file stops startup.
--@column ops_schema_migrations.applied_at classification=internal billable=false | When it was applied, Unix milliseconds UTC.
--@column ops_schema_migrations.duration_ms classification=internal billable=false | How long applying it took, in milliseconds.

CREATE TABLE ops_migration_run (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  module      TEXT NOT NULL,
  version     INTEGER NOT NULL,
  name        TEXT NOT NULL,
  started_at  INTEGER NOT NULL,
  finished_at INTEGER NOT NULL,
  outcome     TEXT NOT NULL CHECK (outcome IN ('applied', 'failed')),
  error       TEXT
) STRICT;
--@table ops_migration_run classification=internal billable=false | Log of every attempt to apply a migration, including failed ones.
--@column ops_migration_run.id classification=internal billable=false | Attempt number.
--@column ops_migration_run.module classification=internal billable=false | Name of the module that owns the migration.
--@column ops_migration_run.version classification=internal billable=false | Migration number inside the module.
--@column ops_migration_run.name classification=internal billable=false | Short name taken from the migration file name.
--@column ops_migration_run.started_at classification=internal billable=false | When the attempt started, Unix milliseconds UTC.
--@column ops_migration_run.finished_at classification=internal billable=false | When the attempt ended, Unix milliseconds UTC.
--@column ops_migration_run.outcome classification=internal billable=false | applied or failed.
--@column ops_migration_run.error classification=internal billable=false | Error text of a failed attempt; empty when it was applied.

CREATE TABLE ops_data_catalog (
  table_name     TEXT NOT NULL,
  column_name    TEXT NOT NULL DEFAULT '',
  module         TEXT NOT NULL,
  classification TEXT NOT NULL CHECK (classification IN ('public', 'internal', 'personal', 'sensitive', 'content')),
  billable       INTEGER NOT NULL CHECK (billable IN (0, 1)),
  comment        TEXT NOT NULL,
  PRIMARY KEY (table_name, column_name)
) STRICT;
--@table ops_data_catalog classification=internal billable=false | One row per table and per column: what it holds, how sensitive it is and whether it is billable. Rebuilt from the migrations at every startup.
--@column ops_data_catalog.table_name classification=internal billable=false | Table the row describes.
--@column ops_data_catalog.column_name classification=internal billable=false | Column the row describes; empty for the table itself.
--@column ops_data_catalog.module classification=internal billable=false | Module that owns the table.
--@column ops_data_catalog.classification classification=internal billable=false | public, internal, personal, sensitive or content (D-44).
--@column ops_data_catalog.billable classification=internal billable=false | 1 when the data may be billed for later, otherwise 0.
--@column ops_data_catalog.comment classification=internal billable=false | Plain-language description.
