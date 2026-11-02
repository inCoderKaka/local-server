package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// runDoctorJSON runs `doctor --json` and decodes the report.
func runDoctorJSON(t *testing.T, ta *testApp) (int, report) {
	t.Helper()
	code := ta.run(context.Background(), []string{"doctor", "--json", "--data-dir", ta.dir})
	var rep report
	if err := json.Unmarshal([]byte(ta.out.String()), &rep); err != nil {
		t.Fatalf("doctor --json printed %q: %v", ta.out.String(), err)
	}
	return code, rep
}

func (r report) byName(t *testing.T, name string) check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %q check in %+v", name, r.Checks)
	return check{}
}

func deadURL() string {
	s := httptest.NewServer(http.NotFoundHandler())
	u := s.URL
	s.Close()
	return u
}

// With no database file, doctor still reports (warn and fail states, no crash) and creates
// nothing. Whether Ollama answers at the default address depends on the machine, so the exit code
// is checked only against the report's ok flag.
func TestDoctorWithNoDatabaseAndNoOllama(t *testing.T) {
	ta := newTestApp(t, false)
	ta.dir = filepath.Join(ta.dir, "fresh") // does not exist
	code, rep := runDoctorJSON(t, ta)
	if len(rep.Checks) == 0 {
		t.Fatal("no checks")
	}
	if db := rep.byName(t, "database"); db.Status != statusWarn || db.Advice == "" {
		t.Errorf("database check = %+v; want warn with advice", db)
	}
	if dd := rep.byName(t, "data_directory"); dd.Status != statusOK || !strings.Contains(dd.Detail, "does not exist yet") {
		t.Errorf("data_directory check = %+v", dd)
	}
	if exists(ta.dir) {
		t.Error("doctor created the data directory")
	}
	if code != 0 && code != 1 {
		t.Errorf("exit = %d", code)
	}
	if (code == 0) != rep.OK {
		t.Errorf("exit %d does not match ok=%v", code, rep.OK)
	}
	if gets, sets := ta.kc.calls(); sets != 0 || gets == 0 {
		t.Errorf("keychain gets %d sets %d; want a read and no write", gets, sets)
	}
}

func TestDoctorReportsOllamaStates(t *testing.T) {
	cases := []struct {
		name       string
		url        func(t *testing.T) string
		wantStatus string
		wantDetail string
		wantExit   int
	}{
		{"ok", func(t *testing.T) string { return ollamaStub(t, "0.12.6").URL }, statusOK, "Ollama 0.12.6", 0},
		{"too old", func(t *testing.T) string { return ollamaStub(t, "0.3.0").URL }, statusFail, "older than the minimum", 1},
		{"unreachable", func(*testing.T) string { return deadURL() }, statusFail, "cannot reach Ollama", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ta := newTestApp(t, false)
			seed(t, ta.dir, merge(loopbackAny, map[string]string{keyOllamaBaseURL: tc.url(t)}))
			code, rep := runDoctorJSON(t, ta)
			oc := rep.byName(t, "ollama")
			if oc.Status != tc.wantStatus || !strings.Contains(oc.Detail, tc.wantDetail) {
				t.Errorf("ollama check = %+v; want %s containing %q", oc, tc.wantStatus, tc.wantDetail)
			}
			if code != tc.wantExit {
				t.Errorf("exit = %d, want %d", code, tc.wantExit)
			}
			if db := rep.byName(t, "database"); db.Status != statusOK {
				t.Errorf("database check = %+v", db)
			}
		})
	}
}

func TestDoctorKeychain(t *testing.T) {
	t.Run("never creates a key", func(t *testing.T) {
		ta := newTestApp(t, false)
		seed(t, ta.dir, map[string]string{keyOllamaBaseURL: ollamaStub(t, "0.12.6").URL})
		_, rep := runDoctorJSON(t, ta)
		if c := rep.byName(t, "keychain"); c.Status != statusOK {
			t.Errorf("keychain = %+v", c)
		}
		if _, sets := ta.kc.calls(); sets != 0 {
			t.Errorf("doctor wrote to the keychain %d times", sets)
		}
		if exists(filepath.Join(ta.dir, masterKeyFileName)) {
			t.Error("doctor created a key file")
		}
	})
	t.Run("a stuck keychain is bounded by the setting", func(t *testing.T) {
		ta := newTestApp(t, false)
		seed(t, ta.dir, map[string]string{keyOllamaBaseURL: ollamaStub(t, "0.12.6").URL, hostkeys.KeyKeychainTimeout: "30ms"})
		block := make(chan struct{})
		defer close(block)
		ta.kc.block = block
		_, rep := runDoctorJSON(t, ta)
		c := rep.byName(t, "keychain")
		if c.Status != statusWarn || !strings.Contains(c.Advice, hostkeys.KeyKeychainTimeout) {
			t.Errorf("keychain = %+v; want a warn naming %s", c, hostkeys.KeyKeychainTimeout)
		}
	})
	// An existing install cannot start while the keychain is stuck (serve never falls back to the
	// key file after a timeout), so doctor fails rather than warns.
	t.Run("an existing install with a stuck keychain fails", func(t *testing.T) {
		ta := newTestApp(t, true)
		seed(t, ta.dir, merge(loopbackAny, map[string]string{keyOllamaBaseURL: ollamaStub(t, "0.12.6").URL, hostkeys.KeyKeychainTimeout: "30ms"}))
		if code := startServe(t, ta, "--engine", "fake").stop(); code != 0 {
			t.Fatalf("first run exit = %d; stderr:\n%s", code, ta.err.String())
		}
		block := make(chan struct{})
		defer close(block)
		ta.kc.mu.Lock()
		ta.kc.block = block
		ta.kc.mu.Unlock()
		code, rep := runDoctorJSON(t, ta)
		c := rep.byName(t, "keychain")
		if c.Status != statusFail || !strings.Contains(c.Advice, hostkeys.KeyKeychainTimeout) {
			t.Errorf("keychain = %+v; want a fail naming %s", c, hostkeys.KeyKeychainTimeout)
		}
		if code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
	})
}

func TestDoctorPorts(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	ta := newTestApp(t, false)
	seed(t, ta.dir, map[string]string{keyOllamaBaseURL: ollamaStub(t, "0.12.6").URL,
		keyConsumerListen: busy.Addr().String(), keyAdminListen: "127.0.0.1:0"})
	_, rep := runDoctorJSON(t, ta)
	if c := rep.byName(t, "consumer_port"); c.Status != statusWarn || !strings.Contains(c.Advice, keyConsumerListen) {
		t.Errorf("consumer_port = %+v; want a warn naming the setting", c)
	}
	if c := rep.byName(t, "admin_port"); c.Status != statusOK {
		t.Errorf("admin_port = %+v", c)
	}
}

// doctor says to raise scheduler.ollama_num_parallel together with OLLAMA_NUM_PARALLEL and shows
// the current value (req_001 D-95).
func TestDoctorConcurrencyAdvice(t *testing.T) {
	ta := newTestApp(t, false)
	seed(t, ta.dir, map[string]string{keyOllamaBaseURL: ollamaStub(t, "0.12.6").URL, "scheduler.ollama_num_parallel": "3"})
	_, rep := runDoctorJSON(t, ta)
	c := rep.byName(t, "concurrency")
	contains(t, "detail", c.Detail, "scheduler.ollama_num_parallel is 3", "at most 3")
	contains(t, "advice", c.Advice, "OLLAMA_NUM_PARALLEL", "scheduler.ollama_num_parallel", "both")
}

func TestDoctorSchemaMismatchFails(t *testing.T) {
	ta := newTestApp(t, false)
	// A database a smaller registry created: only the settings module.
	db, err := storage.Open(context.Background(), storage.DefaultConfig(filepath.Join(ta.dir, dbFileName), settings.Module()))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	code, rep := runDoctorJSON(t, ta)
	c := rep.byName(t, "database")
	if c.Status != statusFail || !strings.Contains(c.Detail, "schema differs") {
		t.Errorf("database = %+v; want a schema failure", c)
	}
	if code != 1 || rep.OK {
		t.Errorf("exit %d ok=%v; want exit 1", code, rep.OK)
	}
	// An older database is migrated by serve.
	contains(t, "advice", c.Advice, "local-server serve", "migrate it")
}

// A database that records a higher version than this build embeds cannot be migrated by this
// build, so doctor must not tell the host to run serve.
func TestDoctorNewerDatabaseAdvice(t *testing.T) {
	ta := newTestApp(t, false)
	seed(t, ta.dir, nil)
	d, err := openDatabase(context.Background(), filepath.Join(ta.dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	err = d.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := tx.Exec(`INSERT INTO ops_schema_migrations (module, version, name, checksum, applied_at, duration_ms) VALUES ('storage', 9999, 'from_the_future', 'x', 0, 0)`)
		return err
	})
	d.db.Close()
	if err != nil {
		t.Fatal(err)
	}
	code, rep := runDoctorJSON(t, ta)
	c := rep.byName(t, "database")
	if c.Status != statusFail || !strings.Contains(c.Detail, "schema differs") {
		t.Fatalf("database = %+v; want a schema failure", c)
	}
	contains(t, "advice", c.Advice, "the local-server build that created this database", "cannot migrate")
	if strings.Contains(c.Advice, "local-server serve") {
		t.Errorf("advice %q tells the host to run serve against a newer database", c.Advice)
	}
	if strings.Contains(c.Detail, "to migrate") {
		t.Errorf("detail %q tells the host to migrate a newer database", c.Detail)
	}
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
}

func TestDoctorBadStoredSettingFailsAndKeepsGoing(t *testing.T) {
	ta := newTestApp(t, false)
	seed(t, ta.dir, nil)
	d, err := openDatabase(context.Background(), filepath.Join(ta.dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	err = d.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := tx.Exec(`INSERT INTO hst_setting (key, scope, value, updated_by, updated_at) VALUES (?, '', 'nonsense', 'host', 0)`, keyConsumerListen)
		return err
	})
	d.db.Close()
	if err != nil {
		t.Fatal(err)
	}
	code, rep := runDoctorJSON(t, ta)
	if c := rep.byName(t, "settings"); c.Status != statusFail || !strings.Contains(c.Detail, keyConsumerListen) {
		t.Errorf("settings = %+v", c)
	}
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	rep.byName(t, "consumer_port") // the later checks still ran
}

func TestDoctorHumanOutput(t *testing.T) {
	ta := newTestApp(t, false)
	seed(t, ta.dir, map[string]string{keyOllamaBaseURL: ollamaStub(t, "0.12.6").URL})
	if code := ta.run(context.Background(), []string{"doctor", "--data-dir", ta.dir}); code != 0 {
		t.Fatalf("exit = %d; out:\n%s", code, ta.out.String())
	}
	contains(t, "output", ta.out.String(), "[ok  ] ollama:", "[ok  ] database:", "->")
}

// The JSON shape: {"checks":[{"name","status","detail","advice"?}], ...}.
func TestDoctorJSONShape(t *testing.T) {
	ta := newTestApp(t, false)
	seed(t, ta.dir, map[string]string{keyOllamaBaseURL: ollamaStub(t, "0.12.6").URL})
	ta.run(context.Background(), []string{"doctor", "--json", "--data-dir", ta.dir})
	var raw map[string]any
	if err := json.Unmarshal([]byte(ta.out.String()), &raw); err != nil {
		t.Fatal(err)
	}
	checks, _ := raw["checks"].([]any)
	if len(checks) == 0 {
		t.Fatal("checks is empty")
	}
	for _, c := range checks {
		m := c.(map[string]any)
		for _, k := range []string{"name", "status", "detail"} {
			if s, _ := m[k].(string); s == "" {
				t.Errorf("check %v lacks %q", m, k)
			}
		}
		switch m["status"] {
		case statusOK, statusWarn, statusFail:
		default:
			t.Errorf("check %v has status %v", m["name"], m["status"])
		}
	}
	for _, k := range []string{"version", "data_dir", "ok"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("report lacks %q", k)
		}
	}
}

// doctor writes nothing into the data directory it did not find (no database, no files).
func TestDoctorLeavesNoFiles(t *testing.T) {
	ta := newTestApp(t, false)
	ta.run(context.Background(), []string{"doctor", "--json", "--data-dir", ta.dir})
	ents, err := os.ReadDir(ta.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("doctor left %v in the data directory", ents)
	}
}
