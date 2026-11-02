package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/settings"
)

// loopbackAny makes both listeners take a free port.
var loopbackAny = map[string]string{keyConsumerListen: "127.0.0.1:0", keyMetricsListen: "127.0.0.1:0"}

func merge(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// running is a serve started in the background.
type running struct {
	t      *testing.T
	cancel context.CancelFunc
	exit   chan int
	addrs  listening
}

// startServe runs `serve` with args until the test stops it. It fails the test if serve exits
// before it listens.
func startServe(t *testing.T, ta *testApp, args ...string) *running {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan listening, 1)
	ta.onListening = func(l listening) { ready <- l }
	r := &running{t: t, cancel: cancel, exit: make(chan int, 1)}
	go func() { r.exit <- ta.run(ctx, append([]string{"serve", "--data-dir", ta.dir}, args...)) }()
	select {
	case r.addrs = <-ready:
	case code := <-r.exit:
		cancel()
		t.Fatalf("serve exited with %d before it listened; stderr:\n%s", code, ta.err.String())
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatalf("serve did not listen in time; stderr:\n%s", ta.err.String())
	}
	t.Cleanup(func() { cancel() })
	return r
}

// stop sends the shutdown signal and returns the exit code.
func (r *running) stop() int {
	r.t.Helper()
	r.cancel()
	select {
	case code := <-r.exit:
		return code
	case <-time.After(15 * time.Second):
		r.t.Fatal("serve did not stop")
		return -1
	}
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

var codeRe = regexp.MustCompile(`(?m)^    (\S+)$`)

func ollamaStub(t *testing.T, version string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			io.WriteString(w, `{"version":"`+version+`"}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// With no terminal the first run refuses before the master key or any host row exists, and the
// keychain is not even asked (req_001 D-101).
func TestServeFirstRunWithoutTerminalRefuses(t *testing.T) {
	ta := newTestApp(t, false)
	code := ta.run(context.Background(), []string{"serve", "--engine", "fake", "--data-dir", ta.dir})
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr:\n%s", code, ta.err.String())
	}
	contains(t, "stderr", ta.err.String(), "first run", "local-server serve", "terminal")
	if n := count(t, ta.dir, `SELECT count(*) FROM hst_host`); n != 0 {
		t.Errorf("hst_host has %d rows, want 0", n)
	}
	if n := count(t, ta.dir, `SELECT count(*) FROM hst_key`); n != 0 {
		t.Errorf("hst_key has %d rows, want 0", n)
	}
	if exists(filepath.Join(ta.dir, masterKeyFileName)) {
		t.Error("a master key file was created")
	}
	if gets, sets := ta.kc.calls(); gets != 0 || sets != 0 {
		t.Errorf("the keychain was used (gets %d, sets %d)", gets, sets)
	}
	if strings.Contains(ta.err.String(), "recovery code:") {
		t.Error("a recovery code was printed without a terminal")
	}
}

// A first run on a terminal prints the recovery code once to stderr, never to the log; a later
// start (no terminal needed) prints none and serves the consumer API and /metrics.
func TestServeFirstRunShowsRecoveryCodeOnce(t *testing.T) {
	ta := newTestApp(t, true)
	seed(t, ta.dir, loopbackAny)

	r := startServe(t, ta, "--engine", "fake")
	m := codeRe.FindStringSubmatch(ta.err.String())
	if m == nil || !strings.Contains(ta.err.String(), "recovery code") {
		t.Fatalf("no recovery code on stderr:\n%s", ta.err.String())
	}
	recovery := m[1]
	if n := strings.Count(ta.err.String(), recovery); n != 1 {
		t.Errorf("the recovery code appears %d times on stderr, want once", n)
	}
	if n := count(t, ta.dir, `SELECT count(*) FROM hst_host`); n != 1 {
		t.Errorf("hst_host has %d rows, want 1", n)
	}
	if _, sets := ta.kc.calls(); sets != 1 {
		t.Errorf("keychain writes = %d, want 1 (the master key)", sets)
	}
	if status, body := get(t, "http://"+r.addrs.Consumer+"/v1/health"); status != http.StatusUnauthorized {
		t.Errorf("GET /v1/health without a key = %d (%s), want 401 from the gateway", status, body)
	}
	status, body := get(t, "http://"+r.addrs.Metrics+"/metrics")
	if status != 200 || !strings.Contains(body, "telemetry_log_write_errors_total") {
		t.Errorf("GET /metrics = %d; want the slogprom registry", status)
	}
	if code := r.stop(); code != 0 {
		t.Fatalf("exit = %d after shutdown; stderr:\n%s", code, ta.err.String())
	}

	logs, err := os.ReadFile(filepath.Join(ta.dir, filepath.FromSlash(logFileName)))
	if err != nil {
		t.Fatalf("no log file: %v", err)
	}
	if strings.Contains(string(logs), recovery) {
		t.Error("the recovery code reached the log file")
	}
	contains(t, "log file", string(logs), `"msg":"listening"`, `"msg":"shutting down"`)

	// Second start: an existing install needs no terminal and shows no code.
	ta2 := newTestApp(t, false)
	ta2.dir, ta2.kc = ta.dir, ta.kc
	ta2.keychain = ta.kc
	r2 := startServe(t, ta2, "--engine", "fake")
	if strings.Contains(ta2.err.String(), "recovery code") {
		t.Error("a later start printed a recovery code")
	}
	if code := r2.stop(); code != 0 {
		t.Errorf("second start exit = %d; stderr:\n%s", code, ta2.err.String())
	}
}

// Workers run under the supervisor: the host-metrics sampler writes its first row.
func TestServeRunsWorkers(t *testing.T) {
	ta := newTestApp(t, true)
	seed(t, ta.dir, loopbackAny)
	r := startServe(t, ta, "--engine", "fake")
	defer r.stop()
	waitFor(t, "a host-metrics row", func() bool { return count(t, ta.dir, `SELECT count(*) FROM hst_metric_minute`) > 0 })
}

// An existing install whose keychain does not answer stops with an error naming the setting; it
// neither falls back to the key file nor creates a key (req_001 D-81, D-102).
func TestServeExistingInstallKeychainTimeout(t *testing.T) {
	ta := newTestApp(t, true)
	seed(t, ta.dir, loopbackAny)
	if code := startServe(t, ta, "--engine", "fake").stop(); code != 0 {
		t.Fatalf("first run exit = %d", code)
	}
	seed(t, ta.dir, map[string]string{hostkeys.KeyKeychainTimeout: "50ms"})
	block := make(chan struct{})
	defer close(block)
	ta.kc.block = block
	ta2 := newTestApp(t, false)
	ta2.dir, ta2.kc, ta2.keychain = ta.dir, ta.kc, ta.kc
	code := ta2.run(context.Background(), []string{"serve", "--engine", "fake", "--data-dir", ta.dir})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	contains(t, "stderr", ta2.err.String(), hostkeys.KeyKeychainTimeout, "unlock the keychain")
	if exists(filepath.Join(ta.dir, masterKeyFileName)) {
		t.Error("an existing install fell back to a key file")
	}
	if n := count(t, ta.dir, `SELECT count(*) FROM hst_host`); n != 1 {
		t.Errorf("hst_host has %d rows, want the 1 from the first run", n)
	}
}

// A first run whose keychain does not answer keeps the master key in the protected file.
func TestServeFirstRunKeychainTimeoutUsesKeyFile(t *testing.T) {
	ta := newTestApp(t, true)
	seed(t, ta.dir, merge(loopbackAny, map[string]string{hostkeys.KeyKeychainTimeout: "50ms"}))
	block := make(chan struct{})
	defer close(block)
	ta.kc.block = block
	r := startServe(t, ta, "--engine", "fake")
	if code := r.stop(); code != 0 {
		t.Fatalf("exit = %d; stderr:\n%s", code, ta.err.String())
	}
	if !exists(filepath.Join(ta.dir, masterKeyFileName)) {
		t.Error("no key file after a first run with a stuck keychain")
	}
}

func TestServeOllamaVersionCheck(t *testing.T) {
	t.Run("too old stops startup", func(t *testing.T) {
		ta := newTestApp(t, true)
		seed(t, ta.dir, merge(loopbackAny, map[string]string{keyOllamaBaseURL: ollamaStub(t, "0.1.0").URL}))
		code := ta.run(context.Background(), []string{"serve", "--data-dir", ta.dir})
		if code != 1 {
			t.Fatalf("exit = %d, want 1", code)
		}
		contains(t, "stderr", ta.err.String(), "older than the minimum", "upgrade Ollama")
	})
	t.Run("supported version serves", func(t *testing.T) {
		ta := newTestApp(t, true)
		seed(t, ta.dir, merge(loopbackAny, map[string]string{keyOllamaBaseURL: ollamaStub(t, "0.12.6").URL}))
		r := startServe(t, ta)
		if code := r.stop(); code != 0 {
			t.Errorf("exit = %d; stderr:\n%s", code, ta.err.String())
		}
	})
	t.Run("unreachable warns and serves", func(t *testing.T) {
		ta := newTestApp(t, true)
		dead := httptest.NewServer(http.NotFoundHandler())
		url := dead.URL
		dead.Close()
		seed(t, ta.dir, merge(loopbackAny, map[string]string{keyOllamaBaseURL: url}))
		r := startServe(t, ta)
		contains(t, "stderr", ta.err.String(), "warning: cannot reach Ollama", keyOllamaBaseURL)
		if code := r.stop(); code != 0 {
			t.Errorf("exit = %d", code)
		}
	})
}

func TestServeListenFailureNamesTheSetting(t *testing.T) {
	busy := httptest.NewServer(http.NotFoundHandler())
	defer busy.Close()
	addr := strings.TrimPrefix(busy.URL, "http://")
	ta := newTestApp(t, true)
	seed(t, ta.dir, map[string]string{keyConsumerListen: addr, keyMetricsListen: "127.0.0.1:0"})
	code := ta.run(context.Background(), []string{"serve", "--engine", "fake", "--data-dir", ta.dir})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	contains(t, "stderr", ta.err.String(), keyConsumerListen, addr)
}

// registerSettings puts the retention rules of every table with a retention setting in place
// before the retention worker starts (req_001 D-77, D-83).
func TestRegisterSettingsRegistersRetentionRules(t *testing.T) {
	ctx := context.Background()
	d, err := openDatabase(ctx, filepath.Join(t.TempDir(), dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer d.db.Close()
	pruned, err := d.st.PruneOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tables := map[string]bool{}
	for _, p := range pruned {
		tables[p.Table] = true
	}
	for _, want := range []string{"syn_change", "ntf_event"} {
		if !tables[want] {
			t.Errorf("no retention rule for %s; rules cover %v", want, tables)
		}
	}
	if _, ok := d.st.Definition(settings.KeyLogSinkErrorInterval); !ok {
		t.Error("telemetry.log_sink_error_interval is not registered")
	}
}
