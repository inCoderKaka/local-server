package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// syncBuffer is a bytes.Buffer safe for a server goroutine to write while a test reads.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// fakeKeychain is an in-memory hostkeys.Keychain that counts its calls. Tests never touch the
// developer's real keychain.
type fakeKeychain struct {
	mu     sync.Mutex
	secret string
	gets   int
	sets   int
	// block, when set, makes Get wait for it to close.
	block chan struct{}
}

func (f *fakeKeychain) Get(_, _ string) (string, error) {
	f.mu.Lock()
	f.gets++
	block := f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.secret == "" {
		return "", hostkeys.ErrKeychainNotFound
	}
	return f.secret, nil
}

func (f *fakeKeychain) Set(_, _, secret string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets++
	f.secret = secret
	return nil
}

func (f *fakeKeychain) calls() (gets, sets int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gets, f.sets
}

// testApp is an app whose terminal, keychain and environment are fakes.
type testApp struct {
	*app
	out, err *syncBuffer
	kc       *fakeKeychain
	dir      string
	logDir   string // the user log folder, outside the data directory
}

func newTestApp(t *testing.T, terminal bool) *testApp {
	t.Helper()
	ta := &testApp{out: &syncBuffer{}, err: &syncBuffer{}, kc: &fakeKeychain{}, dir: t.TempDir(), logDir: t.TempDir()}
	ta.app = &app{
		stdout: ta.out, stderr: ta.err,
		getenv:           func(string) string { return "" },
		userConfigDir:    func() (string, error) { return filepath.Join(ta.dir, "config"), nil },
		userLogDir:       func() (string, error) { return ta.logDir, nil },
		stderrIsTerminal: func() bool { return terminal },
		stdinIsTerminal:  func() bool { return terminal },
		stdin:            strings.NewReader(""),
		readPassword:     func() (string, error) { return "", io.EOF },
		keychain:         ta.kc,
	}
	return ta
}

// seed opens the database in dir with the registry, writes the settings and closes it, as a host
// changing settings between two starts would.
func seed(t *testing.T, dir string, kv map[string]string) {
	t.Helper()
	ctx := context.Background()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	d, err := openDatabase(ctx, filepath.Join(dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer d.db.Close()
	if err := d.st.Register(scheduler.Defs()...); err != nil { // settings that serve registers when it builds the scheduler
		t.Fatal(err)
	}
	for k, v := range kv {
		if err := d.st.Set(ctx, settings.Host, k, "", v); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
	}
}

// count runs a count query on the database in dir, opened read-only.
func count(t *testing.T, dir, query string) int {
	t.Helper()
	db, err := storage.OpenReadOnly(context.Background(), filepath.Join(dir, dbFileName), registryModules()...)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.Reader().QueryRow(context.Background(), query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func contains(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s = %q; want it to contain %q", what, got, w)
		}
	}
}

// waitFor polls cond for up to five seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
