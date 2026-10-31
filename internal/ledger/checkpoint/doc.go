// Package checkpoint is part of the ledger module (docs/modules/ledger.md). It signs
// checkpoints of the usage ledger, serves proofs against them and publishes the host keys
// (req_001 D-16, D-27, D-52, D-54, D-65).
//
// # Checkpoint note (external contract)
//
// A checkpoint is a C2SP tlog-checkpoint inside a C2SP signed note (c2sp.org/tlog-checkpoint,
// c2sp.org/signed-note). The note text is
//
//	<origin>\n<tree size, decimal>\n<root hash, standard base64>\n
//
// followed by a blank line and one signature line "— <key name> <base64(4-byte key hash,
// Ed25519 signature)>" (signature type 0x01). The key name equals the origin. Both come from
// the setting ledger.checkpoint_origin; when it is empty the origin is
// "local-server/ledger/<host id>". The key hash is the first four bytes of
// SHA-256(name, newline, 0x01, public key). The verifier key a consumer pins is
// "<name>+<hash as 8 hex digits>+<base64(0x01, public key)>", published as
// ls_note_verifier_key on /v1/keys. A consumer that gets a checkpoint verifies it with Verify
// and the checkpoint keys of that list, retired keys included.
//
// # Proofs
//
// ProofForUsage returns the first checkpoint that covers a leaf with its RFC 6962 inclusion
// proof (D-52); until one exists it returns a NotYetCoveredError that carries the Retry-After
// the gateway sends with 409. Consistency returns the proof that an earlier tree size is a
// prefix of the latest checkpoint.
//
// # Worker
//
// Worker.Run writes a checkpoint when ledger.checkpoint_min_leaves new leaves exist, or when
// ledger.checkpoint_max_interval has passed since the last checkpoint and at least one leaf is
// new, and calls hostkeys.EnsureCurrent every ledger.key_check_interval. Every time is read
// from a settings store at the moment of use and from an injectable Clock (D-38, D-54).
package checkpoint
