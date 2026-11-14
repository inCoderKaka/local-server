package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// The environment of the browser end-to-end harness (web/e2e, `make web-admin-e2e`, D-152).
const (
	// e2eReadyFileEnv names the file the harness writes the listener URLs to once serve listens.
	// Without it TestE2EServer skips, so `go test ./...` never runs it.
	e2eReadyFileEnv = "LS_E2E_READY_FILE"
	// e2eDataDirEnv names the data directory to use; empty means a temp dir of the test.
	e2eDataDirEnv = "LS_E2E_DATA_DIR"
	// e2eRollupInterval is the usage.rollup.interval of the run, so a call shows on the usage
	// page within seconds instead of after the default minute. It goes in as the host setting,
	// before the first start, because the worker reads it after each pass.
	e2eRollupInterval = "1s"
)

// e2eReady is what the harness writes to the ready file.
type e2eReady struct {
	Admin    string `json:"admin"`
	Consumer string `json:"consumer"`
}

// TestE2EServer is not a unit test: it is the server the Playwright end-to-end test drives
// (web/e2e). It runs the real `serve --engine fake` code in this process with the same fakes as
// the other tests here: an in-memory keychain (the OS keychain is never touched), a fake
// terminal (so the first run can start without one), a fake sleep lock and no Tailscale. The
// engine is the fake engine, so no Ollama is needed. Both listeners take a free loopback port;
// their URLs go to the ready file as JSON. It serves until SIGINT or SIGTERM, then stops serve.
//
// Run it as a compiled test binary so the signals reach it:
//
//	CGO_ENABLED=0 go test -c -o bin/e2e-harness ./cmd/local-server
//	LS_E2E_READY_FILE=... bin/e2e-harness -test.run '^TestE2EServer$' -test.timeout 0 -test.v
func TestE2EServer(t *testing.T) {
	readyFile := os.Getenv(e2eReadyFileEnv)
	if readyFile == "" {
		t.Skip(e2eReadyFileEnv + " is not set: this is the server of the browser end-to-end test (make web-admin-e2e)")
	}
	ta := newTestApp(t, true)
	if dir := os.Getenv(e2eDataDirEnv); dir != "" {
		ta.dir = dir
	}
	seed(t, ta.dir, loopbackAny)
	seedUsage(t, ta.dir, map[string]string{usage.KeyRollupInterval: e2eRollupInterval})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	r := startServe(t, ta, "--engine", "fake")
	b, err := json.Marshal(e2eReady{Admin: "http://" + r.addrs.Admin, Consumer: "http://" + r.addrs.Consumer})
	if err != nil {
		t.Fatal(err)
	}
	// Written whole, then renamed, so the reader never sees half a file.
	tmp := readyFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, readyFile); err != nil {
		t.Fatal(err)
	}
	t.Logf("serving: admin %s, consumer %s", r.addrs.Admin, r.addrs.Consumer)

	var code int
	select {
	case <-ctx.Done():
		code = r.stop()
	case code = <-r.exit:
		t.Errorf("serve exited on its own with %d; stderr is withheld (it holds the recovery code)", code)
	}
	gets, sets := ta.kc.calls()
	t.Logf("stopped with exit %d; fake keychain calls: %d reads, %d writes", code, gets, sets)
	if code != 0 {
		t.Errorf("serve exit = %d", code)
	}
}

// seedUsage writes usage settings to the database in dir before the first start, as seed does
// for the scheduler's settings.
func seedUsage(t *testing.T, dir string, kv map[string]string) {
	t.Helper()
	ctx := context.Background()
	d, err := openDatabase(ctx, filepath.Join(dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer d.db.Close()
	if err := d.st.Register(usage.Defs()...); err != nil {
		t.Fatal(err)
	}
	for k, v := range kv {
		if err := d.st.Set(ctx, settings.Host, k, "", v); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
	}
}
