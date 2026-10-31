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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// Defaults for FileConfig. Startup will overwrite them from the log-retention
// settings (D-40, D-54) once the settings module (PLT-F-004) exists; until
// then these are the documented defaults.
const (
	DefaultMaxSizeMB  = 50
	DefaultMaxAgeDays = 14
)

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
		w, closer = lj, lj
	}
	jh := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: cfg.Level})
	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	return &Telemetry{
		logger:     slog.New(requestIDHandler{jh}),
		reg:        reg,
		closer:     closer,
		counters:   map[string]*counter{},
		gauges:     map[string]*gauge{},
		histograms: map[string]*histogram{},
	}, nil
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

// Counter implements telemetry.Telemetry.
func (t *Telemetry) Counter(name string, labelNames ...string) telemetry.Counter {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c, ok := t.counters[name]; ok {
		return c
	}
	checkLabels(name, labelNames)
	v := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: name}, labelNames)
	t.reg.MustRegister(v)
	c := &counter{v}
	t.counters[name] = c
	return c
}

// Gauge implements telemetry.Telemetry.
func (t *Telemetry) Gauge(name string, labelNames ...string) telemetry.Gauge {
	t.mu.Lock()
	defer t.mu.Unlock()
	if g, ok := t.gauges[name]; ok {
		return g
	}
	checkLabels(name, labelNames)
	v := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: name}, labelNames)
	t.reg.MustRegister(v)
	g := &gauge{v}
	t.gauges[name] = g
	return g
}

// Histogram implements telemetry.Telemetry.
func (t *Telemetry) Histogram(name string, labelNames ...string) telemetry.Histogram {
	t.mu.Lock()
	defer t.mu.Unlock()
	if h, ok := t.histograms[name]; ok {
		return h
	}
	checkLabels(name, labelNames)
	v := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: name, Help: name}, labelNames)
	t.reg.MustRegister(v)
	h := &histogram{v}
	t.histograms[name] = h
	return h
}

type counter struct{ v *prometheus.CounterVec }

func (c *counter) Inc(lv ...string)            { c.v.WithLabelValues(lv...).Inc() }
func (c *counter) Add(d float64, lv ...string) { c.v.WithLabelValues(lv...).Add(d) }

type gauge struct{ v *prometheus.GaugeVec }

func (g *gauge) Set(x float64, lv ...string) { g.v.WithLabelValues(lv...).Set(x) }
func (g *gauge) Add(d float64, lv ...string) { g.v.WithLabelValues(lv...).Add(d) }

type histogram struct{ v *prometheus.HistogramVec }

func (h *histogram) Observe(x float64, lv ...string) { h.v.WithLabelValues(lv...).Observe(x) }
