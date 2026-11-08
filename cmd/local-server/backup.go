package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/inCoderKaka/local-server/internal/admin"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot"
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot/folder"
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot/gzipcodec"
	"github.com/inCoderKaka/local-server/internal/offhost/snapshot/s3"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// The backup adapters are wired here and only here (docs/glossary.md "Adapter"): the snapshot
// package defines the Compressor and Target ports; cmd picks gzip, the local folder and the
// S3-compatible bucket (req_001 D-41, D-103).

// backupTargets is the snapshot.TargetFactory of the host.
func backupTargets(_ context.Context, cfg snapshot.TargetConfig) (snapshot.Target, error) {
	switch cfg.Kind {
	case snapshot.TargetFolder:
		return folder.New(cfg.Folder)
	case snapshot.TargetS3:
		return s3.New(cfg.S3, nil)
	}
	return nil, fmt.Errorf("setting %s is %q; it must be %s or %s", snapshot.KeyTarget, cfg.Kind, snapshot.TargetS3, snapshot.TargetFolder)
}

// backupCompressor is the default compressor (gzip), and restoreCompressors the codecs restore
// can read, by the name a snapshot's header carries.
func backupCompressor() snapshot.Compressor { return gzipcodec.Codec{} }

func restoreCompressors() map[string]snapshot.Compressor {
	c := backupCompressor()
	return map[string]snapshot.Compressor{c.Name(): c}
}

// Environment variables the restore and backup-credentials commands read. The recovery code and
// the bucket secret are never taken from a flag, which a process list would show.
const (
	envRecoveryCode = "LS_RECOVERY_CODE"
	envBackupKeyID  = "LS_BACKUP_ACCESS_KEY_ID"
	envBackupSecret = "LS_BACKUP_SECRET_ACCESS_KEY"
)

// readSecretLine reads one line: from the terminal without echo when stdin is one, else from
// the next line of stdin (for a pipe). The prompt goes to stderr.
func (a *app) readSecretLine(ctx context.Context, in *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(a.stderr, prompt)
	var s string
	var err error
	if a.stdinIsTerminal() {
		s, err = promptOrCancel(ctx, a.readPassword)
		fmt.Fprintln(a.stderr)
	} else {
		s, err = promptOrCancel(ctx, func() (string, error) { return in.ReadString('\n') })
		if err == io.EOF && s != "" {
			err = nil
		}
	}
	if ctx.Err() != nil {
		return "", errors.New("interrupted")
	}
	if err != nil {
		return "", errors.New("nothing was entered")
	}
	return strings.TrimSpace(s), nil
}

// readVisibleLine reads one line that may be shown (a bucket access key id).
func (a *app) readVisibleLine(ctx context.Context, in *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(a.stderr, prompt)
	s, err := promptOrCancel(ctx, func() (string, error) { return in.ReadString('\n') })
	if ctx.Err() != nil {
		return "", errors.New("interrupted")
	}
	if err != nil && (err != io.EOF || s == "") {
		return "", errors.New("nothing was entered")
	}
	return strings.TrimSpace(s), nil
}

// backupCredentials is `local-server backup-credentials`: it stores the bucket access key sealed
// with the host master key in the setting backup.s3.sealed_secret (req_001 D-40). The id and secret
// come from LS_BACKUP_ACCESS_KEY_ID and LS_BACKUP_SECRET_ACCESS_KEY, else from prompts (the
// secret is not echoed). Neither is printed or logged. Exit codes: 0 stored, 1 failed, 2 usage.
func (a *app) backupCredentials(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("backup-credentials", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	dataDirFlag := fs.String("data-dir", "", "data directory (default: $LS_DATA_DIR, else local-server in the user configuration directory)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(a.stderr, "local-server backup-credentials: no arguments are accepted")
		return 2
	}
	dir, err := a.dataDir(*dataDirFlag)
	if err != nil {
		fmt.Fprintf(a.stderr, "local-server: %v\n", err)
		return 1
	}
	if st, err := term.GetState(int(os.Stdin.Fd())); err == nil {
		defer term.Restore(int(os.Stdin.Fd()), st)
	}
	if err := a.runBackupCredentials(ctx, dir); err != nil {
		fmt.Fprintf(a.stderr, "local-server backup-credentials: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.stdout, "The bucket access key is stored, sealed with this host's master key (setting %s).\n", snapshot.KeyS3SealedSecret)
	return 0
}

func (a *app) runBackupCredentials(ctx context.Context, dataDir string) error {
	d, keys, err := a.openExistingHost(ctx, dataDir)
	if err != nil {
		return err
	}
	defer d.db.Close()

	var c snapshot.Credentials
	c.AccessKeyID, c.SecretAccessKey = a.getenv(envBackupKeyID), a.getenv(envBackupSecret)
	if c.AccessKeyID == "" || c.SecretAccessKey == "" {
		in := bufio.NewReader(a.stdin)
		if c.AccessKeyID, err = a.readVisibleLine(ctx, in, "Bucket access key id: "); err != nil {
			return err
		}
		if c.SecretAccessKey, err = a.readSecretLine(ctx, in, "Bucket secret access key: "); err != nil {
			return err
		}
	}
	// The audit row never holds the key: both values are redacted. No login here, so the actor is
	// the system (req_001 D-07).
	record := func(tx *storage.Tx) error {
		_, err := audit.Record(tx, audit.Event{Action: admin.ActionSettingChanged,
			Detail: map[string]string{"key": snapshot.KeyS3SealedSecret, "old_value": "[redacted]", "new_value": "[redacted]"}})
		return err
	}
	if err := snapshot.SetCredentials(ctx, d.st, keys, c, record); err != nil {
		return err
	}
	return nil
}

// openExistingHost opens the database of an installed host and its keys, for a command that runs
// beside or without `serve`. It never creates a host or a key.
func (a *app) openExistingHost(ctx context.Context, dataDir string) (*database, *hostkeys.Keys, error) {
	dbPath := filepath.Join(dataDir, dbFileName)
	if _, err := os.Stat(dbPath); err != nil {
		return nil, nil, fmt.Errorf("no host database in %s; if this host keeps its data elsewhere (a service may set LS_DATA_DIR), pass --data-dir; otherwise run `local-server serve` first", dataDir)
	}
	d, err := openDatabase(ctx, dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open the database: %w", err)
	}
	keys, err := a.openKeysNoCreate(ctx, d, dataDir)
	if err != nil {
		d.db.Close()
		return nil, nil, err
	}
	return d, keys, nil
}

// openKeysNoCreate loads the master key (never creating one) and opens key custody.
func (a *app) openKeysNoCreate(ctx context.Context, d *database, dataDir string) (*hostkeys.Keys, error) {
	if done, err := hostkeys.Bootstrapped(ctx, d.db); err != nil {
		return nil, fmt.Errorf("read host state: %w", err)
	} else if !done {
		return nil, errors.New("this host has not been set up yet: run `local-server serve` first")
	}
	timeout, err := d.st.Duration(ctx, hostkeys.KeyKeychainTimeout, "")
	if err != nil {
		return nil, fmt.Errorf("read setting %s: %w", hostkeys.KeyKeychainTimeout, err)
	}
	master, _, err := hostkeys.LoadMasterKey(hostkeys.MasterKeyConfig{
		FallbackPath: filepath.Join(dataDir, masterKeyFileName), Create: false, Timeout: timeout, Keychain: a.keychain})
	if err != nil {
		return nil, err
	}
	keys, fresh, err := hostkeys.Open(ctx, d.db, d.st, master)
	if err != nil {
		return nil, fmt.Errorf("open host keys: %w", err)
	}
	if fresh != "" {
		return nil, errors.New("unexpected: the host keys were created again; nothing was changed")
	}
	return keys, nil
}

// defaultRestorePartSize is the multipart part size passed to the bucket adapter by restore. It
// only reads, so the size never applies; the adapter requires a positive one.
const defaultRestorePartSize = 5 << 20

// restore is `local-server restore`: it replaces the database in the data directory with a
// snapshot, using the recovery code (req_001 D-41, D-72). It runs offline and refuses while a host
// is running. Exit codes: 0 restored, 1 failed, 2 usage.
func (a *app) restore(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	dataDirFlag := fs.String("data-dir", "", "data directory (default: $LS_DATA_DIR, else local-server in the user configuration directory)")
	from := fs.String("from", "", "restore from the snapshots in this folder")
	bucket := fs.String("bucket", "", "restore from this S3-compatible bucket (the access key comes from "+envBackupKeyID+" and "+envBackupSecret+")")
	endpoint := fs.String("endpoint", "", "with --bucket: the service address (empty means Amazon S3)")
	region := fs.String("region", "auto", "with --bucket: the region")
	prefix := fs.String("prefix", "local-server/", "with --bucket: the folder inside the bucket")
	pathStyle := fs.Bool("path-style", true, "with --bucket: address the bucket as <endpoint>/<bucket>")
	name := fs.String("snapshot", "", "the snapshot to restore (default: the newest)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 || (*from != "" && *bucket != "") {
		fmt.Fprintln(a.stderr, "local-server restore: give at most one of --from and --bucket, and no arguments")
		return 2
	}
	dir, err := a.dataDir(*dataDirFlag)
	if err != nil {
		fmt.Fprintf(a.stderr, "local-server: %v\n", err)
		return 1
	}
	src := restoreSource{folder: *from, bucket: *bucket, endpoint: *endpoint, region: *region, prefix: *prefix, pathStyle: *pathStyle, name: *name}
	if err := a.runRestore(ctx, dir, src); err != nil {
		fmt.Fprintf(a.stderr, "local-server restore: %v\n", err)
		return 1
	}
	return 0
}

type restoreSource struct {
	folder, bucket, endpoint, region, prefix string
	pathStyle                                bool
	name                                     string
}

func (a *app) runRestore(ctx context.Context, dataDir string, src restoreSource) error {
	// The recovery code first: nothing is opened or written without it. It is never echoed,
	// logged or kept past the data key.
	in := bufio.NewReader(a.stdin)
	code := strings.TrimSpace(a.getenv(envRecoveryCode))
	if code == "" {
		var err error
		if code, err = a.readSecretLine(ctx, in, "Recovery code: "); err != nil {
			return err
		}
	}
	dataKey, err := hostkeys.RecoverDataKey(code)
	if err != nil {
		return errors.New("the recovery code is not valid (it is 52 letters and digits in groups of four)")
	}
	target, creds, err := a.restoreTarget(ctx, dataDir, src)
	if err != nil {
		return err
	}
	rep, err := snapshot.Restore(ctx, snapshot.RestoreOptions{
		Target: target, Credentials: creds, Name: src.name, DataKey: dataKey, Compressors: restoreCompressors(),
		DBPath: filepath.Join(dataDir, dbFileName), Modules: registryModules(),
		Guard: func(ctx context.Context) error { return a.refuseIfRunning(ctx, dataDir) },
	})
	if err != nil {
		if errors.Is(err, snapshot.ErrWrongKey) {
			if src.name == "" {
				return fmt.Errorf("%w (if this folder or bucket also holds another host's backups, name this host's snapshot with --snapshot); nothing was changed", err)
			}
			return fmt.Errorf("%w; nothing was changed", err)
		}
		return fmt.Errorf("%w; the existing database was not changed", err)
	}
	a.printRestoreReport(rep, dataDir, a.restoredKeysNotice(ctx, dataDir))
	return nil
}

// The restore line about the master key (req_001 D-138). A snapshot never carries the machine's
// master key, so the host keys in a restored database open only where that key survives.
// Today the host refuses to start on a restored database whose keys it cannot open
// (LoadMasterKey with Create false, hostkeys.Open); a new identity comes only from starting over
// without those keys.
const (
	restoreKeyMissing   = "Warning: the snapshot does not carry this machine's master key, and the master key was not found here (not in the OS keychain, and not in %s). The restored host keys cannot be opened, so the host will not start on this database until the original master key is back (the keychain entry or the master.key file); restarting it on the restored data with new signing keys is planned and not built yet.\n"
	restoreKeyMismatch  = "Warning: the snapshot does not carry this machine's master key, and the master key found here does not match the one that sealed the restored host keys. The host will not start on this database until the original master key is back (the keychain entry or the master.key file); restarting it on the restored data with new signing keys is planned and not built yet.\n"
	restoreKeyUnchecked = "Note: could not check whether this machine's master key opens the restored host keys (%v). The host starts only if it does; the snapshot does not carry the master key.\n"
)

// restoredKeysNotice checks, after a successful restore, whether the master key available on this
// machine opens the restored database's sealed host keys, and returns the line to print, or "" when
// it does. It never fails the restore. The check is read-only: the database is opened read-only,
// and hostkeys.Open on a database that already holds the host writes nothing. It never prints the
// key (req_001 D-138, D-40).
func (a *app) restoredKeysNotice(ctx context.Context, dataDir string) string {
	unchecked := func(err error) string { return fmt.Sprintf(restoreKeyUnchecked, scrub(err)) }
	db, err := storage.OpenReadOnly(ctx, filepath.Join(dataDir, dbFileName), registryModules()...)
	if err != nil {
		return unchecked(err)
	}
	defer db.Close()
	st := settings.New(db)
	if err := registerSettings(st); err != nil {
		return unchecked(err)
	}
	done, err := hostkeys.Bootstrapped(ctx, db)
	if err != nil {
		return unchecked(err)
	}
	if !done {
		return "" // no host in the snapshot: nothing is sealed, and nothing is written here
	}
	timeout, err := st.Duration(ctx, hostkeys.KeyKeychainTimeout, "")
	if err != nil {
		return unchecked(err)
	}
	fallback := filepath.Join(dataDir, masterKeyFileName)
	master, _, err := hostkeys.LoadMasterKey(hostkeys.MasterKeyConfig{FallbackPath: fallback, Create: false, Timeout: timeout, Keychain: a.keychain})
	switch {
	case errors.Is(err, hostkeys.ErrMasterKeyNotFound):
		return fmt.Sprintf(restoreKeyMissing, fallback)
	case err != nil:
		return unchecked(err)
	}
	if _, _, err := hostkeys.Open(ctx, db, st, master); err != nil {
		if errors.Is(err, hostkeys.ErrOpen) {
			return restoreKeyMismatch
		}
		return unchecked(err)
	}
	return ""
}

// scrub is an error's text for one line of output.
func scrub(err error) string { return strings.Join(strings.Fields(err.Error()), " ") }

// restoreTarget builds the Target restore reads from: a folder, a bucket named by flags, or, when
// neither is given, the target configured in this host's database. The Credentials are the bucket
// access key, for scrubbing target errors; zero for a folder.
func (a *app) restoreTarget(ctx context.Context, dataDir string, src restoreSource) (snapshot.Target, snapshot.Credentials, error) {
	switch {
	case src.folder != "":
		if fi, err := os.Stat(src.folder); err != nil || !fi.IsDir() {
			return nil, snapshot.Credentials{}, fmt.Errorf("--from %s is not a folder", src.folder)
		}
		t, err := folder.New(src.folder)
		return t, snapshot.Credentials{}, err
	case src.bucket != "":
		c := snapshot.Credentials{AccessKeyID: a.getenv(envBackupKeyID), SecretAccessKey: a.getenv(envBackupSecret)}
		if c.AccessKeyID == "" || c.SecretAccessKey == "" {
			return nil, snapshot.Credentials{}, fmt.Errorf("the bucket access key is read from %s and %s; set both", envBackupKeyID, envBackupSecret)
		}
		t, err := s3.New(snapshot.S3Config{Endpoint: src.endpoint, Region: src.region, Bucket: src.bucket, Prefix: src.prefix,
			PathStyle: src.pathStyle, PartSize: defaultRestorePartSize, Credentials: c}, nil)
		return t, c, err
	}
	// The configured target: this host's database and master key must still exist.
	d, keys, err := a.openExistingHost(ctx, dataDir)
	if err != nil {
		return nil, snapshot.Credentials{}, fmt.Errorf("%w (to restore on a machine without this host's database, name the snapshots with --from FOLDER or --bucket NAME)", err)
	}
	defer d.db.Close() // closed before the database file is replaced
	cfg, err := snapshot.LoadConfig(ctx, d.db, d.st)
	if err != nil {
		return nil, snapshot.Credentials{}, err
	}
	tc := cfg.Target
	if tc.Kind == snapshot.TargetS3 {
		if tc.S3.Credentials, err = snapshot.LoadCredentials(ctx, d.st, keys); err != nil {
			return nil, snapshot.Credentials{}, err
		}
	}
	t, err := backupTargets(ctx, tc)
	return t, tc.S3.Credentials, err
}

// refuseIfRunning stops a restore while a host is running: it tries to bind the consumer and admin
// addresses the database's settings name (the defaults when there is no database). A running host
// holds them, so a failed bind means one is up. It needs no OS lock.
func (a *app) refuseIfRunning(ctx context.Context, dataDir string) error {
	addrs := []string{defaultConsumerListen, defaultAdminListen}
	if db, err := storage.OpenReadOnly(ctx, filepath.Join(dataDir, dbFileName), registryModules()...); err == nil {
		st := settings.New(db)
		if registerSettings(st) == nil {
			for i, key := range []string{keyConsumerListen, keyAdminListen} {
				if v, err := st.String(ctx, key, ""); err == nil && v != "" {
					addrs[i] = v
				}
			}
		}
		db.Close()
	}
	for _, addr := range addrs {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("a local-server host seems to be running (cannot bind %s); stop it first, then restore", addr)
		}
		ln.Close()
	}
	return nil
}

const restoreCheckpointWarning = `Warning: receipts and ledger checkpoints issued after this snapshot was taken will not
verify against the restored ledger. Consumers holding such receipts cannot check them
against this host any more (the restored ledger is shorter than the one they saw).
`

// printRestoreReport prints the outcome of a restore. keyNotice is the line of restoredKeysNotice
// ("" when the master key opens the restored keys), printed before the closing line.
func (a *app) printRestoreReport(rep snapshot.Report, dataDir, keyNotice string) {
	w := a.stdout
	fmt.Fprintf(w, "Restored snapshot %s (taken %s UTC) into %s.\n", rep.Snapshot, rep.TakenAt.Format("2006-01-02 15:04:05"), filepath.Join(dataDir, dbFileName))
	if rep.PreviousDatabase != "" {
		fmt.Fprintf(w, "The database that was there is kept at %s.\n", rep.PreviousDatabase)
	}
	if len(rep.Minimized) == 0 {
		fmt.Fprintln(w, "The snapshot left out no tables.")
	} else {
		fmt.Fprintf(w, "The snapshot left out the rows of these tables (backup minimize settings): %s.\n", strings.Join(rep.Minimized, ", "))
	}
	fmt.Fprintf(w, "Jobs that were queued or running and lost their request, now failed (payload_minimized): %d. Their clients can resubmit with the same Idempotency-Key.\n", rep.JobsFailed)
	if rep.LatestCheckpointSize > 0 {
		fmt.Fprintf(w, "The newest ledger checkpoint in the restored database covers %d usage records.\n", rep.LatestCheckpointSize)
	} else {
		fmt.Fprintln(w, "The restored database holds no ledger checkpoint yet.")
	}
	fmt.Fprint(w, restoreCheckpointWarning)
	fmt.Fprint(w, keyNotice)
	fmt.Fprintln(w, "Start the host again with `local-server serve` (or your service).")
}
