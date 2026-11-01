// Package slogprom is the default Telemetry adapter (D-31): log/slog JSON logs
// to a rotating file (lumberjack, D-58) or a writer, a Prometheus registry
// served as an http.Handler for /metrics, and no-op spans.
package slogprom

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// Defaults used when a FileConfig size or age field is zero. Once the settings
// module (PLT-F-004) exists, startup passes the log-retention settings (D-40,
// D-54, D-58) in FileConfig.
const (
	DefaultMaxSizeMB  = 50
	DefaultMaxAgeDays = 14
	// DefaultSinkErrorInterval is used when Config.SinkErrorInterval is zero.
	DefaultSinkErrorInterval = time.Minute
)

// SinkErrorMetric counts failed writes to the log sink (D-82). Metric names follow
// <module>_<thing>_total for counters (ledger_checkpoints_signed_total, scheduler_refusals_total).
// It has no labels, so no log content can reach it.
const SinkErrorMetric = "telemetry_log_write_errors_total"

// FileConfig configures the rotating log file.
type FileConfig struct {
	// Path of the active log file. Empty means no file: logs go to Config.Out.
	Path string
	// MaxSizeMB is the size at which the file rotates. Zero means DefaultMaxSizeMB.
	MaxSizeMB int
	// MaxAgeDays is how long rotated files are kept. Zero means DefaultMaxAgeDays.
	MaxAgeDays int
	// MaxBackups caps rotated files kept; zero keeps all within MaxAgeDays.
	MaxBackups int
	// Compress gzips rotated files.
	Compress bool
}

// Config configures the adapter.
type Config struct {
	// Level is the minimum log level (default info).
	Level slog.Level
	// File is the rotating log file. Takes precedence over Out.
	File FileConfig
	// Out receives logs when File.Path is empty. Nil means os.Stderr.
	Out io.Writer
	// SinkErrorInterval is the shortest gap between two stderr lines reporting a
	// failing log sink (D-82). Zero means DefaultSinkErrorInterval. Startup passes
	// the telemetry.log_sink_error_interval setting.
	SinkErrorInterval time.Duration
	// Stderr receives the sink-failure line. Nil means os.Stderr. For tests.
	Stderr io.Writer
	// Now is the clock for the rate limit. Nil means time.Now. For tests.
	Now func() time.Time
}

// forbiddenLabels are label names that suggest content; creating a metric with
// one is a programming error and panics (D-31, D-40).
var forbiddenLabels = []string{"prompt", "response", "content", "message", "input", "output", "text", "completion"}

// Telemetry is the slog + Prometheus adapter. It implements telemetry.Telemetry.
type Telemetry struct {
	logger *slog.Logger
	reg    *prometheus.Registry
	closer io.Closer

	mu         sync.Mutex
	counters   map[string]*counter
	gauges     map[string]*gauge
	histograms map[string]*histogram
}

var _ telemetry.Telemetry = (*Telemetry)(nil)

// New builds the adapter.
func New(cfg Config) (*Telemetry, error) {
	var w io.Writer = cfg.Out
	if w == nil {
		w = os.Stderr
	}
	var closer io.Closer
	if cfg.File.Path != "" {
		lj := &lumberjack.Logger{
			Filename:   cfg.File.Path,
			MaxSize:    orDefault(cfg.File.MaxSizeMB, DefaultMaxSizeMB),
			MaxAge:     orDefault(cfg.File.MaxAgeDays, DefaultMaxAgeDays),
			MaxBackups: cfg.File.MaxBackups,
			Compress:   cfg.File.Compress,
		}
		// lumberjack opens the file lazily, on the first write. A zero-length
		// write forces the open (with its MkdirAll) now, so an unwritable path
		// fails New instead of silently dropping every log line.
		if _, err := lj.Write(nil); err != nil {
			return nil, fmt.Errorf("slogprom: open log file %q: %w", cfg.File.Path, err)
		}
		w, closer = lj, lj
	}
	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	t := &Telemetry{
		reg:        reg,
		closer:     closer,
		counters:   map[string]*counter{},
		gauges:     map[string]*gauge{},
		histograms: map[string]*histogram{},
	}
	sw := &sinkWriter{
		w:        w,
		errs:     t.Counter(SinkErrorMetric),
		interval: cfg.SinkErrorInterval,
		stderr:   cfg.Stderr,
		now:      cfg.Now,
	}
	if sw.interval <= 0 {
		sw.interval = DefaultSinkErrorInterval
	}
	if sw.stderr == nil {
		sw.stderr = os.Stderr
	}
	if sw.now == nil {
		sw.now = time.Now
	}
	jh := slog.NewJSONHandler(sw, &slog.HandlerOptions{Level: cfg.Level})
	t.logger = slog.New(requestIDHandler{jh})
	return t, nil
}

// sinkWriter wraps the log sink. slog discards Handle errors, so a failing sink would
// drop every line silently; on a write error it counts the failure and writes at most
// one stderr line per interval (D-82). The line carries the OS error text and a count of
// suppressed errors, never the dropped log line (D-31, D-40).
type sinkWriter struct {
	w        io.Writer
	errs     telemetry.Counter
	interval time.Duration
	stderr   io.Writer
	now      func() time.Time

	mu         sync.Mutex
	reported   bool
	last       time.Time
	suppressed int
}

func (s *sinkWriter) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	if err != nil {
		s.errs.Inc()
		s.report(err)
	}
	return n, err
}

func (s *sinkWriter) report(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.reported && now.Sub(s.last) < s.interval {
		s.suppressed++
		return
	}
	fmt.Fprintf(s.stderr, "local-server: log sink failed: %v (errors suppressed since last report: %d)\n", err, s.suppressed)
	s.reported, s.last, s.suppressed = true, now, 0
}

func orDefault(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}

// Close flushes and closes the log file, if any.
func (t *Telemetry) Close() error {
	if t.closer == nil {
		return nil
	}
	return t.closer.Close()
}

// MetricsHandler serves the Prometheus text format for /metrics. Mounting it
// on the admin listener is done by startup (D-10, D-31).
func (t *Telemetry) MetricsHandler() http.Handler {
	return promhttp.HandlerFor(t.reg, promhttp.HandlerOpts{})
}

// Logger implements telemetry.Telemetry.
func (t *Telemetry) Logger() *slog.Logger { return t.logger }

// StartSpan implements telemetry.Telemetry; spans are no-ops in Phase 1 (D-31).
func (t *Telemetry) StartSpan(ctx context.Context, _ string) (context.Context, telemetry.Span) {
	return ctx, noSpan{}
}

type noSpan struct{}

func (noSpan) End() {}

// requestIDHandler adds the request id from the context to every record.
type requestIDHandler struct{ slog.Handler }

func (h requestIDHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := telemetry.RequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h requestIDHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return requestIDHandler{h.Handler.WithAttrs(a)}
}

func (h requestIDHandler) WithGroup(n string) slog.Handler {
	return requestIDHandler{h.Handler.WithGroup(n)}
}

func checkLabels(name string, labels []string) {
	for _, l := range labels {
		if slices.Contains(forbiddenLabels, strings.ToLower(l)) {
			panic(fmt.Sprintf("slogprom: metric %q: label %q may carry content; content is never a label (D-31, D-40)", name, l))
		}
	}
}

// checkSameLabels panics when a cached metric is asked for again with other
// label names: recording under the first set would mislabel data silently.
func checkSameLabels(name string, cached, labelNames []string) {
	if !slices.Equal(cached, labelNames) {
		panic(fmt.Sprintf("slogprom: metric %q already created with labels %v, got %v", name, cached, labelNames))
	}
}

// Counter implements telemetry.Telemetry.
func (t *Telemetry) Counter(name string, labelNames ...string) telemetry.Counter {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c, ok := t.counters[name]; ok {
		checkSameLabels(name, c.labels, labelNames)
		return c
	}
	checkLabels(name, labelNames)
	v := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: name}, labelNames)
	t.reg.MustRegister(v)
	c := &counter{v, slices.Clone(labelNames)}
	t.counters[name] = c
	return c
}

// Gauge implements telemetry.Telemetry.
func (t *Telemetry) Gauge(name string, labelNames ...string) telemetry.Gauge {
	t.mu.Lock()
	defer t.mu.Unlock()
	if g, ok := t.gauges[name]; ok {
		checkSameLabels(name, g.labels, labelNames)
		return g
	}
	checkLabels(name, labelNames)
	v := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: name}, labelNames)
	t.reg.MustRegister(v)
	g := &gauge{v, slices.Clone(labelNames)}
	t.gauges[name] = g
	return g
}

// Histogram implements telemetry.Telemetry.
func (t *Telemetry) Histogram(name string, labelNames ...string) telemetry.Histogram {
	t.mu.Lock()
	defer t.mu.Unlock()
	if h, ok := t.histograms[name]; ok {
		checkSameLabels(name, h.labels, labelNames)
		return h
	}
	checkLabels(name, labelNames)
	v := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: name, Help: name}, labelNames)
	t.reg.MustRegister(v)
	h := &histogram{v, slices.Clone(labelNames)}
	t.histograms[name] = h
	return h
}

type counter struct {
	v      *prometheus.CounterVec
	labels []string
}

func (c *counter) Inc(lv ...string)            { c.v.WithLabelValues(lv...).Inc() }
func (c *counter) Add(d float64, lv ...string) { c.v.WithLabelValues(lv...).Add(d) }

type gauge struct {
	v      *prometheus.GaugeVec
	labels []string
}

func (g *gauge) Set(x float64, lv ...string) { g.v.WithLabelValues(lv...).Set(x) }
func (g *gauge) Add(d float64, lv ...string) { g.v.WithLabelValues(lv...).Add(d) }

type histogram struct {
	v      *prometheus.HistogramVec
	labels []string
}

func (h *histogram) Observe(x float64, lv ...string) { h.v.WithLabelValues(lv...).Observe(x) }
