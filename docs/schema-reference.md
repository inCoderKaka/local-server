# Schema reference

Generated from the migrations by `go generate ./internal/storage` (storage module, D-44). Do not edit by hand.
Classification: public, internal, personal, sensitive or content. Billable: whether the data may be billed for later.

## ops_data_catalog

Module: storage. Classification: internal. Billable: false.

One row per table and per column: what it holds, how sensitive it is and whether it is billable. Rebuilt from the migrations at every startup.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| table_name | TEXT | no | PK | internal | false | Table the row describes. |
| column_name | TEXT | no | PK | internal | false | Column the row describes; empty for the table itself. |
| module | TEXT | no |  | internal | false | Module that owns the table. |
| classification | TEXT | no |  | internal | false | public, internal, personal, sensitive or content (D-44). |
| billable | INTEGER | no |  | internal | false | 1 when the data may be billed for later, otherwise 0. |
| comment | TEXT | no |  | internal | false | Plain-language description. |

## ops_migration_run

Module: storage. Classification: internal. Billable: false.

Log of every attempt to apply a migration, including failed ones.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| id | INTEGER | no | PK | internal | false | Attempt number. |
| module | TEXT | no |  | internal | false | Name of the module that owns the migration. |
| version | INTEGER | no |  | internal | false | Migration number inside the module. |
| name | TEXT | no |  | internal | false | Short name taken from the migration file name. |
| started_at | INTEGER | no |  | internal | false | When the attempt started, Unix milliseconds UTC. |
| finished_at | INTEGER | no |  | internal | false | When the attempt ended, Unix milliseconds UTC. |
| outcome | TEXT | no |  | internal | false | applied or failed. |
| error | TEXT | yes |  | internal | false | Error text of a failed attempt; empty when it was applied. |

## ops_schema_migrations

Module: storage. Classification: internal. Billable: false.

Which migration of which module has been applied, with the checksum of its file at that time.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| module | TEXT | no | PK | internal | false | Name of the module that owns the migration. |
| version | INTEGER | no | PK | internal | false | Migration number inside the module, increasing. |
| name | TEXT | no |  | internal | false | Short name taken from the migration file name. |
| checksum | TEXT | no |  | internal | false | SHA-256 of the migration file as applied; a changed file stops startup. |
| applied_at | INTEGER | no |  | internal | false | When it was applied, Unix milliseconds UTC. |
| duration_ms | INTEGER | no |  | internal | false | How long applying it took, in milliseconds. |

## syn_change

Module: storage. Classification: internal. Billable: false.

Every change to a synced row, written in the same transaction as the change, waiting to be shipped off the host.

| Column | Type | Null | Key | Classification | Billable | Description |
|---|---|---|---|---|---|---|
| seq | INTEGER | no | PK | internal | false | Increasing sequence number; the cursor of each sync target points into it. |
| dataset | TEXT | no |  | internal | false | Dataset the changed row belongs to. |
| table_name | TEXT | no |  | internal | false | Table of the changed row. |
| pk | TEXT | no |  | internal | false | Primary key of the changed row. |
| version | INTEGER | no |  | internal | false | Row version after the change. |
| op | TEXT | no |  | internal | false | insert, update or delete. |
| payload | BLOB | yes |  | internal | false | The row as the module chose to record it; never prompts or responses (D-31, D-40). |
| created_at | INTEGER | no |  | internal | false | When the change was captured, Unix milliseconds UTC. |
