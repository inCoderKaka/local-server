# storage module

## Purpose

Opens the SQLite database, runs per-module forward-only migrations and provides the unit of work. The only module that imports a database driver.

## Owns

- Packages: `internal/storage/` (built by PLT-F-002).
- Tables: `ops_schema_migrations`, `ops_migration_run`, `ops_data_catalog`, migrations in `internal/storage/migrations/`. Also `syn_change` (migration 2): it is written in the same transaction as the change it records, so the unit of work owns its creation; the off-host module adds `syn_cursor` and ships the rows. Migration 3 adds the `syn_change_created_at` index used by the `sync.change.retention` delete (D-77).
- Port: `repositories, unit of work`. Public surface: `Open(ctx, Config)`, `DB.Write(ctx, func(*Tx) error)`, `Tx.Exec/Query/QueryRow/Capture`, `DB.Reader()`, `DB.SchemaDoc`, `Module{Name, Prefixes, Migrations}`, `ErrNoRows` (alias of `sql.ErrNoRows`, PLT-F-056), `OpenReadOnly` (`mode=ro`, no migrations, refuses a schema that differs from the build's; `local-server doctor` reads stored settings through it, PLT-F-020). `Open` refuses to boot when a foreign key's parent table is missing, when the parent's owning module (from `ops_data_catalog`) is registered after the child's, or when `PRAGMA foreign_key_check` finds a violating row (table and rowid only; PLT-F-057). Package comment in `internal/storage/doc.go` is the contract.
- Generated: `docs/schema-reference.md` (`go generate ./cmd/local-server`, over every registered module; `TestSchemaDoc` in `cmd/local-server` fails when it is stale).

## Behaviour

- One SQLite file in WAL mode, `foreign_keys` and `busy_timeout` in the DSN of every connection. One writer goroutine groups queued units of work into one `BEGIN IMMEDIATE` batch, each unit in a savepoint, so a failing unit rolls back alone and its audit, event, outbox and `syn_change` rows with it. A read pool of `query_only` connections serves reads. The write connection runs `synchronous=FULL` by default, or `NORMAL`, from `Config.Synchronous` (D-76).
- Migrations: forward-only `NNNN_name.sql` per module, embedded, applied at startup; SHA-256 checksums in `ops_schema_migrations`; a changed or missing applied file, or an older new one, stops startup.
- To correct the catalog line of an applied migration, or add a missing one, add a new migration that carries only the corrected `--@` lines; a later migration's line overrides an earlier one.
- Catalog: `--@table` and `--@column` lines in migrations (comment, classification, billable) are collected into `ops_data_catalog`; startup fails if any table or column lacks one or a table lacks its module's prefix.
- Configuration: busy timeout, batch size, batch window, read-pool size and synchronous mode are `Config` fields with documented defaults. storage cannot import settings: the host setting `storage.synchronous` is registered by `settings.RegisterStorage` and read by startup (PLT-F-020), applied on the next start (D-76). Busy timeout, batch size, batch window and read-pool size have no stored setting yet (D-38, D-54).

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-09, D-28, D-44. The module map is in `docs/design_docs/lld.md` section 1.
