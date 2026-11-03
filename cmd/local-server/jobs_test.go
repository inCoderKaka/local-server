package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// A start marks the jobs the last stop interrupted failed(host_restart) before anything runs
// (req_001 D-38, D-54), and the job worker runs under the supervisor (PLT-F-022).
func TestServeFailsInterruptedJobsAndRunsTheJobWorker(t *testing.T) {
	ta := newTestApp(t, true)
	seed(t, ta.dir, loopbackAny)
	d, err := openDatabase(context.Background(), filepath.Join(ta.dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	err = d.db.Write(context.Background(), func(tx *storage.Tx) error {
		for _, j := range []struct{ id, key, status string }{
			{"0192e5a0-0001-7000-8000-000000000001", "interrupted", "running"},
			{"0192e5a0-0001-7000-8000-000000000002", "orphan", "queued"},
		} {
			if _, err := tx.Exec(`INSERT INTO sch_job (id, consumer_id, principal_id, access_key_id, grant_version_id, idempotency_key, model, identity, status, created_at)
				VALUES (?, 'c', 'p', 'k', 'g', ?, 'm', '{}', ?, ?)`, j.id, j.key, j.status, now); err != nil {
				return err
			}
			if j.status == "running" { // a payload is left behind a job the stop interrupted
				if _, err := tx.Exec(`INSERT INTO sch_job_payload (job_id, sealed, created_at) VALUES (?, x'00', ?)`, j.id, now); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.db.Close(); err != nil {
		t.Fatal(err)
	}

	r := startServe(t, ta, "--engine", "fake")
	defer r.stop()
	if n := count(t, ta.dir, `SELECT count(*) FROM sch_job WHERE idempotency_key = 'interrupted' AND status = 'failed' AND failure_reason = 'host_restart'`); n != 1 {
		t.Fatalf("interrupted job not failed(host_restart) once serve listens: %d", n)
	}
	if n := count(t, ta.dir, `SELECT count(*) FROM sch_job_payload`); n != 0 {
		t.Fatalf("%d payloads kept for a job that failed", n)
	}
	// The worker runs: a queued job with no payload can never run, and it is failed by the worker.
	waitFor(t, "the job worker to settle the orphan", func() bool {
		return count(t, ta.dir, `SELECT count(*) FROM sch_job WHERE idempotency_key = 'orphan' AND status = 'failed' AND failure_reason = 'payload_minimized'`) == 1
	})
}
