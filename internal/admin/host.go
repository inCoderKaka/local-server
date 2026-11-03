package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/hostmetrics"
)

// versioner is what an engine adapter may offer on top of the engine port: its version.
type versioner interface {
	Version(ctx context.Context) (string, error)
}

// AdminGetHost implements GET /admin/v1/host.
func (a *Admin) AdminGetHost(ctx context.Context) (*api.HostInfo, error) { return a.hostInfo(ctx) }

// AdminPauseSharing implements POST /admin/v1/host/pause. The host module audits host.paused in
// the same unit of work as the state change, with the admin as the actor (req_001 D-06, D-07).
func (a *Admin) AdminPauseSharing(ctx context.Context) (*api.HostInfo, error) {
	if _, err := a.cfg.Sharing.Pause(ctx, PrincipalIDOf(ctx)); err != nil {
		return nil, fmt.Errorf("admin: pause sharing: %w", err)
	}
	return a.hostInfo(ctx)
}

// AdminResumeSharing implements POST /admin/v1/host/resume (host.resumed is audited likewise).
func (a *Admin) AdminResumeSharing(ctx context.Context) (*api.HostInfo, error) {
	if _, err := a.cfg.Sharing.Resume(ctx, PrincipalIDOf(ctx)); err != nil {
		return nil, fmt.Errorf("admin: resume sharing: %w", err)
	}
	return a.hostInfo(ctx)
}

func (a *Admin) hostInfo(ctx context.Context) (*api.HostInfo, error) {
	now := a.now()
	state, err := a.cfg.Sharing.Effective(ctx)
	if err != nil && !errors.Is(err, host.ErrInvalidSchedule) {
		return nil, fmt.Errorf("admin: sharing state: %w", err)
	}
	out := &api.HostInfo{HostKid: a.cfg.Keys.HostID(), SharingState: api.SharingState(state), SoftwareVersion: a.cfg.Version}
	if at, ok, err := a.cfg.Sharing.PausedAt(ctx); err != nil {
		return nil, fmt.Errorf("admin: paused at: %w", err)
	} else if ok {
		out.PausedAt = api.NewOptNilDateTime(at.UTC())
	}
	if state == host.Quiet {
		if sch, err := host.LoadSchedule(ctx, a.cfg.Settings); err == nil {
			if end := sch.NextEnd(now); !end.IsZero() {
				out.SharingResumesAt = api.NewOptNilDateTime(end.UTC())
			}
		}
	}
	if !out.PausedAt.Set {
		out.PausedAt.SetToNull()
	}
	if !out.SharingResumesAt.Set {
		out.SharingResumesAt.SetToNull()
	}

	out.EngineState = api.HostInfoEngineStateUnavailable
	out.EngineVersion.SetToNull()
	timeout, err := a.durationSetting(ctx, KeyEngineProbeTimeout)
	if err != nil {
		return nil, fmt.Errorf("admin: read settings: %w", err)
	}
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if _, err := a.cfg.Engine.Loaded(pctx); err != nil {
		if ctx.Err() == nil {
			a.log.WarnContext(ctx, "admin: model engine did not answer the probe", "err", err)
		}
		return out, nil
	}
	out.EngineState = api.HostInfoEngineStateAvailable
	if v, ok := a.cfg.Engine.(versioner); ok {
		if ver, err := v.Version(pctx); err == nil && ver != "" {
			out.EngineVersion = api.NewOptNilString(ver)
		}
	}
	return out, nil
}

// metric maps a stored or live figure to the API: null when the measure says it was not read
// (hostmetrics stores such a value as 0), with the measure as the method so a value is never
// guessed (req_001 D-31).
func metricInt(v int64, m hostmetrics.Measure) api.MetricValue {
	if m == hostmetrics.MeasureUnavailable || m == "" {
		return api.MetricValue{Value: nilFloat(), Method: methodOf(m)}
	}
	return api.MetricValue{Value: api.NewNilFloat64(float64(v)), Method: string(m)}
}

func metricFloat(v float64, m hostmetrics.Measure) api.MetricValue {
	if m == hostmetrics.MeasureUnavailable || m == "" {
		return api.MetricValue{Value: nilFloat(), Method: methodOf(m)}
	}
	return api.MetricValue{Value: api.NewNilFloat64(v), Method: string(m)}
}

func nilFloat() api.NilFloat64 {
	var n api.NilFloat64
	n.SetToNull()
	return n
}

func methodOf(m hostmetrics.Measure) string {
	if m == "" {
		return string(hostmetrics.MeasureUnavailable)
	}
	return string(m)
}

// sliceCPUMeasure says how the slice's CPU was measured: derived, unless neither part was read.
func sliceCPUMeasure(localServer, ollama hostmetrics.Measure) hostmetrics.Measure {
	if localServer == hostmetrics.MeasureUnavailable && ollama == hostmetrics.MeasureUnavailable {
		return hostmetrics.MeasureUnavailable
	}
	return hostmetrics.MeasureDerived
}

// unattributed is the schema's "unattributed" figure. The OS reports no figure for these
// processes, so memory and CPU are null; the method says how many processes were unreadable.
func unattributed(n int) api.ResourceUse {
	method := string(hostmetrics.MeasureUnavailable)
	if n > 0 {
		method = fmt.Sprintf("%s: the OS would not report %d processes", method, n)
	}
	return api.ResourceUse{
		MemoryBytes: api.MetricValue{Value: nilFloat(), Method: method},
		CPUPercent:  api.MetricValue{Value: nilFloat(), Method: method},
	}
}

func sampleOfReading(r hostmetrics.Reading) api.HostMetricsSample {
	return api.HostMetricsSample{
		SampledAt: r.At.UTC(),
		Total: api.ResourceUse{
			MemoryBytes: metricInt(r.Host.MemoryUsed.Value, r.Host.MemoryUsed.Measure),
			CPUPercent:  metricFloat(r.Host.CPU.Value, r.Host.CPU.Measure)},
		ShareBudgetSlice: api.ResourceUse{
			MemoryBytes: metricInt(r.Slice.Memory.Value, r.Slice.Memory.Measure),
			CPUPercent:  metricFloat(r.Slice.CPU.Value, sliceCPUMeasure(r.Slice.LocalServerCPU.Measure, r.Slice.OllamaCPU.Measure))},
		OtherApps: api.ResourceUse{
			MemoryBytes: metricInt(r.Other.Memory.Value, r.Other.Memory.Measure),
			CPUPercent:  metricFloat(r.Other.CPU.Value, r.Other.CPU.Measure)},
		Unattributed: unattributed(r.Unattributed),
	}
}

func sampleOfMinute(m hostmetrics.Minute) api.HostMetricsSample {
	ms := m.Measures
	return api.HostMetricsSample{
		SampledAt: m.SampledAt.UTC(),
		Total: api.ResourceUse{
			MemoryBytes: metricInt(m.MemoryUsed, ms["host_memory"]),
			CPUPercent:  metricFloat(m.CPUPercent, ms["host_cpu"])},
		ShareBudgetSlice: api.ResourceUse{
			MemoryBytes: metricInt(m.SliceMemory, ms["slice"]),
			CPUPercent:  metricFloat(m.SliceCPU, sliceCPUMeasure(ms["local_server_cpu"], ms["ollama_cpu"]))},
		OtherApps: api.ResourceUse{
			MemoryBytes: metricInt(m.OtherMemory, ms["other"]),
			CPUPercent:  metricFloat(m.OtherCPU, ms["other_cpu"])},
		Unattributed: unattributed(m.Unattributed),
	}
}

// AdminGetHostMetrics implements GET /admin/v1/host-metrics: the latest sample, or one reading
// taken now when the sampler has not sampled yet. The figures are labelled with how they were
// measured (req_001 D-31).
func (a *Admin) AdminGetHostMetrics(ctx context.Context) (*api.HostMetricsSample, error) {
	r, ok := a.cfg.Sampler.Current()
	if !ok {
		var err error
		if r, err = a.cfg.Sampler.Read(ctx); err != nil {
			return nil, fmt.Errorf("admin: read host metrics: %w", err)
		}
	}
	out := sampleOfReading(r)
	return &out, nil
}

// AdminGetHostMetricsHistory implements GET /admin/v1/host-metrics/history: the stored
// per-minute samples in [from, to), oldest first. Without from, the range reaches back
// admin.metrics_history_default_range from to (which defaults to now). A longer range than
// admin.metrics_history_max_points minutes returns only the newest points: the range is cut at
// the start, so a huge request reads no more rows than it returns.
func (a *Admin) AdminGetHostMetricsHistory(ctx context.Context, params api.AdminGetHostMetricsHistoryParams) (*api.HostMetricsHistory, error) {
	rng, err := a.durationSetting(ctx, KeyMetricsHistoryRange)
	if err != nil {
		return nil, err
	}
	points, err := a.intSetting(ctx, KeyMetricsHistoryPoints)
	if err != nil {
		return nil, err
	}
	to := a.now()
	if v, ok := params.To.Get(); ok {
		to = v
	}
	from := to.Add(-rng)
	if v, ok := params.From.Get(); ok {
		from = v
	}
	if !from.Before(to) {
		return nil, badRequest("from", "The start of the range must be before its end.")
	}
	// One row per minute at most, so the newest `points` rows lie in the last `points` minutes.
	if floor := to.Add(-time.Duration(min(points, int64(1)<<27)) * time.Minute); from.Before(floor) {
		from = floor
	}
	minutes, err := a.cfg.Sampler.GetRange(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("admin: read host metrics history: %w", err)
	}
	if int64(len(minutes)) > points {
		minutes = minutes[int64(len(minutes))-points:]
	}
	out := &api.HostMetricsHistory{Data: make([]api.HostMetricsSample, 0, len(minutes))}
	for _, m := range minutes {
		out.Data = append(out.Data, sampleOfMinute(m))
	}
	return out, nil
}
