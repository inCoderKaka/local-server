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
//   - Limiter.CountAdmitted when the scheduler has admitted the call (the request counter, D-107);
//   - Limiter.Record after the metering transaction has committed, to raise quota.near_limit.
//
// # Quotas
//
// A quota is a list of windowed entries {metric: tokens|requests, limit, window} on the grant
// version (grants.Version.Quotas) and on the key's narrowing (auth.Narrowing.Quotas), at most one
// entry per (metric, window); different windows on one subject are enforced independently
// (req_001 D-107). Every entry is checked: Check refuses with *QuotaError (code quota_exceeded,
// Retry-After the time to the end of that window) when the use in the window that contains now has
// reached the limit; a limit of 0 refuses every call. The key's entries count that key's use; the
// grant's entries count the use of the grant's consumer across all its keys.
//
// Windows are fixed, [start, end): each starts at a whole multiple of its length counted from
// 0001-01-01 00:00 UTC (time.Time.Truncate), so a length that divides 24h starts a window at every
// midnight UTC. An instant exactly at the end belongs to the next window.
//
// A call is charged to the window that contains the moment it was admitted, the instant the
// scheduler admitted it, not the moment it finished. A request entry reads the persisted request
// counter (use_request_counter), which CountAdmitted raises for the window of the admission
// instant; it counts every admitted call, metered or not (req_001 D-107, held from D-100). A token
// entry sums use_record.total_tokens of the records whose started_at (the admission time) falls
// in the window. A call admitted just before a boundary and finished just after it is therefore
// counted once, in the window it started in. The gateway takes started_at right after
// CountAdmitted, so the two instants differ by a few microseconds or milliseconds; a call admitted
// within that gap before a boundary may count its request in one window and its tokens in the
// next. Tokens only appear once the record is written, after
// the call ends, so the tokens of a call still running count in no window until then. Check runs
// before the scheduler admits a call and counts neither the tokens of calls still running nor,
// for the request entries, calls still queued, so concurrent calls can overshoot a quota by what
// is queued or in flight.
//
// The deprecated single figures (grants.Version.LegacyQuotaTokensPerDay and
// LegacyQuotaRequestsPerDay, and auth.Narrowing.QuotaTokensPerDay and QuotaRequestsPerDay) are
// still enforced, as one entry each with the window limits.quota_window read at check time, when
// the same subject has no entry of that metric. Nothing writes them any more. CheckWindow and
// LegacyWindow are the helpers the admin layer uses when it writes entries.
//
// # Persisted and in-memory state
//
// The request counters are persisted, so they survive a restart; CountAdmitted prunes the ones
// whose window ended more than limits.counter_retention ago. Rate buckets, failure counters and
// near-limit marks live in memory and start empty after a restart. A restart therefore forgets a
// lockout, and can raise one more quota.near_limit event for a window that already raised one.
// Failure counters are keyed by remote host and key prefix only (never the secret) and are evicted
// after limits.tracker_idle_ttl; their number is capped by limits.tracker_max_entries.
//
// # Settings
//
// Every value is a host-owned setting registered by Defs (req_001 D-38, D-54).
package limits
