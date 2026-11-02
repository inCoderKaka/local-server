# usage module

## Purpose

Metering of tokens and request counts per consumer.

## Owns

- Packages: `internal/usage/` (built by PLT-F-015, PLT-F-016; the rollups and usage endpoint are designed: PLT-F-023; not built).
- Tables: `use_*`, migrations in `internal/usage/migrations/`. Built so far (PLT-F-015): `use_record`, written by `internal/ledger` together with its ledger leaf.
- Port: read functions `usage.Tokens` and `usage.Count` (`internal/usage/records.go`) sum tokens or count `use_record` rows of a consumer or access key over a time range. The limits module uses them for quotas (D-98).

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-44. The module map is in `docs/design_docs/lld.md` section 1.
