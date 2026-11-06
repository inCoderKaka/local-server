// Package power is part of the power module; see docs/modules/power.md. It holds the sleep lock
// while the host is sharing (req_001 D-30, D-06): the Power port, one adapter per OS, and the
// Keeper worker that takes and releases the lock as the sharing state changes (PLT-F-042).
package power
