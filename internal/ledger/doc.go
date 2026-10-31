// Package ledger is part of the ledger module; see docs/modules/ledger.md. It records each
// metered call as an immutable usage record plus a leaf of an append-only Merkle log, and
// signs the receipt the consumer gets (req_001 D-16, D-27, D-39, D-52).
//
// # Recording
//
// Ledger.Record runs one unit of work (storage.DB.Write): it inserts the use_record, appends
// the leaf to ldg_leaf, stores the tlog hashes in ldg_hash, captures a syn_change row and
// signs the receipt, all in the same batched BEGIN IMMEDIATE transaction. Either everything
// is written or nothing is. The receipt is signed inside the unit, after the leaf index is
// fixed and before the record is inserted, so use_record carries the receipt signature from
// its first write and never changes (D-40). The caller gets the leaf index, the tree size
// that leaf creates and the receipt (a JWS) only after the batch has committed. Ledger.RecordTx does the same inside a unit of
// work the caller already owns.
//
// If metering keeps failing the gateway answers anyway with Ledger.SignUnmetered (D-51).
//
// # Tree
//
// The log is an RFC 6962 Merkle tree kept with golang.org/x/mod/sumdb/tlog. The leaf hash is
// tlog.RecordHash(CanonicalLeaf(usage)); ldg_hash holds the tlog stored hashes. Checkpoints
// (signed tree heads) and proofs are built on top of these tables by PLT-F-016. The log
// has one writer, the storage writer goroutine, so leaf indexes have no gaps (D-39).
//
// # Canonical leaf (external contract, version 1)
//
// A consumer rebuilds the leaf from the fields of its receipt and checks an inclusion proof,
// so the encoding is fixed and versioned. The leaf bytes are the RFC 8785 (JSON
// Canonicalization Scheme) form of this JSON object, which has only strings, integers and
// one null, so no number formatting rules apply:
//
//	{"access_key_id":…,"completion_tokens":…,"consumer_id":…,"finished_at":…,
//	 "grant_version_id":…,"job_id":… or null,"model":…,"operation":…,"outcome":…,
//	 "prompt_tokens":…,"started_at":…,"total_tokens":…,"usage_id":…,"v":1}
//
// Keys are in ascending order and there is no white space. started_at and finished_at are
// RFC 3339 UTC with exactly three fractional digits ("2006-01-02T15:04:05.000Z"), the same
// text as in the receipt. Ids are lower-case hyphenated UUIDs. The values are those of the
// receipt payload; host_kid, metering, leaf_index and tree_size are not part of the leaf.
// Latency, key ids and the signature are not part of it either. The leaf holds no prompt or
// response (D-31, D-40). Any change of this encoding needs a new "v".
//
// # Receipt
//
// A receipt is a JWS in compact serialization (RFC 7515) signed with the host's receipt key
// (hostkeys.Receipt) and the fully specified algorithm "Ed25519" (RFC 9864). The protected
// header is {"alg":"Ed25519","kid":<receipt key id>,"typ":"ls-receipt+jws"}; the payload is
// the UsageReceipt schema of api/openapi.yaml (type Receipt). A metered receipt carries
// leaf_index and tree_size (= leaf_index + 1) but no checkpoint (D-52). An unmetered
// receipt (D-51) carries neither. Verify checks a receipt against a receipt public key.
package ledger
