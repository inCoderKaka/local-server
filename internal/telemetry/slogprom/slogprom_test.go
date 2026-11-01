package slogprom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

type failWriter struct{ err error }

func (f failWriter) Write([]byte) (int, error) { return 0, f.err }

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func TestSinkFailureCountsAndRateLimitsStderr(t *testing.T) {
	var errOut bytes.Buffer
	clk := &fakeClock{t: time.Unix(1000, 0)}
	tel, err := New(Config{
		Out: failWriter{errors.New("disk full")}, Stderr: &errOut,
		SinkErrorInterval: time.Minute, Now: clk.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	secret := "tell me the launch codes"
	log := func() { tel.Logger().Info("chat", "prompt", secret) }
	log() // reported
	log() // suppressed
	log() // suppressed
	lines := strings.Split(strings.TrimSpace(errOut.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "log sink failed: disk full") || !strings.Contains(lines[0], "suppressed since last report: 0") {
		t.Fatalf("first report wrong: %q", errOut.String())
	}
	clk.t = clk.t.Add(59 * time.Second)
	log()
	if strings.Count(errOut.String(), "\n") != 1 {
		t.Fatalf("reported inside the interval: %q", errOut.String())
	}
	clk.t = clk.t.Add(2 * time.Second)
	log()
	lines = strings.Split(strings.TrimSpace(errOut.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "suppressed since last report: 3") {
		t.Fatalf("second report wrong: %q", errOut.String())
	}
	if strings.Contains(errOut.String(), secret) || strings.Contains(errOut.String(), "chat") {
		t.Fatalf("log content leaked to stderr: %q", errOut.String())
	}
	rec := httptest.NewRecorder()
	tel.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "telemetry_log_write_errors_total 5") {
		t.Fatalf("counter missing or wrong:\n%s", rec.Body.String())
	}
}

func TestSinkWriterPassesThroughAndHealthyWritesAreQuiet(t *testing.T) {
	var errOut, out bytes.Buffer
	tel, err := New(Config{Out: &out, Stderr: &errOut})
	if err != nil {
		t.Fatal(err)
	}
	tel.Logger().Info("hello")
	if !strings.Contains(out.String(), "hello") || errOut.Len() != 0 {
		t.Fatalf("out=%q stderr=%q", out.String(), errOut.String())
	}
	sw := &sinkWriter{w: failWriter{io.ErrShortWrite}, errs: tel.Counter(SinkErrorMetric), interval: time.Minute, stderr: &errOut, now: time.Now}
	if n, err := sw.Write([]byte("abc")); n != 0 || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("not passed through: %d %v", n, err)
	}
}
