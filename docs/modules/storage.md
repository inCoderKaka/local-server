# storage module

## Purpose

Opens the SQLite database, runs per-module forward-only migrations and provides the unit of work. The only module that imports a database driver.

## Owns

- Packages: `internal/storage/` (built by PLT-F-002).
- Tables: `ops_*`, migrations in `internal/storage/migrations/`.
- Port: `repositories, unit of work`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-09, D-28, D-44. The module map is in `docs/design_docs/lld.md` section 1.
