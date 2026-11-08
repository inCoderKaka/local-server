# offhost module

## Purpose

Backups and opt-in platform sync with per-dataset keys.

## Owns

- Packages: `internal/offhost/` (the port `OffHostSync` is designed: PLT-F-044..F-046; platform sync is not built). Built by PLT-F-044: `internal/offhost/snapshot/` (the snapshot adapter: off-host backup and restore) with the sub-packages below.
- Tables: `syn_*`, migrations in `internal/offhost/migrations/`. PLT-F-044 adds no table: its state is settings in `hst_setting`.
- Port: `OffHostSync`.

### Snapshot adapter (`internal/offhost/snapshot`, PLT-F-044)

A backup (`Runner.Backup`; `Runner.Run` is the background worker, started as the `backup` worker by `cmd/local-server`) does, in order:

1. Checks req_001 D-69 over the current schema (`CheckMinimize`). A break fails the run with a `*MinimizeError` that names the tables, before anything is copied, deleted or sent (D-71).
2. `VACUUM INTO` a temporary copy in a private folder (`storage.DB.VacuumInto`; the folder is created 0700 with `os.MkdirTemp`, the copy is chmod 0600, and the folder is removed on every way out).
3. Deletes the rows of the tables set to minimize from the copy with `foreign_keys=ON`, children first, in one transaction, then `VACUUM`s the copy so no free page keeps a deleted row (`storage.MinimizeFile`). The live database is never touched.
4. Records the SHA-256 and size of the copy, compresses it through the compressor port, and encrypts the compressed stream with the host data key in framed chunks (`format.go`), then sends the file through the target port, checks the stored size, and prunes old snapshots by the retention settings.

File format, version 1: a plaintext header (`LSBK`, version, compressor name, chunk size), then frames, each sealed with `hostkeys.Seal` under the data key (AES-256-GCM, random nonce). The first frame is the metadata (JSON: format, time taken, the tables minimized, the copy's size and SHA-256); then the compressed database in chunks of `backup.chunk_bytes`; the last frame is marked final. The associated data of a frame is the header, the frame number and its kind, so reordering, repeating, dropping or truncating frames fails to open. A wrong recovery code fails on the first frame, before anything is written. Chunking keeps memory flat for a large database; a single `Seal` would hold it whole.

Ports and adapters (adapters are sub-packages only `cmd/local-server` imports; `tools/archcheck` enforces it):

| Port | Where it is defined | Adapters |
|---|---|---|
| `Compressor` (req_001 D-103) | `snapshot` | `snapshot/gzipcodec` (standard library gzip, the default) |
| `Target` (snapshot target; D-41, D-59, D-103) | `snapshot` | `snapshot/folder` (a host-chosen folder; files written to a temporary name and renamed), `snapshot/s3` (any S3-compatible bucket through the AWS SDK for Go v2: Cloudflare R2 by default, Google Cloud Storage through its XML API with HMAC keys) |

The S3 adapter sends a snapshot no larger than `backup.s3.part_size_bytes` in one request and larger ones as a multipart upload (created, parts in order, completed, and aborted on any failure). The abort runs only inside the process. If local-server is killed or loses power mid-upload, the bucket keeps the uploaded parts of an unfinished multipart upload. They are not visible as an object and may be billed, so set a bucket lifecycle rule that aborts incomplete multipart uploads after a few days. It turns off the SDK's default request checksums (`WhenRequired`) because R2 and GCS do not take them. `snapshot/s3fake` is an in-process S3 server for tests (path-style; put, multipart, head, get, list with paging, delete); the real-bucket test is `bucket_test.go` behind the build tag `bucket` (`go test -tags bucket -run TestRealBucket ./internal/offhost/snapshot/`, credentials from `LS_TEST_BUCKET_*` environment variables).

Settings (all owned by the host, req_001 D-38, D-54; read when used):

| Key | Default | Meaning |
|---|---|---|
| `backup.enabled` | `false` | Backups run only when true. |
| `backup.interval` | `5m` | Time between backups, counted from the last successful one (so a restart does not back up at once). Applies when the worker next wakes. |
| `backup.target` | `s3` | `s3` or `folder`. |
| `backup.folder.path` | empty | An absolute path. Folder for the `folder` target. Give each host its own folder: pruning and restore treat every backup file in it as this host's. |
| `backup.s3.endpoint`, `.region`, `.bucket`, `.prefix` | empty, `auto`, empty, `local-server/` | The bucket. An empty endpoint means Amazon S3, which then needs a real region, not `auto`. Give each host its own prefix: pruning and restore treat every backup file under it as this host's. |
| `backup.s3.path_style` | `true` | `<endpoint>/<bucket>` addressing. |
| `backup.s3.part_size_bytes` | `16777216` | Multipart part size. Amazon S3 needs at least 5 MiB for every part but the last. |
| `backup.s3.abort_timeout` | `1m` | Longest local-server waits for the bucket to cancel an unfinished multipart upload after a failed backup. The cancel is sent even after `backup.run_timeout` has run out, so this also bounds how long a failed backup can take. |
| `backup.s3.sealed_secret` | empty | The bucket access key id and secret, sealed with the host master key (D-40). Written only by `local-server backup-credentials`; a write of anything that is not a sealed value is refused. |
| `backup.retention_count`, `backup.retention_age` | `100`, `0s` | Snapshots beyond the newest N, or older than the age, are deleted after a successful backup; 0 turns a limit off; the newest is always kept. Only objects named like our snapshots are ever deleted. |
| `backup.chunk_bytes` | `1048576` | Encryption chunk size, at least `65536`, recorded in each snapshot. |
| `backup.temp_dir` | empty (OS temporary folder) | Where the temporary copies are made; needs room for the database twice. |
| `backup.run_timeout` | `1h` | Longest one backup may take. The folder adapter also uses it at the start of each backup: leftover `.incoming-*` files older than this, left by a crash or kill, are removed. |
| `backup.failure_notify_interval` | `1h` | While backups keep failing, the host is notified at the first failure and then at most this often. |
| `backup.last_success_at` | `0` | Unix milliseconds of the last verified backup; written by the worker. |
| `backup.minimize.<table>` | `true` for `sch_job_payload`, `sch_job_result`, `idn_session`; `false` otherwise | One switch per table of `ops_data_catalog` (req_001 D-64). Registered when the database opens (`snapshot.RegisterSettings`). For the protected tables `ops_schema_migrations`, `hst_host` and `hst_key` (D-139) the key exists but is fixed at `false`: every write, `true` or `false`, is refused with an error naming the table and why, and a stored value is never read. |

Minimize rule (D-69): a table can be set to minimize only when every table that references it, directly or through others (`pragma_foreign_key_list`, `storage.DB.ForeignKeyReferrers`), is also set to minimize. The settings validator refuses a write that breaks it, in either direction (minimizing a parent whose referrer is kept, or keeping a referrer of a minimized parent), and the error names the tables. A table the settings do not list (a table added by a migration without a catalog entry) counts as kept. Three tables are protected and always kept (D-139, refining D-64 and D-68; `snapshot.ProtectedTables`, `snapshot.IsProtected`): minimizing `ops_schema_migrations` makes a restore fail at migration time, and minimizing `hst_host` or `hst_key` leaves a snapshot that cannot recover the host's keys. A write to their `backup.minimize.<table>` setting is refused with an error that names the table and the reason; `minimizeSettings` and `LoadConfig` treat them as kept even when an older database holds a stored `true`, so the D-69 check never sees them as minimized. Every run re-checks the rule over the current schema (D-71); a failed run logs without content and raises the `backup.failed` event (payload: a reason code and the table names, never content) on the event bus, which the in-app notification channel (PLT-F-029) delivers to the host admin.

Restore (`local-server restore`, `snapshot.Restore`), offline and before the host next starts (D-72): reads the newest (or named) snapshot from `--from FOLDER`, from `--bucket NAME` (with `--endpoint`, `--region`, `--prefix`, `--path-style`; access key from `LS_BACKUP_ACCESS_KEY_ID` and `LS_BACKUP_SECRET_ACCESS_KEY`), or, with neither, from the target configured in this host's database. The recovery code is read from `LS_RECOVERY_CODE` or a prompt (not echoed) and is the data key. It decrypts and decompresses into a private file next to the database, checks size and SHA-256, opens that file with the build's modules (which migrates a snapshot from an older build and refuses one from a newer build), runs SQLite's integrity and foreign-key checks, and, when the snapshot left out `sch_job_payload`, marks every queued or running job without a payload `failed(payload_minimized)` (`jobs.FailPayloadMinimized`, D-68; it runs before the host's `host_restart` pass, so it takes precedence, D-72). It then refuses if a host is running (it tries to bind the consumer and admin addresses from the settings), moves the old database and its `-wal` and `-shm` aside as `local-server.db.before-restore-<timestamp>` and renames the prepared file into place. The report names the tables the snapshot minimized, the number of jobs marked, the newest ledger checkpoint size, and warns that receipts and checkpoints issued after the snapshot will not verify (D-109). A restore that fails leaves the existing database untouched; a wrong recovery code writes nothing.

`local-server backup-credentials` stores the bucket access key sealed with the host master key (`hostkeys.Keys.SealSecret`, bound to the setting key); the id and secret come from `LS_BACKUP_ACCESS_KEY_ID` and `LS_BACKUP_SECRET_ACCESS_KEY` or from prompts (the secret is not echoed). `snapshot.Credentials` never prints its secret (`String`, `GoString`, `LogValue`), and errors from the target are scrubbed of the key.

OS-agnostic and OS-dependent: nothing in this module is OS-dependent and it has no build tags. Files are replaced with `os.Rename` (atomic replace on macOS, Linux and Windows) and a running host is detected by binding its listener addresses, not by a file lock, so no OS adapter is needed. The one tolerated difference is that fsync of a folder is best effort (Windows cannot open a folder for it).

The master key and a restore (D-138, refining D-71 and D-72): a snapshot does not carry the machine's host master key, and it is never put in the snapshot. The recovery code (the host data key) recovers the data on the same machine, or where `master.key` or the keychain entry survives. The host is told to back up `master.key` (or the keychain entry) itself. After a total loss of the machine without it, the sealed host keys (`hst_host.data_key_sealed`, `hst_key.private_sealed`) cannot be opened, and the host continues only under a new identity: new signing keys, which consumers must trust again (D-138). After a successful restore, `local-server restore` checks, read-only, whether the master key available on this machine opens the restored host keys (the database is opened read-only; the key is only looked up with `Create` false and never printed) and prints one line before its closing line when it does not: that the snapshot does not carry the master key, whether the key was not found or does not match (`hostkeys.ErrMasterKeyNotFound`, `hostkeys.ErrOpen`), or, when the check itself failed (for example a keychain timeout), that it could not be checked. The check never changes the exit code. What the code does today: `serve` on a restored database judges first run from the `hst_host` row, so it never creates a master key for it, and refuses to start while the key is missing or does not open the keys. A new identity comes only from starting over without those keys; no command re-identifies a restored host today.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters. The AWS SDK is importable only from `internal/offhost/snapshot/s3`.

## Decisions

D-44, D-28, D-41, D-59, D-64, D-68, D-69, D-71, D-72, D-103, D-109, D-138, D-139. The module map is in `docs/design_docs/lld.md` section 1.
