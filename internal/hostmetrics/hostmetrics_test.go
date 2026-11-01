package hostmetrics

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine/fake"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

const gib = int64(1) << 30

type fakeProbe struct {
	total, used uint64
	cores       int
	cpu         float64
	procs       []procInfo
	usage       map[int32]procUsage
	coresErr    error
	cpuErr      error
}

func (f *fakeProbe) Memory(context.Context) (uint64, uint64, error) { return f.total, f.used, nil }
func (f *fakeProbe) CPUCores(context.Context) (int, error)          { return f.cores, f.coresErr }
func (f *fakeProbe) CPUPercent(context.Context) (float64, error)    { return f.cpu, f.cpuErr }
func (f *fakeProbe) Processes(context.Context) ([]procInfo, error)  { return f.procs, nil }
func (f *fakeProbe) Usage(_ context.Context, pid int32) (procUsage, error) {
	u, ok := f.usage[pid]
	if !ok {
		return procUsage{}, errors.New("access denied")
	}
	return u, nil
}

const ownPID = 100

func baseProbe() *fakeProbe {
	return &fakeProbe{
		total: uint64(16 * gib), used: uint64(10 * gib), cores: 4, cpu: 50,
		procs: []procInfo{
			{PID: ownPID, Name: "local-server", Readable: true},
			{PID: 200, Name: "Ollama.exe", Readable: true},
			{PID: 300, Name: "browser", Readable: true},
		},
		usage: map[int32]procUsage{
			ownPID: {RSS: uint64(gib / 2), CPU: 40},
			200:    {RSS: uint64(2 * gib), CPU: 100},
		},
	}
}

type gaugeRec struct {
	mu   sync.Mutex
	vals map[string]float64
}

func (g *gaugeRec) Set(v float64, l ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.vals[key(l)] = v
}
func (g *gaugeRec) Add(float64, ...string) {}
func (g *gaugeRec) Delete(l ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.vals, key(l))
}
func (g *gaugeRec) get(l ...string) (float64, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	v, ok := g.vals[key(l)]
	return v, ok
}
func key(l []string) string {
	k := ""
	for _, s := range l {
		k += s + "|"
	}
	return k
}

// recTel records gauge values; everything else is a no-op.
type recTel struct {
	telemetry.Telemetry
	gauges map[string]*gaugeRec
}

func (r *recTel) Gauge(name string, _ ...string) telemetry.Gauge {
	if g, ok := r.gauges[name]; ok {
		return g
	}
	g := &gaugeRec{vals: map[string]float64{}}
	r.gauges[name] = g
	return g
}

type env struct {
	db  *storage.DB
	st  *settings.Store
	tel *recTel
	now time.Time
	s   *Sampler
	p   *fakeProbe
	eng *fake.Engine
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(t.TempDir(), "t.db"),
		settings.Module(), host.Module()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{db: db, st: settings.New(db), tel: &recTel{Telemetry: telemetry.Nop(), gauges: map[string]*gaugeRec{}},
		now: time.Date(2026, 3, 1, 12, 0, 10, 0, time.UTC), p: baseProbe(),
		eng: fake.New(fake.WithModels(fake.ModelSpec{Name: "m1", MemoryBytes: gib}))}
	e.s, err = newSampler(db, e.st, e.tel, e.eng, func() time.Time { return e.now }, e.p, ownPID)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) sample(t *testing.T) Reading {
	t.Helper()
	r, err := e.s.SampleOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (e *env) set(t *testing.T, key, v string) {
	t.Helper()
	if err := e.st.Set(context.Background(), settings.Host, key, "", v); err != nil {
		t.Fatal(err)
	}
}

func TestSliceAndOther(t *testing.T) {
	e := newEnv(t)
	if err := e.eng.Load(context.Background(), "m1", time.Minute); err != nil {
		t.Fatal(err)
	}
	r := e.sample(t)
	// The fake engine reports the model fully on the accelerator, so Ollama's RSS is the larger.
	if want := gib/2 + 2*gib; r.Slice.Memory.Value != want {
		t.Fatalf("slice memory = %d, want %d", r.Slice.Memory.Value, want)
	}
	if r.Slice.Memory.Value > r.Host.MemoryTotal.Value || r.Slice.Clamped {
		t.Fatalf("slice %d must fit total %d unclamped", r.Slice.Memory.Value, r.Host.MemoryTotal.Value)
	}
	if want := 10*gib - r.Slice.Memory.Value; r.Other.Memory.Value != want {
		t.Fatalf("other = %d, want %d", r.Other.Memory.Value, want)
	}
	if r.Slice.LoadedModelsMemory.Measure != MeasureEngineLoaded || r.Slice.OllamaMemory.Measure != MeasureProcessRSS ||
		r.Slice.Memory.Measure != MeasureDerived || r.Host.MemoryTotal.Measure != MeasureHostTotal {
		t.Fatalf("measures: %+v", r)
	}
	// 40/4 + 100/4 = 35 percent of the host; host 50 so other 15.
	if r.Slice.CPU.Value != 35 || r.Other.CPU.Value != 15 {
		t.Fatalf("cpu slice %v other %v", r.Slice.CPU.Value, r.Other.CPU.Value)
	}
	if r.Host.Accelerator.Measure != MeasureUnavailable || r.Host.Accelerator.Reason == "" {
		t.Fatalf("accelerator: %+v", r.Host.Accelerator)
	}
	if r.Unattributed != 0 {
		t.Fatalf("unattributed = %d", r.Unattributed)
	}
}

func TestSliceClampedToTotal(t *testing.T) {
	e := newEnv(t)
	e.p.usage[200] = procUsage{RSS: uint64(40 * gib), CPU: 2000}
	r := e.sample(t)
	if r.Slice.Memory.Value != r.Host.MemoryTotal.Value || !r.Slice.Clamped {
		t.Fatalf("slice %d clamped=%v, want total %d", r.Slice.Memory.Value, r.Slice.Clamped, r.Host.MemoryTotal.Value)
	}
	if r.Other.Memory.Value != 0 || r.Other.CPU.Value < 0 || r.Slice.CPU.Value > r.Host.CPU.Value {
		t.Fatalf("other/slice not clamped: %+v", r)
	}
}

func TestUnknownCoresMakesProcessCPUUnavailable(t *testing.T) {
	e := newEnv(t)
	e.p.coresErr = errors.New("no cores")
	r := e.sample(t)
	if r.Host.CPUCores != 0 {
		t.Fatalf("cores = %d, want 0", r.Host.CPUCores)
	}
	if r.Slice.LocalServerCPU.Measure != MeasureUnavailable || r.Slice.OllamaCPU.Measure != MeasureUnavailable {
		t.Fatalf("cpu should be unavailable: %+v", r.Slice)
	}
	if r.Slice.LocalServerMemory.Measure != MeasureProcessRSS || r.Slice.OllamaMemory.Measure != MeasureProcessRSS {
		t.Fatalf("memory should stay measured: %+v", r.Slice)
	}
}

func TestUnprimedProcessCPUUnavailable(t *testing.T) {
	e := newEnv(t)
	e.p.usage[ownPID] = procUsage{RSS: uint64(gib / 2), CPUUnavailable: true}
	r := e.sample(t)
	if r.Slice.LocalServerCPU.Measure != MeasureUnavailable || r.Slice.LocalServerMemory.Measure != MeasureProcessRSS {
		t.Fatalf("local-server: %+v", r.Slice)
	}
	if r.Slice.OllamaCPU.Measure != MeasureProcessRSS {
		t.Fatalf("ollama cpu should be measured: %+v", r.Slice.OllamaCPU)
	}
	e.p.usage[200] = procUsage{RSS: uint64(2 * gib), CPUUnavailable: true}
	r = e.sample(t)
	if r.Slice.OllamaCPU.Measure != MeasureUnavailable || r.Slice.OllamaMemory.Measure != MeasureProcessRSS {
		t.Fatalf("ollama: %+v", r.Slice)
	}
}

func TestSliceCPUCappedWithoutHostCPU(t *testing.T) {
	e := newEnv(t)
	e.p.cpuErr = errors.New("no host cpu")
	e.p.usage[ownPID] = procUsage{RSS: uint64(gib / 2), CPU: 400}
	e.p.usage[200] = procUsage{RSS: uint64(2 * gib), CPU: 400}
	r := e.sample(t)
	if r.Slice.CPU.Value != 100 || !r.Slice.Clamped || r.Other.CPU.Measure != MeasureUnavailable {
		t.Fatalf("slice cpu %v clamped %v other %+v", r.Slice.CPU.Value, r.Slice.Clamped, r.Other.CPU)
	}
}

func TestUnreadableProcessIsUnattributed(t *testing.T) {
	e := newEnv(t)
	e.p.procs = append(e.p.procs, procInfo{PID: 400, Readable: false}, procInfo{PID: 500, Readable: false})
	// A second Ollama process the OS refuses, and a macOS silent zero.
	e.p.procs = append(e.p.procs, procInfo{PID: 201, Name: "ollama", Readable: true}, procInfo{PID: 202, Name: "ollama", Readable: true})
	e.p.usage[202] = procUsage{RSS: 0}
	r := e.sample(t)
	if r.Unattributed != 4 {
		t.Fatalf("unattributed = %d, want 4", r.Unattributed)
	}
	if r.Slice.OllamaMemory.Value != 2*gib {
		t.Fatalf("ollama memory = %d, readable process only", r.Slice.OllamaMemory.Value)
	}
	if v, _ := e.tel.gauges["hostmetrics_unattributed_processes"].get(); v != 4 {
		t.Fatalf("gauge = %v", v)
	}
}

func TestLoadedFailureDegrades(t *testing.T) {
	e := newEnv(t)
	e.eng.InjectError(fake.OpLoaded, errors.New("ollama down"), 1)
	r := e.sample(t)
	if r.Slice.LoadedModelsMemory.Measure != MeasureUnavailable || r.Slice.LoadedModelsMemory.Value != 0 {
		t.Fatalf("loaded: %+v", r.Slice.LoadedModelsMemory)
	}
	if r.Slice.Memory.Value != gib/2+2*gib {
		t.Fatalf("slice should use processes only, got %d", r.Slice.Memory.Value)
	}
	// A nil lister behaves the same.
	rd := newReader(e.p, nil, ownPID, nil, e.s.ollamaNames)
	if got, err := rd.Read(context.Background()); err != nil || got.Slice.LoadedModelsMemory.Measure != MeasureUnavailable {
		t.Fatalf("nil lister: %v %+v", err, got.Slice.LoadedModelsMemory)
	}
}

func TestOllamaNamesSetting(t *testing.T) {
	e := newEnv(t)
	e.set(t, KeyOllamaNames, "browser, other")
	r := e.sample(t)
	if r.Slice.OllamaMemory.Measure != MeasureUnavailable {
		t.Fatalf("ollama should not match: %+v", r.Slice.OllamaMemory)
	}
	e.p.usage[300] = procUsage{RSS: uint64(gib)}
	if r = e.sample(t); r.Slice.OllamaMemory.Value != gib {
		t.Fatalf("browser should match: %+v", r.Slice.OllamaMemory)
	}
}

func TestPerMinuteUpsertAndRange(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.sample(t)
	e.now = e.now.Add(20 * time.Second) // same minute
	e.p.used = uint64(11 * gib)
	e.sample(t)
	e.now = e.now.Add(time.Minute) // next minute
	e.sample(t)

	start := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	got, err := e.s.GetRange(ctx, start, start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2", len(got))
	}
	if !got[0].Start.Equal(start) || got[0].MemoryUsed != 11*gib {
		t.Fatalf("first minute should hold the later sample: %+v", got[0])
	}
	if got[0].Measures["slice"] != MeasureDerived || got[0].Measures["host_memory"] != MeasureHostTotal {
		t.Fatalf("measures: %v", got[0].Measures)
	}
	if _, ok := got[0].Measures["other_cpu"]; !ok {
		t.Fatalf("other_cpu measure missing: %v", got[0].Measures)
	}
	one, _ := e.s.GetRange(ctx, start.Add(time.Minute), start.Add(2*time.Minute))
	if len(one) != 1 {
		t.Fatalf("range filter: %d", len(one))
	}
	cur, ok := e.s.Current()
	if !ok || !cur.At.Equal(e.now) {
		t.Fatalf("current: %v %v", ok, cur.At)
	}
}

func TestRetentionPrunesAtSetting(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if d, err := e.st.Duration(ctx, KeyRetention, ""); err != nil || d != 168*time.Hour {
		t.Fatalf("retention default = %v, %v", d, err)
	}
	// The settings store clock is real, so write one old and one current row.
	now := time.Now()
	e.now = now.Add(-8 * 24 * time.Hour)
	e.sample(t)
	e.now = now
	e.sample(t)
	pr, err := e.st.PruneOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var rows int64
	for _, p := range pr {
		if p.Table == "hst_metric_minute" {
			rows = p.Rows
		}
	}
	if rows != 1 {
		t.Fatalf("pruned %d, want 1 (%v)", rows, pr)
	}
	got, _ := e.s.GetRange(ctx, now.Add(-30*24*time.Hour), now.Add(time.Hour))
	if len(got) != 1 {
		t.Fatalf("rows left = %d", len(got))
	}
}

func TestGauges(t *testing.T) {
	e := newEnv(t)
	e.sample(t)
	mem := e.tel.gauges["hostmetrics_memory_bytes"]
	if v, ok := mem.get("host_total", "host_total"); !ok || v != float64(16*gib) {
		t.Fatalf("host_total gauge = %v %v", v, ok)
	}
	if _, ok := mem.get("ollama", "process_rss"); !ok {
		t.Fatal("ollama gauge missing")
	}
	if _, ok := mem.get("loaded_accelerator", "engine_loaded"); !ok {
		t.Fatal("loaded_accelerator gauge missing")
	}
	if _, ok := mem.get("other", "derived"); !ok {
		t.Fatal("other gauge missing")
	}
	if _, ok := e.tel.gauges["hostmetrics_cpu_percent"].get("slice", "derived"); !ok {
		t.Fatal("slice cpu gauge missing")
	}
}

func TestRunHonoursInterval(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.set(t, KeySampleInterval, "0s")
	done := make(chan struct{})
	go func() { e.s.Run(ctx); close(done) }()
	time.Sleep(100 * time.Millisecond)
	if _, ok := e.s.Current(); ok {
		t.Fatal("sampled while disabled")
	}
	cancel()
	<-done

	// Enabled: samples at once and keeps going.
	ctx2, cancel2 := context.WithCancel(context.Background())
	e.set(t, KeySampleInterval, "5ms")
	done = make(chan struct{})
	go func() { e.s.Run(ctx2); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := e.s.Current(); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no sample")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel2()
	<-done
}

func TestDefs(t *testing.T) {
	e := newEnv(t)
	d, ok := e.st.Definition(KeySampleInterval)
	if !ok || d.Default != "15s" || d.Owner != settings.Host {
		t.Fatalf("sample interval def: %+v", d)
	}
}

var _ LoadedLister = (*fake.Engine)(nil)

// The real probe reads this machine: the smoke test only checks what any machine satisfies.
func TestRealProbeSmoke(t *testing.T) {
	ctx := context.Background()
	r := newReader(newGopsutilProbe(), nil, int32(os.Getpid()), nil, func(context.Context) ([]string, error) { return []string{"ollama"}, nil })
	got, err := r.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host.MemoryTotal.Value <= 0 || got.Host.CPUCores < 1 {
		t.Fatalf("host totals: %+v", got.Host)
	}
	if got.Slice.Memory.Value > got.Host.MemoryTotal.Value || got.Other.Memory.Value < 0 {
		t.Fatalf("slice %d total %d other %d", got.Slice.Memory.Value, got.Host.MemoryTotal.Value, got.Other.Memory.Value)
	}
}

func TestSliceMemoryNoDoubleCount(t *testing.T) {
	const gib = int64(1 << 30)
	for _, c := range []struct {
		name                      string
		ls, rss, size, vram, want int64
	}{
		{"weights inside Ollama RSS", gib, 5 * gib, 4 * gib, 0, 6 * gib},
		{"weights outside Ollama RSS", gib, gib, 4 * gib, 0, 5 * gib},
		{"model on the accelerator", gib, 2 * gib, 4 * gib, 4 * gib, 3 * gib},
		{"partly offloaded", gib, gib, 4 * gib, 3 * gib, 2 * gib},
		{"accelerator above size", gib, gib, gib, 2 * gib, 2 * gib},
	} {
		if got := sliceMemory(c.ls, c.rss, c.size, c.vram); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestUnloadedModelSeriesDeleted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.eng.Load(ctx, "m1", time.Minute); err != nil {
		t.Fatal(err)
	}
	e.sample(t)
	g := e.tel.gauges["hostmetrics_loaded_model_bytes"]
	if v, ok := g.get("m1"); !ok || v != float64(gib) {
		t.Fatalf("loaded model series = %v, %v; want %d", v, ok, gib)
	}
	if err := e.eng.Unload(ctx, "m1"); err != nil {
		t.Fatal(err)
	}
	e.sample(t)
	if _, ok := g.get("m1"); ok {
		t.Fatal("series of an unloaded model must be deleted")
	}
	// The engine failing deletes the series of a model that was loaded: a stale size is a guess.
	if err := e.eng.Load(ctx, "m1", time.Minute); err != nil {
		t.Fatal(err)
	}
	e.sample(t)
	e.eng.InjectError(fake.OpLoaded, errors.New("down"), 1)
	e.sample(t)
	if _, ok := g.get("m1"); ok {
		t.Fatal("series must be deleted while the engine cannot be asked")
	}
}
