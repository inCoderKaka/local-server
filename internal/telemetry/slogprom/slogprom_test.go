package slogprom

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/telemetry"
)

func TestPromptLogsAsRedacted(t *testing.T) {
	var buf bytes.Buffer
	tel, err := New(Config{Out: &buf})
	if err != nil {
		t.Fatal(err)
	}
	secret := "tell me the launch codes"
	ctx := telemetry.WithRequestID(context.Background(), "req-1")
	tel.Logger().InfoContext(ctx, "chat", "prompt", telemetry.Redact(secret), "n", 3)
	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("content leaked: %s", out)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["prompt"] != "[redacted]" || rec["request_id"] != "req-1" {
		t.Fatalf("unexpected record: %v", rec)
	}
}

func TestMetricsHandlerExposesCounter(t *testing.T) {
	tel, err := New(Config{Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	tel.Counter("ls_requests_total", "outcome").Inc("ok")
	tel.Counter("ls_requests_total", "outcome").Add(2, "ok")
	tel.Gauge("ls_queue_depth").Set(4)
	tel.Histogram("ls_latency_seconds").Observe(0.2)
	rec := httptest.NewRecorder()
	tel.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{`ls_requests_total{outcome="ok"} 3`, "ls_queue_depth 4", "ls_latency_seconds_count 1"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in metrics output", want)
		}
	}
}

func TestContentLabelPanics(t *testing.T) {
	tel, err := New(Config{Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("want panic for content label")
		}
	}()
	tel.Counter("bad_total", "prompt")
}

func TestFileSinkRotates(t *testing.T) {
	dir := t.TempDir()
	tel, err := New(Config{File: FileConfig{Path: filepath.Join(dir, "ls.log"), MaxSizeMB: 1}})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 1000)
	for i := 0; i < 1500; i++ { // about 1.5 MB
		tel.Logger().Info("fill", "pad", line)
	}
	if err := tel.Close(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) < 2 {
		t.Fatalf("want a rotated file next to the active one, got %d entries", len(entries))
	}
}

func TestSpanIsNoop(t *testing.T) {
	tel, err := New(Config{Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ctx2, sp := tel.StartSpan(ctx, "x")
	sp.End()
	if ctx2 != ctx {
		t.Fatal("no-op span must not change the context")
	}
}

func TestFileSinkBadPathErrors(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{File: FileConfig{Path: filepath.Join(f, "ls.log")}}); err == nil {
		t.Fatal("want an error for a log path under a regular file")
	}
}

func TestLabelMismatchPanics(t *testing.T) {
	tel, err := New(Config{Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	tel.Counter("x_total", "a")
	defer func() {
		if recover() == nil {
			t.Fatal("want panic when a metric is re-created with other labels")
		}
	}()
	tel.Counter("x_total", "b")
}
