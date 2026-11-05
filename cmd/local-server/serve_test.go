package main

import (
	"context"
	"errors"
	"io"
	"net"
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
	"github.com/inCoderKaka/local-server/internal/storage"
)

// loopbackAny makes both listeners take a free port.
var loopbackAny = map[string]string{keyConsumerListen: "127.0.0.1:0", keyAdminListen: "127.0.0.1:0"}

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

// testClient bounds every request a test makes to a local listener.
var testClient = &http.Client{Timeout: 10 * time.Second}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := testClient.Get(url)
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
	if strings.Contains(ta.err.String(), "master key is kept in the file") {
		t.Error("a working keychain still printed the key-file notice")
	}
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
	status, body := get(t, "http://"+r.addrs.Admin+"/metrics")
	if status != 200 || !strings.Contains(body, "telemetry_log_write_errors_total") {
		t.Errorf("GET /metrics = %d; want the slogprom registry", status)
	}
	if code := r.stop(); code != 0 {
		t.Fatalf("exit = %d after shutdown; stderr:\n%s", code, ta.err.String())
	}

	logs, err := os.ReadFile(filepath.Join(ta.logDir, logFileName))
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
	if status, body := get(t, "http://"+r2.addrs.Consumer+"/v1/health"); status != http.StatusUnauthorized {
		t.Errorf("second start: GET /v1/health without a key = %d (%s), want 401 from the gateway", status, body)
	}
	status, body = get(t, "http://"+r2.addrs.Admin+"/metrics")
	if status != 200 || !strings.Contains(body, "telemetry_log_write_errors_total") {
		t.Errorf("second start: GET /metrics = %d; want the slogprom registry", status)
	}
	if code := r2.stop(); code != 0 {
		t.Errorf("second start exit = %d; stderr:\n%s", code, ta2.err.String())
	}
}

// The admin listener refuses a request whose Host header is not a loopback name, which stops a
// DNS-rebinding page from reading /metrics.
func TestAdminListenerRefusesForeignHost(t *testing.T) {
	ta := newTestApp(t, true)
	seed(t, ta.dir, loopbackAny)
	r := startServe(t, ta, "--engine", "fake")
	defer r.stop()
	_, port, err := net.SplitHostPort(r.addrs.Admin)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		host string
		want int
	}{
		{"evil.example", http.StatusForbidden},
		{"evil.example:" + port, http.StatusForbidden},
		{r.addrs.Admin, http.StatusOK},
		{"localhost:" + port, http.StatusOK},
	} {
		req, err := http.NewRequest("GET", "http://"+r.addrs.Admin+"/metrics", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = tc.host
		resp, err := testClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("GET /metrics with Host %q = %d, want %d", tc.host, resp.StatusCode, tc.want)
		}
	}
}

// The admin API is on the admin listener only. The consumer listener answers 404 to every
// /admin/v1 route; the admin listener answers them, and refuses a foreign Host on the API as it
// does on /metrics (req_001 D-10, D-104).
func TestAdminRoutesLiveOnTheAdminListenerOnly(t *testing.T) {
	ta := newTestApp(t, true)
	seed(t, ta.dir, loopbackAny)
	r := startServe(t, ta, "--engine", "fake")
	defer r.stop()
	for _, path := range []string{"/admin/v1/setup", "/admin/v1/usage?granularity=day", "/admin/v1/session", "/admin/v1/host"} {
		if status, body := get(t, "http://"+r.addrs.Consumer+path); status != http.StatusNotFound {
			t.Errorf("consumer listener GET %s = %d (%s), want 404", path, status, body)
		}
	}
	if status, body := get(t, "http://"+r.addrs.Consumer+"/metrics"); status != http.StatusNotFound {
		t.Errorf("consumer listener GET /metrics = %d (%s), want 404", status, body)
	}
	status, body := get(t, "http://"+r.addrs.Admin+"/admin/v1/setup")
	if status != http.StatusOK || !strings.Contains(body, `"admin_exists":false`) {
		t.Errorf("admin listener GET /admin/v1/setup = %d (%s)", status, body)
	}
	if status, _ := get(t, "http://"+r.addrs.Admin+"/admin/v1/usage?granularity=day"); status != http.StatusUnauthorized {
		t.Errorf("admin listener GET /admin/v1/usage without a session = %d, want 401", status)
	}
	if status, _ := get(t, "http://"+r.addrs.Admin+"/v1/health"); status != http.StatusNotFound {
		t.Errorf("admin listener GET /v1/health = %d, want 404", status)
	}
	for _, path := range []string{"/admin/v1/setup", "/metrics"} {
		req, err := http.NewRequest("GET", "http://"+r.addrs.Admin+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = "evil.example"
		resp, err := testClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("admin listener GET %s with a foreign Host = %d, want 403", path, resp.StatusCode)
		}
	}
	// A first-run admin created over HTTP can log in, and the session reaches a protected route.
	resp, err := testClient.Post("http://"+r.addrs.Admin+"/admin/v1/setup", "application/json",
		strings.NewReader(`{"username":"serve-admin","password":"a long enough password"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || len(resp.Cookies()) != 1 {
		t.Fatalf("POST /admin/v1/setup = %d, %d cookies", resp.StatusCode, len(resp.Cookies()))
	}
	hostReq, _ := http.NewRequest("GET", "http://"+r.addrs.Admin+"/admin/v1/host", nil)
	hostReq.AddCookie(resp.Cookies()[0])
	hr, err := testClient.Do(hostReq)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(hr.Body)
	hr.Body.Close()
	if hr.StatusCode != http.StatusOK || !strings.Contains(string(b), `"engine_state":"available"`) || !strings.Contains(string(b), `"software_version":"`+version+`"`) {
		t.Errorf("GET /admin/v1/host = %d %s", hr.StatusCode, b)
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

// A first run whose keychain does not answer keeps the master key in the protected file, says so
// on stderr, and writes nothing to the keychain; a later start reads the file.
func TestServeFirstRunKeychainTimeoutUsesKeyFile(t *testing.T) {
	ta := newTestApp(t, true)
	seed(t, ta.dir, merge(loopbackAny, map[string]string{hostkeys.KeyKeychainTimeout: "50ms"}))
	block := make(chan struct{})
	ta.kc.mu.Lock()
	ta.kc.block = block
	ta.kc.mu.Unlock()
	r := startServe(t, ta, "--engine", "fake")
	if code := r.stop(); code != 0 {
		t.Fatalf("exit = %d; stderr:\n%s", code, ta.err.String())
	}
	keyFile := filepath.Join(ta.dir, masterKeyFileName)
	if !exists(keyFile) {
		t.Error("no key file after a first run with a stuck keychain")
	}
	contains(t, "stderr", ta.err.String(), keyFile)
	if _, sets := ta.kc.calls(); sets != 0 {
		t.Errorf("keychain writes = %d, want 0 after a timeout", sets)
	}

	// Release the stuck call; the keychain now answers "not found", so the next start reads the
	// key file.
	close(block)
	ta.kc.mu.Lock()
	ta.kc.block = nil
	ta.kc.mu.Unlock()
	ta2 := newTestApp(t, false)
	ta2.dir, ta2.kc, ta2.keychain = ta.dir, ta.kc, ta.kc
	r2 := startServe(t, ta2, "--engine", "fake")
	if code := r2.stop(); code != 0 {
		t.Fatalf("second start exit = %d; stderr:\n%s", code, ta2.err.String())
	}
	if _, sets := ta.kc.calls(); sets != 0 {
		t.Errorf("keychain writes = %d after the second start, want 0", sets)
	}
	if strings.Contains(ta2.err.String(), "recovery code") || strings.Contains(ta2.err.String(), "master key is kept in the file") {
		t.Errorf("a later start printed a first-run notice:\n%s", ta2.err.String())
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

// With no user log folder, logs go inside the data directory and the start says so.
func TestServeLogsInDataDirWithoutUserLogFolder(t *testing.T) {
	ta := newTestApp(t, true)
	ta.userLogDir = func() (string, error) { return "", errors.New("no log folder") }
	seed(t, ta.dir, loopbackAny)
	if code := startServe(t, ta, "--engine", "fake").stop(); code != 0 {
		t.Fatalf("exit = %d; stderr:\n%s", code, ta.err.String())
	}
	if p := filepath.Join(ta.dir, fallbackLogDir, logFileName); !exists(p) {
		t.Errorf("no log file at %s", p)
	}
	contains(t, "stderr", ta.err.String(), "cannot find the user log folder")
}

func TestServeListenFailureNamesTheSetting(t *testing.T) {
	busy := httptest.NewServer(http.NotFoundHandler())
	defer busy.Close()
	addr := strings.TrimPrefix(busy.URL, "http://")
	ta := newTestApp(t, true)
	seed(t, ta.dir, map[string]string{keyConsumerListen: addr, keyAdminListen: "127.0.0.1:0"})
	code := ta.run(context.Background(), []string{"serve", "--engine", "fake", "--data-dir", ta.dir})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	contains(t, "stderr", ta.err.String(), keyConsumerListen, addr)
}

// A start that cannot bind its listeners (a running instance holds the port) leaves the running
// jobs alone: they are marked failed(host_restart) only once both listeners are bound.
func TestServeListenFailureLeavesRunningJobs(t *testing.T) {
	ta := newTestApp(t, true)
	seed(t, ta.dir, loopbackAny)
	if code := startServe(t, ta, "--engine", "fake").stop(); code != 0 {
		t.Fatalf("first run exit = %d; stderr:\n%s", code, ta.err.String())
	}
	d, err := openDatabase(context.Background(), filepath.Join(ta.dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	err = d.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := tx.Exec(`INSERT INTO sch_job (id, consumer_id, principal_id, access_key_id, grant_version_id, idempotency_key, model, identity, status, created_at)
			VALUES ('job-1', 'c-1', 'p-1', 'k-1', 'gv-1', 'idem-1', 'm', '{}', 'running', 1)`)
		return err
	})
	d.db.Close()
	if err != nil {
		t.Fatal(err)
	}

	busy := httptest.NewServer(http.NotFoundHandler())
	defer busy.Close()
	addr := strings.TrimPrefix(busy.URL, "http://")
	seed(t, ta.dir, map[string]string{keyConsumerListen: addr})
	ta2 := newTestApp(t, false)
	ta2.dir, ta2.kc, ta2.keychain = ta.dir, ta.kc, ta.kc
	code := ta2.run(context.Background(), []string{"serve", "--engine", "fake", "--data-dir", ta.dir})
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr:\n%s", code, ta2.err.String())
	}
	contains(t, "stderr", ta2.err.String(), keyConsumerListen, addr)
	if n := count(t, ta.dir, `SELECT count(*) FROM sch_job WHERE status = 'running'`); n != 1 {
		t.Errorf("running jobs = %d after a start that failed to bind, want the 1 left alone", n)
	}
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
