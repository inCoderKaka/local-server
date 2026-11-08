package snapshot_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot"
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot/folder"
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot/gzipcodec"
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot/s3"
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot/s3fake"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// fullModules is the registry of cmd/local-server (registry.go), in the same order: the whole
// migrated schema. cmd/local-server's TestBackupDefaultsOverRegistry checks the real registry
// against the same rule, so a module added there and not here cannot slip past.
func fullModules() []storage.Module {
	return []storage.Module{settings.Module(), host.Module(), identity.Module(), grants.Module(), audit.Module(),
		events.Module(), usage.Module(), ledger.Module(), scheduler.Module()}
}

const (
	bucket    = "backups"
	accessKey = "AKIDTESTKEY0001"
	secretKey = "SECRET-never-appears-anywhere-1234567890"
)

// knownPayload stands for the sealed bytes of a queued job's request: ciphertext that must not
// reach a snapshot with default settings.
var knownPayload = append([]byte("KNOWN-PAYLOAD-CIPHERTEXT-"), bytes.Repeat([]byte{0xA7, 0x13, 0x5C}, 40)...)

type env struct {
	t     *testing.T
	ctx   context.Context
	dir   string
	db    *storage.DB
	st    *settings.Store
	keys  *hostkeys.Keys
	code  string
	bus   *events.Bus
	logs  *syncBuffer
	clock *fakeClock
}

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

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := storage.Open(ctx, storage.DefaultConfig(filepath.Join(dir, "live.db"), fullModules()...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := settings.New(db)
	if err := snapshot.RegisterSettings(ctx, db, st); err != nil {
		t.Fatal(err)
	}
	master := make([]byte, hostkeys.KeySize)
	rand.Read(master)
	keys, code, err := hostkeys.Open(ctx, db, st, master)
	if err != nil || code == "" {
		t.Fatalf("hostkeys: %q %v", code, err)
	}
	e := &env{t: t, ctx: ctx, dir: dir, db: db, st: st, keys: keys, code: code, bus: events.New(),
		logs: &syncBuffer{}, clock: &fakeClock{t: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)}}
	e.seed()
	return e
}

func (e *env) exec(q string, args ...any) {
	e.t.Helper()
	err := e.db.Write(e.ctx, func(tx *storage.Tx) error { _, err := tx.Exec(q, args...); return err })
	if err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
}

func (e *env) set(key, value string) {
	e.t.Helper()
	if err := e.st.Set(e.ctx, settings.Host, key, "", value); err != nil {
		e.t.Fatalf("set %s: %v", key, err)
	}
}

// seed fills kept tables (an admin and a ledger checkpoint), a login session, and four jobs: one
// queued, one running (both with a payload), one succeeded (with a result) and one cancelled.
func (e *env) seed() {
	now := e.clock.Now().UnixMilli()
	e.exec(`INSERT INTO idn_principal (id, kind, created_at, updated_at) VALUES ('p1', 'admin', ?, ?)`, now, now)
	e.exec(`INSERT INTO idn_admin_user (id, principal_id, username, created_at, updated_at) VALUES ('a1', 'p1', 'kept-admin-name', ?, ?)`, now, now)
	e.exec(`INSERT INTO idn_session (id, principal_id, token_hash, created_at, expires_at) VALUES ('s1', 'p1', 'SESSION-TOKEN-HASH-MARKER', ?, ?)`, now, now+1000)
	for _, j := range []struct{ id, status string }{{"job-queued", "queued"}, {"job-running", "running"}, {"job-done", "succeeded"}, {"job-cancelled", "cancelled"}} {
		e.exec(`INSERT INTO sch_job (id, consumer_id, principal_id, access_key_id, grant_version_id, idempotency_key, model, identity, status, created_at)
			VALUES (?, 'c1', 'p1', 'k1', 'g1', ?, 'm', '{}', ?, ?)`, j.id, "idem-"+j.id, j.status, now)
	}
	for _, id := range []string{"job-queued", "job-running"} {
		e.exec(`INSERT INTO sch_job_payload (job_id, sealed, created_at) VALUES (?, ?, ?)`, id, knownPayload, now)
	}
	e.exec(`INSERT INTO sch_job_result (job_id, sealed, receipt, created_at, expires_at) VALUES ('job-done', ?, 'receipt', ?, ?)`, []byte("RESULT-CIPHERTEXT-MARKER"), now, now+1000)
	e.exec(`INSERT INTO ldg_checkpoint (tree_size, root_hash, note, key_id, created_at) VALUES (7, zeroblob(32), 'note', 'kid', ?)`, now)
	e.set(snapshot.KeyFolderPath, filepath.Join(e.dir, "unused"))
}

// spyCodec records every byte handed to the compressor: the uncompressed snapshot.
type spyCodec struct {
	gzipcodec.Codec
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func newSpy() spyCodec { return spyCodec{mu: &sync.Mutex{}, buf: &bytes.Buffer{}} }

func (s spyCodec) NewWriter(w io.Writer) (io.WriteCloser, error) {
	inner, err := s.Codec.NewWriter(w)
	if err != nil {
		return nil, err
	}
	return &spyWriter{WriteCloser: inner, s: s}, nil
}

type spyWriter struct {
	io.WriteCloser
	s spyCodec
}

func (w *spyWriter) Write(p []byte) (int, error) {
	w.s.mu.Lock()
	w.s.buf.Write(p)
	w.s.mu.Unlock()
	return w.WriteCloser.Write(p)
}

// plain returns what the compressor saw.
func (s spyCodec) plain() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.buf.Bytes()...)
}

// factory builds the adapters, as cmd/local-server does.
func factory(_ context.Context, cfg snapshot.TargetConfig) (snapshot.Target, error) {
	switch cfg.Kind {
	case snapshot.TargetFolder:
		return folder.New(cfg.Folder)
	case snapshot.TargetS3:
		return s3.New(cfg.S3, nil)
	}
	return nil, fmt.Errorf("unknown target %q", cfg.Kind)
}

func (e *env) runner(codec snapshot.Compressor) *snapshot.Runner {
	e.t.Helper()
	if codec == nil {
		codec = gzipcodec.Codec{}
	}
	r, err := snapshot.NewRunner(snapshot.RunnerConfig{DB: e.db, Settings: e.st, Keys: e.keys, Events: e.bus,
		Log:     slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Targets: factory, Compressor: codec, Now: e.clock.Now})
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

// useBucket points the backup at an s3fake server with credentials sealed under the master key.
func (e *env) useBucket(fake *s3fake.Server, partSize int) {
	e.t.Helper()
	e.set(snapshot.KeyTarget, snapshot.TargetS3)
	e.set(snapshot.KeyS3Endpoint, fake.URL)
	e.set(snapshot.KeyS3Bucket, bucket)
	e.set(snapshot.KeyS3Prefix, "snaps/")
	e.set(snapshot.KeyS3PartSize, fmt.Sprint(partSize))
	e.set(snapshot.KeyS3PathStyle, "true")
	if err := snapshot.SetCredentials(e.ctx, e.st, e.keys, snapshot.Credentials{AccessKeyID: accessKey, SecretAccessKey: secretKey}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) useFolder(dir string) {
	e.t.Helper()
	e.set(snapshot.KeyTarget, snapshot.TargetFolder)
	e.set(snapshot.KeyFolderPath, dir)
}

// dump returns every table's rows as sorted text lines, keyed by table name.
func dump(t *testing.T, db *storage.DB) map[string][]string {
	t.Helper()
	ctx := context.Background()
	rows, err := db.Reader().Query(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, n)
	}
	rows.Close()
	out := map[string][]string{}
	for _, n := range names {
		var cols int
		if err := db.Reader().QueryRow(ctx, `SELECT count(*) FROM pragma_table_info(?)`, n).Scan(&cols); err != nil {
			t.Fatal(err)
		}
		rs, err := db.Reader().Query(ctx, `SELECT * FROM "`+n+`"`)
		if err != nil {
			t.Fatal(err)
		}
		lines := []string{}
		for rs.Next() {
			vals := make([]any, cols)
			ptrs := make([]any, cols)
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rs.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			lines = append(lines, fmt.Sprint(vals...))
		}
		rs.Close()
		sort.Strings(lines)
		out[n] = lines
	}
	return out
}

func (e *env) restoreOpts(target snapshot.Target, dbPath string) snapshot.RestoreOptions {
	e.t.Helper()
	key, err := hostkeys.RecoverDataKey(e.code)
	if err != nil {
		e.t.Fatal(err)
	}
	return snapshot.RestoreOptions{Target: target, DataKey: key, Compressors: map[string]snapshot.Compressor{"gzip": gzipcodec.Codec{}},
		DBPath: dbPath, Modules: fullModules(), Now: e.clock.Now}
}

func TestBackupThenRestoreThroughTheS3Fake(t *testing.T) {
	e := newEnv(t)
	fake := s3fake.New(bucket, accessKey)
	defer fake.Close()
	fake.PageSize = 2
	e.useBucket(fake, 4096) // small parts: the snapshot must go up as a multipart upload
	spy := newSpy()
	before := dump(t, e.db)

	res, err := e.runner(spy).Backup(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"idn_session", "sch_job_payload", "sch_job_result"}
	if strings.Join(res.Minimized, ",") != strings.Join(want, ",") {
		t.Fatalf("minimized = %v, want the defaults %v", res.Minimized, want)
	}
	keys := fake.Keys()
	if len(keys) != 1 || keys[0] != "snaps/"+res.Name {
		t.Fatalf("stored keys = %v, want snaps/%s", keys, res.Name)
	}
	if fake.Count("POST", "uploads") != 1 || fake.Count("PUT", "partNumber") < 2 || fake.Count("POST", "uploadId") != 1 {
		t.Fatalf("not a multipart upload: %+v", fake.Requests())
	}
	if fake.OpenUploads() != 0 {
		t.Fatal("a multipart upload was left open")
	}

	// Content privacy: the uncompressed snapshot has no byte of the sealed job payload, the result
	// or the session hash, and the stored object shows no plaintext at all.
	plain := spy.plain()
	if len(plain) == 0 || !bytes.HasPrefix(plain, []byte("SQLite format 3\x00")) {
		t.Fatalf("the spy saw %d bytes that are not a database", len(plain))
	}
	for _, secret := range [][]byte{knownPayload, knownPayload[:30], []byte("RESULT-CIPHERTEXT-MARKER"), []byte("SESSION-TOKEN-HASH-MARKER")} {
		if bytes.Contains(plain, secret) {
			t.Fatalf("the uncompressed snapshot contains %q", secret[:16])
		}
	}
	if !bytes.Contains(plain, []byte("kept-admin-name")) {
		t.Fatal("a kept table's row is not in the snapshot")
	}
	stored, _ := fake.Object(keys[0])
	for _, marker := range [][]byte{[]byte("SQLite format 3"), []byte("kept-admin-name"), []byte(secretKey)} {
		if bytes.Contains(stored, marker) {
			t.Fatalf("the stored object shows plaintext %q", marker)
		}
	}

	// Restore with the recovery code.
	tgt, err := s3.New(snapshot.S3Config{Endpoint: fake.URL, Region: "auto", Bucket: bucket, Prefix: "snaps/", PathStyle: true, PartSize: 4096,
		Credentials: snapshot.Credentials{AccessKeyID: accessKey, SecretAccessKey: secretKey}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(t.TempDir(), "new", "local-server.db")
	rep, err := snapshot.Restore(e.ctx, e.restoreOpts(tgt, restored))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Snapshot != res.Name || strings.Join(rep.Minimized, ",") != strings.Join(want, ",") {
		t.Fatalf("report: %+v", rep)
	}
	if rep.JobsFailed != 2 {
		t.Fatalf("report counts %d jobs failed(payload_minimized), want 2", rep.JobsFailed)
	}
	if rep.LatestCheckpointSize != 7 {
		t.Fatalf("report checkpoint size = %d, want 7 (req_001 D-109)", rep.LatestCheckpointSize)
	}
	if rep.PreviousDatabase != "" {
		t.Fatalf("no database existed, but the report keeps one at %s", rep.PreviousDatabase)
	}
	for _, s := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(restored + s); err == nil {
			t.Fatalf("%s left next to the restored database", s)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(restored))
	if len(entries) != 1 {
		t.Fatalf("the restore left %d files in the data folder, want only the database", len(entries))
	}

	db2, err := storage.Open(e.ctx, storage.DefaultConfig(restored, fullModules()...))
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	after := dump(t, db2)
	minimized := map[string]bool{"idn_session": true, "sch_job_payload": true, "sch_job_result": true}
	for table, rows := range before {
		switch {
		case minimized[table]:
			if len(after[table]) != 0 {
				t.Errorf("%s: %d rows survived minimize", table, len(after[table]))
			}
		case table == "sch_job":
			// Queued and running jobs come back failed(payload_minimized) (D-68); the rest as they were.
			for _, id := range []string{"job-queued", "job-running"} {
				var st, reason string
				if err := db2.Reader().QueryRow(e.ctx, `SELECT status, failure_reason FROM sch_job WHERE id=?`, id).Scan(&st, &reason); err != nil || st != "failed" || reason != "payload_minimized" {
					t.Errorf("%s restored as %s/%s (%v), want failed/payload_minimized", id, st, reason, err)
				}
			}
			for _, id := range []string{"job-done", "job-cancelled"} {
				var st string
				if err := db2.Reader().QueryRow(e.ctx, `SELECT status FROM sch_job WHERE id=?`, id).Scan(&st); err != nil || (id == "job-done") != (st == "succeeded") {
					t.Errorf("%s restored as %s (%v)", id, st, err)
				}
			}
			if len(after[table]) != 4 {
				t.Errorf("sch_job has %d rows, want 4", len(after[table]))
			}
		default:
			if strings.Join(rows, "\n") != strings.Join(after[table], "\n") {
				t.Errorf("%s: rows differ after restore\nbefore %v\nafter  %v", table, rows, after[table])
			}
		}
	}
	if n := len(before["idn_admin_user"]); n != 1 {
		t.Fatalf("the test seeded %d admins, want 1", n)
	}
}

func TestBackupToFolderThenRestoreAndWrongCodeWritesNothing(t *testing.T) {
	e := newEnv(t)
	dest := filepath.Join(t.TempDir(), "off-host")
	e.useFolder(dest)
	res, err := e.runner(nil).Backup(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	tgt, err := folder.New(dest)
	if err != nil {
		t.Fatal(err)
	}

	// A wrong recovery code fails and writes nothing: no database, no temporary file.
	dataDir := filepath.Join(t.TempDir(), "data")
	opts := e.restoreOpts(tgt, filepath.Join(dataDir, "local-server.db"))
	wrong := make([]byte, 32)
	rand.Read(wrong)
	opts.DataKey = wrong
	if _, err := snapshot.Restore(e.ctx, opts); !errors.Is(err, snapshot.ErrWrongKey) {
		t.Fatalf("wrong code: %v", err)
	}
	if entries, _ := os.ReadDir(dataDir); len(entries) != 0 {
		t.Fatalf("a failed restore left %d files in the data folder", len(entries))
	}

	// An existing database (with a write-ahead log) is kept aside, never overwritten.
	for name, content := range map[string]string{"local-server.db": "OLD-DB", "local-server.db-wal": "OLD-WAL", "local-server.db-shm": "OLD-SHM"} {
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	guarded := false
	opts = e.restoreOpts(tgt, filepath.Join(dataDir, "local-server.db"))
	opts.Guard = func(context.Context) error { guarded = true; return nil }
	rep, err := snapshot.Restore(e.ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !guarded || rep.Snapshot != res.Name || rep.PreviousDatabase == "" {
		t.Fatalf("report %+v guarded=%v", rep, guarded)
	}
	for suffix, content := range map[string]string{"": "OLD-DB", "-wal": "OLD-WAL", "-shm": "OLD-SHM"} {
		b, err := os.ReadFile(rep.PreviousDatabase + suffix)
		if err != nil || string(b) != content {
			t.Fatalf("kept %s: %q %v", suffix, b, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dataDir, "local-server.db-wal")); err == nil {
		t.Fatal("the old write-ahead log is still next to the restored database")
	}
	if b, _ := os.ReadFile(filepath.Join(dataDir, "local-server.db")); !bytes.HasPrefix(b, []byte("SQLite format 3")) {
		t.Fatal("the restored file is not a database")
	}

	// A refusing Guard leaves everything as it was.
	opts = e.restoreOpts(tgt, filepath.Join(dataDir, "local-server.db"))
	opts.Guard = func(context.Context) error { return errors.New("host is running") }
	before, _ := os.ReadFile(filepath.Join(dataDir, "local-server.db"))
	if _, err := snapshot.Restore(e.ctx, opts); err == nil || !strings.Contains(err.Error(), "host is running") {
		t.Fatalf("guard: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dataDir, "local-server.db"))
	if !bytes.Equal(before, after) {
		t.Fatal("a refused restore changed the database")
	}
	entries, _ := os.ReadDir(dataDir)
	for _, en := range entries {
		if strings.HasPrefix(en.Name(), ".restore-") {
			t.Fatalf("temporary file %s left behind", en.Name())
		}
	}
}

func TestRestoreWithNoSnapshotAndUnknownCodec(t *testing.T) {
	e := newEnv(t)
	dest := filepath.Join(t.TempDir(), "empty")
	tgt, _ := folder.New(dest)
	if _, err := snapshot.Restore(e.ctx, e.restoreOpts(tgt, filepath.Join(t.TempDir(), "x.db"))); !errors.Is(err, snapshot.ErrNoSnapshots) {
		t.Fatalf("empty target: %v", err)
	}
	e.useFolder(dest)
	if _, err := e.runner(nil).Backup(e.ctx); err != nil {
		t.Fatal(err)
	}
	opts := e.restoreOpts(tgt, filepath.Join(t.TempDir(), "x.db"))
	opts.Compressors = map[string]snapshot.Compressor{"zstd": gzipcodec.Codec{}}
	if _, err := snapshot.Restore(e.ctx, opts); err == nil || !strings.Contains(err.Error(), "gzip") {
		t.Fatalf("unknown codec: %v", err)
	}
}

func TestMinimizeSettingWritesRespectD69(t *testing.T) {
	e := newEnv(t)
	setErr := func(table, v string) error {
		return e.st.Set(e.ctx, settings.Host, snapshot.MinimizeKey(table), "", v)
	}
	// sch_job is referenced by sch_job_payload and sch_job_result, which default to minimize: allowed.
	if err := setErr("sch_job", "true"); err != nil {
		t.Fatalf("minimize sch_job with both referrers minimized: %v", err)
	}
	// Keeping a referrer of a minimized table is refused, naming both.
	err := setErr("sch_job_payload", "false")
	if !errors.Is(err, settings.ErrRejected) || !strings.Contains(err.Error(), "sch_job_payload") || !strings.Contains(err.Error(), "sch_job") {
		t.Fatalf("keeping a referrer of a minimized table: %v", err)
	}
	if err := e.st.Reset(e.ctx, settings.Host, snapshot.MinimizeKey("sch_job"), ""); err != nil {
		t.Fatal(err)
	}
	// Now keep the payload, then try to minimize its parent: refused, naming the kept referrer.
	if err := setErr("sch_job_payload", "false"); err != nil {
		t.Fatal(err)
	}
	err = setErr("sch_job", "true")
	if !errors.Is(err, settings.ErrRejected) || !strings.Contains(err.Error(), "sch_job_payload") {
		t.Fatalf("minimize a table whose referrer is kept: %v", err)
	}
	if v, _ := e.st.Bool(e.ctx, snapshot.MinimizeKey("sch_job"), ""); v {
		t.Fatal("the refused write was stored")
	}
	// A table referenced through others: idn_principal is referenced by many kept tables.
	err = setErr("idn_principal", "true")
	if !errors.Is(err, settings.ErrRejected) || !strings.Contains(err.Error(), "aud_event") || !strings.Contains(err.Error(), "idn_admin_user") {
		t.Fatalf("minimize idn_principal: %v", err)
	}
	// Other settings are validated too.
	for key, v := range map[string]string{snapshot.KeyInterval: "0s", snapshot.KeyTarget: "ftp", snapshot.KeyS3Endpoint: "ftp://x",
		snapshot.KeyChunkBytes: "0", snapshot.KeyS3SealedSecret: secretKey, snapshot.KeyRetentionCount: "-1"} {
		if err := e.st.Set(e.ctx, settings.Host, key, "", v); !errors.Is(err, settings.ErrRejected) {
			t.Errorf("%s=%q: %v, want a refusal", key, v, err)
		}
	}
}

// TestProtectedTablesCannotBeMinimized covers req_001 D-139: the minimize switch of
// ops_schema_migrations, hst_host and hst_key refuses every write, names the table and the reason,
// stores nothing, and a stored value an older database holds is never honored.
func TestProtectedTablesCannotBeMinimized(t *testing.T) {
	e := newEnv(t)
	for _, table := range []string{"ops_schema_migrations", "hst_host", "hst_key"} {
		for _, v := range []string{"true", "false"} {
			t.Run(table+"="+v, func(t *testing.T) {
				err := e.st.Set(e.ctx, settings.Host, snapshot.MinimizeKey(table), "", v)
				if !errors.Is(err, settings.ErrRejected) || !strings.Contains(err.Error(), "table "+table+" is protected") ||
					!strings.Contains(err.Error(), "always included in a backup") || !strings.Contains(err.Error(), "keys") {
					t.Fatalf("write %s=%s: %v, want a refusal naming the table and why", table, v, err)
				}
				var n int
				if err := e.db.Reader().QueryRow(e.ctx, `SELECT count(*) FROM hst_setting WHERE key = ?`, snapshot.MinimizeKey(table)).Scan(&n); err != nil || n != 0 {
					t.Fatalf("%d rows stored after a refused write (%v)", n, err)
				}
			})
		}
		def, ok := e.st.Definition(snapshot.MinimizeKey(table))
		if !ok || def.Default != "false" || def.Owner != settings.Host || !strings.Contains(def.Description, "protected") {
			t.Errorf("%s definition = %+v", table, def)
		}
		if !snapshot.IsProtected(table) || snapshot.DefaultMinimize(table) {
			t.Errorf("%s: IsProtected=%v DefaultMinimize=%v", table, snapshot.IsProtected(table), snapshot.DefaultMinimize(table))
		}
	}
	if snapshot.IsProtected("sch_job_payload") || snapshot.IsProtected("idn_session") {
		t.Error("a non-protected table reports protected")
	}
	if got := strings.Join(snapshot.ProtectedTables(), ","); got != "hst_host,hst_key,ops_schema_migrations" {
		t.Errorf("ProtectedTables = %s", got)
	}

	// A non-protected table still works as before.
	if err := e.st.Set(e.ctx, settings.Host, snapshot.MinimizeKey("sch_job_payload"), "", "false"); err != nil {
		t.Fatalf("non-protected table: %v", err)
	}

	// An older database holds a stored "true": the configuration, the D-69 check and a backup keep
	// the table.
	e.exec(`INSERT INTO hst_setting (key, scope, value, updated_by, updated_at) VALUES (?, '', 'true', 'host', 0)`, snapshot.MinimizeKey("hst_key"))
	cfg, err := snapshot.LoadConfig(e.ctx, e.db, e.st)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Minimize["hst_key"] {
		t.Fatal("a stored true for hst_key was honored")
	}
	if got := strings.Join(cfg.MinimizedTables(), ","); got != "idn_session,sch_job_result" {
		t.Fatalf("minimized tables = %s", got)
	}
	refs, err := e.db.ForeignKeyReferrers(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if merr := snapshot.CheckMinimize(refs, cfg.Minimize); merr != nil {
		t.Fatalf("D-69 check: %v", merr)
	}
	e.useFolder(filepath.Join(t.TempDir(), "off-host"))
	res, err := e.runner(nil).Backup(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Minimized, ",") != "idn_session,sch_job_result" {
		t.Fatalf("backup minimized %v, want hst_key kept", res.Minimized)
	}
}

func TestShippedDefaultsSatisfyD69OverTheFullSchema(t *testing.T) {
	e := newEnv(t)
	cfg, err := snapshot.LoadConfig(e.ctx, e.db, e.st)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := e.db.ForeignKeyReferrers(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if merr := snapshot.CheckMinimize(refs, cfg.Minimize); merr != nil {
		t.Fatalf("the shipped defaults break D-69: %v", merr)
	}
	if got := strings.Join(cfg.MinimizedTables(), ","); got != "idn_session,sch_job_payload,sch_job_result" {
		t.Fatalf("default minimized tables = %s", got)
	}
	// Every table of the catalog has a switch, and the switches are the host's, with descriptions.
	var n int
	if err := e.db.Reader().QueryRow(e.ctx, `SELECT count(DISTINCT table_name) FROM ops_data_catalog`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n < 20 || len(cfg.Minimize) != n {
		t.Fatalf("catalog has %d tables, %d switches", n, len(cfg.Minimize))
	}
	for _, d := range e.st.Definitions() {
		if strings.HasPrefix(d.Key, snapshot.KeyMinimizePrefix) && (d.Owner != settings.Host || d.Description == "") {
			t.Errorf("%s: owner %s, description %q", d.Key, d.Owner, d.Description)
		}
	}
}

func TestRunAgainstASchemaThatBreaksD69FailsAndNotifies(t *testing.T) {
	e := newEnv(t)
	dest := filepath.Join(t.TempDir(), "off-host")
	e.useFolder(dest)
	e.set(snapshot.KeyEnabled, "true")
	// A later migration adds a table that references a minimized one and is, as every new table
	// is, kept by default.
	e.exec(`CREATE TABLE tst_added (id INTEGER PRIMARY KEY, job_id TEXT REFERENCES sch_job_payload(job_id)) STRICT`)
	r := e.runner(nil)

	err := r.RunOnce(e.ctx)
	var merr *snapshot.MinimizeError
	if !errors.As(err, &merr) {
		t.Fatalf("RunOnce = %v, want a *MinimizeError", err)
	}
	if !strings.Contains(err.Error(), "tst_added") || !strings.Contains(err.Error(), "sch_job_payload") {
		t.Fatalf("the error does not name the tables: %v", err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("a failed run sent %d files", len(entries))
	}
	// Nothing was emptied in the live database.
	var jobs int
	e.db.Reader().QueryRow(e.ctx, `SELECT count(*) FROM sch_job_payload`).Scan(&jobs)
	if jobs != 2 {
		t.Fatalf("live payload rows = %d, want 2", jobs)
	}
	notes := backupEvents(t, e)
	if len(notes) != 1 {
		t.Fatalf("%d backup.failed events, want 1", len(notes))
	}
	if notes[0].Reason != snapshot.ReasonMinimizeRule || strings.Join(notes[0].Tables, ",") != "sch_job_payload,tst_added" {
		t.Fatalf("event payload %+v", notes[0])
	}
	if !strings.Contains(e.logs.String(), "backup failed") || strings.Contains(e.logs.String(), "KNOWN-PAYLOAD") {
		t.Fatalf("log: %s", e.logs.String())
	}
	// The next failure within the notify interval does not notify again; one after it does.
	e.clock.Advance(5 * time.Minute)
	_ = r.RunOnce(e.ctx)
	if got := len(backupEvents(t, e)); got != 1 {
		t.Fatalf("%d events after a second failure inside the interval, want 1", got)
	}
	e.clock.Advance(2 * time.Hour)
	_ = r.RunOnce(e.ctx)
	if got := len(backupEvents(t, e)); got != 2 {
		t.Fatalf("%d events after a failure past the interval, want 2", got)
	}
}

type failedEvent struct {
	Reason string   `json:"reason"`
	Tables []string `json:"tables"`
}

func backupEvents(t *testing.T, e *env) []failedEvent {
	t.Helper()
	rows, err := e.db.Reader().Query(e.ctx, `SELECT payload FROM ntf_event WHERE type = ? ORDER BY occurred_at, id`, string(events.BackupFailed))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []failedEvent
	for rows.Next() {
		var p string
		rows.Scan(&p)
		var fe failedEvent
		if err := json.Unmarshal([]byte(p), &fe); err != nil {
			t.Fatal(err)
		}
		out = append(out, fe)
	}
	return out
}

func TestRunOnceRecordsSuccessAndPrunesByCountAndAge(t *testing.T) {
	e := newEnv(t)
	dest := filepath.Join(t.TempDir(), "off-host")
	e.useFolder(dest)
	e.set(snapshot.KeyRetentionCount, "3")
	r := e.runner(nil)
	if err := os.MkdirAll(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	// Something that is not ours must survive pruning.
	if err := os.WriteFile(filepath.Join(dest, "notes.txt"), []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := r.RunOnce(e.ctx); err != nil {
			t.Fatal(err)
		}
		e.clock.Advance(time.Minute)
	}
	tgt, _ := folder.New(dest)
	names := listNames(t, tgt)
	if len(names) != 4 || names[0] != "notes.txt" {
		t.Fatalf("after count pruning: %v, want notes.txt and the 3 newest", names)
	}
	last, err := e.st.Int(e.ctx, snapshot.KeyLastSuccessAt, "")
	if err != nil || last != e.clock.Now().Add(-time.Minute).UnixMilli() {
		t.Fatalf("last success %d (%v), want the time of the fifth run", last, err)
	}

	// Age: older than 90 seconds goes, the newest always stays.
	e.set(snapshot.KeyRetentionCount, "0")
	e.set(snapshot.KeyRetentionAge, "90s")
	e.clock.Advance(10 * time.Minute)
	if err := r.RunOnce(e.ctx); err != nil {
		t.Fatal(err)
	}
	names = listNames(t, tgt)
	if len(names) != 2 || names[0] != "notes.txt" {
		t.Fatalf("after age pruning: %v, want notes.txt and the newest", names)
	}
	// Even with every snapshot "too old", the newest stays.
	e.clock.Advance(24 * time.Hour)
	e.set(snapshot.KeyRetentionAge, "1s")
	// (a failed run would have kept all; here the run succeeds and prunes all but the new one)
	if err := r.RunOnce(e.ctx); err != nil {
		t.Fatal(err)
	}
	if names = listNames(t, tgt); len(names) != 2 {
		t.Fatalf("after the last run: %v", names)
	}
	if n := len(backupEvents(t, e)); n != 0 {
		t.Fatalf("%d failure notifications after successful runs", n)
	}
}

func listNames(t *testing.T, tgt snapshot.Target) []string {
	t.Helper()
	objs, err := tgt.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, o := range objs {
		names = append(names, o.Name)
	}
	sort.Strings(names)
	return names
}

func TestCredentialsNeverAppearInLogsErrorsSnapshotsOrSettings(t *testing.T) {
	e := newEnv(t)
	fake := s3fake.New(bucket, accessKey)
	defer fake.Close()
	e.useBucket(fake, 1<<20)
	spy := newSpy()

	// The setting holds the sealed value only.
	raw, err := e.st.String(e.ctx, snapshot.KeyS3SealedSecret, "")
	if err != nil || raw == "" || strings.Contains(raw, secretKey) || strings.Contains(raw, accessKey) {
		t.Fatalf("stored value %q (%v)", raw, err)
	}
	got, err := snapshot.LoadCredentials(e.ctx, e.st, e.keys)
	if err != nil || got.AccessKeyID != accessKey || got.SecretAccessKey != secretKey {
		t.Fatalf("LoadCredentials: %v", err)
	}
	// Credentials do not print.
	for _, s := range []string{fmt.Sprintf("%v", got), fmt.Sprintf("%+v", got), fmt.Sprintf("%#v", got), fmt.Sprintf("%v", snapshot.S3Config{Credentials: got})} {
		if strings.Contains(s, secretKey) || strings.Contains(s, accessKey) {
			t.Fatalf("credentials printed: %s", s)
		}
	}
	var lb bytes.Buffer
	slog.New(slog.NewJSONHandler(&lb, nil)).Info("x", "creds", got)
	if strings.Contains(lb.String(), secretKey) {
		t.Fatalf("credentials logged: %s", lb.String())
	}

	// A good backup: the snapshot (before and after compression) never holds the plain secret.
	if _, err := e.runner(spy).Backup(e.ctx); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(spy.plain(), []byte(secretKey)) {
		t.Fatal("the plain secret is in the uncompressed snapshot")
	}

	// A bucket that rejects the key: the failure is reported without the key.
	if err := snapshot.SetCredentials(e.ctx, e.st, e.keys, snapshot.Credentials{AccessKeyID: "AKIDWRONG0000001", SecretAccessKey: "WRONG-SECRET-9876543210"}); err != nil {
		t.Fatal(err)
	}
	e.set(snapshot.KeyEnabled, "true")
	r := e.runner(nil)
	err = r.RunOnce(e.ctx)
	if err == nil {
		t.Fatal("a rejected key succeeded")
	}
	all := err.Error() + "\n" + e.logs.String()
	for _, secret := range []string{"WRONG-SECRET-9876543210", secretKey} {
		if strings.Contains(all, secret) {
			t.Fatalf("a secret leaked into the error or the log: %s", all)
		}
	}
	if strings.Contains(all, "AKIDWRONG0000001") {
		t.Fatalf("the access key id leaked into the error or the log: %s", all)
	}
	if notes := backupEvents(t, e); len(notes) != 1 || notes[0].Reason != snapshot.ReasonRunFailed {
		t.Fatalf("events %+v", notes)
	}
	// The setting still holds nothing readable.
	raw, _ = e.st.String(e.ctx, snapshot.KeyS3SealedSecret, "")
	if strings.Contains(raw, "WRONG-SECRET") {
		t.Fatal("the new secret is stored in plain text")
	}
	// With no key stored the run says so.
	if err := e.st.Reset(e.ctx, settings.Host, snapshot.KeyS3SealedSecret, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.runner(nil).Backup(e.ctx); !errors.Is(err, snapshot.ErrNoCredentials) {
		t.Fatalf("no credentials: %v", err)
	}
}

func TestMultipartFailureIsAbortedAndLeavesNoObject(t *testing.T) {
	e := newEnv(t)
	fake := s3fake.New(bucket, accessKey)
	defer fake.Close()
	fake.FailPart = 2
	e.useBucket(fake, 4096)
	if _, err := e.runner(nil).Backup(e.ctx); err == nil {
		t.Fatal("a failed part did not fail the backup")
	}
	if fake.OpenUploads() != 0 || len(fake.Keys()) != 0 {
		t.Fatalf("open uploads %d, keys %v: a failed upload left something behind", fake.OpenUploads(), fake.Keys())
	}
	if fake.Count("DELETE", "uploadId") != 1 {
		t.Fatal("the multipart upload was not aborted")
	}
}

func TestTempFilesAreRemovedAndPrivate(t *testing.T) {
	e := newEnv(t)
	tmp := filepath.Join(t.TempDir(), "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	e.set(snapshot.KeyTempDir, tmp)
	e.useFolder(filepath.Join(t.TempDir(), "off-host"))
	if _, err := e.runner(nil).Backup(e.ctx); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
		t.Fatalf("%d temporary entries left after a good run", len(entries))
	}
	// And after a failure partway (the folder cannot be created under a file).
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, []byte("x"), 0o600)
	e.set(snapshot.KeyFolderPath, filepath.Join(blocker, "sub"))
	if _, err := e.runner(nil).Backup(e.ctx); err == nil {
		t.Fatal("an impossible folder succeeded")
	}
	if entries, _ := os.ReadDir(tmp); len(entries) != 0 {
		t.Fatalf("%d temporary entries left after a failed run", len(entries))
	}
}

func TestWorkerBacksUpWhenEnabledAndStopsWithItsContext(t *testing.T) {
	e := newEnv(t)
	dest := filepath.Join(t.TempDir(), "off-host")
	e.useFolder(dest)
	e.set(snapshot.KeyInterval, "20ms")
	// A real clock here: the worker waits in real time.
	r, err := snapshot.NewRunner(snapshot.RunnerConfig{DB: e.db, Settings: e.st, Keys: e.keys, Events: e.bus,
		Log: slog.New(slog.NewTextHandler(e.logs, nil)), Targets: factory, Compressor: gzipcodec.Codec{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	time.Sleep(150 * time.Millisecond)
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("a disabled worker made %d backups", len(entries))
	}
	e.set(snapshot.KeyEnabled, "true")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if entries, _ := os.ReadDir(dest); len(entries) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the worker made no backups; log: %s", e.logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker did not stop when its context ended")
	}
	if last, _ := e.st.Int(e.ctx, snapshot.KeyLastSuccessAt, ""); last == 0 {
		t.Fatal("the worker did not record the last success")
	}
}

// A host that chose to keep job payloads gets its queued jobs back as they were (req_001 D-64,
// D-68): nothing is marked failed and the sealed request is restored byte for byte.
func TestRestoreLeavesJobsAloneWhenThePayloadWasKept(t *testing.T) {
	e := newEnv(t)
	dest := filepath.Join(t.TempDir(), "off-host")
	e.useFolder(dest)
	e.set(snapshot.MinimizeKey("sch_job_payload"), "false")
	res, err := e.runner(nil).Backup(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Minimized, ",") != "idn_session,sch_job_result" {
		t.Fatalf("minimized = %v", res.Minimized)
	}
	tgt, _ := folder.New(dest)
	restored := filepath.Join(t.TempDir(), "local-server.db")
	rep, err := snapshot.Restore(e.ctx, e.restoreOpts(tgt, restored))
	if err != nil {
		t.Fatal(err)
	}
	if rep.JobsFailed != 0 {
		t.Fatalf("%d jobs failed although their payload was kept", rep.JobsFailed)
	}
	db2, err := storage.Open(e.ctx, storage.DefaultConfig(restored, fullModules()...))
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	var status string
	var sealed []byte
	if err := db2.Reader().QueryRow(e.ctx, `SELECT j.status, p.sealed FROM sch_job j JOIN sch_job_payload p ON p.job_id = j.id WHERE j.id = 'job-queued'`).Scan(&status, &sealed); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || !bytes.Equal(sealed, knownPayload) {
		t.Fatalf("queued job restored as %s with %d payload bytes", status, len(sealed))
	}
}
