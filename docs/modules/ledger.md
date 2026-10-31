# ledger module

## Purpose

The signed usage ledger, checkpoints and receipts.

## Owns

- Packages: `internal/ledger/` (built by PLT-F-015, PLT-F-016, PLT-F-023).
- Tables: `ldg_*`, migrations in `internal/ledger/migrations/`. Built so far (PLT-F-015): `ldg_leaf`, `ldg_hash`. The ledger also writes the usage module's `use_record` in the same unit of work, because the receipt it signs commits to both; `ldg_checkpoint` comes with PLT-F-016.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-44. The module map is in `docs/design_docs/lld.md` section 1.
