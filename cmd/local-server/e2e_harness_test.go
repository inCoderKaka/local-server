package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/transport"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// The environment of the browser end-to-end harness (web/e2e, `make web-e2e`, D-152).
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
	// Page is the consumer page listener (https, the test certificate), on a loopback address.
	Page string `json:"page"`
}

// e2eTailscale is the ready fake Tailscale of the tailnet tests with one difference: it hands its
// test certificate to every TLS hello, whatever name it asks for. A browser that opens
// https://127.0.0.1:<port> sends no server name, and the browser test accepts the self-signed
// certificate. Production code is not changed: the consumer page listener still serves only TLS,
// and its credential guard still accepts only loopback and the host's names in Host.
type e2eTailscale struct{ *fakeTailscale }

func (f e2eTailscale) TLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.cert, nil
	}}
}

// TestE2EServer is not a unit test: it is the server the Playwright end-to-end test drives
// (web/e2e). It runs the real `serve --engine fake` code in this process with the same fakes as
// the other tests here: an in-memory keychain (the OS keychain is never touched), a fake
// terminal (so the first run can start without one), a fake sleep lock and no Tailscale. The
// engine is the fake engine, so no Ollama is needed. The consumer API and admin listeners take a
// free loopback port. Tailscale is a ready fake whose tailnet listeners are redirected to free
// loopback ports too, so the consumer page listener (TLS only, as on a real tailnet) runs for the
// consumer page test. The URLs go to the ready file as JSON. It serves until SIGINT or SIGTERM,
// then stops serve.
//
// Run it as a compiled test binary so the signals reach it:
//
//	CGO_ENABLED=0 go test -c -o bin/e2e-harness ./cmd/local-server
//	LS_E2E_READY_FILE=... bin/e2e-harness -test.run '^TestE2EServer$' -test.timeout 0 -test.v
func TestE2EServer(t *testing.T) {
	readyFile := os.Getenv(e2eReadyFileEnv)
	if readyFile == "" {
		t.Skip(e2eReadyFileEnv + " is not set: this is the server of the browser end-to-end tests (make web-e2e)")
	}
	ta := newTestApp(t, true)
	if dir := os.Getenv(e2eDataDirEnv); dir != "" {
		ta.dir = dir
	}
	ts, _ := newReadyTailscale(t)
	ta.transport = e2eTailscale{ts}
	ta.tailnetListen = (&redirectListen{}).listen
	seed(t, ta.dir, loopbackAny)
	seedUsage(t, ta.dir, map[string]string{usage.KeyRollupInterval: e2eRollupInterval})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	r := startServe(t, ta, "--engine", "fake")
	page := upByRole(r.addrs, transport.RoleConsumerPage)
	if page == "" {
		r.stop()
		t.Fatal("serve did not open the consumer page listener")
	}
	b, err := json.Marshal(e2eReady{Admin: "http://" + r.addrs.Admin, Consumer: "http://" + r.addrs.Consumer, Page: "https://" + page})
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
	t.Logf("serving: admin %s, consumer %s, consumer page %s", r.addrs.Admin, r.addrs.Consumer, page)

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
