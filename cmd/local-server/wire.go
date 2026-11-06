package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/inCoderKaka/local-server/internal/admin"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/gateway"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/hostmetrics"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/jobs"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/ledger/checkpoint"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/policy"
	"github.com/inCoderKaka/local-server/internal/power"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/telemetry"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// services are the built modules serve runs.
type services struct {
	// consumer is the consumer API (/v1).
	consumer http.Handler
	// admin is the whole admin listener: /admin/v1 and /metrics, behind the credential guard
	// (PLT-F-024).
	admin http.Handler

	st         *settings.Store
	sched      *scheduler.Scheduler
	sampler    *hostmetrics.Sampler
	checkpoint *checkpoint.Worker
	jobs       *jobs.Service
	jobExec    jobs.Executor
	rollups    *usage.Rollups
	power      *power.Keeper
}

// wire builds every module on d, keys and eng, in dependency order, and returns what serve
// runs; metrics is telemetry's /metrics handler, which the admin listener serves; pw is the OS
// sleep lock (the real one for the build's OS, or a fake in tests). Constructors
// register their own settings; registerSettings did the rest. To add a
// module that serves or runs a worker, build it here and add its worker in startWorkers.
func wire(d *database, keys *hostkeys.Keys, eng engine.Engine, tel telemetry.Telemetry, metrics http.Handler, pw power.Power) (*services, error) {
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
	// Holds the sleep lock while the effective sharing state is sharing (PLT-F-042).
	if sv.power, err = power.NewKeeper(pw, share, st, tel); err != nil {
		return nil, err
	}
	cps, err := checkpoint.New(db, keys, st)
	if err != nil {
		return nil, err
	}
	sv.checkpoint = checkpoint.NewWorker(cps, tel)

	if sv.jobs, err = jobs.New(jobs.Config{DB: db, Settings: st, Events: bus, Keys: keys, Telemetry: tel}); err != nil {
		return nil, err
	}

	// Adds usage records to the minute, hour and day rollups the usage endpoint reads (F-023).
	if sv.rollups, err = usage.NewRollups(db, st, tel); err != nil {
		return nil, err
	}

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
		Scheduler: sv.sched, Sharing: share, Engine: eng, Ledger: ledger.New(db, keys), Checkpoints: cps, Jobs: sv.jobs, Telemetry: tel})
	if err != nil {
		return nil, fmt.Errorf("gateway: %w", err)
	}
	sv.consumer, sv.jobExec = gw.Handler(), gw.JobExecutor()

	// The admin API shares the settings, the sharing state, the sampler, the auth service and the
	// event bus built above; it reads the host kid from the keys and the usage rollups from the
	// usage module (req_001 D-104).
	auditLog, err := audit.NewLog(st)
	if err != nil {
		return nil, err
	}
	adm, err := admin.New(admin.Config{DB: db, Settings: st, Identity: ir, Grants: gr, Auth: au, Events: bus, Audit: auditLog, Sharing: share, Sampler: sv.sampler,
		Keys: keys, Engine: eng, Metrics: metrics, Version: version, Telemetry: tel})
	if err != nil {
		return nil, fmt.Errorf("admin: %w", err)
	}
	sv.admin = adm.Handler()
	return sv, nil
}

// failInterrupted marks the jobs a stop interrupted failed(host_restart) (req_001 D-38, D-54).
// serve calls it only after both listeners are bound, so a start that fails (port taken by a
// running instance) never touches that instance's running jobs.
func (sv *services) failInterrupted(ctx context.Context, log *slog.Logger) error {
	n, err := sv.jobs.FailInterrupted(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		log.WarnContext(ctx, "jobs interrupted by the last stop were marked failed (host_restart); clients resubmit with their Idempotency-Key", "jobs", n)
	}
	return nil
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
	// Runs queued jobs through the gateway's admission, engine call and metering (F2).
	sup.Go(ctx, "jobs", func(ctx context.Context) error { sv.jobs.Run(ctx, sv.jobExec); return nil })
	// Adds new usage records to the usage rollups behind GET /v1/usage (PLT-F-023).
	sup.Go(ctx, "usage-rollups", func(ctx context.Context) error { sv.rollups.Run(ctx); return nil })
	// Takes the sleep lock while sharing and releases it when paused, in quiet hours and on stop
	// (PLT-F-042).
	sup.Go(ctx, "power", sv.power.Run)
}
