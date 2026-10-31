-- storage module, migration 2: the change-capture table. It lives here, not in the off-host
-- module, because rows are written in the same transaction as the change they record
-- (Tx.Capture); the off-host module adds syn_cursor and ships the rows later.

CREATE TABLE syn_change (
  seq        INTEGER PRIMARY KEY AUTOINCREMENT,
  dataset    TEXT NOT NULL,
  table_name TEXT NOT NULL,
  pk         TEXT NOT NULL,
  version    INTEGER NOT NULL,
  op         TEXT NOT NULL CHECK (op IN ('insert', 'update', 'delete')),
  payload    BLOB,
  created_at INTEGER NOT NULL
) STRICT;
--@table syn_change classification=internal billable=false | Every change to a synced row, written in the same transaction as the change, waiting to be shipped off the host.
--@column syn_change.seq classification=internal billable=false | Increasing sequence number; the cursor of each sync target points into it.
--@column syn_change.dataset classification=internal billable=false | Dataset the changed row belongs to.
--@column syn_change.table_name classification=internal billable=false | Table of the changed row.
--@column syn_change.pk classification=internal billable=false | Primary key of the changed row.
--@column syn_change.version classification=internal billable=false | Row version after the change.
--@column syn_change.op classification=internal billable=false | insert, update or delete.
--@column syn_change.payload classification=internal billable=false | The row as the module chose to record it; never prompts or responses (D-31, D-40).
--@column syn_change.created_at classification=internal billable=false | When the change was captured, Unix milliseconds UTC.
