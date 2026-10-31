package hostmetrics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// Keys of the host-metrics settings (D-38, D-54). All are owned by the host.
const (
	KeySampleInterval = "host.metrics.sample_interval"
	KeyRetention      = "host.metrics.retention"
	KeyOllamaNames    = "host.metrics.ollama_process_names"
)

// Defs are the host-metrics settings; register them with settings.Store.Register. A sample
// interval of zero or less turns sampling off.
func Defs() []settings.Def {
	return []settings.Def{
		{Key: KeySampleInterval, Kind: settings.Duration, Default: "15s", Owner: settings.Host,
			Description: "How often the host's memory and CPU use is read. Zero or less stops sampling."},
		{Key: KeyRetention, Kind: settings.Duration, Default: "168h", Owner: settings.Host,
			Description: "How long the per-minute host readings are kept. Zero or less keeps them forever."},
		{Key: KeyOllamaNames, Kind: settings.String, Default: "ollama", Owner: settings.Host,
			Description: "Process names, separated by commas, that count as Ollama when working out the share-budget slice. Case and a Windows .exe suffix are ignored."},
	}
}

// Sampler takes a reading on a ticker, sets the gauges, keeps the latest reading and writes the
// per-minute row (D-40).
type Sampler struct {
	reader *Reader
	db     *storage.DB
	st     *settings.Store
	tel    telemetry.Telemetry

	memory       telemetry.Gauge
	cpu          telemetry.Gauge
	unattributed telemetry.Gauge

	mu      sync.Mutex
	current *Reading
}

// NewSampler registers the host-metrics settings and the retention rule of hst_metric_minute with
// st and returns a Sampler that reads the real OS. loaded may be nil (the loaded models are then
// reported unavailable). A nil clock means time.Now.
func NewSampler(db *storage.DB, st *settings.Store, tel telemetry.Telemetry, loaded LoadedLister, clock Clock) (*Sampler, error) {
	return newSampler(db, st, tel, loaded, clock, newGopsutilProbe(), int32(os.Getpid()))
}

func newSampler(db *storage.DB, st *settings.Store, tel telemetry.Telemetry, loaded LoadedLister, clock Clock, p probe, pid int32) (*Sampler, error) {
	if err := st.Register(Defs()...); err != nil {
		return nil, err
	}
	if err := st.RegisterRetention(settings.Rule{Table: "hst_metric_minute", TimeColumn: "minute", Setting: KeyRetention}); err != nil {
		return nil, err
	}
	s := &Sampler{db: db, st: st, tel: tel}
	s.reader = newReader(p, loaded, pid, clock, s.ollamaNames)
	s.memory = tel.Gauge("hostmetrics_memory_bytes", "scope", "measure")
	s.cpu = tel.Gauge("hostmetrics_cpu_percent", "scope", "measure")
	s.unattributed = tel.Gauge("hostmetrics_unattributed_processes")
	return s, nil
}

// Read implements HostMetrics.
func (s *Sampler) Read(ctx context.Context) (Reading, error) { return s.reader.Read(ctx) }

func (s *Sampler) ollamaNames(ctx context.Context) ([]string, error) {
	v, err := s.st.String(ctx, KeyOllamaNames, "")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range strings.Split(v, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out, nil
}

// Current returns the latest reading, or false before the first sample.
func (s *Sampler) Current() (Reading, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return Reading{}, false
	}
	return *s.current, true
}

// SampleOnce takes one reading, sets the gauges, keeps it as Current and writes the row of its
// minute (a later sample in the same minute replaces the earlier one).
func (s *Sampler) SampleOnce(ctx context.Context) (Reading, error) {
	r, err := s.reader.Read(ctx)
	if err != nil {
		return Reading{}, err
	}
	s.setGauges(r)
	s.mu.Lock()
	s.current = &r
	s.mu.Unlock()
	return r, s.store(ctx, r)
}

func (s *Sampler) setGauges(r Reading) {
	mem := func(scope string, b Bytes) { s.memory.Set(float64(b.Value), scope, string(b.Measure)) }
	cpu := func(scope string, p Percent) { s.cpu.Set(p.Value, scope, string(p.Measure)) }
	mem("host_total", r.Host.MemoryTotal)
	mem("host_used", r.Host.MemoryUsed)
	mem("local_server", r.Slice.LocalServerMemory)
	mem("ollama", r.Slice.OllamaMemory)
	mem("loaded_models", r.Slice.LoadedModelsMemory)
	mem("slice", r.Slice.Memory)
	mem("other", r.Other.Memory)
	cpu("host", r.Host.CPU)
	cpu("local_server", r.Slice.LocalServerCPU)
	cpu("ollama", r.Slice.OllamaCPU)
	cpu("slice", r.Slice.CPU)
	cpu("other", r.Other.CPU)
	s.unattributed.Set(float64(r.Unattributed))
}

// measures is the JSON stored in hst_metric_minute.measures.
func measures(r Reading) map[string]Measure {
	return map[string]Measure{
		"host_memory":   r.Host.MemoryUsed.Measure,
		"host_cpu":      r.Host.CPU.Measure,
		"accelerator":   r.Host.Accelerator.Measure,
		"local_server":  r.Slice.LocalServerMemory.Measure,
		"ollama":        r.Slice.OllamaMemory.Measure,
		"loaded_models": r.Slice.LoadedModelsMemory.Measure,
		"slice":         r.Slice.Memory.Measure,
		"other":         r.Other.Memory.Measure,
	}
}

func (s *Sampler) store(ctx context.Context, r Reading) error {
	m, err := json.Marshal(measures(r))
	if err != nil {
		return err
	}
	clamped := 0
	if r.Slice.Clamped {
		clamped = 1
	}
	minute := r.At.UTC().Truncate(time.Minute).UnixMilli()
	return s.db.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.Exec(`INSERT INTO hst_metric_minute
  (minute, sampled_at, mem_total_bytes, mem_used_bytes, cpu_cores, cpu_percent, slice_mem_bytes, slice_cpu_percent,
   other_mem_bytes, other_cpu_percent, unattributed, slice_clamped, measures)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(minute) DO UPDATE SET
  sampled_at = excluded.sampled_at, mem_total_bytes = excluded.mem_total_bytes, mem_used_bytes = excluded.mem_used_bytes,
  cpu_cores = excluded.cpu_cores, cpu_percent = excluded.cpu_percent, slice_mem_bytes = excluded.slice_mem_bytes,
  slice_cpu_percent = excluded.slice_cpu_percent, other_mem_bytes = excluded.other_mem_bytes,
  other_cpu_percent = excluded.other_cpu_percent, unattributed = excluded.unattributed,
  slice_clamped = excluded.slice_clamped, measures = excluded.measures`,
			minute, r.At.UnixMilli(), r.Host.MemoryTotal.Value, r.Host.MemoryUsed.Value, r.Host.CPUCores, r.Host.CPU.Value,
			r.Slice.Memory.Value, r.Slice.CPU.Value, r.Other.Memory.Value, r.Other.CPU.Value, r.Unattributed, clamped, string(m))
		return err
	})
}

// Minute is one stored row of hst_metric_minute.
type Minute struct {
	Start        time.Time // the start of the minute, UTC
	SampledAt    time.Time
	MemoryTotal  int64
	MemoryUsed   int64
	CPUCores     int
	CPUPercent   float64
	SliceMemory  int64
	SliceCPU     float64
	OtherMemory  int64
	OtherCPU     float64
	Unattributed int
	SliceClamped bool
	// Measures says how each value was measured, keyed as in the measures column.
	Measures map[string]Measure
}

// GetRange returns the stored minutes from (inclusive) to to (exclusive), oldest first.
func (s *Sampler) GetRange(ctx context.Context, from, to time.Time) ([]Minute, error) {
	rows, err := s.db.Reader().Query(ctx, `SELECT minute, sampled_at, mem_total_bytes, mem_used_bytes, cpu_cores, cpu_percent,
  slice_mem_bytes, slice_cpu_percent, other_mem_bytes, other_cpu_percent, unattributed, slice_clamped, measures
FROM hst_metric_minute WHERE minute >= ? AND minute < ? ORDER BY minute`, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Minute
	for rows.Next() {
		var m Minute
		var minute, sampled, clamped int64
		var raw string
		if err := rows.Scan(&minute, &sampled, &m.MemoryTotal, &m.MemoryUsed, &m.CPUCores, &m.CPUPercent,
			&m.SliceMemory, &m.SliceCPU, &m.OtherMemory, &m.OtherCPU, &m.Unattributed, &clamped, &raw); err != nil {
			return nil, err
		}
		m.Start, m.SampledAt, m.SliceClamped = time.UnixMilli(minute).UTC(), time.UnixMilli(sampled).UTC(), clamped == 1
		if err := json.Unmarshal([]byte(raw), &m.Measures); err != nil {
			return nil, fmt.Errorf("hostmetrics: measures of minute %d: %w", minute, err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Run samples now and then every host.metrics.sample_interval (read after each sample, so a
// change takes effect on the next wait) until ctx ends. While the interval is zero or less it
// takes no samples and looks again after the setting's default interval. A failed sample is
// logged (counts and errors only) and the loop continues.
func (s *Sampler) Run(ctx context.Context) {
	for {
		wait, err := s.st.Duration(ctx, KeySampleInterval, "")
		if err != nil {
			s.tel.Logger().ErrorContext(ctx, "host metrics: read sample interval", "error", err)
		}
		if err == nil && wait > 0 {
			if _, err := s.SampleOnce(ctx); err != nil && ctx.Err() == nil {
				s.tel.Logger().ErrorContext(ctx, "host metrics: sample", "error", err)
			}
			if wait, err = s.st.Duration(ctx, KeySampleInterval, ""); err != nil || wait <= 0 {
				wait = s.defaultInterval()
			}
		} else {
			wait = s.defaultInterval()
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// defaultInterval is how long Run waits while sampling is off, so a later change is noticed.
func (s *Sampler) defaultInterval() time.Duration {
	if d, ok := s.st.Definition(KeySampleInterval); ok {
		if v, err := time.ParseDuration(d.Default); err == nil && v > 0 {
			return v
		}
	}
	return time.Minute
}
