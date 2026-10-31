package checkpoint

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/mod/sumdb/tlog"

	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// Keys is the host key custody the service needs; *hostkeys.Keys implements it.
type Keys interface {
	Sign(p hostkeys.Purpose, msg []byte) (keyID string, sig []byte, err error)
	HostID() string
	PublicKeys(ctx context.Context) ([]hostkeys.PublicKey, error)
	EnsureCurrent(ctx context.Context) ([]hostkeys.Purpose, error)
}

// Clock is the time source; tests pass a fake so waits are deterministic.
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
}

// Timer is a one-shot timer.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

type realClock struct{}

func (realClock) Now() time.Time                 { return time.Now() }
func (realClock) NewTimer(d time.Duration) Timer { return realTimer{time.NewTimer(d)} }

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time { return r.t.C }
func (r realTimer) Stop() bool          { return r.t.Stop() }

// Errors returned by the read API.
var (
	ErrNoCheckpoint = errors.New("checkpoint: no checkpoint has been signed yet")
	ErrUnknownUsage = errors.New("checkpoint: no ledger leaf for this usage id")
	ErrBadTreeSize  = errors.New("checkpoint: tree size is not between 1 and the latest checkpoint's")
	// ErrNotYetCovered matches (errors.Is) the *NotYetCoveredError of ProofForUsage.
	ErrNotYetCovered = errors.New("checkpoint: no checkpoint covers this leaf yet")
)

// NotYetCoveredError says a leaf exists but no checkpoint covers it yet (D-52). The gateway
// answers 409 with Retry-After set to RetryAfter.
type NotYetCoveredError struct {
	LeafIndex  int64
	RetryAfter time.Duration
}

func (e *NotYetCoveredError) Error() string {
	return fmt.Sprintf("checkpoint: leaf %d is not covered by a checkpoint yet; retry in %s", e.LeafIndex, e.RetryAfter)
}

func (e *NotYetCoveredError) Is(target error) bool { return target == ErrNotYetCovered }

// Checkpoint is a stored, signed tree head.
type Checkpoint struct {
	TreeSize int64
	RootHash tlog.Hash
	// Note is the C2SP signed note, verbatim.
	Note string
	// KeyID is the hostkeys id of the checkpoint key that signed it.
	KeyID     string
	CreatedAt time.Time
}

// Service builds checkpoints and answers proof queries over the ledger tables.
type Service struct {
	db       *storage.DB
	keys     Keys
	settings *settings.Store
	clock    Clock
}

// New registers the checkpoint settings and returns a Service. db must have been opened with
// ledger.Module() (and the modules that one needs); keys is a *hostkeys.Keys.
func New(db *storage.DB, keys Keys, st *settings.Store) (*Service, error) {
	return newService(db, keys, st, realClock{})
}

func newService(db *storage.DB, keys Keys, st *settings.Store, c Clock) (*Service, error) {
	if err := st.Register(Defs()...); err != nil {
		return nil, err
	}
	return &Service{db: db, keys: keys, settings: st, clock: c}, nil
}

// origin returns the origin line and key name in force.
func (s *Service) origin(ctx context.Context) (string, error) {
	o, err := s.settings.String(ctx, KeyOrigin, "")
	if err != nil {
		return "", err
	}
	if o == "" {
		o = "local-server/ledger/" + s.keys.HostID()
	}
	if !validName(o) {
		return "", fmt.Errorf("%w (setting %s)", ErrBadKeyName, KeyOrigin)
	}
	return o, nil
}

var errKeyRotated = errors.New("checkpoint: the checkpoint key rotated while signing")

// Build signs a checkpoint of the whole log as it is now and stores it. It returns false and
// writes nothing when the log is empty or has no leaf the last checkpoint does not cover.
func (s *Service) Build(ctx context.Context) (Checkpoint, bool, error) {
	origin, err := s.origin(ctx)
	if err != nil {
		return Checkpoint{}, false, err
	}
	for range 3 {
		cp, made, err := s.build(ctx, origin)
		if errors.Is(err, errKeyRotated) {
			continue
		}
		return cp, made, err
	}
	return Checkpoint{}, false, errKeyRotated
}

func (s *Service) build(ctx context.Context, origin string) (Checkpoint, bool, error) {
	pks, err := s.keys.PublicKeys(ctx)
	if err != nil {
		return Checkpoint{}, false, err
	}
	pubs := map[string]ed25519.PublicKey{}
	for _, pk := range pks {
		if pk.Purpose == hostkeys.Checkpoint {
			pubs[pk.ID] = pk.Key
		}
	}
	var out Checkpoint
	made := false
	err = s.db.Write(ctx, func(tx *storage.Tx) error {
		made = false
		var size, last int64
		if err := tx.QueryRow(`SELECT COALESCE(MAX(leaf_index) + 1, 0) FROM ldg_leaf`).Scan(&size); err != nil {
			return fmt.Errorf("checkpoint: read log size: %w", err)
		}
		if err := tx.QueryRow(`SELECT COALESCE(MAX(tree_size), 0) FROM ldg_checkpoint`).Scan(&last); err != nil {
			return fmt.Errorf("checkpoint: read last checkpoint: %w", err)
		}
		if size == 0 || size <= last {
			return nil
		}
		root, err := tlog.TreeHash(size, ledger.TxHashes(tx))
		if err != nil {
			return fmt.Errorf("checkpoint: tree hash at %d: %w", size, err)
		}
		text := Text{Origin: origin, TreeSize: size, RootHash: root}.Format()
		kid, sig, err := s.keys.Sign(hostkeys.Checkpoint, []byte(text))
		if err != nil {
			return err
		}
		pub, ok := pubs[kid]
		if !ok {
			return errKeyRotated
		}
		signed := signedNote(text, origin, pub, sig)
		at := s.clock.Now()
		if _, err := tx.Exec(`INSERT INTO ldg_checkpoint (tree_size, root_hash, note, key_id, created_at) VALUES (?, ?, ?, ?, ?)`,
			size, root[:], signed, kid, at.UnixMilli()); err != nil {
			return fmt.Errorf("checkpoint: insert ldg_checkpoint: %w", err)
		}
		out = Checkpoint{TreeSize: size, RootHash: root, Note: signed, KeyID: kid, CreatedAt: at.UTC()}
		made = true
		return nil
	})
	if err != nil {
		return Checkpoint{}, false, err
	}
	return out, made, nil
}

// LeafCount returns the number of leaves in the log.
func (s *Service) LeafCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.Reader().QueryRow(ctx, `SELECT COALESCE(MAX(leaf_index) + 1, 0) FROM ldg_leaf`).Scan(&n)
	return n, err
}

const checkpointCols = `SELECT tree_size, root_hash, note, key_id, created_at FROM ldg_checkpoint `

// queryCheckpoint returns the first checkpoint the query yields, or ErrNoCheckpoint.
func (s *Service) queryCheckpoint(ctx context.Context, tail string, args ...any) (Checkpoint, error) {
	rows, err := s.db.Reader().Query(ctx, checkpointCols+tail, args...)
	if err != nil {
		return Checkpoint{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Checkpoint{}, err
		}
		return Checkpoint{}, ErrNoCheckpoint
	}
	var cp Checkpoint
	var root []byte
	var at int64
	if err := rows.Scan(&cp.TreeSize, &root, &cp.Note, &cp.KeyID, &at); err != nil {
		return Checkpoint{}, err
	}
	if len(root) != len(cp.RootHash) {
		return Checkpoint{}, errors.New("checkpoint: stored root hash has the wrong length")
	}
	copy(cp.RootHash[:], root)
	cp.CreatedAt = time.UnixMilli(at).UTC()
	return cp, nil
}

// Latest returns the newest checkpoint, or ErrNoCheckpoint.
func (s *Service) Latest(ctx context.Context) (Checkpoint, error) {
	return s.queryCheckpoint(ctx, `ORDER BY tree_size DESC LIMIT 1`)
}

// UsageProof is a leaf's inclusion proof against the first checkpoint that covers it.
type UsageProof struct {
	UsageID    string
	LeafIndex  int64
	LeafHash   tlog.Hash
	Checkpoint Checkpoint
	// Proof is the RFC 6962 inclusion proof: hashes from the leaf up to Checkpoint.RootHash.
	Proof tlog.RecordProof
}

// ProofForUsage returns the first checkpoint that covers the usage record's leaf and the
// inclusion proof (D-52). It returns ErrUnknownUsage when the usage id has no leaf (an
// unmetered receipt, or no such record), and a *NotYetCoveredError (errors.Is
// ErrNotYetCovered) while no checkpoint covers the leaf.
func (s *Service) ProofForUsage(ctx context.Context, usageID string) (UsageProof, error) {
	p := UsageProof{UsageID: usageID}
	rows, err := s.db.Reader().Query(ctx, `SELECT leaf_index, leaf_hash FROM ldg_leaf WHERE usage_id = ?`, usageID)
	if err != nil {
		return UsageProof{}, err
	}
	var hash []byte
	if rows.Next() {
		err = rows.Scan(&p.LeafIndex, &hash)
	} else {
		err = rows.Err()
		if err == nil {
			err = ErrUnknownUsage
		}
	}
	if cerr := rows.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return UsageProof{}, err
	}
	if len(hash) != len(p.LeafHash) {
		return UsageProof{}, errors.New("checkpoint: stored leaf hash has the wrong length")
	}
	copy(p.LeafHash[:], hash)

	cp, err := s.queryCheckpoint(ctx, `WHERE tree_size > ? ORDER BY tree_size LIMIT 1`, p.LeafIndex)
	if errors.Is(err, ErrNoCheckpoint) {
		every, derr := s.settings.Duration(ctx, KeyMaxInterval, "")
		if derr != nil {
			return UsageProof{}, derr
		}
		return UsageProof{}, &NotYetCoveredError{LeafIndex: p.LeafIndex, RetryAfter: every}
	}
	if err != nil {
		return UsageProof{}, err
	}
	p.Checkpoint = cp
	if p.Proof, err = tlog.ProveRecord(cp.TreeSize, p.LeafIndex, ledger.DBHashes(ctx, s.db)); err != nil {
		return UsageProof{}, fmt.Errorf("checkpoint: inclusion proof: %w", err)
	}
	return p, nil
}

// Consistency returns the latest checkpoint and the RFC 6962 proof that the tree of size from
// is a prefix of it. from must be between 1 and the latest tree size, else ErrBadTreeSize.
func (s *Service) Consistency(ctx context.Context, from int64) (Checkpoint, tlog.TreeProof, error) {
	cp, err := s.Latest(ctx)
	if err != nil {
		return Checkpoint{}, nil, err
	}
	if from < 1 || from > cp.TreeSize {
		return Checkpoint{}, nil, ErrBadTreeSize
	}
	proof, err := tlog.ProveTree(cp.TreeSize, from, ledger.DBHashes(ctx, s.db))
	if err != nil {
		return Checkpoint{}, nil, fmt.Errorf("checkpoint: consistency proof: %w", err)
	}
	return cp, proof, nil
}

// EncodeHashes returns hashes as standard base64 strings, as the API carries them.
func EncodeHashes[T ~[]tlog.Hash](hs T) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = base64.StdEncoding.EncodeToString(h[:])
	}
	return out
}

func logger() *slog.Logger { return slog.Default() }
