-- ledger module, migration 2: signed checkpoints of the Merkle log (D-16, D-27, D-52, D-65).
-- A checkpoint is a C2SP tlog-checkpoint signed note over the tree head, signed with the
-- dedicated checkpoint key. The row is written once and never changes.

CREATE TABLE ldg_checkpoint (
  tree_size  INTEGER PRIMARY KEY CHECK (tree_size >= 1),
  root_hash  BLOB NOT NULL CHECK (length(root_hash) = 32),
  note       TEXT NOT NULL,
  key_id     TEXT NOT NULL,
  created_at INTEGER NOT NULL
) STRICT;
--@table ldg_checkpoint classification=internal billable=false | One signed checkpoint of the usage ledger: the tree head at a size, as a C2SP signed note. Immutable, kept forever.
--@column ldg_checkpoint.tree_size classification=internal billable=false | Number of leaves the checkpoint covers (leaves 0 to tree_size - 1); at least 1.
--@column ldg_checkpoint.root_hash classification=internal billable=false | The RFC 6962 root hash of the first tree_size leaves (32 bytes).
--@column ldg_checkpoint.note classification=internal billable=false | The signed note text verbatim: origin, size, base64 root and the signature line.
--@column ldg_checkpoint.key_id classification=internal billable=false | The hostkeys id of the checkpoint key that signed it; the key stays published after it retires.
--@column ldg_checkpoint.created_at classification=internal billable=false | When the checkpoint was signed, Unix milliseconds UTC.

CREATE TRIGGER ldg_checkpoint_no_update BEFORE UPDATE ON ldg_checkpoint
BEGIN SELECT RAISE(ABORT, 'ldg_checkpoint is immutable'); END;
CREATE TRIGGER ldg_checkpoint_no_delete BEFORE DELETE ON ldg_checkpoint
BEGIN SELECT RAISE(ABORT, 'ldg_checkpoint is kept forever'); END;
