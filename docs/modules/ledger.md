# ledger module

## Purpose

The signed usage ledger, checkpoints and receipts.

## Owns

- Packages: `internal/ledger/` (PLT-F-015) and `internal/ledger/checkpoint/` (PLT-F-016: checkpoint worker, proofs, `/v1/keys` key set, note and receipt verification; PLT-F-023 is still to come).
- Tables: `ldg_*`, migrations in `internal/ledger/migrations/`. Built so far (PLT-F-015): `ldg_leaf`, `ldg_hash`. The ledger also writes the usage module's `use_record` in the same unit of work, because the receipt it signs commits to both; `ldg_checkpoint` (PLT-F-016, written only by `internal/ledger/checkpoint`, immutable).
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-44. The module map is in `docs/design_docs/lld.md` section 1.
