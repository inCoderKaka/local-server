package hostmetrics

import (
	"context"
	"sync"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

// probe is the small view of the OS the reader needs. The gopsutil implementation is below; tests
// pass a fake, so no test but the smoke test depends on the machine.
type probe interface {
	// Memory returns the host's total and in-use memory in bytes.
	Memory(ctx context.Context) (total, used uint64, err error)
	CPUCores(ctx context.Context) (int, error)
	// CPUPercent is the CPU use of the whole host, 0 to 100.
	CPUPercent(ctx context.Context) (float64, error)
	// Processes lists the running processes by name. A process whose name the OS refused is
	// listed with Readable false.
	Processes(ctx context.Context) ([]procInfo, error)
	// Usage returns one process's resident memory and CPU. CPU is a percent of one core, so it
	// can exceed 100.
	Usage(ctx context.Context, pid int32) (procUsage, error)
}

type procInfo struct {
	PID      int32
	Name     string
	Readable bool
}

type procUsage struct {
	RSS uint64
	CPU float64
	// CPUUnavailable says the CPU figure could not be measured (the first reading of that process,
	// or the OS refused); CPU is then 0.
	CPUUnavailable bool
}

// procState is a cached Process object and whether its CPU window has been started.
type procState struct {
	p      *process.Process
	primed bool
}

// gopsutilProbe reads the OS through gopsutil, which needs no cgo on macOS, Linux or Windows.
// gopsutil measures CPU as the change since the object's last call and returns 0 on the first call,
// so the probe keeps each object and reports the first CPU figure as unavailable.
type gopsutilProbe struct {
	mu    sync.Mutex
	procs map[int32]*procState
}

func newGopsutilProbe() *gopsutilProbe { return &gopsutilProbe{procs: map[int32]*procState{}} }

func (*gopsutilProbe) Memory(ctx context.Context) (uint64, uint64, error) {
	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return 0, 0, err
	}
	return v.Total, v.Used, nil
}

func (*gopsutilProbe) CPUCores(ctx context.Context) (int, error) {
	return cpu.CountsWithContext(ctx, true)
}

func (*gopsutilProbe) CPUPercent(ctx context.Context) (float64, error) {
	p, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil || len(p) == 0 {
		return 0, err
	}
	return p[0], nil
}

func (g *gopsutilProbe) Processes(ctx context.Context) ([]procInfo, error) {
	ps, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]procInfo, 0, len(ps))
	g.mu.Lock()
	defer g.mu.Unlock()
	seen := make(map[int32]bool, len(ps))
	for _, p := range ps {
		seen[p.Pid] = true
		name, err := p.NameWithContext(ctx)
		out = append(out, procInfo{PID: p.Pid, Name: name, Readable: err == nil && name != ""})
	}
	for pid := range g.procs {
		if !seen[pid] {
			delete(g.procs, pid)
		}
	}
	return out, nil
}

func (g *gopsutilProbe) Usage(ctx context.Context, pid int32) (procUsage, error) {
	g.mu.Lock()
	st, ok := g.procs[pid]
	if !ok {
		p, err := process.NewProcessWithContext(ctx, pid)
		if err != nil {
			g.mu.Unlock()
			return procUsage{}, err
		}
		st = &procState{p: p}
		g.procs[pid] = st
	}
	wasPrimed := st.primed
	g.mu.Unlock()
	mi, err := st.p.MemoryInfoWithContext(ctx)
	if err != nil {
		return procUsage{}, err
	}
	pct, err := st.p.PercentWithContext(ctx, 0)
	if err == nil {
		g.mu.Lock()
		st.primed = true
		g.mu.Unlock()
	}
	unavailable := err != nil || !wasPrimed
	if unavailable {
		pct = 0
	}
	return procUsage{RSS: mi.RSS, CPU: pct, CPUUnavailable: unavailable}, nil
}
