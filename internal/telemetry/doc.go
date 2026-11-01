// Package telemetry is the Telemetry port (D-31): a thin interface for
// structured logs, counters, gauges (a gauge series can be deleted),
// histograms and spans, plus the request id helpers and the Redacted value
// type that keeps prompts and responses out of logs (D-31, D-40). It imports
// only the standard library (log/slog supplies the port's logger type) and no
// third-party logging, metrics or rotation library; the default adapter is in
// internal/telemetry/slogprom. Modules import this package only. See
// docs/modules/telemetry.md.
package telemetry
