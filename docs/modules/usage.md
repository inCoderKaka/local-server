# usage module

## Purpose

Metering of tokens and request counts per consumer.

## Owns

- Packages: `internal/usage/` (built by PLT-F-015, PLT-F-016, PLT-F-023).
- Tables: `use_*`, migrations in `internal/usage/migrations/`. Built so far (PLT-F-015): `use_record`, written by `internal/ledger` together with its ledger leaf.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-44. The module map is in `docs/design_docs/lld.md` section 1.
