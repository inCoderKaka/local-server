-- storage module, migration 3: index for the syn_change retention delete (D-77).
-- The sync.change.retention rule deletes syn_change rows whose created_at is older than the
-- retention. Without this index every run scanned the whole table inside the single writer.

CREATE INDEX syn_change_created_at ON syn_change (created_at);
