package gateway

import (
	"context"
	"errors"
	"time"

	"github.com/go-faster/jx"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/gateway/stream"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/scheduler"
)

// streamChat is F1 steps 6 to 9 for stream: true (req_001 D-33, D-39): the engine call, the SSE
// body (package stream) and the metering, which runs when the stream ends. The handler returns
// once the engine produced its first chunk, so an engine error or a first-token timeout still
// answers with a plain HTTP error. The ticket is released by the stream when the engine call
// ends, and the usage is written then, not when this function returns.
func (g *Gateway) streamChat(ctx context.Context, c call, in chatInput, p plan, tk *scheduler.Ticket, admission time.Duration) (api.CreateChatCompletionRes, error) {
	startedAt := g.now()
	t0 := time.Now()
	usageID := ledger.NewUsageID()
	keepAlive := tk.KeepAlive()
	op, model := ledger.OpChatCompletions, in.model
	finish := func(r stream.Result) (string, error) {
		engineTime := time.Since(t0)
		switch r.Outcome {
		case ledger.OutcomeAborted: // the client went away; the receipt has nowhere to go
			g.meterAborted(ctx, c, op, model, r.CompletionTokens, startedAt)
			return "", nil
		case ledger.OutcomeFailed:
			m, err := g.meterPartial(ctx, c, usageID, op, model, ledger.OutcomeFailed, r.CompletionTokens, startedAt)
			if err != nil {
				return "", err
			}
			if m.committed {
				g.afterMetered(ctx, c)
			}
			return m.receipt, nil
		}
		u := ledger.Usage{UsageID: usageID, ConsumerID: c.id.ConsumerID, AccessKeyID: c.id.KeyID,
			GrantVersionID: c.id.GrantVersionID, Operation: op, Model: model, Outcome: ledger.OutcomeCompleted,
			PromptTokens: r.PromptTokens, CompletionTokens: r.CompletionTokens, StartedAt: startedAt, FinishedAt: g.now()}
		m, err := g.meter(ctx, c, u)
		if err != nil {
			return "", err
		}
		if m.committed {
			g.afterMetered(ctx, c)
		}
		g.observeOverhead(op, c, engineTime, admission)
		return m.receipt, nil
	}
	st, err := stream.Open(ctx, stream.Options{
		ID: "chatcmpl-" + usageID, Model: model, Created: startedAt.Unix(), IncludeUsage: in.includeUsage,
		FirstToken: c.cfg.streamFirstToken, Idle: c.cfg.streamIdle, Total: c.cfg.streamTotal,
		Release: tk.Release, Finish: finish, ErrorEvent: g.streamErrorEvent(ctx),
	}, func(ectx context.Context) (engine.ChatStream, error) {
		return g.cfg.Engine.Chat(ectx, engine.ChatRequest{Model: in.model, Messages: in.messages, NumCtx: p.numCtx,
			NumPredict: p.numPredict, KeepAlive: keepAlive, OnContextOverflow: p.overflow})
	})
	if err != nil {
		if ctx.Err() != nil { // the client went away before the first chunk (req_001 D-38 aborted)
			g.meterAborted(ctx, c, op, model, 0, startedAt)
		}
		return nil, err
	}
	return &api.CreateChatCompletionOKTextEventStreamHeaders{Response: api.CreateChatCompletionOKTextEventStream{Data: st}}, nil
}

// streamErrorEvent returns the function that renders an error that ended a started stream as the
// OpenAI error envelope, the same one an HTTP error carries. The text is fixed; the cause is
// logged by type or code only.
func (g *Gateway) streamErrorEvent(ctx context.Context) func(error) []byte {
	return func(err error) []byte {
		e, internal := classify(err, g.now())
		var te *stream.TimeoutError
		var ee *engine.Error
		switch {
		case internal:
			g.log.ErrorContext(ctx, "gateway: streamed request failed", "err", err)
		case errors.As(err, &te):
			g.log.WarnContext(ctx, "gateway: streamed engine call timed out", "kind", te.Kind)
		case errors.As(err, &ee):
			g.log.WarnContext(ctx, "gateway: streamed engine call failed", "status", e.status, "code", string(ee.Code))
		}
		enc := new(jx.Encoder)
		e.response().Response.Encode(enc)
		return enc.Bytes()
	}
}
