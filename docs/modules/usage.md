# usage module

## Purpose

Metering of tokens and request counts per consumer.

## Owns

- Packages: `internal/usage/` (built by PLT-F-015, PLT-F-016, PLT-F-023).
- Tables: `use_*`, migrations in `internal/usage/migrations/`. `use_record` (PLT-F-015), written by `internal/ledger` together with its ledger leaf; `use_rollup` and `use_rollup_state` (PLT-F-023, migration 3): minute, hour and day totals per consumer, key and model, built by the `usage-rollups` worker from a rowid watermark; `use_request_counter` (migration 4, req_001 D-107): the calls admitted per consumer or access key, quota window length and fixed window, metered or not, numbers and ids only. Migration 4 also indexes `use_record` by `(access_key_id, started_at)` and `(consumer_id, started_at)`.
- Port: read function `usage.Count` (`internal/usage/records.go`) counts `use_record` rows of a consumer or access key by `finished_at` over a time range. `usage.TokensStartedIn` sums tokens by `started_at`, the admission time, which the limits module uses for token quotas (D-107). The request counter has `usage.AddRequest` (one upsert in the caller's unit of work), `usage.RequestCount` and `usage.PruneRequestCounters`; the limits module owns what a window means and when to prune (`limits.counter_retention`).
- Query: `usage.QueryRollups` returns rollup pages by consumer, key, model, time range and granularity, with a keyset cursor. The gateway serves it at `GET /v1/usage`; PLT-F-024 serves it at `/admin/v1/usage` (D-104).
- Settings: `usage.rollup.interval`, `usage.rollup.batch_rows`, `usage.rollup.minute_retention` (7 d), `usage.rollup.hour_retention` (90 d), `usage.rollup.day_retention` (forever) and `usage.page_size`.
- Metrics: `usage_rollup_records_total`, `usage_rollup_errors_total`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-44, D-107. The module map is in `docs/design_docs/lld.md` section 1.
