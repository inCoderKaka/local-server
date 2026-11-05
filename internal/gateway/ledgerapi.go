package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/ledger/checkpoint"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// GetHostKeys implements GET /v1/keys: the host's public keys as a JWK Set (req_001 D-27).
func (g *Gateway) GetHostKeys(ctx context.Context) (*api.HostKeySet, error) {
	ks, err := g.cfg.Checkpoints.KeySet(ctx)
	if err != nil {
		return nil, fmt.Errorf("gateway: key set: %w", err)
	}
	// checkpoint.KeySet is the HostKeySet schema in Go (it has a golden test of its own), so the
	// conversion goes through its JSON form and stays in step with the schema.
	b, err := json.Marshal(ks)
	if err != nil {
		return nil, err
	}
	var out api.HostKeySet
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("gateway: key set does not match the HostKeySet schema: %w", err)
	}
	return &out, nil
}

// GetLedgerCheckpoint implements GET /v1/ledger/checkpoint: the latest signed checkpoint, and
// with from_tree_size the consistency proof from that earlier size (req_001 D-52).
func (g *Gateway) GetLedgerCheckpoint(ctx context.Context, params api.GetLedgerCheckpointParams) (*api.LedgerCheckpoint, error) {
	if from, ok := params.FromTreeSize.Get(); ok {
		cp, proof, err := g.cfg.Checkpoints.Consistency(ctx, from)
		if err != nil {
			return nil, err
		}
		out := checkpointBody(cp)
		out.FromTreeSize = api.NewOptNilInt64(from)
		out.ConsistencyProof = api.NewOptNilStringArray(checkpoint.EncodeHashes(proof))
		return out, nil
	}
	cp, err := g.cfg.Checkpoints.Latest(ctx)
	if err != nil {
		return nil, err
	}
	return checkpointBody(cp), nil
}

func checkpointBody(cp checkpoint.Checkpoint) *api.LedgerCheckpoint {
	return &api.LedgerCheckpoint{Checkpoint: cp.Note, TreeSize: cp.TreeSize, RootHash: base64.StdEncoding.EncodeToString(cp.RootHash[:])}
}

// GetUsageProof implements GET /v1/usage/{usage_id}/proof. A consumer gets the proof of its
// own records only; another consumer's record answers 404 like an unknown one.
func (g *Gateway) GetUsageProof(ctx context.Context, params api.GetUsageProofParams) (*api.UsageProof, error) {
	id, err := g.caller(ctx)
	if err != nil {
		return nil, err
	}
	usageID := params.UsageID.String()
	owner, err := usage.ConsumerOf(ctx, g.cfg.DB.Reader(), usageID)
	if err != nil {
		return nil, err
	}
	if owner != id.ConsumerID {
		return nil, usage.ErrNotFound
	}
	p, err := g.cfg.Checkpoints.ProofForUsage(ctx, usageID)
	if err != nil {
		return nil, err
	}
	uid, err := uuid.Parse(p.UsageID)
	if err != nil {
		return nil, fmt.Errorf("gateway: stored usage id: %w", err)
	}
	return &api.UsageProof{UsageID: uid, LeafIndex: p.LeafIndex, LeafHash: base64.StdEncoding.EncodeToString(p.LeafHash[:]),
		TreeSize: p.Checkpoint.TreeSize, Checkpoint: p.Checkpoint.Note, InclusionProof: checkpoint.EncodeHashes(p.Proof)}, nil
}
