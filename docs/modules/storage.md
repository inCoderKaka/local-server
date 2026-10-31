# storage module

## Purpose

Opens the SQLite database, runs per-module forward-only migrations and provides the unit of work. The only module that imports a database driver.

## Owns

- Packages: `internal/storage/` (built by PLT-F-002).
- Tables: `ops_schema_migrations`, `ops_migration_run`, `ops_data_catalog`, migrations in `internal/storage/migrations/`. Also `syn_change` (migration 2): it is written in the same transaction as the change it records, so the unit of work owns its creation; the off-host module adds `syn_cursor` and ships the rows.
- Port: `repositories, unit of work`. Public surface: `Open(ctx, Config)`, `DB.Write(ctx, func(*Tx) error)`, `Tx.Exec/Query/QueryRow/Capture`, `DB.Reader()`, `DB.SchemaDoc`, `Module{Name, Prefixes, Migrations}`. Package comment in `internal/storage/doc.go` is the contract.
- Generated: `docs/schema-reference.md` (`go generate ./internal/storage`; `TestSchemaDoc` fails when it is stale).

## Behaviour

- One SQLite file in WAL mode, `foreign_keys` and `busy_timeout` in the DSN of every connection. One writer goroutine groups queued units of work into one `BEGIN IMMEDIATE` batch, each unit in a savepoint, so a failing unit rolls back alone and its audit, event, outbox and `syn_change` rows with it. A read pool of `query_only` connections serves reads.
- Migrations: forward-only `NNNN_name.sql` per module, embedded, applied at startup; SHA-256 checksums in `ops_schema_migrations`; a changed or missing applied file, or an older new one, stops startup.
- Catalog: `--@table` and `--@column` lines in migrations (comment, classification, billable) are collected into `ops_data_catalog`; startup fails if any table or column lacks one or a table lacks its module's prefix.
- Configuration: busy timeout, batch size, batch window and read-pool size are `Config` fields with documented defaults; the runtime settings module (PLT-F-004) is not built yet (D-38, D-54).

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-09, D-28, D-44. The module map is in `docs/design_docs/lld.md` section 1.
