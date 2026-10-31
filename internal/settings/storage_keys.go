package settings

import "github.com/inCoderKaka/local-server/internal/storage"

// KeySyncChangeRetention is the host-owned setting for how long unacknowledged syn_change
// rows are kept (D-77).
const KeySyncChangeRetention = "sync.change.retention"

// StorageSynchronousDef is the host-owned setting for SQLite's synchronous mode under WAL
// (D-76, D-38). storage cannot import settings, so the definition lives here and the
// caller registers it, reads the value before storage.Open and passes it in
// storage.Config.Synchronous. A change applies on the next start. Default FULL: a power
// loss must not drop batches whose signed receipts consumers already hold.
var StorageSynchronousDef = Def{
	Key: storage.KeySynchronous, Kind: String, Default: storage.SyncFull, Owner: Host,
	Description: "SQLite durability under power loss: FULL (default) or NORMAL (faster, may lose the last committed batches). Applies on next start.",
}

// SyncChangeRetentionDef is the definition of KeySyncChangeRetention (D-77): 30 days.
var SyncChangeRetentionDef = Def{
	Key: KeySyncChangeRetention, Kind: Duration, Default: "720h", Owner: Host,
	Description: "How long change rows that no sync target has acknowledged are kept before they are deleted. Zero keeps them forever.",
}

// SyncChangeRule prunes syn_change rows older than KeySyncChangeRetention (D-77).
//
// Tx.Capture always writes syn_change (D-41 makes shipping opt-in, not capture). The
// syn_cursor table does not exist yet (PLT-F-044..F-046), so until it does every row is
// unacknowledged and the rule needs no Where. The off-host ticket that adds syn_cursor
// must extend this rule's Where so it matches only unacknowledged rows, excluding the
// acknowledged rows at or below the lowest cursor; those keep D-40's rule (kept 7 d after
// the acknowledgement) under their own rule.
var SyncChangeRule = Rule{Table: "syn_change", TimeColumn: "created_at", Setting: KeySyncChangeRetention}

// RegisterStorage registers the settings and retention rules that belong to the storage
// module's tables: storage.synchronous (D-76) and sync.change.retention with its rule (D-77).
func (s *Store) RegisterStorage() error {
	if err := s.Register(StorageSynchronousDef, SyncChangeRetentionDef); err != nil {
		return err
	}
	return s.RegisterRetention(SyncChangeRule)
}
