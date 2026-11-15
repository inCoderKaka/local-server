package requests

import (
	"encoding/json"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// PurgeConsumerTx deletes every resource request of the consumer with all the comments on them, the
// host's too, in the caller's unit of work, and returns how many requests it deleted. The consumer's
// message, ask and comments are personal text, so the purge of a deleted consumer removes them
// (req_001 D-136). Each deleted request leaves a delete change in syn_change with the ids only. The
// text is never read or logged.
func (s *Service) PurgeConsumerTx(tx *storage.Tx, consumerID string) (int64, error) {
	type gone struct {
		id      string
		version int64
	}
	rows, err := tx.Query(`SELECT id, row_version FROM req_resource_request WHERE consumer_id = ? ORDER BY id`, consumerID)
	if err != nil {
		return 0, err
	}
	ids, err := collect(rows, func(r storage.Row) (gone, error) {
		var g gone
		return g, r.Scan(&g.id, &g.version)
	})
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	// The comments go explicitly, not through the cascade.
	if _, err := tx.Exec(`DELETE FROM req_comment WHERE request_id IN (SELECT id FROM req_resource_request WHERE consumer_id = ?)`, consumerID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM req_resource_request WHERE consumer_id = ?`, consumerID); err != nil {
		return 0, err
	}
	for _, g := range ids {
		b, err := json.Marshal(map[string]any{"id": g.id, "consumer_id": consumerID})
		if err != nil {
			return 0, err
		}
		if err := tx.Capture(storage.Change{Dataset: Dataset, Table: "req_resource_request", PK: g.id, Version: g.version + 1, Op: "delete", Payload: b}); err != nil {
			return 0, err
		}
	}
	return int64(len(ids)), nil
}
