// Package enginetest is the shared contract suite every engine.Engine adapter
// and the fake must pass (req_001 D-31, D-48). It is tokenizer agnostic: it
// only relies on a long prompt overflowing a small context.
package enginetest

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine"
)

// Model names the engine under test must serve.
const (
	// ChatModel is an installed chat model whose trained context is positive.
	ChatModel = "chat-test"
	// EmbedModel is an installed embedding model.
	EmbedModel = "embed-test"
	// MissingModel is not installed.
	MissingModel = "absent-test"
)

// Factory returns a fresh engine serving ChatModel and EmbedModel. Each test
// calls it for its own engine, so state never leaks between tests.
type Factory func(t *testing.T) engine.Engine

const (
	smallCtx   = 64
	maxPredict = 256
	timeout    = 10 * time.Second
)

func longText(words int) string { return strings.TrimSpace(strings.Repeat("alpha ", words)) }

func chatReq(policy engine.OverflowPolicy, msgs ...engine.Message) engine.ChatRequest {
	return chatReqFor(ChatModel, policy, msgs...)
}

func chatReqFor(model string, policy engine.OverflowPolicy, msgs ...engine.Message) engine.ChatRequest {
	return engine.ChatRequest{Model: model, Messages: msgs, NumCtx: smallCtx, NumPredict: maxPredict,
		KeepAlive: time.Minute, OnContextOverflow: policy}
}

func user(s string) engine.Message { return engine.Message{Role: engine.RoleUser, Content: s} }

func ctxT(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}

// drain reads a stream to its end and returns the text and the final chunk.
func drain(t *testing.T, s engine.ChatStream) (string, engine.ChatChunk, int) {
	t.Helper()
	var sb strings.Builder
	var last engine.ChatChunk
	chunks := 0
	for {
		c, err := s.Recv()
		if errors.Is(err, io.EOF) {
			return sb.String(), last, chunks
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		chunks++
		sb.WriteString(c.Content)
		last = c
	}
}

// chatErr runs a chat and returns the error from Chat or from the first Recv.
func chatErr(ctx context.Context, e engine.Engine, req engine.ChatRequest) error {
	s, err := e.Chat(ctx, req)
	if err != nil {
		return err
	}
	defer s.Close()
	_, err = s.Recv()
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// Run executes the contract suite against engines made by f, serving the
// default ChatModel and EmbedModel.
func Run(t *testing.T, f Factory) { RunModels(t, f, ChatModel, EmbedModel) }

// RunModels is Run with the installed chat and embedding model names given,
// for a runtime whose models cannot be named ChatModel and EmbedModel.
func RunModels(t *testing.T, f Factory, chatModel, embedModel string) {
	chatReq := func(policy engine.OverflowPolicy, msgs ...engine.Message) engine.ChatRequest {
		return chatReqFor(chatModel, policy, msgs...)
	}
	t.Run("ListModels", func(t *testing.T) {
		ms, err := f(t).ListModels(ctxT(t))
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, m := range ms {
			seen[m.Name] = true
		}
		if !seen[chatModel] || !seen[embedModel] || seen[MissingModel] {
			t.Fatalf("ListModels names = %v", seen)
		}
	})

	t.Run("ModelInfo", func(t *testing.T) {
		e := f(t)
		info, err := e.ModelInfo(ctxT(t), chatModel)
		if err != nil {
			t.Fatal(err)
		}
		if info.ContextLength <= 0 {
			t.Fatalf("ContextLength = %d, want > 0", info.ContextLength)
		}
		ei, err := e.ModelInfo(ctxT(t), embedModel)
		if err != nil {
			t.Fatal(err)
		}
		has := false
		for _, c := range ei.Capabilities {
			has = has || c == engine.CapEmbedding
		}
		if !has {
			t.Fatalf("embed model capabilities = %v, want %q", ei.Capabilities, engine.CapEmbedding)
		}
		if _, err := e.ModelInfo(ctxT(t), MissingModel); !errors.Is(err, engine.ErrModelNotFound) {
			t.Fatalf("missing model err = %v, want model_not_found", err)
		}
	})

	t.Run("ChatStreamsWithTokenCounts", func(t *testing.T) {
		s, err := f(t).Chat(ctxT(t), chatReq("", user("say hello please")))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		text, last, chunks := drain(t, s)
		if chunks < 2 || text == "" {
			t.Fatalf("chunks = %d text = %q, want a streamed answer", chunks, text)
		}
		if !last.Done || last.PromptTokens <= 0 || last.CompletionTokens <= 0 {
			t.Fatalf("final chunk = %+v, want Done with native token counts", last)
		}
		if _, err := s.Recv(); !errors.Is(err, io.EOF) {
			t.Fatalf("Recv after end = %v, want io.EOF", err)
		}
	})

	t.Run("ChatCancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctxT(t))
		s, err := f(t).Chat(ctx, chatReq("", user("say hello please")))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		cancel()
		for i := 0; i < 1000; i++ {
			if _, err := s.Recv(); err != nil {
				if errors.Is(err, io.EOF) {
					t.Fatal("stream ended normally after cancel")
				}
				return
			}
		}
		t.Fatal("stream kept going after cancel")
	})

	t.Run("ChatInvalidRequest", func(t *testing.T) {
		req := chatReq("", user("hi"))
		req.NumCtx = 0
		if err := chatErr(ctxT(t), f(t), req); !errors.Is(err, engine.ErrInvalidRequest) {
			t.Fatalf("num_ctx 0: err = %v, want invalid_request", err)
		}
		req = chatReq("", user("hi"))
		req.NumPredict = 0
		if err := chatErr(ctxT(t), f(t), req); !errors.Is(err, engine.ErrInvalidRequest) {
			t.Fatalf("num_predict 0: err = %v, want invalid_request", err)
		}
	})

	t.Run("ChatMissingModel", func(t *testing.T) {
		req := chatReq("", user("hi"))
		req.Model = MissingModel
		if err := chatErr(ctxT(t), f(t), req); !errors.Is(err, engine.ErrModelNotFound) {
			t.Fatalf("err = %v, want model_not_found", err)
		}
	})

	t.Run("ChatOverflow", func(t *testing.T) {
		long := user(longText(smallCtx * 8))
		for _, p := range []engine.OverflowPolicy{"", engine.OverflowRefuse} {
			if err := chatErr(ctxT(t), f(t), chatReq(p, long)); !errors.Is(err, engine.ErrContextTooLong) {
				t.Fatalf("policy %q: err = %v, want context_too_long", p, err)
			}
		}
		// truncate keeps the last message and drops the oldest ones.
		s, err := f(t).Chat(ctxT(t), chatReq(engine.OverflowTruncate, long, long, user("short question")))
		if err != nil {
			t.Fatalf("truncate: %v", err)
		}
		defer s.Close()
		_, last, _ := drain(t, s)
		if !last.Done || last.PromptTokens <= 0 {
			t.Fatalf("truncate final chunk = %+v", last)
		}
	})

	t.Run("Embed", func(t *testing.T) {
		res, err := f(t).Embed(ctxT(t), engine.EmbedRequest{Model: embedModel, Input: []string{"one two", "three"},
			NumCtx: smallCtx, KeepAlive: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Embeddings) != 2 || len(res.Embeddings[0]) == 0 || len(res.Embeddings[0]) != len(res.Embeddings[1]) || res.PromptTokens <= 0 {
			t.Fatalf("embed result: %d vectors, tokens %d", len(res.Embeddings), res.PromptTokens)
		}
	})

	t.Run("EmbedInvalidAndMissing", func(t *testing.T) {
		e := f(t)
		if _, err := e.Embed(ctxT(t), engine.EmbedRequest{Model: embedModel, NumCtx: smallCtx}); !errors.Is(err, engine.ErrInvalidRequest) {
			t.Fatalf("empty input: err = %v, want invalid_request", err)
		}
		if _, err := e.Embed(ctxT(t), engine.EmbedRequest{Model: embedModel, Input: []string{"x"}}); !errors.Is(err, engine.ErrInvalidRequest) {
			t.Fatalf("num_ctx 0: err = %v, want invalid_request", err)
		}
		if _, err := e.Embed(ctxT(t), engine.EmbedRequest{Model: MissingModel, Input: []string{"x"}, NumCtx: smallCtx}); !errors.Is(err, engine.ErrModelNotFound) {
			t.Fatalf("missing model: err = %v, want model_not_found", err)
		}
	})

	t.Run("EmbedOverflow", func(t *testing.T) {
		req := engine.EmbedRequest{Model: embedModel, Input: []string{longText(smallCtx * 8)}, NumCtx: smallCtx}
		if _, err := f(t).Embed(ctxT(t), req); !errors.Is(err, engine.ErrContextTooLong) {
			t.Fatalf("refuse: err = %v, want context_too_long", err)
		}
		req.OnContextOverflow = engine.OverflowTruncate
		res, err := f(t).Embed(ctxT(t), req)
		// Some runtimes still fail near the limit even when truncating (Ollama
		// #14186); that must be the typed error, never a panic or a bare error.
		if err != nil && !errors.Is(err, engine.ErrContextTooLong) {
			t.Fatalf("truncate: err = %v, want nil or context_too_long", err)
		}
		if err == nil && len(res.Embeddings) != 1 {
			t.Fatalf("truncate: %d vectors, want 1", len(res.Embeddings))
		}
	})

	t.Run("LoadUnloadLoaded", func(t *testing.T) {
		e := f(t)
		ctx := ctxT(t)
		if err := e.Load(ctx, chatModel, time.Minute); err != nil {
			t.Fatal(err)
		}
		ls, err := e.Loaded(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, m := range ls {
			if m.Name == chatModel {
				found = true
				if m.SizeBytes <= 0 {
					t.Fatalf("loaded size = %d, want > 0", m.SizeBytes)
				}
			}
		}
		if !found {
			t.Fatalf("Loaded = %+v, want %s", ls, chatModel)
		}
		if err := e.Unload(ctx, chatModel); err != nil {
			t.Fatal(err)
		}
		ls, err = e.Loaded(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range ls {
			if m.Name == chatModel {
				t.Fatalf("model still loaded after Unload: %+v", ls)
			}
		}
		if err := e.Load(ctx, MissingModel, time.Minute); !errors.Is(err, engine.ErrModelNotFound) {
			t.Fatalf("load missing: err = %v, want model_not_found", err)
		}
	})
}

// RunUnavailable checks the behaviour when the runtime cannot be reached. f
// returns an engine whose runtime is down.
func RunUnavailable(t *testing.T, f Factory) {
	e := f(t)
	ctx := ctxT(t)
	if _, err := e.ListModels(ctx); !errors.Is(err, engine.ErrEngineUnavailable) {
		t.Fatalf("ListModels: err = %v, want engine_unavailable", err)
	}
	if err := chatErr(ctx, e, chatReq("", user("hi"))); !errors.Is(err, engine.ErrEngineUnavailable) {
		t.Fatalf("Chat: err = %v, want engine_unavailable", err)
	}
	if _, err := e.Loaded(ctx); !errors.Is(err, engine.ErrEngineUnavailable) {
		t.Fatalf("Loaded: err = %v, want engine_unavailable", err)
	}
}
