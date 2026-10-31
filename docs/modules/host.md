# host module

## Purpose

The host machine's own state: sharing on, paused or quiet, its keys, and measured host and share-budget metrics.

## Owns

- Packages: `internal/host/`, `internal/hostkeys/`, `internal/hostmetrics/` (built by PLT-F-005, PLT-F-012, PLT-F-013).
- Tables: `hst_*`, migrations in `internal/host/migrations/`.
- Port: `HostMetrics`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-21, D-31, D-44. The module map is in `docs/design_docs/lld.md` section 1.
