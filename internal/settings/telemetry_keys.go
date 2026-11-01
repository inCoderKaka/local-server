package settings

// KeyLogSinkErrorInterval is the host-owned setting for the shortest gap between two stderr
// lines reporting a failing log sink (D-82, D-38). slogprom cannot import settings, so the
// definition lives here and startup registers it and passes the value in
// slogprom.Config.SinkErrorInterval.
const KeyLogSinkErrorInterval = "telemetry.log_sink_error_interval"

// LogSinkErrorIntervalDef is the definition of KeyLogSinkErrorInterval: one line a minute.
var LogSinkErrorIntervalDef = Def{
	Key: KeyLogSinkErrorInterval, Kind: Duration, Default: "1m", Owner: Host,
	Description: "Shortest gap between two stderr lines reporting that the log file cannot be written. Errors in between are counted on /metrics and in the next line.",
}
