-- usage module, migration 3: usage rollups (PLT-F-023; D-27, D-38, D-40, D-104).
-- use_rollup holds the sums of use_record rows per minute, hour and day, per consumer, access
-- key and model. The rollup worker fills it from use_record, which stays the source of truth
-- and is never changed; the usage query reads this table only. Numbers and ids only: never a
-- prompt or a response (D-31, D-40). Ids are copies, as in use_record, so there is no foreign
-- key and a row stays true after a key is revoked or a consumer deleted.
-- Minute buckets are kept 7 days and hour buckets 90 days by default, day buckets forever; the
-- host owns the three retention settings and the settings retention worker prunes the rows
-- (rules registered by usage.NewRollups, one per granularity).

CREATE TABLE use_rollup (
  granularity       TEXT NOT NULL CHECK (granularity IN ('minute', 'hour', 'day')),
  bucket_start      INTEGER NOT NULL,
  consumer_id       TEXT NOT NULL,
  access_key_id     TEXT NOT NULL,
  model             TEXT NOT NULL,
  requests          INTEGER NOT NULL CHECK (requests >= 0),
  prompt_tokens     INTEGER NOT NULL CHECK (prompt_tokens >= 0),
  completion_tokens INTEGER NOT NULL CHECK (completion_tokens >= 0),
  total_tokens      INTEGER NOT NULL CHECK (total_tokens >= 0),
  PRIMARY KEY (granularity, bucket_start, consumer_id, access_key_id, model)
) STRICT;
--@table use_rollup classification=internal billable=true | Usage totals for one bucket (minute, hour or day), consumer, access key and model, summed from use_record by the rollup worker. Numbers and ids only.
--@column use_rollup.granularity classification=internal billable=true | Bucket size: minute, hour or day.
--@column use_rollup.bucket_start classification=internal billable=true | Start of the bucket, Unix milliseconds UTC, aligned to the bucket size.
--@column use_rollup.consumer_id classification=internal billable=true | The consumer, copied from the use_record rows.
--@column use_rollup.access_key_id classification=internal billable=true | The access key, copied from the use_record rows.
--@column use_rollup.model classification=internal billable=true | The model that answered.
--@column use_rollup.requests classification=internal billable=true | Metered calls in the bucket, whatever their outcome (completed, aborted or failed).
--@column use_rollup.prompt_tokens classification=internal billable=true | Prompt tokens in the bucket.
--@column use_rollup.completion_tokens classification=internal billable=true | Generated tokens in the bucket.
--@column use_rollup.total_tokens classification=internal billable=true | Prompt plus generated tokens in the bucket.

-- One consumer reads its own rows in the order the usage query returns them: equality columns
-- first, then the order of the keyset cursor (bucket_start, access_key_id, model).
CREATE INDEX use_rollup_consumer ON use_rollup (consumer_id, granularity, bucket_start, access_key_id, model);

-- The rollup worker's place in use_record: the last rowid already added to use_rollup. It moves
-- in the same unit of work as the sums it covers, so a stop in the middle loses or repeats
-- nothing. use_record is never updated or deleted (triggers), so its rowids only grow.
CREATE TABLE use_rollup_state (
  id          INTEGER PRIMARY KEY CHECK (id = 1),
  last_rowid  INTEGER NOT NULL CHECK (last_rowid >= 0)
) STRICT;
--@table use_rollup_state classification=internal billable=false | The single row saying how far the rollup worker has read use_record.
--@column use_rollup_state.id classification=internal billable=false | Always 1: there is one row.
--@column use_rollup_state.last_rowid classification=internal billable=false | The rowid of the last use_record row already added to use_rollup; 0 before the first.
