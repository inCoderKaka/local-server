# limits module

## Purpose

Rate and quota limits applied at admission (F1 step 3, after auth and before policy): windowed token and request quotas per access key and per grant (a quota is a list of `{metric, limit, window}` entries, at most one per metric and window, D-107; a call is charged to the window that holds its admission time, so a call spanning a window boundary counts once; a requests entry counts every admitted call, metered or not: the gateway raises the persisted request counter with `Limiter.CountAdmitted`, and Check reads it), the deprecated single-figure quotas (`quota_tokens_per_day`, `quota_requests_per_day`) still enforced as one entry each with the window `limits.quota_window`, a requests-per-window token bucket per key, a request body size limit, lockout after repeated failed key checks (a success clears only its key prefix's counter, D-99), and the `quota.near_limit` event (once per quota subject per window, threshold in `limits.near_limit_percent`). Failure counters, rate buckets and near-limit marks live in memory and reset on restart; request counters survive a restart and are pruned when their window ended more than `limits.counter_retention` ago. Token use is read from `use_record` by `started_at` through `internal/usage` (`usage.TokensStartedIn`) and request use from `use_request_counter` (`usage.RequestCount`); no table is owned. Every value is a host-owned `limits.*` setting (`Defs()`); `Limiter.ProtectionsEnabled` tells public mode which of rate limit, lockout and body limit are on. The call order for the gateway is in the package doc of `internal/limits`.

## Owns

- Packages: `internal/limits/` (PLT-F-017).
- Tables: none. The request counter table `use_request_counter` belongs to the usage module and is written through `usage.AddRequest`.
- Settings (host-owned): the `limits.*` keys in `Defs()`, among them `limits.quota_window` (24h; the window of the deprecated single-figure quotas), `limits.quota_window_min` (1m) and `limits.quota_window_max` (720h) (the bounds of a window the admin layer accepts when it writes a quota entry; stored entries are never rechecked) and `limits.counter_retention` (168h).
- Helpers for the admin layer: `limits.CheckWindow` and `limits.LegacyWindow`.
- Port: none defined here. It reads `use_record` and `use_request_counter` through `internal/usage` (`usage.TokensStartedIn`, `usage.RequestCount`) and emits through `events.Notifier`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-25, D-107. The module map is in `docs/design_docs/lld.md` section 1.
