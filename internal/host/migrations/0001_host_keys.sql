-- host module, migration 1: the host row and its signing keys (D-25, D-27, D-40, D-41, D-65).
-- Private keys and the host data key are never stored in the clear: they are sealed with the host
-- master key, which lives in the OS keychain or a user-only file and never in this database (D-40).
-- The host data key is shown once as the recovery code (D-41); the code is not stored.

CREATE TABLE hst_host (
  id                  TEXT PRIMARY KEY,
  created_at          INTEGER NOT NULL,
  data_key_sealed     BLOB NOT NULL
) STRICT;
-- One host row per install.
CREATE UNIQUE INDEX hst_host_one ON hst_host ((0));
--@table hst_host classification=internal billable=false | This install's host: one row, whose id is the host's global id.
--@column hst_host.id classification=public billable=false | The host's global id: the base64url text of the host identity public key (D-25).
--@column hst_host.created_at classification=internal billable=false | When the host identity was created, Unix milliseconds UTC.
--@column hst_host.data_key_sealed classification=sensitive billable=false | The host data key sealed with the host master key; it protects queued job content and off-host copies.

CREATE TABLE hst_key (
  id             TEXT PRIMARY KEY,
  host_id        TEXT NOT NULL REFERENCES hst_host(id) ON DELETE RESTRICT,
  purpose        TEXT NOT NULL CHECK (purpose IN ('identity', 'receipt', 'checkpoint')),
  public_key     BLOB NOT NULL,
  private_sealed BLOB NOT NULL,
  signature      BLOB,
  created_at     INTEGER NOT NULL,
  retired_at     INTEGER,
  CHECK ((purpose = 'identity') = (signature IS NULL))
) STRICT;
CREATE INDEX hst_key_current ON hst_key (host_id, purpose, retired_at);
-- At most one live key per purpose; a retired key is kept for verification.
CREATE UNIQUE INDEX hst_key_one_live ON hst_key (host_id, purpose) WHERE retired_at IS NULL;
--@table hst_key classification=internal billable=false | An Ed25519 key of the host: the identity root, the receipt-signing subkeys and the checkpoint-signing keys, current and retired.
--@column hst_key.id classification=public billable=false | Key id: the first 16 hex digits of the SHA-256 of the public key.
--@column hst_key.host_id classification=internal billable=false | The host the key belongs to.
--@column hst_key.purpose classification=internal billable=false | identity (the root), receipt (signs usage receipts) or checkpoint (signs ledger checkpoints).
--@column hst_key.public_key classification=public billable=false | The 32-byte Ed25519 public key, published at /v1/keys.
--@column hst_key.private_sealed classification=sensitive billable=false | The private key sealed with the host master key.
--@column hst_key.signature classification=public billable=false | The identity key's signature over this key's certificate; empty for the identity key itself.
--@column hst_key.created_at classification=internal billable=false | When the key was created, Unix milliseconds UTC.
--@column hst_key.retired_at classification=internal billable=false | When the key stopped signing, Unix milliseconds UTC; empty while it is the live key. A retired key is kept so old signatures still verify.
