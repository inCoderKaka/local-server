package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// Keys is what the runner needs from the host's key custody: the data key snapshots are encrypted
// with, and the master-key sealer for the bucket access key. *hostkeys.Keys is one.
type Keys interface {
	Sealer
	DataKey() []byte
}

// RunnerConfig is the input of NewRunner.
type RunnerConfig struct {
	DB       *storage.DB
	Settings *settings.Store
	Keys     Keys
	// Events receives backup.failed (req_001 D-71). Required.
	Events events.Notifier
	Log    *slog.Logger
	// Targets builds the snapshot target; Compressor is the default codec (gzip). Both are the
	// adapters cmd/local-server wires.
	Targets    TargetFactory
	Compressor Compressor
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Runner takes snapshots of the host's database: VACUUM INTO a private copy, delete the rows of
// the tables set to minimize, vacuum the copy, compress, encrypt with the host data key, send to
// the target, verify, prune (req_001 D-41, D-64).
type Runner struct {
	cfg RunnerConfig
	now func() time.Time

	mu           sync.Mutex
	failing      bool
	lastNotified time.Time
}

// NewRunner returns a Runner.
func NewRunner(cfg RunnerConfig) (*Runner, error) {
	if cfg.DB == nil || cfg.Settings == nil || cfg.Keys == nil || cfg.Events == nil || cfg.Targets == nil || cfg.Compressor == nil {
		return nil, errors.New("snapshot: a runner needs a database, settings, keys, an event notifier, a target factory and a compressor")
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	r := &Runner{cfg: cfg, now: cfg.Now}
	if r.now == nil {
		r.now = time.Now
	}
	return r, nil
}

// Result describes one finished backup.
type Result struct {
	// Name is the snapshot's name in the target.
	Name string
	// Bytes is the size of the stored, encrypted file; DatabaseBytes the size of the database
	// copy before compression.
	Bytes, DatabaseBytes int64
	// Minimized are the tables whose rows were left out.
	Minimized []string
	// Pruned is how many old snapshots were deleted; PruneError is why pruning stopped early.
	// A prune failure does not fail the backup: the new snapshot is stored and verified.
	Pruned     int
	PruneError error
	Duration   time.Duration
}

var nameRE = regexp.MustCompile(`^snapshot-(\d{8}T\d{6}\.\d{3}Z)\.lsbk$`)

const nameTimeLayout = "20060102T150405.000Z"

// snapshotName is the name of a snapshot taken at t. Names sort by time.
func snapshotName(t time.Time) string {
	return "snapshot-" + t.UTC().Format(nameTimeLayout) + ".lsbk"
}

// snapshotTime parses the time out of a snapshot name; ok is false for any other name.
func snapshotTime(name string) (time.Time, bool) {
	m := nameRE.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, false
	}
	t, err := time.Parse(nameTimeLayout, m[1])
	return t, err == nil
}

// scrubbed hides credential text in an error's message while keeping the error chain.
type scrubbed struct {
	msg string
	err error
}

func (e *scrubbed) Error() string { return e.msg }
func (e *scrubbed) Unwrap() error { return e.err }

// scrub returns err with every occurrence of the credentials removed from its message.
func scrub(err error, c Credentials) error {
	if err == nil || c.Empty() {
		return err
	}
	msg := err.Error()
	out := msg
	for _, secret := range []string{c.SecretAccessKey, c.AccessKeyID} {
		if secret != "" {
			out = strings.ReplaceAll(out, secret, "[redacted]")
		}
	}
	if out == msg {
		return err
	}
	return &scrubbed{msg: out, err: err}
}

// buildTarget makes the Target the configuration names, with the stored bucket access key for an
// s3 target. The returned Credentials are for scrub.
func (r *Runner) buildTarget(ctx context.Context, cfg Config) (Target, Credentials, error) {
	tc := cfg.Target
	var creds Credentials
	if tc.Kind == TargetS3 {
		var err error
		if creds, err = LoadCredentials(ctx, r.cfg.Settings, r.cfg.Keys); err != nil {
			return nil, Credentials{}, err
		}
		tc.S3.Credentials = creds
	}
	t, err := r.cfg.Targets(ctx, tc)
	if err != nil {
		return nil, creds, scrub(err, creds)
	}
	return t, creds, nil
}

// Backup takes one snapshot now and sends it to the target. It does not record the time or
// notify the host; Run does. A table setting that breaks req_001 D-69 over the current schema
// fails the run with a *MinimizeError before anything is copied, deleted or sent (D-71).
func (r *Runner) Backup(ctx context.Context) (res Result, err error) {
	start := r.now()
	cfg, err := LoadConfig(ctx, r.cfg.DB, r.cfg.Settings)
	if err != nil {
		return Result{}, err
	}
	refs, err := r.cfg.DB.ForeignKeyReferrers(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("read the table references: %w", err)
	}
	if merr := CheckMinimize(refs, cfg.Minimize); merr != nil {
		return Result{}, merr
	}
	target, creds, err := r.buildTarget(ctx, cfg)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = scrub(err, creds) }()
	if cfg.ChunkBytes < minChunkBytes || cfg.ChunkBytes > maxChunkSize {
		return Result{}, fmt.Errorf("setting %s must be between %d and %d", KeyChunkBytes, minChunkBytes, maxChunkSize)
	}

	// A crash or kill can leave temporary files of an earlier run in a folder target. Any Put still
	// running is younger than the run timeout, so only older ones go.
	if sw, ok := target.(interface {
		RemoveStaleIncoming(time.Duration) (int, error)
	}); ok {
		older := cfg.RunTimeout
		if older <= 0 {
			older = defaultRunTimeout
		}
		if n, _ := sw.RemoveStaleIncoming(older); n > 0 {
			r.cfg.Log.Info("backup: removed leftover temporary files of earlier runs from the folder", "count", n)
		}
	}

	dir, err := os.MkdirTemp(cfg.TempDir, "ls-backup-")
	if err != nil {
		return Result{}, fmt.Errorf("make the private temporary folder (setting %s): %w", KeyTempDir, err)
	}
	defer os.RemoveAll(dir) // also on every error below: the copy of the database never stays behind
	copyPath := filepath.Join(dir, "database.sqlite")
	if err := r.cfg.DB.VacuumInto(ctx, copyPath); err != nil {
		return Result{}, err
	}
	_ = os.Chmod(copyPath, 0o600) // the folder is already user-only; best effort where the OS has no modes
	minimized := cfg.MinimizedTables()
	if err := storage.MinimizeFile(ctx, copyPath, minimized); err != nil {
		return Result{}, err
	}
	sum, dbBytes, err := hashFile(ctx, copyPath)
	if err != nil {
		return Result{}, err
	}

	encPath := filepath.Join(dir, "snapshot.lsbk")
	if err := r.encrypt(ctx, copyPath, encPath, cfg.ChunkBytes, Meta{
		CreatedAt: start.UnixMilli(), Minimized: minimized, DatabaseBytes: dbBytes, DatabaseSHA256: sum,
	}); err != nil {
		return Result{}, err
	}
	if err := os.Remove(copyPath); err != nil { // the plain copy is not needed any more
		return Result{}, err
	}
	enc, err := os.Open(encPath)
	if err != nil {
		return Result{}, err
	}
	defer enc.Close()
	fi, err := enc.Stat()
	if err != nil {
		return Result{}, err
	}
	name := snapshotName(start)
	if err := target.Put(ctx, name, &ctxReader{ctx: ctx, r: enc}, fi.Size()); err != nil {
		return Result{}, fmt.Errorf("send the snapshot to the target: %w", err)
	}
	got, err := target.Stat(ctx, name)
	if err != nil {
		return Result{}, fmt.Errorf("verify the snapshot in the target: %w", err)
	}
	if got.Size != fi.Size() {
		return Result{}, fmt.Errorf("verify the snapshot in the target: it holds %d bytes, %d were sent", got.Size, fi.Size())
	}
	res = Result{Name: name, Bytes: fi.Size(), DatabaseBytes: dbBytes, Minimized: minimized}
	res.Pruned, res.PruneError = prune(ctx, target, cfg, name, r.now())
	res.PruneError = scrub(res.PruneError, creds)
	res.Duration = r.now().Sub(start)
	return res, nil
}

// encrypt compresses the database copy and writes it, sealed in frames, to dst (mode 0600).
func (r *Runner) encrypt(ctx context.Context, src, dst string, chunk int, meta Meta) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, out.Close()) }()
	fw, err := newFrameWriter(out, r.cfg.Keys.DataKey(), r.cfg.Compressor.Name(), chunk, meta)
	if err != nil {
		return err
	}
	cw, err := r.cfg.Compressor.NewWriter(fw)
	if err != nil {
		return err
	}
	if _, err := io.Copy(cw, &ctxReader{ctx: ctx, r: in}); err != nil {
		cw.Close()
		return err
	}
	if err := cw.Close(); err != nil {
		return err
	}
	if err := fw.Close(); err != nil {
		return err
	}
	return out.Sync()
}

// ctxReader makes a long copy stop when ctx ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

func hashFile(ctx context.Context, path string) (sum string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, &ctxReader{ctx: ctx, r: f})
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// prune deletes the snapshots beyond the retention settings and returns how many it deleted. It
// looks only at objects named like our snapshots, so it never removes anything else in the
// folder or bucket, and it never deletes keep (the snapshot just taken) or the newest snapshot.
func prune(ctx context.Context, t Target, cfg Config, keep string, now time.Time) (int, error) {
	if cfg.RetentionCount <= 0 && cfg.RetentionAge <= 0 {
		return 0, nil
	}
	objs, err := t.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("list snapshots to prune: %w", err)
	}
	type snap struct {
		name string
		at   time.Time
	}
	var snaps []snap
	for _, o := range objs {
		if at, ok := snapshotTime(o.Name); ok {
			snaps = append(snaps, snap{o.Name, at})
		}
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].name > snaps[j].name }) // newest first
	deleted := 0
	for i, s := range snaps {
		if i == 0 || s.name == keep {
			continue
		}
		old := cfg.RetentionCount > 0 && int64(i) >= cfg.RetentionCount
		if cfg.RetentionAge > 0 && now.Sub(s.at) > cfg.RetentionAge {
			old = true
		}
		if !old {
			continue
		}
		if err := t.Delete(ctx, s.name); err != nil {
			return deleted, fmt.Errorf("delete an old snapshot: %w", err)
		}
		deleted++
	}
	return deleted, nil
}

// Failure reasons in the backup.failed event payload.
const (
	ReasonMinimizeRule = "minimize_rule_broken"
	ReasonRunFailed    = "run_failed"
)

type failedPayload struct {
	Reason string   `json:"reason"`
	Tables []string `json:"tables,omitempty"`
}

// RunOnce takes one backup and records the outcome: on success the time in
// backup.last_success_at and the end of a failure streak; on failure a log line without content
// and, at the first failure of a streak and then at most every backup.failure_notify_interval, a
// backup.failed host notification with no content (req_001 D-71, D-67).
func (r *Runner) RunOnce(ctx context.Context) error {
	cfg, cerr := LoadConfig(ctx, r.cfg.DB, r.cfg.Settings)
	timeout := defaultRunTimeout
	if cerr == nil && cfg.RunTimeout > 0 {
		timeout = cfg.RunTimeout
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := r.Backup(rctx)
	if err == nil {
		if serr := r.cfg.Settings.Set(ctx, settings.Host, KeyLastSuccessAt, "", fmt.Sprint(r.now().UnixMilli())); serr != nil {
			r.cfg.Log.ErrorContext(ctx, "backup: could not record the time of the last backup", "error", serr)
		}
		r.mu.Lock()
		r.failing = false
		r.mu.Unlock()
		r.cfg.Log.InfoContext(ctx, "backup stored", "snapshot", res.Name, "bytes", res.Bytes, "minimized_tables", len(res.Minimized),
			"pruned", res.Pruned, "duration", res.Duration.String())
		if res.PruneError != nil {
			r.cfg.Log.WarnContext(ctx, "backup: pruning old snapshots failed", "error", res.PruneError)
		}
		return nil
	}
	if ctx.Err() != nil { // the host is stopping: not a failure of the backup
		return ctx.Err()
	}
	payload := failedPayload{Reason: ReasonRunFailed}
	var merr *MinimizeError
	if errors.As(err, &merr) {
		payload = failedPayload{Reason: ReasonMinimizeRule, Tables: merr.Tables()}
	}
	r.cfg.Log.ErrorContext(ctx, "backup failed", "reason", payload.Reason, "tables", payload.Tables, "error", err)
	r.notifyFailure(ctx, payload, cfg)
	return err
}

func (r *Runner) notifyFailure(ctx context.Context, p failedPayload, cfg Config) {
	every := cfg.NotifyInterval
	if every <= 0 {
		every = defaultNotifyEvery
	}
	r.mu.Lock()
	now := r.now()
	due := !r.failing || now.Sub(r.lastNotified) >= every
	r.failing = true
	if due {
		r.lastNotified = now
	}
	r.mu.Unlock()
	if !due {
		return
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return
	}
	nctx := context.WithoutCancel(ctx)
	err = r.cfg.Events.Write(nctx, r.cfg.DB, func(tx *storage.Tx) error {
		_, err := r.cfg.Events.Emit(tx, events.Event{Type: events.BackupFailed, Payload: raw})
		return err
	})
	if err != nil {
		r.cfg.Log.ErrorContext(ctx, "backup: could not raise the backup.failed notification", "error", err)
		// The notification did not go out: the next failed run is due to notify again (req_001 D-71).
		r.mu.Lock()
		r.lastNotified = time.Time{}
		r.mu.Unlock()
	}
}

// Run is the background worker: it takes a backup when one is due (backup.interval after the last
// successful one, so a restart does not back up again at once), until ctx ends. A run's failure
// is handled by RunOnce; the loop continues. It does nothing while backup.enabled is false.
func (r *Runner) Run(ctx context.Context) {
	r.sweepTemp(ctx)
	for {
		wait := r.step(ctx)
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// sweepTemp removes the private folders of backups that a crash or a kill left behind: folders
// named like a backup's, older than backup.run_timeout (a run still going is younger). The
// leftover holds a plain copy of the database, so it must not stay on disk. Errors are ignored:
// the sweep is best effort. File times are wall-clock, so this uses time.Now, not the runner clock.
func (r *Runner) sweepTemp(ctx context.Context) {
	cfg, err := LoadConfig(ctx, r.cfg.DB, r.cfg.Settings)
	if err != nil {
		return
	}
	root := cfg.TempDir
	if root == "" {
		root = os.TempDir()
	}
	maxAge := cfg.RunTimeout
	if maxAge <= 0 {
		maxAge = defaultRunTimeout
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	removed := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "ls-backup-") {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) <= maxAge {
			continue
		}
		if os.RemoveAll(filepath.Join(root, e.Name())) == nil {
			removed++
		}
	}
	if removed > 0 {
		r.cfg.Log.InfoContext(ctx, "backup: removed leftover temporary folders of earlier runs", "count", removed)
	}
}

// step runs a backup if one is due and returns how long to wait before looking again.
func (r *Runner) step(ctx context.Context) time.Duration {
	cfg, err := LoadConfig(ctx, r.cfg.DB, r.cfg.Settings)
	if err != nil {
		if ctx.Err() == nil {
			r.cfg.Log.ErrorContext(ctx, "backup: cannot read the settings", "error", err)
		}
		return defaultInterval
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	if !cfg.Enabled {
		return interval
	}
	if !cfg.LastSuccess.IsZero() {
		if due := cfg.LastSuccess.Add(interval).Sub(r.now()); due > 0 && due <= interval {
			return due
		}
	}
	_ = r.RunOnce(ctx)
	return interval
}
