package gateway

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/ledger"
)

// CreateEmbedding implements POST /v1/embeddings (F1, req_001 D-39).
func (g *Gateway) CreateEmbedding(ctx context.Context, req *api.EmbeddingRequest, _ api.CreateEmbeddingParams) (*api.EmbeddingListHeaders, error) {
	c, err := g.newCall(ctx)
	if err != nil {
		return nil, err
	}
	dec, err := g.evaluate(ctx, c, req.Model)
	if err != nil {
		return nil, err
	}
	in, aerr := validateEmbed(req, c.cfg.maxEmbeddingInputs)
	if aerr != nil {
		return nil, aerr
	}
	p, aerr := planEmbed(c.cfg, dec, c.id.Limits.OnContextOverflow, in)
	if aerr != nil {
		return nil, aerr
	}
	tk, admission, err := g.admit(ctx, c, in.model, dec, p)
	if err != nil {
		return nil, err
	}
	defer tk.Release()
	startedAt := g.now()
	t0 := time.Now()
	ectx, cancel := context.WithTimeout(ctx, c.cfg.engineTimeout)
	res, err := g.cfg.Engine.Embed(ectx, engine.EmbedRequest{Model: in.model, Input: in.inputs, NumCtx: p.numCtx,
		KeepAlive: tk.KeepAlive(), OnContextOverflow: p.overflow})
	cancel()
	engineTime := time.Since(t0)
	tk.Release()
	if err != nil {
		if ctx.Err() != nil { // the client went away while the engine ran (req_001 D-38 aborted)
			g.meterAborted(ctx, c, ledger.NewUsageID(), ledger.OpEmbeddings, in.model, 0, startedAt)
		}
		var ee *engine.Error
		if errors.As(err, &ee) && ee.Code == engine.CodeContextTooLong {
			return nil, newErr(http.StatusBadRequest, typeInvalidRequest, codeContextTooLong, "input", "An input text is longer than the context limit.")
		}
		return nil, err
	}
	if len(res.Embeddings) != len(in.inputs) {
		return nil, engine.Errorf(engine.CodeEngineError, nil, "the engine returned %d vectors for %d inputs", len(res.Embeddings), len(in.inputs))
	}
	u := ledger.Usage{UsageID: ledger.NewUsageID(), ConsumerID: c.id.ConsumerID, AccessKeyID: c.id.KeyID,
		GrantVersionID: c.id.GrantVersionID, Operation: ledger.OpEmbeddings, Model: in.model, Outcome: ledger.OutcomeCompleted,
		PromptTokens: int64(res.PromptTokens), StartedAt: startedAt, FinishedAt: g.now()}
	m, err := g.meter(ctx, c, u)
	if err != nil {
		return nil, err
	}
	if m.committed {
		g.afterMetered(ctx, c)
	}
	g.observeOverhead(ledger.OpEmbeddings, c, engineTime, admission)
	data := make([]api.Embedding, len(res.Embeddings))
	for i, v := range res.Embeddings {
		e := api.Embedding{Object: "embedding", Index: i}
		if in.base64 {
			e.Embedding.SetString(encodeFloat32s(v))
		} else {
			e.Embedding.SetFloat32Array(v)
		}
		data[i] = e
	}
	return &api.EmbeddingListHeaders{
		LSReceipt: m.receipt,
		Response: api.EmbeddingList{Object: "list", Data: data, Model: in.model,
			Usage: api.EmbeddingUsage{PromptTokens: u.PromptTokens, TotalTokens: u.PromptTokens}},
	}, nil
}

// encodeFloat32s is the base64 encoding_format: little-endian float32, as OpenAI sends it.
func encodeFloat32s(v []float32) string {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return base64.StdEncoding.EncodeToString(b)
}
