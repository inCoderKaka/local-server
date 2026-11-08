# scheduler module

## Purpose

Admission, queueing and the job state machine for interactive requests and queued jobs. `internal/scheduler` admits requests in memory; `internal/jobs` stores background jobs, runs the job worker and holds the job state machine (PLT-F-022). The gateway serves the four `/v1/jobs` endpoints and implements `jobs.Executor`, so the worker goes through the same policy, admission, engine, metering and receipt steps as an interactive call (D-39).

## Owns

- Packages: `internal/scheduler/`, `internal/jobs/` (`scheduler` built by PLT-F-014; `jobs` built by PLT-F-022).
- Tables: `sch_job`, `sch_job_payload` and `sch_job_result` (migration `internal/scheduler/migrations/0001_job_tables.sql`, registered by `scheduler.Module()`). The payload and the result are the only content-bearing tables: sealed with the host data key, never synced, no `syn_change` capture. The scheduler's own queue stays in memory.
- Settings: `scheduler.*` (host); `jobs.deadline` and `jobs.result_ttl` (consumer-owned, one value per consumer id), `jobs.poll_interval`, `jobs.retry_interval`, `jobs.max_active` and `jobs.retention` (host; how long a finished job record is kept, `0s` keeps it forever, and a job whose result can still be fetched is never pruned).
- Metrics and events: `scheduler_refusals_total{code}`, `scheduler_unloads_total{reason}`, `scheduler_unload_failures_total{reason}`, `jobs_finished_total{status}`; emits `job.done` (ids and times only).
- Port: none exported. It defines consumer-side `Engine` and `Sharing` interfaces for what it calls, and `jobs.Executor` / `jobs.Slot`, which the gateway implements. `jobs.FailPayloadMinimized(ctx, db, now)` marks queued and running jobs that have no payload row `failed(payload_minimized)`; `local-server restore` calls it on a restored database whose snapshot left out `sch_job_payload` (PLT-F-044, D-68, D-72).

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-22, D-26, D-38, D-40, D-44, D-54, D-68, D-85, D-93, D-95. The module map is in `docs/design_docs/lld.md` section 1.
