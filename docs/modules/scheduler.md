# scheduler module

## Purpose

Admission, queueing and the job state machine for interactive requests and queued jobs.

## Owns

- Packages: `internal/scheduler/`, `internal/jobs/` (built by PLT-F-014, PLT-F-022).
- Tables: `sch_*`, migrations in `internal/scheduler/migrations/`.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-38, D-44. The module map is in `docs/design_docs/lld.md` section 1.
