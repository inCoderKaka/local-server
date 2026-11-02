package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"

	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/engine/ollama"
	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// Check statuses. Doctor exits 1 if any check is "fail".
const (
	statusOK   = "ok"
	statusWarn = "warn"
	statusFail = "fail"
)

// check is one line of the doctor report.
type check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Advice string `json:"advice,omitempty"`
}

// report is the doctor output, and the shape of `doctor --json`.
type report struct {
	Version string  `json:"version"`
	DataDir string  `json:"data_dir"`
	OK      bool    `json:"ok"`
	Checks  []check `json:"checks"`
}

func (a *app) doctor(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	dataDirFlag := fs.String("data-dir", "", "data directory (default: $LS_DATA_DIR, else local-server in the user configuration directory)")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(a.stderr, "local-server doctor: no arguments are accepted")
		return 2
	}
	dir, err := a.dataDir(*dataDirFlag)
	if err != nil {
		fmt.Fprintf(a.stderr, "local-server: %v\n", err)
		return 1
	}
	rep := a.runDoctor(ctx, dir)
	if *asJSON {
		enc := json.NewEncoder(a.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintf(a.stderr, "local-server: %v\n", err)
			return 1
		}
	} else {
		a.printReport(rep)
	}
	if !rep.OK {
		return 1
	}
	return 0
}

func (a *app) printReport(rep report) {
	fmt.Fprintf(a.stdout, "local-server %s doctor (data directory %s)\n\n", rep.Version, rep.DataDir)
	for _, c := range rep.Checks {
		fmt.Fprintf(a.stdout, "[%-4s] %s: %s\n", c.Status, c.Name, c.Detail)
		if c.Advice != "" {
			fmt.Fprintf(a.stdout, "       -> %s\n", c.Advice)
		}
	}
}

// runDoctor runs every check. It never creates or writes anything that serve owns: the database
// is opened read-only (and not at all when it does not exist yet), and the keychain is only read.
func (a *app) runDoctor(ctx context.Context, dir string) report {
	rep := report{Version: version, DataDir: dir}
	add := func(c check) { rep.Checks = append(rep.Checks, c) }

	add(checkDataDir(dir))

	// Settings: the stored values when the database can be read, else the defaults.
	st, vals, dbCheck, closeDB := doctorSettings(ctx, filepath.Join(dir, dbFileName))
	defer closeDB()
	add(dbCheck)
	cfg, err := loadStartup(ctx, vals)
	if err != nil {
		add(check{Name: "settings", Status: statusFail, Detail: err.Error(),
			Advice: "reset the setting to its default; the checks below use the defaults"})
		cfg, _ = loadStartup(ctx, defaultValues{st})
	}

	add(checkOllama(ctx, cfg))
	add(a.checkKeychain(cfg))
	add(checkPort("consumer_port", cfg.ConsumerListen, keyConsumerListen))
	add(checkPort("metrics_port", cfg.MetricsListen, keyMetricsListen))
	add(checkParallel(ctx, vals))

	rep.OK = true
	for _, c := range rep.Checks {
		if c.Status == statusFail {
			rep.OK = false
		}
	}
	return rep
}

// newStore returns a settings store with every setting doctor reads registered. With a nil db it
// can only answer definitions, not stored values.
func newStore(db *storage.DB) (*settings.Store, error) {
	st := settings.New(db)
	if err := registerSettings(st); err != nil {
		return nil, err
	}
	return st, st.Register(scheduler.Defs()...)
}

// doctorSettings opens the database read-only and returns the store over it, or the defaults
// when it is missing or unreadable, with the database check and a function that closes it.
func doctorSettings(ctx context.Context, path string) (*settings.Store, values, check, func()) {
	c := check{Name: "database"}
	defaults, err := newStore(nil)
	if err != nil {
		panic(err) // fixed definitions
	}
	fallback := defaultValues{defaults}
	ro, err := storage.OpenReadOnly(ctx, path, registryModules()...)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		c.Status, c.Detail = statusWarn, "no database yet at "+path+"; settings show their defaults"
		c.Advice = "run `local-server serve` once in a terminal to set up this host"
		return defaults, fallback, c, func() {}
	case errors.Is(err, storage.ErrSchemaMismatch):
		c.Status, c.Detail = statusFail, err.Error()
		c.Advice = "run the local-server build that created this database, or `local-server serve` with this build to migrate it"
		return defaults, fallback, c, func() {}
	case err != nil:
		c.Status, c.Detail = statusFail, err.Error()
		return defaults, fallback, c, func() {}
	}
	st, err := newStore(ro)
	if err != nil {
		ro.Close()
		c.Status, c.Detail = statusFail, err.Error()
		return defaults, fallback, c, func() {}
	}
	c.Status, c.Detail = statusOK, "schema matches this build ("+path+")"
	return st, st, c, func() { ro.Close() }
}

func checkDataDir(dir string) check {
	c := check{Name: "data_directory"}
	probe := dir
	for {
		fi, err := os.Stat(probe)
		if err == nil {
			if !fi.IsDir() {
				c.Status, c.Detail = statusFail, probe+" is not a directory"
				return c
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			c.Status, c.Detail = statusFail, err.Error()
			return c
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			c.Status, c.Detail = statusFail, "no part of "+dir+" exists"
			return c
		}
		probe = parent
	}
	f, err := os.CreateTemp(probe, ".local-server-doctor-*")
	if err != nil {
		c.Status, c.Detail = statusFail, "cannot write to "+probe+": "+err.Error()
		c.Advice = "fix its permissions, or pass --data-dir (or set LS_DATA_DIR) for a directory you own"
		return c
	}
	f.Close()
	os.Remove(f.Name())
	if probe == dir {
		c.Status, c.Detail = statusOK, dir+" is writable"
	} else {
		c.Status, c.Detail = statusOK, dir+" does not exist yet; serve creates it ("+probe+" is writable)"
	}
	return c
}

func checkOllama(ctx context.Context, cfg startupConfig) check {
	c := check{Name: "ollama"}
	ad := ollama.New(ollama.Config{BaseURL: cfg.OllamaURL})
	defer ad.Close()
	got, err := ad.Version(ctx)
	if err != nil {
		c.Status, c.Detail = statusFail, fmt.Sprintf("cannot reach Ollama at %s: %v", cfg.OllamaURL, err)
		c.Advice = "start Ollama, or set " + keyOllamaBaseURL + " to its address"
		return c
	}
	if err := ad.CheckVersion(ctx); err != nil {
		if errors.Is(err, engine.ErrUnsupportedVersion) {
			c.Status, c.Detail = statusFail, fmt.Sprintf("Ollama %s at %s is older than the minimum %s", got, cfg.OllamaURL, ollama.DefaultMinVersion)
			c.Advice = "upgrade Ollama"
			return c
		}
		c.Status, c.Detail = statusFail, err.Error()
		return c
	}
	c.Status, c.Detail = statusOK, fmt.Sprintf("Ollama %s at %s (minimum %s)", got, cfg.OllamaURL, ollama.DefaultMinVersion)
	return c
}

// checkKeychain only reads the keychain, bounded by hostkeys.keychain_timeout. It never creates
// or writes a key (req_001 D-81, D-102).
func (a *app) checkKeychain(cfg startupConfig) check {
	c := check{Name: "keychain"}
	st, err := hostkeys.CheckKeychain(a.keychain, cfg.KeychainTimeout)
	switch {
	case err == nil && st == hostkeys.KeychainHasKey:
		c.Status, c.Detail = statusOK, "the OS keychain answered and holds the host master key"
	case err == nil:
		c.Status, c.Detail = statusOK, "the OS keychain answered and holds no master key (a first run creates one; otherwise the key is in the key file)"
	case errors.Is(err, hostkeys.ErrKeychainTimeout):
		c.Status, c.Detail = statusWarn, err.Error()
		c.Advice = "unlock the keychain, or raise " + hostkeys.KeyKeychainTimeout + "; on a headless host a first run keeps the key in a protected file instead"
	default:
		c.Status, c.Detail = statusWarn, "the OS keychain is unavailable: "+err.Error()
		c.Advice = "a first run keeps the master key in a protected file instead; an existing install that keeps its key in the keychain cannot start until it is unlocked"
	}
	return c
}

// checkPort tries to bind the address and lets it go again.
func checkPort(name, addr, key string) check {
	c := check{Name: name}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		c.Status, c.Detail = statusWarn, fmt.Sprintf("cannot listen on %s: %v", addr, err)
		c.Advice = "if local-server is already running this is expected; otherwise free the port or change " + key
		return c
	}
	ln.Close()
	c.Status, c.Detail = statusOK, addr+" is free"
	return c
}

// checkParallel says how to raise concurrency: both settings must change together (req_001 D-95).
func checkParallel(ctx context.Context, v values) check {
	c := check{Name: "concurrency", Status: statusOK}
	par, err1 := v.Int(ctx, scheduler.KeyOllamaNumParallel, "")
	total, err2 := v.Int(ctx, scheduler.KeyTotalConcurrency, "")
	if err := errors.Join(err1, err2); err != nil {
		c.Status, c.Detail = statusWarn, "cannot read the scheduler settings: "+err.Error()
		return c
	}
	c.Detail = fmt.Sprintf("%s is %d and %s is %d, so local-server runs at most %d request(s) at a time", scheduler.KeyOllamaNumParallel, par, scheduler.KeyTotalConcurrency, total, min(par, total))
	c.Advice = "to serve more at once, raise both: set OLLAMA_NUM_PARALLEL to N in Ollama's environment and restart Ollama, then set " +
		scheduler.KeyOllamaNumParallel + " to the same N (and " + scheduler.KeyTotalConcurrency + " if it is lower); raising only one changes nothing or makes Ollama queue requests itself"
	return c
}
