package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/engine/fake"
	"github.com/inCoderKaka/local-server/internal/engine/ollama"
	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/power"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/telemetry/slogprom"
)

// Engine kinds of serve --engine.
const (
	engineOllama = "ollama"
	engineFake   = "fake"
)

// errFirstRunNeedsTerminal is why a first run with no terminal stops (req_001 D-101).
var errFirstRunNeedsTerminal = errors.New("this is the first run, and it must show the one-time recovery code on a terminal, but stderr is not one; " +
	"run `local-server serve` once in a terminal (no host and no key were created); after that it can run under a service manager")

func (a *app) serve(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	dataDirFlag := fs.String("data-dir", "", "data directory (default: $LS_DATA_DIR, else local-server in the user configuration directory)")
	engineKind := fs.String("engine", engineOllama, "engine to share: ollama, or fake for local runs and demos")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 || (*engineKind != engineOllama && *engineKind != engineFake) {
		fmt.Fprintln(a.stderr, "local-server serve: --engine must be ollama or fake, and no arguments are accepted")
		return 2
	}
	dir, err := a.dataDir(*dataDirFlag)
	if err != nil {
		fmt.Fprintf(a.stderr, "local-server: %v\n", err)
		return 1
	}
	if err := a.runServe(ctx, dir, *engineKind); err != nil {
		fmt.Fprintf(a.stderr, "local-server: %v\n", err)
		return 1
	}
	return 0
}

// runServe is the startup order of req_001 D-39 F9: data directory, database and migrations,
// settings, keys, checks, listeners, workers; and the reverse on shutdown.
func (a *app) runServe(ctx context.Context, dataDir, engineKind string) (err error) {
	// Before the database opens, there are no log settings: plain stderr.
	boot := slog.New(slog.NewTextHandler(a.stderr, nil))
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create the data directory: %w", err)
	}
	boot.Info("opening the database", "data_dir", dataDir)
	d, err := openDatabase(ctx, filepath.Join(dataDir, dbFileName))
	if err != nil {
		return fmt.Errorf("open the database: %w", err)
	}
	defer d.db.Close() // last: workers and servers are done with it by then

	cfg, err := loadStartup(ctx, d.st)
	if err != nil {
		return err
	}
	// From here logs go to the rotating file in the user log folder (the data directory's logs
	// folder when there is none), with the level and rotation from the settings (req_001 D-40).
	logDir, logDirErr := a.userLogDir()
	if logDirErr != nil {
		boot.Warn("cannot find the user log folder; logging inside the data directory", "error", logDirErr)
		logDir = filepath.Join(dataDir, fallbackLogDir)
	}
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return fmt.Errorf("create the log folder: %w", err)
	}
	logPath := filepath.Join(logDir, logFileName)
	boot.Info("log file", "path", logPath)
	tel, err := slogprom.New(slogprom.Config{
		Level: cfg.LogLevel,
		File: slogprom.FileConfig{Path: logPath,
			MaxSizeMB: cfg.LogMaxSizeMB, MaxAgeDays: cfg.LogMaxAgeDays},
		SinkErrorInterval: sinkErrorInterval(d),
		Stderr:            a.stderr,
	})
	if err != nil {
		return err
	}
	defer tel.Close()
	log := tel.Logger()

	keys, err := a.openKeys(ctx, d, cfg, dataDir)
	if err != nil {
		return err
	}

	var eng engine.Engine
	switch engineKind {
	case engineFake:
		eng = fake.New()
	default:
		ad := ollama.New(ollamaConfig(cfg))
		defer ad.Close()
		if err := a.checkOllama(ctx, ad, cfg, log); err != nil {
			return err
		}
		eng = ad
	}

	pw := a.power
	if pw == nil {
		pw = power.New(log)
	}
	sv, err := wire(d, keys, eng, tel, tel.MetricsHandler(), pw)
	if err != nil {
		return fmt.Errorf("wire modules: %w", err)
	}

	consumerLn, err := net.Listen("tcp", cfg.ConsumerListen)
	if err != nil {
		return fmt.Errorf("listen on %s (setting %s): %w", cfg.ConsumerListen, keyConsumerListen, err)
	}
	adminLn, err := net.Listen("tcp", cfg.AdminListen)
	if err != nil {
		consumerLn.Close()
		return fmt.Errorf("listen on %s (setting %s): %w", cfg.AdminListen, keyAdminListen, err)
	}
	// Only now, with both ports held: a start that cannot bind must not fail the running jobs of
	// the instance that holds them.
	if err := sv.failInterrupted(ctx, log); err != nil {
		consumerLn.Close()
		adminLn.Close()
		return fmt.Errorf("mark interrupted jobs: %w", err)
	}
	httpLog := slog.NewLogLogger(log.Handler(), slog.LevelWarn)
	tracker := newInflight()
	consumer := &http.Server{Handler: tracker.wrap(sv.consumer), ReadHeaderTimeout: cfg.ReadHeaderTimeout, IdleTimeout: cfg.IdleTimeout, ErrorLog: httpLog}
	admin := &http.Server{Handler: sv.admin, ReadHeaderTimeout: cfg.ReadHeaderTimeout, IdleTimeout: cfg.IdleTimeout, ErrorLog: httpLog}

	serveErr := make(chan error, 2)
	for _, s := range []struct {
		srv *http.Server
		ln  net.Listener
	}{{consumer, consumerLn}, {admin, adminLn}} {
		go func() {
			if err := s.srv.Serve(s.ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serveErr <- err
			}
		}()
	}
	fmt.Fprintf(a.stderr, "local-server %s: consumer API on http://%s, admin listener on http://%s (admin API at /admin/v1, metrics at /metrics); press Ctrl-C to stop\n",
		version, consumerLn.Addr(), adminLn.Addr())
	log.Info("listening", "consumer", consumerLn.Addr().String(), "admin", adminLn.Addr().String(), "engine", engineKind)
	if a.onListening != nil {
		a.onListening(listening{Consumer: consumerLn.Addr().String(), Admin: adminLn.Addr().String()})
	}

	// Workers outlive the servers: they stop only after the requests in flight have finished, or
	// the wait for the force-closed ones has run out.
	workersCtx, stopWorkers := context.WithCancel(context.WithoutCancel(ctx))
	sup := newSupervisor(tel, func() time.Duration { return cfg.RestartBackoff })
	sv.startWorkers(workersCtx, sup, log)

	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err = <-serveErr:
		log.Error("listener failed; shutting down", "error", err)
		err = fmt.Errorf("listener failed: %w", err)
	}
	// Graceful: stop accepting, let requests in flight finish for the setting-owned time, then
	// close what is left; closing does not stop their handlers, so wait for them to record their
	// usage for up to the same time again; then the workers; the deferred calls close telemetry
	// and the database.
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for _, s := range []*http.Server{consumer, admin} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if serr := s.Shutdown(sctx); serr != nil {
				s.Close()
				log.Warn("shutdown timed out; connections closed", "error", serr)
			}
		}()
	}
	wg.Wait()
	if drained, remaining := tracker.wait(cfg.ShutdownTimeout); !drained {
		log.Warn("requests still running after shutdown; stopping anyway", "remaining", remaining)
	}
	stopWorkers()
	sup.Wait()
	return err
}

// sinkErrorInterval reads telemetry.log_sink_error_interval through the settings store, so a
// change applies from the next stderr line (req_001 D-82). It falls back to slogprom's default
// when the read fails. It must not log (slogprom holds its handler lock while calling it).
func sinkErrorInterval(d *database) func() time.Duration {
	return func() time.Duration {
		v, err := d.st.Duration(context.Background(), settings.KeyLogSinkErrorInterval, "")
		if err != nil || v <= 0 {
			return slogprom.DefaultSinkErrorInterval
		}
		return v
	}
}

// openKeys loads the master key and opens key custody. A first run is judged from the host rows
// in the database, never from a missing key file (req_001 D-102). On a first run the recovery
// code is printed once, straight to stderr and never through the logger, and only when stderr is
// a terminal; with no terminal the first run stops before the master key or any host row exists
// (req_001 D-101).
func (a *app) openKeys(ctx context.Context, d *database, cfg startupConfig, dataDir string) (*hostkeys.Keys, error) {
	done, err := hostkeys.Bootstrapped(ctx, d.db)
	if err != nil {
		return nil, fmt.Errorf("read host state: %w", err)
	}
	first := !done
	if first && !a.stderrIsTerminal() {
		return nil, errFirstRunNeedsTerminal
	}
	master, src, err := hostkeys.LoadMasterKey(hostkeys.MasterKeyConfig{
		FallbackPath: filepath.Join(dataDir, masterKeyFileName),
		Create:       first,
		Timeout:      cfg.KeychainTimeout,
		Keychain:     a.keychain,
	})
	if err != nil {
		return nil, err
	}
	if first && src == hostkeys.SourceFile {
		fmt.Fprintf(a.stderr, "local-server: the OS keychain could not be used, so the host master key is kept in the file %s, readable only by you\n", filepath.Join(dataDir, masterKeyFileName))
	}
	keys, code, openErr := hostkeys.Open(ctx, d.db, d.st, master)
	// A code comes back only once the host rows are committed, so it is shown even when loading
	// them then failed: it is not offered again.
	if code != "" {
		if _, err := fmt.Fprintf(a.stderr, recoveryCodeNotice, code); err != nil {
			return nil, fmt.Errorf("show the recovery code: %w", err)
		}
	}
	if openErr != nil {
		return nil, fmt.Errorf("open host keys: %w", openErr)
	}
	return keys, nil
}

const recoveryCodeNotice = `
local-server: first run. This is your recovery code:

    %s

It restores this host's backups on this computer, or on another one where the host master key
survives. Backups do not contain the master key; it is kept in the OS keychain or in the file
master.key in the data folder, so copy master.key (or the keychain entry) somewhere safe and
private. Write the code down and keep it somewhere safe. It is not written to any log or file.

`

// checkOllama is the engine check of F9 step 4. An Ollama that cannot be reached is not fatal:
// the gateway answers engine_unavailable until it is up (req_001 D-39 F1 step 9). One that is
// older than the minimum version is, because the context-overflow guarantee of req_001 D-48
// depends on it.
func (a *app) checkOllama(ctx context.Context, ad *ollama.Adapter, cfg startupConfig, log *slog.Logger) error {
	err := ad.CheckVersion(ctx)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, engine.ErrUnsupportedVersion):
		return fmt.Errorf("%w; upgrade Ollama (setting %s is %s)", err, keyOllamaBaseURL, redactURL(cfg.OllamaURL))
	}
	fmt.Fprintf(a.stderr, "local-server: warning: cannot reach Ollama at %s (setting %s): %v; requests get engine_unavailable until it is running\n", redactURL(cfg.OllamaURL), keyOllamaBaseURL, err)
	log.Warn("ollama not reachable at startup", "url", redactURL(cfg.OllamaURL), "error", err.Error())
	return nil
}
