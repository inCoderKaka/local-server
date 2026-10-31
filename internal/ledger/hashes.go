package ledger

import (
	"context"
	"fmt"

	"golang.org/x/mod/sumdb/tlog"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// TxHashes returns a tlog.HashReader over ldg_hash inside a unit of work; it sees the unit's
// own earlier writes.
func TxHashes(tx *storage.Tx) tlog.HashReader { return txHashes{tx} }

// DBHashes returns a tlog.HashReader over ldg_hash that reads committed data outside a unit of
// work, for proofs. ctx bounds every read.
func DBHashes(ctx context.Context, db *storage.DB) tlog.HashReader { return dbHashes{ctx, db} }

type dbHashes struct {
	ctx context.Context
	db  *storage.DB
}

func (r dbHashes) ReadHashes(indexes []int64) ([]tlog.Hash, error) {
	out := make([]tlog.Hash, len(indexes))
	for i, idx := range indexes {
		var b []byte
		if err := r.db.Reader().QueryRow(r.ctx, `SELECT hash FROM ldg_hash WHERE hash_index = ?`, idx).Scan(&b); err != nil {
			return nil, fmt.Errorf("%w: index %d: %v", errMissingHash, idx, err)
		}
		if len(b) != len(out[i]) {
			return nil, fmt.Errorf("%w: index %d has %d bytes", errMissingHash, idx, len(b))
		}
		copy(out[i][:], b)
	}
	return out, nil
}
