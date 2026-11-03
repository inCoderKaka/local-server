// Package usage is part of the usage module; see docs/modules/usage.md. It owns the use_
// tables.
//
// use_record is the immutable usage record (req_001 D-16, D-40), created by migration 1. The
// ledger module (internal/ledger) writes it together with its ledger leaf in one unit of work,
// because the receipt it signs commits to both.
//
// use_rollup holds the sums of those records per minute, hour and day, per consumer, access key
// and model (PLT-F-023; migration 3). Rollups is the background worker that adds each record to
// its three buckets exactly once, moving the use_rollup_state watermark in the same unit of work;
// the host owns its interval and the retention of each granularity as settings (req_001 D-38,
// D-40) and the settings retention worker prunes the rows. QueryRollups is the one usage query,
// served to a consumer at /v1/usage by the gateway and to the host at /admin/v1/usage by the
// admin API (req_001 D-104). It reads use_rollup only.
//
// Rollups hold numbers and ids only: never a prompt or a response (D-31, D-40).
package usage
