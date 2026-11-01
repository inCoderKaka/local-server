package gateway

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/ledger"
)

// CreateChatCompletion implements POST /v1/chat/completions (F1, req_001 D-39); stream: true
// goes to streamChat. Auth and limits already ran in the security handler.
func (g *Gateway) CreateChatCompletion(ctx context.Context, req *api.ChatCompletionRequest, _ api.CreateChatCompletionParams) (api.CreateChatCompletionRes, error) {
	c, err := g.newCall(ctx)
	if err != nil {
		return nil, err
	}
	dec, err := g.evaluate(ctx, c, req.Model)
	if err != nil {
		return nil, err
	}
	in, aerr := validateChat(req)
	if aerr != nil {
		return nil, aerr
	}
	p, aerr := planChat(c.cfg, dec, c.id.Limits.OnContextOverflow, in)
	if aerr != nil {
		return nil, aerr
	}
	tk, admission, err := g.admit(ctx, c, in.model, dec, p)
	if err != nil {
		return nil, err
	}
	if in.stream {
		return g.streamChat(ctx, c, in, p, tk, admission)
	}
	defer tk.Release()
	startedAt := g.now()
	t0 := time.Now()
	text, last, chunks, err := g.runChat(ctx, c, tk.KeepAlive(), in, p)
	engineTime := time.Since(t0)
	tk.Release()
	if err != nil {
		if ctx.Err() != nil { // the client went away while the engine ran (req_001 D-38 aborted)
			g.meterAborted(ctx, c, ledger.OpChatCompletions, in.model, int64(chunks), startedAt)
		}
		return nil, err
	}
	u := ledger.Usage{UsageID: ledger.NewUsageID(), ConsumerID: c.id.ConsumerID, AccessKeyID: c.id.KeyID,
		GrantVersionID: c.id.GrantVersionID, Operation: ledger.OpChatCompletions, Model: in.model, Outcome: ledger.OutcomeCompleted,
		PromptTokens: int64(last.PromptTokens), CompletionTokens: int64(last.CompletionTokens), StartedAt: startedAt, FinishedAt: g.now()}
	m, err := g.meter(ctx, c, u)
	if err != nil {
		return nil, err
	}
	if m.committed {
		g.afterMetered(ctx, c)
	}
	g.observeOverhead(ledger.OpChatCompletions, c, engineTime, admission)
	finish := api.FinishReasonStop
	if last.DoneReason == "length" {
		finish = api.FinishReasonLength
	}
	return &api.ChatCompletionHeaders{
		LSReceipt: api.NewOptString(m.receipt),
		Response: api.ChatCompletion{
			ID: "chatcmpl-" + m.usageID, Object: "chat.completion", Created: u.FinishedAt.Unix(), Model: in.model,
			Choices: []api.ChatCompletionChoice{{Index: 0, FinishReason: finish,
				Message: api.ChatCompletionMessage{Role: api.ChatCompletionMessageRoleAssistant, Content: api.NewNilString(text)}}},
			Usage: api.CompletionUsage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, TotalTokens: u.TotalTokens()},
		},
	}, nil
}

// runChat is F1 step 6 for a non-streamed answer: it reads the engine's stream to its end.
// The reply text lives in memory for the length of the call and is never logged. It also returns
// how many chunks with content arrived before the stream ended or failed.
func (g *Gateway) runChat(ctx context.Context, c call, keepAlive time.Duration, in chatInput, p plan) (string, engine.ChatChunk, int, error) {
	ectx, cancel := context.WithTimeout(ctx, c.cfg.engineTimeout)
	defer cancel()
	s, err := g.cfg.Engine.Chat(ectx, engine.ChatRequest{Model: in.model, Messages: in.messages, NumCtx: p.numCtx,
		NumPredict: p.numPredict, KeepAlive: keepAlive, OnContextOverflow: p.overflow})
	if err != nil {
		return "", engine.ChatChunk{}, 0, err
	}
	defer s.Close()
	var sb strings.Builder
	var last engine.ChatChunk
	done := false
	chunks := 0
	for {
		ch, err := s.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", engine.ChatChunk{}, chunks, err
		}
		if ch.Content != "" {
			chunks++
		}
		sb.WriteString(ch.Content)
		if ch.Done {
			last, done = ch, true
		}
	}
	if !done {
		return "", engine.ChatChunk{}, chunks, engine.Errorf(engine.CodeEngineError, nil, "the stream ended without a final chunk")
	}
	return sb.String(), last, chunks, nil
}
