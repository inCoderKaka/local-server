package telemetry

import (
	"context"
	"log/slog"
)

// Telemetry is the thin port every module uses for observability (D-31).
//
// Metric names and label names are fixed when a metric is created. Label values
// are passed per call, in the order the label names were given. Label values
// must be bounded, non-content strings (an outcome, a model name, a status
// class), never prompt or response text, and never an unbounded id.
type Telemetry interface {
	// Logger returns the structured logger. Use the Context methods
	// (InfoContext and so on) so the request id is attached. Never log
	// prompt or response text; wrap any such value in Redacted.
	Logger() *slog.Logger
	// Counter returns the counter with this name, creating it on first use.
	Counter(name string, labelNames ...string) Counter
	// Gauge returns the gauge with this name, creating it on first use.
	Gauge(name string, labelNames ...string) Gauge
	// Histogram returns the histogram with this name, creating it on first use.
	Histogram(name string, labelNames ...string) Histogram
	// StartSpan starts a span. The Phase 1 adapters return the context
	// unchanged and a span that does nothing.
	StartSpan(ctx context.Context, name string) (context.Context, Span)
}

// Counter only goes up.
type Counter interface {
	Inc(labelValues ...string)
	Add(delta float64, labelValues ...string)
}

// Gauge is a value that goes up and down.
type Gauge interface {
	Set(value float64, labelValues ...string)
	Add(delta float64, labelValues ...string)
}

// Histogram records observations in buckets.
type Histogram interface {
	Observe(value float64, labelValues ...string)
}

// Span marks a unit of work. End must be called once.
type Span interface {
	End()
}

// Nop returns a Telemetry that records nothing and discards logs. It is for
// tests and for modules built before startup wires the real adapter.
func Nop() Telemetry { return nop{} }

type nop struct{}

type nopMetric struct{}

type nopSpan struct{}

var discard = slog.New(slog.DiscardHandler)

func (nop) Logger() *slog.Logger                                            { return discard }
func (nop) Counter(string, ...string) Counter                               { return nopMetric{} }
func (nop) Gauge(string, ...string) Gauge                                   { return nopMetric{} }
func (nop) Histogram(string, ...string) Histogram                           { return nopMetric{} }
func (nop) StartSpan(ctx context.Context, _ string) (context.Context, Span) { return ctx, nopSpan{} }

func (nopMetric) Inc(...string)              {}
func (nopMetric) Add(float64, ...string)     {}
func (nopMetric) Set(float64, ...string)     {}
func (nopMetric) Observe(float64, ...string) {}
func (nopSpan) End()                         {}
