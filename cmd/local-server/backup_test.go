package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot"
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot/s3fake"
)

// backupSettings makes a host back up to folder every 50ms.
func backupSettings(folder string) map[string]string {
	return merge(loopbackAny, map[string]string{
		snapshot.KeyEnabled: "true", snapshot.KeyTarget: snapshot.TargetFolder, snapshot.KeyFolderPath: folder, snapshot.KeyInterval: "50ms"})
}

func snapshotFiles(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".lsbk") {
			out = append(out, e.Name())
		}
	}
	return out
}

// restoreApp is an app for the restore command over the install of ta, with the given lines on
// stdin (not a terminal), as a pipe would give them.
func restoreApp(t *testing.T, ta *testApp, stdin string) *testApp {
	t.Helper()
	ra := newTestApp(t, false)
	ra.dir, ra.kc, ra.keychain, ra.logDir = ta.dir, ta.kc, ta.kc, ta.logDir
	ra.stdin = strings.NewReader(stdin)
	return ra
}

func fileHash(t *testing.T, path string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

// A running host backs up to a folder; after the database is lost, restore with the recovery
// code brings it back, from the folder named by --from and then from the configured target, and
// the host starts on the restored database.
func TestBackupWorkerThenRestore(t *testing.T) {
	ta := newTestApp(t, true)
	offhost := filepath.Join(t.TempDir(), "offhost")
	seed(t, ta.dir, backupSettings(offhost))
	r := startServe(t, ta, "--engine", "fake")
	m := codeRe.FindStringSubmatch(ta.err.String())
	if m == nil {
		t.Fatalf("no recovery code:\n%s", ta.err.String())
	}
	code := m[1]
	waitFor(t, "a snapshot in the folder", func() bool { return len(snapshotFiles(offhost)) > 0 })
	waitFor(t, "last success recorded", func() bool {
		return count(t, ta.dir, `SELECT count(*) FROM hst_setting WHERE key = 'backup.last_success_at' AND CAST(value AS INTEGER) > 0`) == 1
	})
	if code := r.stop(); code != 0 {
		t.Fatalf("serve exit = %d", code)
	}
	// Disk loss: the database is gone; the master key is still in the keychain.
	dbPath := filepath.Join(ta.dir, dbFileName)
	for _, s := range []string{"", "-wal", "-shm"} {
		os.Remove(dbPath + s)
	}

	// The recovery code is not echoed or written anywhere but stdin.
	ra := restoreApp(t, ta, code+"\n")
	if exit := ra.run(context.Background(), []string{"restore", "--data-dir", ta.dir, "--from", offhost}); exit != 0 {
		t.Fatalf("restore exit = %d; stderr:\n%s", exit, ra.err.String())
	}
	contains(t, "stdout", ra.out.String(), "Restored snapshot", "sch_job_payload", "payload_minimized): 0", "will not\nverify")
	if strings.Contains(ra.out.String()+ra.err.String(), code) {
		t.Fatal("the recovery code was echoed")
	}
	// Same machine, master key still there: no line about the master key (req_001 D-138).
	if strings.Contains(ra.out.String(), "master key") {
		t.Fatalf("a restore on the same machine warned about the master key:\n%s", ra.out.String())
	}
	if !exists(dbPath) {
		t.Fatal("no database after restore")
	}
	if n := count(t, ta.dir, `SELECT count(*) FROM hst_host`); n != 1 {
		t.Fatalf("hst_host rows = %d", n)
	}
	if n := count(t, ta.dir, `SELECT count(*) FROM sch_job_payload`); n != 0 {
		t.Fatalf("payload rows = %d", n)
	}

	// The restored host starts with the same master key, and takes backups again.
	r = startServe(t, ta, "--engine", "fake")
	if code := r.stop(); code != 0 {
		t.Fatalf("serve on the restored database exit = %d; stderr:\n%s", code, ta.err.String())
	}

	// Restore again, now from the configured target and with the database still there: it is kept.
	ra = restoreApp(t, ta, code+"\n")
	if exit := ra.run(context.Background(), []string{"restore", "--data-dir", ta.dir}); exit != 0 {
		t.Fatalf("restore from the configured target exit = %d; stderr:\n%s", exit, ra.err.String())
	}
	contains(t, "stdout", ra.out.String(), "is kept at")
	kept, _ := filepath.Glob(dbPath + ".before-restore-*")
	if len(kept) == 0 {
		t.Fatal("the database that was replaced is not kept")
	}
}

// A snapshot does not carry the master key (req_001 D-138). Restore on a machine whose keychain is
// empty and whose data folder holds no master.key, or holds another key, succeeds and prints one
// line saying the restored host keys cannot be opened; the check changes nothing in the database.
func TestRestoreWithoutTheMasterKeyPrintsOneLine(t *testing.T) {
	ta := newTestApp(t, true)
	offhost := filepath.Join(t.TempDir(), "offhost")
	seed(t, ta.dir, backupSettings(offhost))
	r := startServe(t, ta, "--engine", "fake")
	m := codeRe.FindStringSubmatch(ta.err.String())
	if m == nil {
		t.Fatalf("no recovery code:\n%s", ta.err.String())
	}
	code := m[1]
	waitFor(t, "a snapshot in the folder", func() bool { return len(snapshotFiles(offhost)) > 0 })
	r.stop()
	dbPath := filepath.Join(ta.dir, dbFileName)
	for _, s := range []string{"", "-wal", "-shm"} {
		os.Remove(dbPath + s)
	}

	for name, c := range map[string]struct {
		secret string // what the new machine's keychain holds
		want   string
	}{
		"no master key": {"", "was not found here"},
		"another key":   {base64.StdEncoding.EncodeToString(make([]byte, hostkeys.KeySize)), "does not match"},
	} {
		t.Run(name, func(t *testing.T) {
			ra := restoreApp(t, ta, code+"\n")
			ra.kc = &fakeKeychain{secret: c.secret}
			ra.keychain = ra.kc
			if exit := ra.run(context.Background(), []string{"restore", "--data-dir", ta.dir, "--from", offhost}); exit != 0 {
				t.Fatalf("restore exit = %d; stderr:\n%s", exit, ra.err.String())
			}
			out := ra.out.String()
			contains(t, "stdout", out, "Restored snapshot", "does not carry this machine's master key", c.want, "new identity")
			if n := strings.Count(out, "master key"); n < 1 || strings.Count(out, "Warning: the snapshot does not carry") != 1 {
				t.Fatalf("want exactly one master key line:\n%s", out)
			}
			if strings.Index(out, "does not carry") > strings.Index(out, "Start the host again") {
				t.Fatalf("the master key line comes after the closing line:\n%s", out)
			}
			if strings.Contains(out+ra.err.String(), code) || (c.secret != "" && strings.Contains(out+ra.err.String(), c.secret)) {
				t.Fatal("a secret was printed")
			}
			if exists(filepath.Join(ta.dir, masterKeyFileName)) {
				t.Fatal("the check created a master key file")
			}
			if gets, sets := ra.kc.calls(); sets != 0 || gets == 0 {
				t.Fatalf("keychain gets=%d sets=%d; the check must only read", gets, sets)
			}
			// The check itself leaves the restored database as it is.
			before, entries := fileHash(t, dbPath), dirNames(t, ta.dir)
			if notice := ra.restoredKeysNotice(context.Background(), ta.dir); !strings.Contains(notice, "master key") {
				t.Fatalf("notice = %q", notice)
			}
			if fileHash(t, dbPath) != before || strings.Join(dirNames(t, ta.dir), ",") != strings.Join(entries, ",") {
				t.Fatal("the master key check changed the restored database")
			}
			// What the line says is what serve does today: it refuses to start on keys it cannot open
			// (it does not create a new identity), and says so.
			sa := newTestApp(t, false)
			sa.dir, sa.kc, sa.keychain, sa.logDir = ta.dir, ra.kc, ra.kc, ta.logDir
			if exit := sa.run(context.Background(), []string{"serve", "--data-dir", ta.dir, "--engine", "fake"}); exit == 0 {
				t.Fatal("serve started on a database whose keys it cannot open")
			}
			if n := count(t, ta.dir, `SELECT count(*) FROM hst_host`); n != 1 {
				t.Fatalf("hst_host rows = %d after the refused start", n)
			}
		})
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestRestoreRefusesWithoutTheRightCodeAndWritesNothing(t *testing.T) {
	ta := newTestApp(t, true)
	offhost := filepath.Join(t.TempDir(), "offhost")
	seed(t, ta.dir, backupSettings(offhost))
	r := startServe(t, ta, "--engine", "fake")
	waitFor(t, "a snapshot", func() bool { return len(snapshotFiles(offhost)) > 0 })
	r.stop()
	dbPath := filepath.Join(ta.dir, dbFileName)
	before := fileHash(t, dbPath)
	entriesBefore, _ := os.ReadDir(ta.dir)

	wrong := strings.Repeat("AAAA-", 12) + "AAAA" // a well-formed code of another key
	for name, c := range map[string]struct{ stdin, want string }{
		"wrong code":     {wrong + "\n", "cannot decrypt"},
		"malformed code": {"hello\n", "not valid"},
		"no code":        {"", "nothing was entered"},
	} {
		ra := restoreApp(t, ta, c.stdin)
		if exit := ra.run(context.Background(), []string{"restore", "--data-dir", ta.dir, "--from", offhost}); exit != 1 {
			t.Errorf("%s: exit = %d, want 1", name, exit)
		}
		contains(t, name+": stderr", ra.err.String(), c.want)
		if fileHash(t, dbPath) != before {
			t.Errorf("%s: the database changed", name)
		}
		if entries, _ := os.ReadDir(ta.dir); len(entries) != len(entriesBefore) {
			t.Errorf("%s: the data folder gained files", name)
		}
	}
	// Usage errors.
	ra := restoreApp(t, ta, "")
	if exit := ra.run(context.Background(), []string{"restore", "--data-dir", ta.dir, "--from", offhost, "--bucket", "b"}); exit != 2 {
		t.Errorf("both sources: exit = %d, want 2", exit)
	}
	ra = restoreApp(t, ta, "")
	if exit := ra.run(context.Background(), []string{"restore", "--data-dir", ta.dir, "--from", filepath.Join(ta.dir, "missing")}); exit != 1 {
		t.Errorf("missing folder: exit = %d, want 1", exit)
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return "127.0.0.1:" + strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

func TestRestoreRefusesWhileAHostIsRunning(t *testing.T) {
	ta := newTestApp(t, true)
	offhost := filepath.Join(t.TempDir(), "offhost")
	cfg := backupSettings(offhost)
	cfg[keyConsumerListen], cfg[keyAdminListen] = freePort(t), freePort(t)
	seed(t, ta.dir, cfg)
	r := startServe(t, ta, "--engine", "fake")
	m := codeRe.FindStringSubmatch(ta.err.String())
	waitFor(t, "a snapshot", func() bool { return len(snapshotFiles(offhost)) > 0 })

	ra := restoreApp(t, ta, m[1]+"\n")
	if exit := ra.run(context.Background(), []string{"restore", "--data-dir", ta.dir, "--from", offhost}); exit != 1 {
		t.Fatalf("exit = %d, want 1; stderr:\n%s", exit, ra.err.String())
	}
	contains(t, "stderr", ra.err.String(), "seems to be running")
	if entries, _ := filepath.Glob(filepath.Join(ta.dir, "*.before-restore-*")); len(entries) != 0 {
		t.Fatal("a running host's database was moved")
	}
	if code := r.stop(); code != 0 {
		t.Fatalf("serve exit = %d", code)
	}
}

// The bucket path end to end: the access key is stored sealed through backup-credentials, the
// host backs up to an S3 fake, and restore reads it back by flags and environment.
func TestBackupCredentialsAndBucketRoundTrip(t *testing.T) {
	const keyID, secret = "AKIDCMDTEST0001", "CMD-SECRET-must-stay-out-of-everything"
	fake := s3fake.New("bkt", keyID)
	defer fake.Close()
	ta := newTestApp(t, true)
	seed(t, ta.dir, merge(loopbackAny))
	code := firstRun(t, ta, false)

	// Credentials: from stdin lines (a pipe), never from flags; sealed in the setting.
	ca := restoreApp(t, ta, keyID+"\n"+secret+"\n")
	if exit := ca.run(context.Background(), []string{"backup-credentials", "--data-dir", ta.dir}); exit != 0 {
		t.Fatalf("backup-credentials exit = %d; stderr:\n%s", exit, ca.err.String())
	}
	if strings.Contains(ca.out.String()+ca.err.String(), secret) {
		t.Fatal("the secret was echoed")
	}
	dbBytes, _ := os.ReadFile(filepath.Join(ta.dir, dbFileName))
	walBytes, _ := os.ReadFile(filepath.Join(ta.dir, dbFileName+"-wal"))
	if strings.Contains(string(dbBytes)+string(walBytes), secret) {
		t.Fatal("the secret is in the database file in plain text")
	}
	if n := count(t, ta.dir, `SELECT count(*) FROM hst_setting WHERE key = 'backup.s3.sealed_secret' AND value LIKE 'sealed:v1:%'`); n != 1 {
		t.Fatalf("sealed setting rows = %d", n)
	}
	// And from the environment.
	ca = restoreApp(t, ta, "")
	ca.getenv = func(k string) string { return map[string]string{envBackupKeyID: keyID, envBackupSecret: secret}[k] }
	if exit := ca.run(context.Background(), []string{"backup-credentials", "--data-dir", ta.dir}); exit != 0 {
		t.Fatalf("backup-credentials from the environment exit = %d; stderr:\n%s", exit, ca.err.String())
	}
	// A host that was never set up cannot take credentials.
	empty := newTestApp(t, true)
	empty.stdin = strings.NewReader("a\nb\n")
	if exit := empty.run(context.Background(), []string{"backup-credentials", "--data-dir", empty.dir}); exit != 1 {
		t.Fatalf("exit without a host = %d", exit)
	}

	seed(t, ta.dir, merge(loopbackAny, map[string]string{
		snapshot.KeyEnabled: "true", snapshot.KeyTarget: snapshot.TargetS3, snapshot.KeyInterval: "50ms",
		snapshot.KeyS3Endpoint: fake.URL, snapshot.KeyS3Bucket: "bkt", snapshot.KeyS3Prefix: "host/", snapshot.KeyS3PartSize: "4096"}))
	r := startServe(t, ta, "--engine", "fake")
	waitFor(t, "a snapshot in the bucket", func() bool { return len(fake.Keys()) > 0 })
	if code := r.stop(); code != 0 {
		t.Fatalf("serve exit = %d", code)
	}
	if fake.Count("POST", "uploads") == 0 {
		t.Fatal("the snapshot did not go up as a multipart upload with a 4096-byte part size")
	}
	if strings.Contains(ta.err.String(), secret) {
		t.Fatal("the secret reached stderr")
	}
	logs, _ := os.ReadFile(filepath.Join(ta.logDir, logFileName))
	if strings.Contains(string(logs), secret) {
		t.Fatal("the secret reached the log file")
	}

	// Restore from the bucket by flags, with the key in the environment, onto a machine that has
	// lost its data folder (the master key is the one thing it kept).
	os.Remove(filepath.Join(ta.dir, dbFileName))
	os.Remove(filepath.Join(ta.dir, dbFileName+"-wal"))
	os.Remove(filepath.Join(ta.dir, dbFileName+"-shm"))
	ra := restoreApp(t, ta, "")
	ra.getenv = func(k string) string {
		return map[string]string{envBackupKeyID: keyID, envBackupSecret: secret, envRecoveryCode: code}[k]
	}
	if exit := ra.run(context.Background(), []string{"restore", "--data-dir", ta.dir, "--bucket", "bkt", "--endpoint", fake.URL, "--prefix", "host/"}); exit != 0 {
		t.Fatalf("restore from the bucket exit = %d; stderr:\n%s", exit, ra.err.String())
	}
	contains(t, "stdout", ra.out.String(), "Restored snapshot")
	if n := count(t, ta.dir, `SELECT count(*) FROM hst_host`); n != 1 {
		t.Fatalf("hst_host rows after restore = %d", n)
	}
	// Without the key in the environment the bucket restore says what is missing.
	ra = restoreApp(t, ta, code+"\n")
	if exit := ra.run(context.Background(), []string{"restore", "--data-dir", ta.dir, "--bucket", "bkt"}); exit != 1 {
		t.Fatalf("exit = %d", exit)
	}
	contains(t, "stderr", ra.err.String(), envBackupKeyID)
}

// The rule of req_001 D-69 holds for the shipped defaults over the registry of this build: a
// module added to the registry whose tables break it fails here.
func TestBackupDefaultsSatisfyD69OverTheRegistry(t *testing.T) {
	ctx := context.Background()
	d, err := openDatabase(ctx, filepath.Join(t.TempDir(), dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer d.db.Close()
	cfg, err := snapshot.LoadConfig(ctx, d.db, d.st)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := d.db.ForeignKeyReferrers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if merr := snapshot.CheckMinimize(refs, cfg.Minimize); merr != nil {
		t.Fatalf("the shipped defaults break D-69: %v", merr)
	}
	if got := strings.Join(cfg.MinimizedTables(), ","); got != "idn_session,sch_job_payload,sch_job_result" {
		t.Fatalf("default minimized tables = %s", got)
	}
}
