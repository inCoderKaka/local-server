package settings

import (
	"context"
	"errors"
	"testing"
	"time"
)

// PLT-B-012 (req_001 D-82, D-38): the log-sink report interval is a host setting, default 1m.
func TestLogSinkErrorIntervalDefault(t *testing.T) {
	s := newStore(t)
	if err := s.RegisterTelemetry(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if d, err := s.Duration(ctx, KeyLogSinkErrorInterval, ""); err != nil || d != time.Minute {
		t.Fatalf("default = %v, %v; want 1m", d, err)
	}
	for _, bad := range []string{"0s", "-1s"} {
		if err := s.Set(ctx, Host, KeyLogSinkErrorInterval, "", bad); !errors.Is(err, ErrRejected) {
			t.Fatalf("Set(%q) = %v; want ErrRejected", bad, err)
		}
	}
	if err := s.Set(ctx, Host, KeyLogSinkErrorInterval, "", "30s"); err != nil {
		t.Fatal(err)
	}
	if d, err := s.Duration(ctx, KeyLogSinkErrorInterval, ""); err != nil || d != 30*time.Second {
		t.Fatalf("after set = %v, %v; want 30s", d, err)
	}
}
