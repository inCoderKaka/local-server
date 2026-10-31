package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/scheduler"
)

func loadedNames(t *testing.T, e *env) map[string]bool {
	t.Helper()
	l, err := e.eng.Loaded(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, m := range l {
		out[m.Name] = true
	}
	return out
}

func TestIdleModelUnloadsAfterKeepAlive(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyKeepAlive, "5m")
	tk := e.admit(req("a", scheduler.ClassInteractive, "m1"))
	if tk.KeepAlive() != 5*time.Minute {
		t.Fatalf("ticket keep-alive = %v, want 5m", tk.KeepAlive())
	}
	e.clk.Advance(time.Hour)
	if err := e.s.Sweep(e.ctx); err != nil {
		t.Fatal(err)
	}
	if !loadedNames(t, e)["m1"] {
		t.Fatal("a model with a running request was unloaded")
	}
	tk.Release()
	e.clk.Advance(4*time.Minute + 59*time.Second)
	if err := e.s.Sweep(e.ctx); err != nil {
		t.Fatal(err)
	}
	if !loadedNames(t, e)["m1"] {
		t.Fatal("unloaded before the keep-alive passed")
	}
	e.clk.Advance(time.Second)
	if err := e.s.Sweep(e.ctx); err != nil {
		t.Fatal(err)
	}
	if loadedNames(t, e)["m1"] {
		t.Fatal("idle model still loaded after the keep-alive")
	}
}

func TestNegativeKeepAliveNeverUnloads(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyKeepAlive, "-1s")
	e.admit(req("a", scheduler.ClassInteractive, "m1")).Release()
	e.clk.Advance(1000 * time.Hour)
	if err := e.s.Sweep(e.ctx); err != nil {
		t.Fatal(err)
	}
	if !loadedNames(t, e)["m1"] {
		t.Fatal("model unloaded under a negative keep-alive")
	}
}

func TestPreloadAtStartAndOnSettingChange(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyKeepAlive, "1m")
	e.set(scheduler.KeyPreloadModels, "m1")
	if err := e.s.Apply(e.ctx); err != nil {
		t.Fatal(err)
	}
	if got := loadedNames(t, e); !got["m1"] || got["m2"] {
		t.Fatalf("after start loaded = %v, want m1", got)
	}
	// A preloaded model is kept past the keep-alive.
	e.clk.Advance(time.Hour)
	if err := e.s.Sweep(e.ctx); err != nil {
		t.Fatal(err)
	}
	if !loadedNames(t, e)["m1"] {
		t.Fatal("preloaded model was idle-unloaded")
	}
	if tk := e.admit(req("a", scheduler.ClassInteractive, "m1")); tk.KeepAlive() >= 0 {
		t.Fatalf("preloaded model keep-alive = %v, want until unloaded", tk.KeepAlive())
	} else {
		tk.Release()
	}

	// The setting changes: m2 is loaded, m1 goes back to the normal policy.
	e.set(scheduler.KeyPreloadModels, " m2 ")
	if err := e.s.Apply(e.ctx); err != nil {
		t.Fatal(err)
	}
	if got := loadedNames(t, e); !got["m1"] || !got["m2"] {
		t.Fatalf("after change loaded = %v, want m1 and m2", got)
	}
	e.clk.Advance(2 * time.Minute)
	if err := e.s.Sweep(e.ctx); err != nil {
		t.Fatal(err)
	}
	if got := loadedNames(t, e); got["m1"] || !got["m2"] {
		t.Fatalf("after sweep loaded = %v, want only m2", got)
	}
}

func TestPreloadRespectsBudget(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyMemoryBudgetBytes, "1073741824") // 1 GiB: one model
	e.set(scheduler.KeyPreloadModels, "m1,m2")
	err := e.s.Apply(e.ctx)
	if err == nil {
		t.Fatal("second preload over budget was not reported")
	}
	if got := loadedNames(t, e); !got["m1"] || got["m2"] {
		t.Fatalf("loaded = %v, want only m1", got)
	}
}

func TestRunPreloadsAndSweeps(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyPreloadModels, "m1")
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan struct{})
	go func() { e.s.Run(ctx); close(done) }()
	for i := 0; i < 2000 && !loadedNames(t, e)["m1"]; i++ {
		time.Sleep(time.Millisecond)
	}
	if !loadedNames(t, e)["m1"] {
		t.Fatal("Run did not preload at start")
	}
	e.set(scheduler.KeyPreloadModels, "m1,m2")
	time.Sleep(20 * time.Millisecond)
	e.clk.Advance(time.Minute) // the maintenance interval passes
	for i := 0; i < 2000 && !loadedNames(t, e)["m2"]; i++ {
		time.Sleep(time.Millisecond)
	}
	if !loadedNames(t, e)["m2"] {
		t.Fatal("Run did not preload on the setting change")
	}
	cancel()
	<-done
}
