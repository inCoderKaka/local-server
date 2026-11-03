// Package jobs is part of the scheduler module; see docs/modules/scheduler.md.
//
// It is the async jobs API's domain (req_001 D-26): a consumer queues a chat completion as a
// background job, and fetches the answer later, so a disconnect never loses the work. The gateway
// serves the endpoints (F2, req_001 D-39) and implements Executor for the worker; this package
// owns the state machine, the storage and the worker loop.
//
// State machine (req_001 D-38, kept by D-54; terminal states are never reopened):
//
//	queued  -(admitted)->            running
//	running -(engine done)->         succeeded  (usage, ledger and receipt written, result stored, job.done)
//	running -(engine error)->        failed     (tokens used metered)
//	queued, running -(cancel)->      cancelled  (a running job's engine call is cancelled, tokens so far metered)
//	queued  -(deadline)->            expired
//	running -(host restart)->        failed(host_restart)
//
// A job stays queued while the host is paused or in quiet hours, because the worker hands it to
// the scheduler with scheduler.Request.Job set.
//
// Content: the request lives in sch_job_payload and the answer in sch_job_result, both sealed with
// the host data key under an AAD that binds the table, the job and the consumer. They are never
// synced, never logged and never put in a metric, an error text or an event payload (req_001
// D-31, D-40). The payload is deleted at every terminal state. A result is deleted when the
// consumer's job-result TTL passes, or once a fetch has written it whole to the consumer
// (req_001 D-85); a dropped connection leaves it fetchable.
//
// Idempotency: an Idempotency-Key is unique per access key. A duplicate returns the existing job,
// except that a job which ended failed with host_restart or payload_minimized (req_001 D-38, D-68)
// no longer holds its key, so the same key starts a new attempt.
//
// Every time, count and size is a runtime setting read at the moment of use (req_001 D-38, D-54);
// see Defs.
package jobs
