package ledger

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"golang.org/x/mod/sumdb/tlog"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Module returns the storage registration of the ledger migrations (prefix ldg_). The
// database must also be opened with usage.Module(): the record is written to use_record.
func Module() storage.Module {
	sub, err := fs.Sub(ownMigrations, "migrations")
	if err != nil {
		panic(err) // the embedded directory is fixed at build time
	}
	return storage.Module{Name: "ledger", Prefixes: []string{"ldg_"}, Migrations: sub}
}

// Dataset and table named in the syn_change row of a record (D-40: the usage dataset).
const (
	changeDataset = "usage"
	changeTable   = "use_record"
)

// Ledger records usage and signs receipts.
type Ledger struct {
	db     *storage.DB
	signer Signer
	now    func() time.Time
}

// New returns a Ledger over db, which must have been opened with usage.Module() and
// Module(), signing receipts with signer (a *hostkeys.Keys).
func New(db *storage.DB, signer Signer) *Ledger {
	return &Ledger{db: db, signer: signer, now: time.Now}
}

// Recorded is the outcome of a recorded call.
type Recorded struct {
	UsageID   string
	LeafIndex int64
	// TreeSize is the size of the log once this leaf was appended: LeafIndex + 1.
	TreeSize int64
	// Receipt is the signed receipt for the LS-Receipt header (or the final stream event).
	Receipt Signed
}

// Record writes the usage record, its ledger leaf and its change row in one unit of work and
// signs the receipt in it (D-39 F1 step 8). It returns after the batch the unit joined has
// committed; on an error nothing is written. Use RecordTx to join a unit you already own.
func (l *Ledger) Record(ctx context.Context, u Usage) (Recorded, error) {
	var out Recorded
	err := l.db.Write(ctx, func(tx *storage.Tx) error {
		var err error
		out, err = l.RecordTx(tx, u)
		return err
	})
	if err != nil {
		return Recorded{}, err
	}
	return out, nil
}

// RecordTx is Record inside the caller's unit of work. The result is valid only if the
// unit commits; if fn returns an error, the leaf index it returned was never used.
func (l *Ledger) RecordTx(tx *storage.Tx, u Usage) (Recorded, error) {
	u, err := u.normalized()
	if err != nil {
		return Recorded{}, err
	}
	// The storage writer is the only writer and this runs inside its transaction, so the next
	// index read here is the one this insert takes.
	var n int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(leaf_index) + 1, 0) FROM ldg_leaf`).Scan(&n); err != nil {
		return Recorded{}, fmt.Errorf("ledger: read log size: %w", err)
	}
	leaf := canonicalLeaf(u)
	leafHash := tlog.RecordHash(leaf)
	hashes, err := tlog.StoredHashesForRecordHash(n, leafHash, txHashes{tx})
	if err != nil {
		return Recorded{}, fmt.Errorf("ledger: tree hashes for leaf %d: %w", n, err)
	}
	r := u.receipt(l.signer.HostID())
	r.Metering = Metered
	size := n + 1
	r.LeafIndex, r.TreeSize = &n, &size
	signed, err := l.signReceipt(r)
	if err != nil {
		return Recorded{}, err
	}
	at := l.now().UnixMilli()
	var job any
	if u.JobID != "" {
		job = u.JobID
	}
	if _, err := tx.Exec(`INSERT INTO use_record (id, consumer_id, access_key_id, grant_version_id, job_id, operation, model, outcome,
		prompt_tokens, completion_tokens, total_tokens, latency_ms, started_at, finished_at, receipt_kid, receipt_signature, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.UsageID, u.ConsumerID, u.AccessKeyID, u.GrantVersionID, job, u.Operation, u.Model, u.Outcome,
		u.PromptTokens, u.CompletionTokens, u.TotalTokens(), u.FinishedAt.Sub(u.StartedAt).Milliseconds(),
		u.StartedAt.UnixMilli(), u.FinishedAt.UnixMilli(), signed.KeyID, signed.Signature, at); err != nil {
		return Recorded{}, fmt.Errorf("ledger: insert use_record: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO ldg_leaf (leaf_index, usage_id, leaf_hash, created_at) VALUES (?, ?, ?, ?)`,
		n, u.UsageID, leafHash[:], at); err != nil {
		return Recorded{}, fmt.Errorf("ledger: insert ldg_leaf: %w", err)
	}
	base := tlog.StoredHashIndex(0, n)
	for i, h := range hashes {
		if _, err := tx.Exec(`INSERT INTO ldg_hash (hash_index, hash) VALUES (?, ?)`, base+int64(i), h[:]); err != nil {
			return Recorded{}, fmt.Errorf("ledger: insert ldg_hash: %w", err)
		}
	}
	// The change row carries the canonical leaf: ids, numbers and times, no content (D-31, D-40).
	if err := tx.Capture(storage.Change{Dataset: changeDataset, Table: changeTable, PK: u.UsageID, Version: 1, Op: "insert", Payload: leaf}); err != nil {
		return Recorded{}, fmt.Errorf("ledger: capture change: %w", err)
	}
	return Recorded{UsageID: u.UsageID, LeafIndex: n, TreeSize: size, Receipt: signed}, nil
}

// txHashes reads tlog stored hashes from ldg_hash inside a unit of work; it sees the unit's
// own earlier writes.
type txHashes struct{ tx *storage.Tx }

var errMissingHash = errors.New("ledger: stored hash missing from ldg_hash")

func (r txHashes) ReadHashes(indexes []int64) ([]tlog.Hash, error) {
	out := make([]tlog.Hash, len(indexes))
	for i, idx := range indexes {
		var b []byte
		if err := r.tx.QueryRow(`SELECT hash FROM ldg_hash WHERE hash_index = ?`, idx).Scan(&b); err != nil {
			return nil, fmt.Errorf("%w: index %d: %v", errMissingHash, idx, err)
		}
		if len(b) != len(out[i]) {
			return nil, fmt.Errorf("%w: index %d has %d bytes", errMissingHash, idx, len(b))
		}
		copy(out[i][:], b)
	}
	return out, nil
}
