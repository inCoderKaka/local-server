// Package usage is part of the usage module; see docs/modules/usage.md. It owns the use_
// tables. So far that is use_record, the immutable usage record (req_001 D-16, D-40),
// created by migration 1 here. The ledger module (internal/ledger) writes the record
// together with its ledger leaf in one unit of work, because the receipt it signs commits to
// both. Rollups and the usage endpoint come with PLT-F-023.
package usage
