-- scheduler module, migration 1: background jobs (D-26, D-38, D-40, D-54, D-85).
-- sch_job holds a job's state and the metadata of its request: ids, the model, times and the
-- reason it failed, never a prompt or a response (D-31, D-40). The request content lives in
-- sch_job_payload and the answer in sch_job_result, each encrypted with the host data key, never
-- synced and never captured in syn_change. No foreign key points out of the module: ids are
-- copies, like use_record, so a job row stays true after a key is revoked or a consumer deleted.

CREATE TABLE sch_job (
  id               TEXT PRIMARY KEY,
  consumer_id      TEXT NOT NULL,
  principal_id     TEXT NOT NULL,
  access_key_id    TEXT NOT NULL,
  grant_version_id TEXT NOT NULL,
  idempotency_key  TEXT NOT NULL CHECK (idempotency_key <> ''),
  model            TEXT NOT NULL,
  identity         TEXT NOT NULL,
  status           TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'expired')),
  failure_reason   TEXT CHECK (failure_reason IS NULL OR failure_reason <> ''),
  created_at       INTEGER NOT NULL,
  not_before       INTEGER,
  deadline_at      INTEGER,
  started_at       INTEGER,
  finished_at      INTEGER,
  CHECK (status <> 'failed' OR failure_reason IS NOT NULL)
) STRICT;
--@table sch_job classification=internal billable=false | One background job: who submitted it, with which Idempotency-Key, its state and times. Holds no prompt and no response; those are in sch_job_payload and sch_job_result.
--@column sch_job.id classification=internal billable=false | Job id, UUIDv7.
--@column sch_job.consumer_id classification=internal billable=false | The consumer that submitted the job; only this consumer can see it.
--@column sch_job.principal_id classification=internal billable=false | The consumer's principal, the target of the job.done event.
--@column sch_job.access_key_id classification=internal billable=false | The access key the job was submitted with; the Idempotency-Key is unique per access key.
--@column sch_job.grant_version_id classification=internal billable=false | The grant version in force when the job was submitted; the job is metered under it.
--@column sch_job.idempotency_key classification=internal billable=false | The client-chosen Idempotency-Key. A resubmission with the same key returns this job, unless it failed with host_restart or payload_minimized.
--@column sch_job.model classification=internal billable=false | The model the job asked for.
--@column sch_job.identity classification=internal billable=false | JSON copy of the submitting key's identity and limits (ids and numbers only), used to check policy and quota again when the job is admitted.
--@column sch_job.status classification=internal billable=false | queued, running, succeeded, failed, cancelled or expired. The last four are terminal and never reopen.
--@column sch_job.failure_reason classification=internal billable=false | Why a failed job failed: host_restart, payload_minimized or an error code such as engine_unavailable; null otherwise.
--@column sch_job.created_at classification=internal billable=false | When the job was queued, Unix milliseconds UTC.
--@column sch_job.not_before classification=internal billable=false | A queued job is not offered to the scheduler before this time (a policy time window that is closed, or a retry wait); null means at once. Unix milliseconds UTC.
--@column sch_job.deadline_at classification=internal billable=false | A queued job that has not started by this time expires; null means it never expires. From the consumer's jobs.deadline setting, Unix milliseconds UTC.
--@column sch_job.started_at classification=internal billable=false | When the scheduler admitted the job and it started running, Unix milliseconds UTC.
--@column sch_job.finished_at classification=internal billable=false | When the job reached a terminal state, Unix milliseconds UTC.

-- A key is unique per access key. A job that ended failed with host_restart or payload_minimized
-- leaves the index, so the same key starts a new attempt while the old row stays as history.
CREATE UNIQUE INDEX sch_job_idempotency ON sch_job (access_key_id, idempotency_key)
  WHERE NOT (status = 'failed' AND failure_reason IN ('host_restart', 'payload_minimized'));
CREATE INDEX sch_job_status_created ON sch_job (status, created_at);
CREATE INDEX sch_job_consumer ON sch_job (consumer_id, created_at);

CREATE TABLE sch_job_payload (
  job_id     TEXT PRIMARY KEY REFERENCES sch_job (id) ON DELETE CASCADE,
  sealed     BLOB NOT NULL,
  created_at INTEGER NOT NULL
) STRICT;
--@table sch_job_payload classification=content billable=false | A queued job's request content, encrypted with the host data key. Deleted when the job reaches any terminal state. Never synced and left out of backups by default.
--@column sch_job_payload.job_id classification=internal billable=false | The job.
--@column sch_job_payload.sealed classification=content billable=false | The request (messages, token limit and request properties) sealed with AES-256-GCM under the host data key, bound to the table, the job id and the consumer.
--@column sch_job_payload.created_at classification=internal billable=false | When the payload was stored, Unix milliseconds UTC.

CREATE TABLE sch_job_result (
  job_id     TEXT PRIMARY KEY REFERENCES sch_job (id) ON DELETE CASCADE,
  sealed     BLOB NOT NULL,
  receipt    TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
) STRICT;
--@table sch_job_result classification=content billable=false | A succeeded job's answer, encrypted with the host data key, served only to the job's consumer. Deleted once a fetch has written it whole to the consumer (D-85) or when it passes expires_at. Never synced and left out of backups by default.
--@column sch_job_result.job_id classification=internal billable=false | The job.
--@column sch_job_result.sealed classification=content billable=false | The chat completion sealed with AES-256-GCM under the host data key, bound to the table, the job id and the consumer.
--@column sch_job_result.receipt classification=internal billable=false | The signed usage receipt (JWS) returned in the LS-Receipt header with the result; holds counts and ids only.
--@column sch_job_result.created_at classification=internal billable=false | When the result was stored, Unix milliseconds UTC.
--@column sch_job_result.expires_at classification=internal billable=false | When the unfetched result is deleted: the job finished plus the consumer's jobs.result_ttl setting at that time, Unix milliseconds UTC.

CREATE INDEX sch_job_result_expires ON sch_job_result (expires_at);
