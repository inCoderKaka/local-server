-- usage module, migration 4: the request counter and started_at indexes (req_001 D-107).
-- use_request_counter counts every call the gateway admitted, metered or not, per subject (a
-- consumer or an access key), quota window length and fixed window. It replaces the in-memory
-- counter of the limits module, so a count survives a restart. Numbers and ids only: never a
-- prompt or a response (D-31, D-40). Ids are copies, as in use_record, so there is no foreign key.
-- The limits module raises it with one upsert per applicable window when the scheduler admits a
-- call and prunes rows whose window ended more than limits.counter_retention ago.

CREATE TABLE use_request_counter (
  subject_kind    TEXT NOT NULL CHECK (subject_kind IN ('consumer', 'access_key')),
  subject_id      TEXT NOT NULL,
  window_ms       INTEGER NOT NULL CHECK (window_ms >= 1),
  window_start_ms INTEGER NOT NULL,
  request_count   INTEGER NOT NULL CHECK (request_count >= 0),
  PRIMARY KEY (subject_kind, subject_id, window_ms, window_start_ms)
) STRICT;
--@table use_request_counter classification=internal billable=true | Calls admitted for one subject in one fixed quota window, metered or not. Numbers and ids only.
--@column use_request_counter.subject_kind classification=internal billable=true | Whose calls are counted: consumer (all its keys) or access_key.
--@column use_request_counter.subject_id classification=internal billable=true | The consumer id or access key id, copied.
--@column use_request_counter.window_ms classification=internal billable=true | Length of the quota window in milliseconds.
--@column use_request_counter.window_start_ms classification=internal billable=true | Start of the fixed window, Unix milliseconds UTC, a whole multiple of the window length.
--@column use_request_counter.request_count classification=internal billable=true | Calls admitted in the window so far.

-- The prune deletes by window end; the quota check reads by primary key.
CREATE INDEX use_request_counter_window ON use_request_counter (window_start_ms);

-- The token quota sums use_record rows by admission time, started_at, so a call admitted near the
-- end of a window and finished in the next is counted once, in the window it was admitted in.
-- Equality column first, then the range column, as in migration 2.
CREATE INDEX use_record_key_started ON use_record (access_key_id, started_at);
CREATE INDEX use_record_consumer_started ON use_record (consumer_id, started_at);
