# usage module

## Purpose

Metering of tokens and request counts per consumer.

## Owns

- Packages: `internal/usage/` (built by PLT-F-015, PLT-F-016, PLT-F-023).
- Tables: `use_*`, migrations in `internal/usage/migrations/`. `use_record` (PLT-F-015), written by `internal/ledger` together with its ledger leaf; `use_rollup` and `use_rollup_state` (PLT-F-023, migration 3): minute, hour and day totals per consumer, key and model, built by the `usage-rollups` worker from a rowid watermark.
- Port: read functions `usage.Tokens` and `usage.Count` (`internal/usage/records.go`) sum tokens or count `use_record` rows of a consumer or access key over a time range. The limits module uses them for quotas (D-98).
- Query: `usage.QueryRollups` returns rollup pages by consumer, key, model, time range and granularity, with a keyset cursor. The gateway serves it at `GET /v1/usage`; PLT-F-024 serves it at `/admin/v1/usage` (D-104).
- Settings: `usage.rollup.interval`, `usage.rollup.batch_rows`, `usage.rollup.minute_retention` (7 d), `usage.rollup.hour_retention` (90 d), `usage.rollup.day_retention` (forever) and `usage.page_size`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-44. The module map is in `docs/design_docs/lld.md` section 1.
