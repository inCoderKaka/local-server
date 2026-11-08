package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/inCoderKaka/local-server/internal/jobs"
	"github.com/inCoderKaka/local-server/internal/ledger/checkpoint"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// ErrNoSnapshots means the target holds no snapshot to restore.
var ErrNoSnapshots = errors.New("snapshot: the target holds no snapshot")

// RestoreOptions is the input of Restore.
type RestoreOptions struct {
	// Target is where the snapshots are.
	Target Target
	// Credentials is the bucket access key, if the target is a bucket, so that target errors are
	// scrubbed of it.
	Credentials Credentials
	// Name is the snapshot to restore; empty means the newest.
	Name string
	// DataKey is the host data key, from the recovery code (hostkeys.RecoverDataKey).
	DataKey []byte
	// Compressors are the codecs this build can read, by Name.
	Compressors map[string]Compressor
	// DBPath is the database file to replace. Its folder is created if missing.
	DBPath string
	// Modules are the storage modules of this build (cmd/local-server's registry): the restored
	// copy is opened with them, which also migrates a snapshot from an older build forward and
	// refuses one from a newer build.
	Modules []storage.Module
	// Guard, when not nil, runs once the snapshot is decrypted, checked and prepared, just before
	// the database file is replaced. An error stops the restore with the old database untouched.
	// cmd/local-server uses it to refuse while a host is running.
	Guard func(ctx context.Context) error
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Report is what a restore did.
type Report struct {
	// Snapshot is the name restored and TakenAt when it was taken.
	Snapshot string
	TakenAt  time.Time
	// Minimized are the tables the snapshot left out (req_001 D-64).
	Minimized []string
	// JobsFailed is how many queued or running jobs were marked failed(payload_minimized)
	// because the snapshot left out their payload (req_001 D-68).
	JobsFailed int
	// LatestCheckpointSize is the tree size of the newest ledger checkpoint in the restored
	// database, 0 when it holds none. Receipts and checkpoints issued after the snapshot was taken
	// will not verify against the restored ledger (req_001 D-109).
	LatestCheckpointSize int64
	// DatabaseBytes is the size of the restored database.
	DatabaseBytes int64
	// PreviousDatabase is where the database that was replaced is kept, "" when there was none.
	PreviousDatabase string
}

// Restore decrypts a snapshot, checks it, prepares it and replaces the database at opts.DBPath.
// It runs offline, before the host next starts (req_001 D-72). The steps, in order, each leaving
// the existing database untouched until the last:
//
//  1. pick the snapshot and open its first frame: a wrong recovery code stops here, before any
//     file is written;
//  2. decompress into a private file next to DBPath, checking the size and SHA-256 recorded in
//     the snapshot;
//  3. open that file with the build's modules and check it (SQLite integrity and foreign keys);
//  4. when the snapshot left out sch_job_payload, mark every queued or running job without a
//     payload failed(payload_minimized) (D-68), before the host's host_restart pass (D-72);
//  5. call Guard, move the old database (and its -wal and -shm) aside under a timestamped name
//     and rename the prepared file into place.
//
// Nothing here logs or returns the recovery code, the data key or any row content.
func Restore(ctx context.Context, opts RestoreOptions) (rep Report, err error) {
	defer func() { err = scrub(err, opts.Credentials) }()
	if opts.Target == nil || len(opts.DataKey) == 0 || opts.DBPath == "" || len(opts.Compressors) == 0 {
		return Report{}, errors.New("snapshot: restore needs a target, the data key, a database path and compressors")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	name := opts.Name
	if name == "" {
		if name, err = latest(ctx, opts.Target); err != nil {
			return Report{}, err
		}
	}
	rc, err := opts.Target.Open(ctx, name)
	if err != nil {
		return Report{}, fmt.Errorf("open snapshot %s: %w", name, err)
	}
	defer rc.Close()
	frames, comp, meta, err := openFrames(rc, opts.DataKey)
	if err != nil {
		return Report{}, err
	}
	codec, ok := opts.Compressors[comp]
	if !ok {
		return Report{}, fmt.Errorf("snapshot: the snapshot is compressed with %q, which this build cannot read", comp)
	}

	dir := filepath.Dir(opts.DBPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Report{}, err
	}
	tmp, err := os.CreateTemp(dir, ".restore-*.sqlite") // 0600
	if err != nil {
		return Report{}, err
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			os.Remove(tmpPath)
			os.Remove(tmpPath + "-wal")
			os.Remove(tmpPath + "-shm")
		}
	}()
	dr, err := codec.NewReader(frames)
	if err != nil {
		tmp.Close()
		return Report{}, fmt.Errorf("snapshot: read the compressed data: %w", err)
	}
	h := sha256.New()
	// One byte more than recorded, so a stream longer than the metadata says is noticed without
	// letting a damaged file fill the disk.
	n, cerr := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(&ctxReader{ctx: ctx, r: dr}, meta.DatabaseBytes+1))
	cerr = errors.Join(cerr, dr.Close(), tmp.Sync(), tmp.Close())
	if cerr != nil {
		return Report{}, cerr
	}
	if n != meta.DatabaseBytes || hex.EncodeToString(h.Sum(nil)) != meta.DatabaseSHA256 {
		return Report{}, errors.New("snapshot: the decrypted database does not match the size and checksum recorded in the snapshot")
	}

	rep = Report{Snapshot: name, TakenAt: meta.Time(), Minimized: append([]string(nil), meta.Minimized...), DatabaseBytes: n}
	if err := prepare(ctx, tmpPath, opts.Modules, meta, now(), &rep); err != nil {
		return Report{}, err
	}
	if opts.Guard != nil {
		if err := opts.Guard(ctx); err != nil {
			return Report{}, err
		}
	}
	prev, err := swap(opts.DBPath, tmpPath, now())
	if err != nil {
		return Report{}, err
	}
	rep.PreviousDatabase = prev
	return rep, nil
}

// latest returns the name of the newest snapshot in the target.
func latest(ctx context.Context, t Target) (string, error) {
	objs, err := t.List(ctx)
	if err != nil {
		return "", fmt.Errorf("list snapshots: %w", err)
	}
	var names []string
	for _, o := range objs {
		if _, ok := snapshotTime(o.Name); ok {
			names = append(names, o.Name)
		}
	}
	if len(names) == 0 {
		return "", ErrNoSnapshots
	}
	sort.Strings(names)
	return names[len(names)-1], nil
}

// prepare opens the restored copy with the build's modules, checks it and applies req_001 D-68.
func prepare(ctx context.Context, path string, mods []storage.Module, meta Meta, now time.Time, rep *Report) (err error) {
	db, err := storage.Open(ctx, storage.DefaultConfig(path, mods...))
	if err != nil {
		return fmt.Errorf("open the restored database with this build: %w", err)
	}
	defer func() {
		err = errors.Join(err, db.Close())
		if err == nil {
			// Closing the last connection folds the write-ahead log into the file. A log that is
			// left would hold changes the renamed file lacks.
			if fi, serr := os.Stat(path + "-wal"); serr == nil && fi.Size() > 0 {
				err = errors.New("snapshot: the restored database kept a write-ahead log after closing")
			}
			os.Remove(path + "-wal")
			os.Remove(path + "-shm")
		}
	}()
	if err := db.IntegrityCheck(ctx); err != nil {
		return err
	}
	for _, t := range meta.Minimized {
		if t == "sch_job_payload" {
			if rep.JobsFailed, err = jobs.FailPayloadMinimized(ctx, db, now); err != nil {
				return err
			}
			break
		}
	}
	rep.LatestCheckpointSize, err = checkpoint.LatestTreeSize(ctx, db)
	return err
}

// swap keeps the database at dbPath (with its -wal and -shm) aside under a timestamped name and
// renames the prepared file into its place. A rename is atomic on every OS Go supports, so the
// path holds either the old database or the new one, never a mix; if a step fails the moves
// already made are undone. It needs no OS-specific code: a running host is refused earlier
// (Guard), not by a file lock. It returns where the old database now is, "" if there was none.
func swap(dbPath, newPath string, now time.Time) (string, error) {
	suffixes := []string{"", "-wal", "-shm"}
	var moved [][2]string
	undo := func() {
		for i := len(moved) - 1; i >= 0; i-- {
			os.Rename(moved[i][1], moved[i][0])
		}
	}
	prev := ""
	if _, err := os.Stat(dbPath); err == nil {
		prev = dbPath + ".before-restore-" + now.UTC().Format("20060102T150405Z")
		for n := 2; ; n++ { // never overwrite an earlier kept copy
			_, err := os.Stat(prev)
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			if err != nil {
				return "", fmt.Errorf("keep the old database aside: %w", err)
			}
			prev = fmt.Sprintf("%s.before-restore-%s-%d", dbPath, now.UTC().Format("20060102T150405Z"), n)
		}
		for _, s := range suffixes {
			if _, err := os.Stat(dbPath + s); err != nil {
				continue
			}
			if err := os.Rename(dbPath+s, prev+s); err != nil {
				undo()
				return "", fmt.Errorf("keep the old database aside: %w", err)
			}
			moved = append(moved, [2]string{dbPath + s, prev + s})
		}
	}
	if err := os.Rename(newPath, dbPath); err != nil {
		undo()
		return "", fmt.Errorf("put the restored database in place: %w", err)
	}
	syncDir(filepath.Dir(dbPath))
	return prev, nil
}

// syncDir makes the renames durable where the OS allows opening a folder; elsewhere it does
// nothing.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
}
