package settings

import (
	"context"
	"fmt"
	"time"
)

// KeyLogSinkErrorInterval is the host-owned setting for the shortest gap between two stderr
// lines reporting a failing log sink (D-82, D-38). slogprom cannot import settings, so the
// definition lives here and startup calls RegisterTelemetry and passes a function reading the
// value as slogprom.Config.SinkErrorInterval.
const KeyLogSinkErrorInterval = "telemetry.log_sink_error_interval"

// LogSinkErrorIntervalDef is the definition of KeyLogSinkErrorInterval: one line a minute.
var LogSinkErrorIntervalDef = Def{
	Key: KeyLogSinkErrorInterval, Kind: Duration, Default: "1m", Owner: Host,
	Description: "Shortest gap between two stderr lines reporting that the log file cannot be written. Errors in between are counted on /metrics and in the next line. A change applies from the next line.",
}

// RegisterTelemetry registers the settings the telemetry adapter reads:
// telemetry.log_sink_error_interval (D-82).
func (s *Store) RegisterTelemetry() error {
	if err := s.Register(LogSinkErrorIntervalDef); err != nil {
		return err
	}
	return s.SetValidator(KeyLogSinkErrorInterval, func(_ context.Context, v string) error {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		if d <= 0 {
			return fmt.Errorf("must be greater than 0")
		}
		return nil
	})
}
