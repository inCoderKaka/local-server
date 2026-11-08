// Package snapshot is the snapshot adapter of the OffHostSync port (docs/modules/offhost.md): the
// host's off-host backup and its restore (req_001 D-41, D-28, D-64, D-69, D-103).
//
// # Backup
//
// Runner.Backup copies the live database with VACUUM INTO into a private temporary folder, deletes
// the rows of the tables the host set to minimize from the copy (storage.MinimizeFile, with
// foreign_keys=ON) and vacuums the copy, so no free page keeps a deleted row. It then compresses
// the copy through the Compressor port, encrypts it in framed chunks with the host data key
// (format.go), sends it through the Target port, checks the stored size and prunes old snapshots
// by the retention settings. Runner.Run does this on a schedule off the request path; a failed run
// is logged without content and raises a backup.failed host notification (D-71).
//
// The ports and their adapters:
//
//   - Compressor: gzipcodec (standard library gzip), the default.
//   - Target: folder (a host-chosen folder) and s3 (an S3-compatible bucket through the AWS SDK
//     for Go v2: Cloudflare R2 by default, Google Cloud Storage through its XML API).
//
// The adapters sit in sub-packages only cmd/local-server imports; this package never does.
// s3fake is an in-process S3 server for tests.
//
// # Minimize (D-64, D-69)
//
// Every table in ops_data_catalog has a host setting backup.minimize.<table>. sch_job_payload,
// sch_job_result and idn_session default to minimize. A table can be set to minimize only when
// every table that references it, directly or through others, is also set to minimize
// (CheckMinimize); the settings validator refuses a write that breaks this and names the tables,
// and every run checks again over the current schema, because a migration can break it (D-71).
//
// # Restore
//
// Restore decrypts with the host data key (the recovery code), checks the database against the
// size and SHA-256 sealed in the snapshot, opens it with the build's modules, marks queued or
// running jobs whose payload was minimized failed(payload_minimized) (D-68, D-72), and replaces
// the database file by renaming, keeping the old one aside.
//
// # Secrets and content
//
// The bucket access key lives in the setting backup.s3.sealed_secret, sealed with the host master
// key (D-40); Credentials never prints its secret. Nothing here logs or stores a prompt, a
// response, a key or the recovery code (D-31, D-40); errors from the target are scrubbed of the
// access key.
package snapshot
