package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine/ollama"
	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/telemetry/slogprom"
)

// Everything except the data directory is a host-owned setting stored in hst_setting, read after
// the database opens and applied on the next start (req_001 D-38, D-39). These are the settings
// startup itself owns; every other package registers its own.
const (
	keyOllamaBaseURL        = "engine.ollama_base_url"
	keyConsumerListen       = "serve.consumer_listen"
	keyMetricsListen        = "serve.metrics_listen"
	keyShutdownTimeout      = "serve.shutdown_timeout"
	keyReadHeaderTimeout    = "serve.read_header_timeout"
	keyLogLevel             = "log.level"
	keyLogMaxAgeDays        = "log.max_age_days"
	keyLogMaxSizeMB         = "log.max_size_mb"
	keyWorkerRestartBackoff = "workers.restart_backoff"
)

// Documented defaults. The consumer and metrics ports are local-server's own choice: Ollama uses
// 11434, so these sit apart from it.
const (
	defaultConsumerListen    = "127.0.0.1:8484"
	defaultMetricsListen     = "127.0.0.1:8485"
	defaultShutdownTimeout   = 10 * time.Second
	defaultReadHeaderTimeout = 10 * time.Second
	defaultLogLevel          = "info"
	defaultRestartBackoff    = 5 * time.Second
)

func startupDefs() []settings.Def {
	h := settings.Host
	return []settings.Def{
		{Key: keyOllamaBaseURL, Kind: settings.String, Default: ollama.DefaultBaseURL, Owner: h,
			Description: "Address of the Ollama server local-server shares, for example http://127.0.0.1:11434. Applies on the next start."},
		{Key: keyConsumerListen, Kind: settings.String, Default: defaultConsumerListen, Owner: h,
			Description: "host:port the consumer API (/v1) listens on. The default, 127.0.0.1:8484, accepts connections from this machine only until the tailnet listener exists. Applies on the next start."},
		{Key: keyMetricsListen, Kind: settings.String, Default: defaultMetricsListen, Owner: h,
			Description: "host:port that serves /metrics. It must be a loopback address (127.0.0.1, ::1 or localhost); the admin listener takes /metrics over later. Applies on the next start."},
		{Key: keyShutdownTimeout, Kind: settings.Duration, Default: defaultShutdownTimeout.String(), Owner: h,
			Description: "Longest a stopping local-server waits for requests in flight to finish before it closes their connections. Applies on the next start."},
		{Key: keyReadHeaderTimeout, Kind: settings.Duration, Default: defaultReadHeaderTimeout.String(), Owner: h,
			Description: "Longest a client may take to send its request headers to a listener, so a stalled client cannot hold a connection forever. Applies on the next start."},
		{Key: keyLogLevel, Kind: settings.String, Default: defaultLogLevel, Owner: h,
			Description: "Least severe log level written to the log file: debug, info, warn or error. Applies on the next start."},
		{Key: keyLogMaxAgeDays, Kind: settings.Int, Default: strconv.Itoa(slogprom.DefaultMaxAgeDays), Owner: h,
			Description: "Days a rotated log file is kept. Applies on the next start."},
		{Key: keyLogMaxSizeMB, Kind: settings.Int, Default: strconv.Itoa(slogprom.DefaultMaxSizeMB), Owner: h,
			Description: "Size in megabytes at which the log file is rotated. Applies on the next start."},
		{Key: keyWorkerRestartBackoff, Kind: settings.Duration, Default: defaultRestartBackoff.String(), Owner: h,
			Description: "How long a background worker that panicked, failed or stopped early waits before it is started again. Applies on the next start."},
	}
}

// registerSettings registers every setting startup reads or that a started package needs
// before its retention worker or its first read (req_001 D-39 F9 step 3): storage, telemetry,
// the keychain timeout, the events retention rule and startup's own. Packages that register
// their settings when constructed (scheduler, gateway, ...) are not listed.
func registerSettings(st *settings.Store) error {
	if err := st.RegisterStorage(); err != nil { // storage.synchronous (D-76), sync.change.retention (D-77)
		return err
	}
	if err := st.RegisterTelemetry(); err != nil { // telemetry.log_sink_error_interval (D-82)
		return err
	}
	if err := events.RegisterRetention(st); err != nil { // events.retention and its rule, before the retention worker (D-83)
		return err
	}
	if err := st.Register(hostkeys.KeychainTimeoutDef); err != nil { // hostkeys.keychain_timeout (D-81, D-102)
		return err
	}
	if err := st.Register(startupDefs()...); err != nil {
		return err
	}
	return registerStartupValidators(st)
}

func registerStartupValidators(st *settings.Store) error {
	for key, fn := range map[string]func(string) error{
		keyOllamaBaseURL:            checkBaseURL,
		keyConsumerListen:           checkListenAddr,
		keyMetricsListen:            checkLoopbackAddr,
		keyShutdownTimeout:          checkPositiveDuration,
		keyReadHeaderTimeout:        checkPositiveDuration,
		keyWorkerRestartBackoff:     checkPositiveDuration,
		hostkeys.KeyKeychainTimeout: checkPositiveDuration,
		keyLogLevel:                 func(v string) error { _, err := parseLevel(v); return err },
		keyLogMaxAgeDays:            checkPositiveInt,
		keyLogMaxSizeMB:             checkPositiveInt,
	} {
		check := fn
		if err := st.SetValidator(key, func(_ context.Context, v string) error { return check(v) }); err != nil {
			return err
		}
	}
	return nil
}

func checkPositiveDuration(v string) error {
	d, err := time.ParseDuration(v)
	if err != nil {
		return err
	}
	if d <= 0 {
		return fmt.Errorf("must be greater than 0")
	}
	return nil
}

func checkPositiveInt(v string) error {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return err
	}
	if n <= 0 {
		return fmt.Errorf("must be greater than 0")
	}
	return nil
}

func checkBaseURL(v string) error {
	u, err := url.Parse(v)
	if err != nil {
		return err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("must be an http:// or https:// address with a host")
	}
	return nil
}

func checkListenAddr(v string) error {
	_, port, err := net.SplitHostPort(v)
	if err != nil {
		return fmt.Errorf("must be host:port: %w", err)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("port %q is not a port number", port)
	}
	return nil
}

func checkLoopbackAddr(v string) error {
	if err := checkListenAddr(v); err != nil {
		return err
	}
	host, _, _ := net.SplitHostPort(v)
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("must be a loopback address (127.0.0.1, ::1 or localhost), not %q", host)
}

func parseLevel(v string) (slog.Level, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(strings.TrimSpace(v))); err != nil {
		return 0, fmt.Errorf("must be debug, info, warn or error: %w", err)
	}
	return l, nil
}

// values is the read side of the settings store; *settings.Store implements it, and so does
// defaultValues for a database that does not exist yet.
type values interface {
	String(ctx context.Context, key, scope string) (string, error)
	Int(ctx context.Context, key, scope string) (int64, error)
	Duration(ctx context.Context, key, scope string) (time.Duration, error)
}

// defaultValues answers every read with the registered default. doctor uses it when there is no
// database file yet (it never creates one).
type defaultValues struct{ st *settings.Store }

func (d defaultValues) def(key string) (string, error) {
	def, ok := d.st.Definition(key)
	if !ok {
		return "", fmt.Errorf("%w: %s", settings.ErrUnknownKey, key)
	}
	return def.Default, nil
}

func (d defaultValues) String(_ context.Context, key, _ string) (string, error) { return d.def(key) }

func (d defaultValues) Int(_ context.Context, key, _ string) (int64, error) {
	v, err := d.def(key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(v, 10, 64)
}

func (d defaultValues) Duration(_ context.Context, key, _ string) (time.Duration, error) {
	v, err := d.def(key)
	if err != nil {
		return 0, err
	}
	return time.ParseDuration(v)
}

// startupConfig is what startup reads from the settings once the database is open.
type startupConfig struct {
	OllamaURL         string
	ConsumerListen    string
	MetricsListen     string
	ShutdownTimeout   time.Duration
	ReadHeaderTimeout time.Duration
	LogLevel          slog.Level
	LogMaxAgeDays     int
	LogMaxSizeMB      int
	RestartBackoff    time.Duration
	KeychainTimeout   time.Duration
}

// loadStartup reads and validates the startup settings. An invalid stored value stops startup
// with an error naming the setting (the admin write path validates too, so this means the row was
// written some other way).
func loadStartup(ctx context.Context, v values) (startupConfig, error) {
	var c startupConfig
	var firstErr error
	str := func(key string, check func(string) error) string {
		s, err := v.String(ctx, key, "")
		if err == nil && check != nil {
			err = check(s)
		}
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("setting %s: %w", key, err)
		}
		return s
	}
	dur := func(key string) time.Duration {
		d, err := v.Duration(ctx, key, "")
		if err == nil {
			err = checkPositiveDuration(d.String())
		}
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("setting %s: %w", key, err)
		}
		return d
	}
	num := func(key string) int {
		n, err := v.Int(ctx, key, "")
		if err == nil && n <= 0 {
			err = fmt.Errorf("must be greater than 0")
		}
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("setting %s: %w", key, err)
		}
		return int(n)
	}
	c.OllamaURL = str(keyOllamaBaseURL, checkBaseURL)
	c.ConsumerListen = str(keyConsumerListen, checkListenAddr)
	c.MetricsListen = str(keyMetricsListen, checkLoopbackAddr)
	c.ShutdownTimeout = dur(keyShutdownTimeout)
	c.ReadHeaderTimeout = dur(keyReadHeaderTimeout)
	lvl := str(keyLogLevel, func(s string) error { _, err := parseLevel(s); return err })
	if l, err := parseLevel(lvl); err == nil {
		c.LogLevel = l
	}
	c.LogMaxAgeDays = num(keyLogMaxAgeDays)
	c.LogMaxSizeMB = num(keyLogMaxSizeMB)
	c.RestartBackoff = dur(keyWorkerRestartBackoff)
	c.KeychainTimeout = dur(hostkeys.KeyKeychainTimeout)
	return c, firstErr
}
