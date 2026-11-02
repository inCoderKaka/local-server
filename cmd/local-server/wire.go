package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/gateway"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/hostmetrics"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/ledger/checkpoint"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/policy"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// services are the built modules serve runs.
type services struct {
	// consumer is the consumer API (/v1).
	consumer http.Handler

	st         *settings.Store
	sched      *scheduler.Scheduler
	sampler    *hostmetrics.Sampler
	checkpoint *checkpoint.Worker
}

// wire builds every module on d, keys and eng, in dependency order, and returns what serve
// runs. Constructors register their own settings; registerSettings did the rest. To add a
// module that serves or runs a worker, build it here and add its worker in startWorkers.
func wire(d *database, keys *hostkeys.Keys, eng engine.Engine, tel telemetry.Telemetry) (*services, error) {
	var err error
	db, st, log := d.db, d.st, tel.Logger()
	bus := events.New(events.WithLogger(log))
	ir, gr := identity.NewRepos(nil), grants.NewRepos(nil)

	sv := &services{st: st}
	share, err := host.NewSharing(db, st, bus, nil) // also registers the host validators
	if err != nil {
		return nil, err
	}
	if sv.sched, err = scheduler.New(eng, st, share, scheduler.Options{Telemetry: tel}); err != nil {
		return nil, err
	}
	if sv.sampler, err = hostmetrics.NewSampler(db, st, tel, eng, nil); err != nil {
		return nil, err
	}
	cps, err := checkpoint.New(db, keys, st)
	if err != nil {
		return nil, err
	}
	sv.checkpoint = checkpoint.NewWorker(cps, tel)

	au, err := auth.New(auth.Config{DB: db, Identity: ir, Grants: gr, Events: bus, Settings: st, Logger: log})
	if err != nil {
		return nil, err
	}
	lim, err := limits.New(limits.Config{DB: db, Identity: ir, Grants: gr, Events: bus, Settings: st, Logger: log})
	if err != nil {
		return nil, err
	}
	pol, err := policy.New(st, gateway.NewRuleSource(db, gr)) // I-200
	if err != nil {
		return nil, err
	}
	gw, err := gateway.New(gateway.Config{DB: db, Settings: st, Events: bus, Grants: gr, Auth: au, Limits: lim, Policy: pol,
		Scheduler: sv.sched, Sharing: share, Engine: eng, Ledger: ledger.New(db, keys), Checkpoints: cps, Telemetry: tel})
	if err != nil {
		return nil, fmt.Errorf("gateway: %w", err)
	}
	sv.consumer = gw.Handler()
	return sv, nil
}

// startWorkers starts every background worker under sup (req_001 D-39 F9 step 5). Each is
// restarted after workers.restart_backoff if it panics, fails or returns early.
func (sv *services) startWorkers(ctx context.Context, sup *supervisor, log *slog.Logger) {
	sup.Go(ctx, "settings-retention", func(ctx context.Context) error {
		sv.st.Run(ctx, func(err error) { log.ErrorContext(ctx, "retention run failed", "error", err) })
		return nil
	})
	sup.Go(ctx, "scheduler", func(ctx context.Context) error { sv.sched.Run(ctx); return nil })
	sup.Go(ctx, "host-metrics", func(ctx context.Context) error { sv.sampler.Run(ctx); return nil })
	// Signs ledger checkpoints and rotates host signing keys (hostkeys.EnsureCurrent).
	sup.Go(ctx, "ledger-checkpoint", sv.checkpoint.Run)
}
