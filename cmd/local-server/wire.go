package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/admin"
	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/consumerauth"
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
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot"
	"github.com/inCoderKaka/local-server/internal/policy"
	"github.com/inCoderKaka/local-server/internal/power"
	"github.com/inCoderKaka/local-server/internal/requests"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/telemetry"
	"github.com/inCoderKaka/local-server/internal/usage"
	"github.com/inCoderKaka/local-server/internal/webui"
	adminui "github.com/inCoderKaka/local-server/web/admin"
	consumerui "github.com/inCoderKaka/local-server/web/consumer"
)

// services are the built modules serve runs.
type services struct {
	// consumer is the consumer API (/v1).
	consumer http.Handler
	// consumerPage is the consumer page listener's handler: the page and its /v1 account routes,
	// the same origin (req_001 D-148).
	consumerPage http.Handler
	// admin is the whole admin listener: /admin/v1, /metrics and the admin UI, behind the
	// credential guard (PLT-F-024, PLT-F-032).
	admin http.Handler

	st         *settings.Store
	sched      *scheduler.Scheduler
	sampler    *hostmetrics.Sampler
	checkpoint *checkpoint.Worker
	jobs       *jobs.Service
	jobExec    jobs.Executor
	rollups    *usage.Rollups
	power      *power.Keeper
	// rules switches off policy rules that failed too many times (PLT-F-026).
	rules func(ctx context.Context)
	// expireRequests expires the open resource requests older than their time-to-live (PLT-F-028).
	expireRequests func(ctx context.Context)
	// purge removes the data of consumers deleted longer ago than the retention (PLT-F-097).
	purge func(ctx context.Context)
	// backup takes the periodic off-host snapshots (PLT-F-044).
	backup *snapshot.Runner
}

// wire builds every module on d, keys and eng, in dependency order, and returns what serve
// runs; metrics is telemetry's /metrics handler, which the admin listener serves; pw is the OS
// sleep lock (the real one for the build's OS, or a fake in tests); tn is the tailnet's transport
// and report. Constructors
// register their own settings; registerSettings did the rest. To add a
// module that serves or runs a worker, build it here and add its worker in startWorkers.
func wire(d *database, keys *hostkeys.Keys, eng engine.Engine, tel telemetry.Telemetry, metrics http.Handler, pw power.Power, tn tailnetWiring) (*services, error) {
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

	authCfg := auth.Config{DB: db, Identity: ir, Grants: gr, Events: bus, Settings: st, Logger: log}
	var nodes consumers.NodeResolver
	if tn.Transport != nil {
		authCfg.WhoIs = nodeOf{tr: tn.Transport, timeout: tn.WhoIsTimeout} // checks a key bound to a node (req_001 D-159)
		nodes = boundedResolver{tr: tn.Transport, timeout: tn.StatusTimeout}
	}
	au, err := auth.New(authCfg)
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

	// Resource requests (PLT-F-028): the consumer routes (page and consumer API) and the admin
	// routes serve this one service; an approval makes its grant version through the admin API's
	// grant operations in the same unit of work.
	rq, err := requests.New(requests.Config{DB: db, Settings: st, Identity: ir, Events: bus, Logger: log})
	if err != nil {
		return nil, fmt.Errorf("requests: %w", err)
	}
	sv.expireRequests = rq.RunSweeper

	// Consumer accounts (PLT-F-027): sign-up, login, sessions and the /v1/me routes. The page
	// listener serves them with the session cookie; the consumer API serves the /v1/me routes that
	// take a key through the same service (req_001 D-39, D-148). Keys and limits reuse the admin
	// API's mapping, which is built below, so they are looked up when a request needs them.
	var adm *admin.Admin
	acct, err := consumerauth.New(consumerauth.Config{DB: db, Settings: st, Identity: ir, Grants: gr, Telemetry: tel, Requests: rq,
		Keys: func(ctx context.Context, id uuid.UUID) (*api.AccessKeyList, error) {
			return adm.AdminListAccessKeys(ctx, api.AdminListAccessKeysParams{ConsumerID: id})
		},
		Limits: func(ctx context.Context, id uuid.UUID) (*api.GrantVersion, error) {
			return adm.AdminGetGrant(ctx, api.AdminGetGrantParams{ConsumerID: id})
		},
		LimitBody: lim.LimitBody,
		// The page listener answers to the host's own tailnet names, like the remote admin listener
		// (admin.tailnet_name names the same host).
		HostNames: func(ctx context.Context) ([]string, error) {
			n, err := st.String(ctx, admin.KeyTailnetName, "")
			if err != nil {
				return nil, err
			}
			names := tn.Hosts
			if n != "" {
				names = append(slices.Clone(names), n)
			}
			return names, nil
		},
		// The built consumer web page, or its stub when the binary was built without npm
		// (PLT-F-036, req_001 D-152), for GET and HEAD outside /v1.
		UI: webui.Handler(webui.Config{Files: consumerui.Files()})})
	if err != nil {
		return nil, fmt.Errorf("consumerauth: %w", err)
	}
	gw, err := gateway.New(gateway.Config{DB: db, Settings: st, Events: bus, Grants: gr, Auth: au, Limits: lim, Policy: pol, Account: acct,
		Scheduler: sv.sched, Sharing: share, Engine: eng, Ledger: ledger.New(db, keys), Checkpoints: cps, Jobs: sv.jobs, Telemetry: tel})
	if err != nil {
		return nil, fmt.Errorf("gateway: %w", err)
	}
	sv.consumer, sv.jobExec = accessKeysOnly(gw.Handler()), gw.JobExecutor()
	sv.consumerPage = acct.Handler()

	// The admin API shares the settings, the sharing state, the sampler, the auth service and the
	// event bus built above; it reads the host kid from the keys and the usage rollups from the
	// usage module (req_001 D-104).
	auditLog, err := audit.NewLog(st)
	if err != nil {
		return nil, err
	}
	adm, err = admin.New(admin.Config{DB: db, Settings: st, Identity: ir, Grants: gr, Auth: au, Events: bus, Requests: rq, Audit: auditLog, Sharing: share, Sampler: sv.sampler,
		Keys: keys, Engine: eng, RuleFailures: gw, Metrics: metrics, Version: version, Telemetry: tel,
		Nodes: nodes, TailnetHosts: tn.Hosts, Tailnet: tn.Report,
		// The built admin UI, or its stub when the binary was built without npm (req_001 D-152).
		// The same handler serves the localhost and the tailnet admin listener.
		UI: webui.Handler(webui.Config{Files: adminui.Files()})})
	if err != nil {
		return nil, fmt.Errorf("admin: %w", err)
	}
	sv.admin, sv.rules, sv.purge = adm.Handler(), adm.RunRuleSweeper, adm.RunConsumerPurge

	// Off-host backups: the snapshot pipeline with the gzip compressor and, through backupTargets,
	// the local-folder and S3-compatible adapters (req_001 D-41, D-103). It raises backup.failed
	// on the event bus (D-71).
	if sv.backup, err = snapshot.NewRunner(snapshot.RunnerConfig{DB: db, Settings: st, Keys: keys, Events: bus, Log: log,
		Targets: backupTargets, Compressor: backupCompressor()}); err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}
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
	// Switches off a policy rule without deny that failed policy.rule.max_failures times (PLT-F-026).
	sup.Go(ctx, "policy-rule-sweeper", func(ctx context.Context) error { sv.rules(ctx); return nil })
	// Expires the open resource requests older than requests.ttl, every requests.sweep_interval
	// (PLT-F-028).
	sup.Go(ctx, "requests-expiry", func(ctx context.Context) error { sv.expireRequests(ctx); return nil })
	// Purges consumers deleted longer ago than consumers.deleted_retention (PLT-F-097).
	sup.Go(ctx, "consumer-purge", func(ctx context.Context) error { sv.purge(ctx); return nil })
	// Sends an encrypted snapshot of the database to the host's folder or bucket every
	// backup.interval while backup.enabled is true (PLT-F-044).
	sup.Go(ctx, "backup", func(ctx context.Context) error { sv.backup.Run(ctx); return nil })
}
