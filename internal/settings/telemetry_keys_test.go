package settings

import (
	"context"
	"testing"
	"time"
)

// PLT-B-012 (req_001 D-82, D-38): the log-sink report interval is a host setting, default 1m.
func TestLogSinkErrorIntervalDefault(t *testing.T) {
	s := newStore(t)
	if err := s.Register(LogSinkErrorIntervalDef); err != nil {
		t.Fatal(err)
	}
	if d, err := s.Duration(context.Background(), KeyLogSinkErrorInterval, ""); err != nil || d != time.Minute {
		t.Fatalf("default = %v, %v; want 1m", d, err)
	}
}
