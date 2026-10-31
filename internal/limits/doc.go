// Package limits is the limits module; see docs/modules/limits.md. It is step 3 of the F1
// pipeline (auth, limits, policy; req_001 D-39): token quotas per access key and per grant,
// a requests-per-window token bucket per key, a request body size limit, lockout after repeated
// failed key authentication, and the quota.near_limit event (req_001 D-24, D-25, D-29).
//
// # Calls
//
// The gateway (PLT-F-018) calls, in this order for one request:
//
//   - Limiter.CheckLockout before auth.Service.Authenticate, so a locked-out address or key
//     prefix is refused without verifying the secret;
//   - Limiter.AuthResult with Authenticate's error (nil on success): failures count toward
//     lockout, success clears only the key prefix's counter, never the address's (req_001 D-99);
//   - Limiter.LimitBody before it reads the body;
//   - Limiter.Check after auth: rate limit, then a fresh check that the key and consumer are
//     still active, then quotas. A refusal is a *RateLimitError, *QuotaError, or one of
//     auth.ErrRevoked, auth.ErrExpired and auth.ErrConsumerNotActive;
//   - Limiter.Record after the metering transaction has committed, to raise quota.near_limit.
//
// # Quotas
//
// A quota is the key's or the grant version's tokens-per-window figure (Narrowing and
// grants.Version store it as quota_tokens_per_day; the window length is the setting
// limits.quota_window, one day by default). Usage is the sum of use_record.total_tokens of
// calls that finished in the current fixed window, aligned to whole multiples of the window length counted from 0001-01-01 UTC (midnight UTC for lengths that divide 24h),
// so Retry-After is the time to the end of the window. A request quota
// (quota_requests_per_day, req_001 D-98) is the same figure per request: Check counts the
// use_record rows of that window against it, with the same scope rules and the same
// quota_exceeded refusal (QuotaError.Unit says which). Until the gateway's admission counter
// exists (req_001 D-100, PLT-F-018), the request quota counts use_record rows, so unmetered
// calls are not yet counted, and calls in flight can overshoot it as they do token quotas.
// D-100 makes it count every admitted call. The key's quota counts that key's
// records; the grant's quota counts all records of the grant's consumer. Check does not count
// tokens of calls still running, so concurrent calls can overshoot a quota by what is in flight.
//
// # In-memory state
//
// Rate buckets, failure counters and near-limit marks live in memory and start empty after a
// restart. A restart therefore forgets a lockout, and can raise one more quota.near_limit event
// for a window that already raised one. Failure counters are keyed by remote host and key
// prefix only (never the secret) and are evicted after limits.tracker_idle_ttl; their number is
// capped by limits.tracker_max_entries.
//
// # Settings
//
// Every value is a host-owned setting registered by Defs (req_001 D-38, D-54).
package limits
