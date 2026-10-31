package hostmetrics

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine"
)

// Measure says how a value was obtained, so a reader can tell a figure the OS reported from one
// worked out from others (D-31).
type Measure string

// The ways a value is measured.
const (
	// MeasureHostTotal is a figure the OS reports for the whole machine. Memory in use is the OS's
	// own count; on macOS it differs from Activity Monitor (compressed and cached memory are
	// accounted differently).
	MeasureHostTotal Measure = "host_total"
	// MeasureProcessRSS is the resident memory (or CPU time) the OS reports for one process. It
	// includes shared pages, so processes can add up to more than the host used.
	MeasureProcessRSS Measure = "process_rss"
	// MeasureEngineLoaded is the memory the engine says its loaded models take (Ollama /api/ps).
	MeasureEngineLoaded Measure = "engine_loaded"
	// MeasureDerived is worked out from other values (the slice sum, or host minus slice).
	MeasureDerived Measure = "derived"
	// MeasureUnavailable means the value could not be read; it is 0 and must not be trusted.
	MeasureUnavailable Measure = "unavailable"
)

// Bytes is a memory figure and how it was measured.
type Bytes struct {
	Value   int64
	Measure Measure
}

// Percent is a CPU figure and how it was measured. Host, slice and other values are shares of the
// whole host, 0 to 100, whatever the core count.
type Percent struct {
	Value   float64
	Measure Measure
}

// Accelerator is the host's GPU or other accelerator memory. It needs vendor tools or cgo, so it
// is reported as unavailable with the reason; the memory of loaded models on the accelerator is
// in Slice.LoadedAccelerator.
type Accelerator struct {
	Measure Measure
	Reason  string
}

// HostTotals are figures for the whole machine.
type HostTotals struct {
	MemoryTotal Bytes
	MemoryUsed  Bytes
	// CPUCores is 0 when the OS did not report it; per-process CPU is then unavailable.
	CPUCores    int
	CPU         Percent
	Accelerator Accelerator
}

// Slice is the share-budget slice: local-server, Ollama and the loaded models.
type Slice struct {
	LocalServerMemory Bytes
	OllamaMemory      Bytes
	// LoadedModelsMemory and LoadedAccelerator come from the engine's Loaded list. They are
	// unavailable when the engine cannot be asked (Ollama down).
	LoadedModelsMemory Bytes
	LoadedAccelerator  Bytes
	LocalServerCPU     Percent
	OllamaCPU          Percent
	// Memory is local-server's RSS plus the larger of Ollama's RSS and the loaded models' host
	// RAM (their size minus their accelerator memory), so a model already in Ollama's RSS is not
	// counted twice (D-91). CPU is the sum. Neither is above the host total; Clamped says they
	// were cut down.
	Memory  Bytes
	CPU     Percent
	Clamped bool
}

// Other is host use outside the slice (the glossary's "Other apps").
type Other struct {
	Memory Bytes
	CPU    Percent
}

// Reading is one sample of the host.
type Reading struct {
	At    time.Time
	Host  HostTotals
	Slice Slice
	Other Other
	// Unattributed counts processes the OS would not let local-server read (access denied on
	// Windows, silent zeros on macOS). Their use is inside Other; it is never guessed.
	Unattributed int
}

// HostMetrics is the port: it takes one reading of the host now.
type HostMetrics interface {
	Read(ctx context.Context) (Reading, error)
}

// LoadedLister is the part of the engine port the reading needs.
type LoadedLister interface {
	Loaded(ctx context.Context) ([]engine.LoadedModel, error)
}

// Clock returns the current time. Tests pass a fixed one.
type Clock func() time.Time

// Reader implements HostMetrics on a probe of the OS and the engine's loaded models.
type Reader struct {
	mu     sync.Mutex
	probe  probe
	loaded LoadedLister
	pid    int32
	clock  Clock
	// ollamaNames returns the process names that count as Ollama (the host.metrics.ollama_process_names
	// setting, read at each reading).
	ollamaNames func(context.Context) ([]string, error)
}

// newReader builds a Reader on any probe; tests pass a fake.
func newReader(p probe, loaded LoadedLister, pid int32, clock Clock, ollamaNames func(context.Context) ([]string, error)) *Reader {
	if clock == nil {
		clock = time.Now
	}
	return &Reader{probe: p, loaded: loaded, pid: pid, clock: clock, ollamaNames: ollamaNames}
}

// Read implements HostMetrics. It fails only when the host memory cannot be read; every other
// gap is reported in the reading as unavailable. Read is safe for concurrent use; calls are
// serialised because each one advances the CPU windows.
func (r *Reader) Read(ctx context.Context) (Reading, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	total, used, err := r.probe.Memory(ctx)
	if err != nil {
		return Reading{}, err
	}
	cores, coresErr := r.probe.CPUCores(ctx)
	coresKnown := coresErr == nil && cores >= 1
	if !coresKnown {
		cores = 0
	}
	out := Reading{At: r.clock()}
	out.Host = HostTotals{
		MemoryTotal: Bytes{int64(total), MeasureHostTotal},
		MemoryUsed:  Bytes{int64(used), MeasureHostTotal},
		CPUCores:    cores,
		CPU:         Percent{Measure: MeasureUnavailable},
		Accelerator: Accelerator{Measure: MeasureUnavailable, Reason: "no vendor-neutral accelerator reader without cgo or elevated rights"},
	}
	if pct, err := r.probe.CPUPercent(ctx); err == nil {
		out.Host.CPU = Percent{clampPct(pct), MeasureHostTotal}
	}

	names, _ := r.ollamaNames(ctx)
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[normalName(n)] = true
	}
	procs, listErr := r.probe.Processes(ctx)
	var ollamaPIDs []int32
	for _, p := range procs {
		switch {
		case !p.Readable:
			out.Unattributed++
		case p.PID != r.pid && want[normalName(p.Name)]:
			ollamaPIDs = append(ollamaPIDs, p.PID)
		}
	}

	s := &out.Slice
	s.LocalServerMemory, s.LocalServerCPU = Bytes{Measure: MeasureUnavailable}, Percent{Measure: MeasureUnavailable}
	if u, err := r.probe.Usage(ctx, r.pid); err == nil && u.RSS > 0 {
		s.LocalServerMemory = Bytes{int64(u.RSS), MeasureProcessRSS}
		if coresKnown && !u.CPUUnavailable {
			s.LocalServerCPU = Percent{clampPct(u.CPU / float64(cores)), MeasureProcessRSS}
		}
	} else {
		out.Unattributed++
	}

	s.OllamaMemory, s.OllamaCPU = Bytes{Measure: MeasureUnavailable}, Percent{Measure: MeasureUnavailable}
	if listErr == nil && len(ollamaPIDs) > 0 {
		var rss uint64
		var cpu float64
		ok := false
		cpuOK := coresKnown
		for _, pid := range ollamaPIDs {
			u, err := r.probe.Usage(ctx, pid)
			if err != nil || u.RSS == 0 {
				out.Unattributed++
				continue
			}
			ok = true
			rss += u.RSS
			cpu += u.CPU
			if u.CPUUnavailable {
				cpuOK = false
			}
		}
		if ok {
			s.OllamaMemory = Bytes{int64(rss), MeasureProcessRSS}
			if cpuOK {
				s.OllamaCPU = Percent{clampPct(cpu / float64(cores)), MeasureProcessRSS}
			}
		}
	}

	s.LoadedModelsMemory, s.LoadedAccelerator = Bytes{Measure: MeasureUnavailable}, Bytes{Measure: MeasureUnavailable}
	if r.loaded != nil {
		if models, err := r.loaded.Loaded(ctx); err == nil {
			var size, vram int64
			for _, m := range models {
				size += m.SizeBytes
				vram += m.VRAMBytes
			}
			s.LoadedModelsMemory = Bytes{size, MeasureEngineLoaded}
			s.LoadedAccelerator = Bytes{vram, MeasureEngineLoaded}
		}
	}

	sliceMem := sliceMemory(s.LocalServerMemory.Value, s.OllamaMemory.Value, s.LoadedModelsMemory.Value, s.LoadedAccelerator.Value)
	if sliceMem > out.Host.MemoryTotal.Value {
		sliceMem, s.Clamped = out.Host.MemoryTotal.Value, true
	}
	s.Memory = Bytes{sliceMem, MeasureDerived}
	sliceCPU := s.LocalServerCPU.Value + s.OllamaCPU.Value
	if out.Host.CPU.Measure == MeasureHostTotal && sliceCPU > out.Host.CPU.Value {
		// The process and host CPU windows differ, so a small overshoot is normal.
		sliceCPU, s.Clamped = out.Host.CPU.Value, true
	} else if sliceCPU > 100 {
		sliceCPU, s.Clamped = 100, true
	}
	s.CPU = Percent{sliceCPU, MeasureDerived}

	// Other apps are what the host uses beyond the slice. Free memory is not "other apps", so the
	// base is memory in use, not the capacity.
	out.Other.Memory = Bytes{max(out.Host.MemoryUsed.Value-sliceMem, 0), MeasureDerived}
	out.Other.CPU = Percent{Measure: MeasureUnavailable}
	if out.Host.CPU.Measure == MeasureHostTotal {
		out.Other.CPU = Percent{max(out.Host.CPU.Value-sliceCPU, 0), MeasureDerived}
	}
	return out, nil
}

func clampPct(v float64) float64 { return min(max(v, 0), 100) }

// normalName lowercases a process name and drops a Windows ".exe" suffix.
func normalName(n string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(n)), ".exe")
}

// sliceMemory is local-server's RSS plus the larger of Ollama's RSS and the loaded models' host
// RAM. Model weights may or may not sit in Ollama's RSS (mmap, CPU inference), so taking the
// larger never counts them twice and never reports less than either measure (D-91).
func sliceMemory(localServer, ollamaRSS, modelsSize, modelsAccelerator int64) int64 {
	modelsRAM := max(modelsSize-modelsAccelerator, 0)
	return localServer + max(ollamaRSS, modelsRAM)
}
