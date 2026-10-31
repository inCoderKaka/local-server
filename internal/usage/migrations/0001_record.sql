-- usage module, migration 1: the immutable usage record (D-16, D-27, D-39, D-40).
-- One row per metered call. It holds numbers and ids only: never a prompt or a response
-- (D-31, D-40). consumer_id, access_key_id and grant_version_id are copies, so the hot
-- path and later rollups need no joins and the row stays true after the originals change.
-- The signed receipt's signature is stored here (D-40); it is written once, in the same
-- insert, because the writer signs after it has fixed the row's ledger leaf.

CREATE TABLE use_record (
  id                TEXT PRIMARY KEY,
  consumer_id       TEXT NOT NULL,
  access_key_id     TEXT NOT NULL,
  grant_version_id  TEXT NOT NULL,
  job_id            TEXT,
  operation         TEXT NOT NULL CHECK (operation IN ('chat.completions', 'embeddings')),
  model             TEXT NOT NULL,
  outcome           TEXT NOT NULL CHECK (outcome IN ('completed', 'aborted', 'failed')),
  prompt_tokens     INTEGER NOT NULL CHECK (prompt_tokens >= 0),
  completion_tokens INTEGER NOT NULL CHECK (completion_tokens >= 0),
  total_tokens      INTEGER NOT NULL CHECK (total_tokens >= 0),
  latency_ms        INTEGER NOT NULL CHECK (latency_ms >= 0),
  started_at        INTEGER NOT NULL,
  finished_at       INTEGER NOT NULL,
  receipt_kid       TEXT NOT NULL,
  receipt_signature BLOB NOT NULL,
  created_at        INTEGER NOT NULL
) STRICT;
--@table use_record classification=internal billable=true | One metered call: who, which model, how many tokens, how long. Immutable and kept forever; never holds prompts or responses.
--@column use_record.id classification=internal billable=true | Usage id (UUID), also in the receipt and the ledger leaf.
--@column use_record.consumer_id classification=internal billable=true | The consumer, copied from the grant at call time.
--@column use_record.access_key_id classification=internal billable=true | The access key that made the call, copied.
--@column use_record.grant_version_id classification=internal billable=true | The grant version the call ran under, copied.
--@column use_record.job_id classification=internal billable=true | The job, when the call ran as one; null otherwise.
--@column use_record.operation classification=internal billable=true | Surface called: chat.completions or embeddings.
--@column use_record.model classification=internal billable=true | The model that answered.
--@column use_record.outcome classification=internal billable=true | completed, aborted (client disconnected) or failed (engine failed).
--@column use_record.prompt_tokens classification=internal billable=true | Prompt tokens, from the engine.
--@column use_record.completion_tokens classification=internal billable=true | Generated tokens, from the engine.
--@column use_record.total_tokens classification=internal billable=true | Prompt plus generated tokens.
--@column use_record.latency_ms classification=internal billable=false | Milliseconds from admission to the end of the call.
--@column use_record.started_at classification=internal billable=true | When the call was admitted, Unix milliseconds UTC.
--@column use_record.finished_at classification=internal billable=true | When the call ended, Unix milliseconds UTC.
--@column use_record.receipt_kid classification=internal billable=false | Id of the receipt-signing key that signed this call's receipt.
--@column use_record.receipt_signature classification=internal billable=false | The receipt's Ed25519 signature (64 bytes), kept so the receipt can be rebuilt and checked.
--@column use_record.created_at classification=internal billable=false | When the record was written, Unix milliseconds UTC.

-- Records are kept forever and never change (D-40).
CREATE TRIGGER use_record_no_update BEFORE UPDATE ON use_record
BEGIN SELECT RAISE(ABORT, 'use_record is immutable'); END;
CREATE TRIGGER use_record_no_delete BEFORE DELETE ON use_record
BEGIN SELECT RAISE(ABORT, 'use_record is kept forever'); END;
