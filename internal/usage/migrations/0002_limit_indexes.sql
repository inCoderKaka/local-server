-- usage module, migration 2: indexes for the quota queries (PLT-B-023; D-25, D-28, D-98).
-- limits.Check and limits.Record sum or count use_record rows of one access key (key quota)
-- or one consumer (grant quota) whose finished_at falls in the current quota window. Without
-- these, every admitted request scanned the whole table. Equality column first, then the
-- range column.

CREATE INDEX use_record_key_finished ON use_record (access_key_id, finished_at);
CREATE INDEX use_record_consumer_finished ON use_record (consumer_id, finished_at);
