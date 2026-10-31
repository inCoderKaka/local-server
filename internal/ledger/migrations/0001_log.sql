-- ledger module, migration 1: the Merkle log of usage records (D-16, D-27).
-- RFC 6962 tree over the leaves, hashed and stored with golang.org/x/mod/sumdb/tlog.
-- Checkpoints (signed tree heads) come with their own migration (PLT-F-016).

CREATE TABLE ldg_leaf (
  leaf_index INTEGER PRIMARY KEY CHECK (leaf_index >= 0),
  usage_id   TEXT NOT NULL UNIQUE,
  leaf_hash  BLOB NOT NULL CHECK (length(leaf_hash) = 32),
  created_at INTEGER NOT NULL
) STRICT;
--@table ldg_leaf classification=internal billable=false | One leaf of the usage ledger: the position of a usage record and the hash of its canonical leaf bytes. Immutable, kept forever.
--@column ldg_leaf.leaf_index classification=internal billable=false | Position in the log, from 0 with no gaps; the tree size after this leaf is leaf_index + 1.
--@column ldg_leaf.usage_id classification=internal billable=false | The use_record this leaf commits to.
--@column ldg_leaf.leaf_hash classification=internal billable=false | tlog.RecordHash of the canonical leaf bytes (SHA-256 over 0x00 and the bytes, RFC 6962).
--@column ldg_leaf.created_at classification=internal billable=false | When the leaf was appended, Unix milliseconds UTC.

CREATE TABLE ldg_hash (
  hash_index INTEGER PRIMARY KEY CHECK (hash_index >= 0),
  hash       BLOB NOT NULL CHECK (length(hash) = 32)
) STRICT;
--@table ldg_hash classification=internal billable=false | The tlog stored hashes of the ledger tree, in tlog.StoredHashIndex order. Immutable, kept forever.
--@column ldg_hash.hash_index classification=internal billable=false | tlog.StoredHashIndex(level, n) of the tree node.
--@column ldg_hash.hash classification=internal billable=false | The node's 32-byte hash.

CREATE TRIGGER ldg_leaf_no_update BEFORE UPDATE ON ldg_leaf
BEGIN SELECT RAISE(ABORT, 'ldg_leaf is immutable'); END;
CREATE TRIGGER ldg_leaf_no_delete BEFORE DELETE ON ldg_leaf
BEGIN SELECT RAISE(ABORT, 'ldg_leaf is kept forever'); END;
CREATE TRIGGER ldg_hash_no_update BEFORE UPDATE ON ldg_hash
BEGIN SELECT RAISE(ABORT, 'ldg_hash is immutable'); END;
CREATE TRIGGER ldg_hash_no_delete BEFORE DELETE ON ldg_hash
BEGIN SELECT RAISE(ABORT, 'ldg_hash is kept forever'); END;
