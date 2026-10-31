# scheduler module

## Purpose

Admission, queueing and the job state machine for interactive requests and queued jobs.

## Owns

- Packages: `internal/scheduler/`, `internal/jobs/` (built by PLT-F-014, PLT-F-022).
- Tables: `sch_*` and their migrations are planned with PLT-F-022 (jobs); the scheduler keeps its queue in memory and owns no table today.
- Metrics: `scheduler_refusals_total{code}`, `scheduler_unloads_total{reason}`, `scheduler_unload_failures_total{reason}`.
- Port: none exported. It defines consumer-side `Engine` and `Sharing` interfaces for what it calls.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-22, D-26, D-38, D-44, D-54, D-93, D-95. The module map is in `docs/design_docs/lld.md` section 1.
