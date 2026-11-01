# limits module

## Purpose

Rate and quota limits applied at admission (F1 step 3, after auth and before policy): token and request quotas per access key and per grant (request quotas, D-98, count every admitted call: the gateway raises an in-memory request counter with `Limiter.CountAdmitted`, and Check reads the larger of it and the window's `use_record` count, D-100), a requests-per-window token bucket per key, a request body size limit, lockout after repeated failed key checks (a success clears only its key prefix's counter, D-99), and the `quota.near_limit` event (once per quota subject per window, threshold in `limits.near_limit_percent`). Failure counters, rate buckets, near-limit marks and request counters live in memory and reset on restart. Quota use is read from `use_record` through `internal/usage` (`usage.Tokens`, `usage.Count`); no table is owned. Every value is a host-owned `limits.*` setting (`Defs()`); `Limiter.ProtectionsEnabled` tells public mode which of rate limit, lockout and body limit are on. The call order for the gateway is in the package doc of `internal/limits`.

## Owns

- Packages: `internal/limits/` (PLT-F-017).
- Tables: none.
- Port: none defined here. It reads `use_record` through `internal/usage` (`usage.Tokens`, `usage.Count`) and emits through `events.Notifier`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-25. The module map is in `docs/design_docs/lld.md` section 1.
