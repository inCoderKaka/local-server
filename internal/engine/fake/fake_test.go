package fake_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/engine/enginetest"
	"github.com/inCoderKaka/local-server/internal/engine/fake"
)

func newEngine(t *testing.T) engine.Engine {
	return fake.New(fake.WithModels(
		fake.ModelSpec{Name: enginetest.ChatModel, ContextLength: 4096},
		fake.ModelSpec{Name: enginetest.EmbedModel, ContextLength: 512, Embedding: true},
	))
}

func TestContract(t *testing.T) { enginetest.Run(t, newEngine) }

func TestChatTimeoutsContract(t *testing.T) {
	enginetest.RunChatTimeouts(t, func(t *testing.T, firstStall, idleStall, fallback time.Duration) engine.Engine {
		return fake.New(fake.WithModels(fake.ModelSpec{Name: enginetest.ChatModel, ContextLength: 4096}),
			fake.WithChatTimeouts(fallback, fallback), fake.WithChunkGap(0, firstStall), fake.WithChunkGap(1, idleStall))
	})
}

func TestChunkGapEndsWhenTheCallIsCancelled(t *testing.T) {
	e := fake.New(fake.WithChunkGap(1, time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	s, err := e.Chat(ctx, engine.ChatRequest{Model: "fake-chat", Messages: []engine.Message{{Role: engine.RoleUser, Content: "hi"}}, NumCtx: 32, NumPredict: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recv(); err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(20*time.Millisecond, cancel)
	if _, err := s.Recv(); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestInjectedErrors(t *testing.T) {
	e := fake.New()
	ctx := context.Background()
	e.InjectError(fake.OpChat, engine.ErrEngineUnavailable, 1)
	req := engine.ChatRequest{Model: "fake-chat", Messages: []engine.Message{{Role: engine.RoleUser, Content: "hi"}}, NumCtx: 32, NumPredict: 8}
	if _, err := e.Chat(ctx, req); !errors.Is(err, engine.ErrEngineUnavailable) {
		t.Fatalf("injected err = %v", err)
	}
	if _, err := e.Chat(ctx, req); err != nil {
		t.Fatalf("second call: %v", err)
	}
	e.InjectError(fake.OpChatStream, engine.ErrTimeout, 1)
	s, err := e.Chat(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recv(); !errors.Is(err, engine.ErrTimeout) {
		t.Fatalf("stream err = %v", err)
	}
	if e.Calls(fake.OpChat) != 3 {
		t.Fatalf("calls = %d", e.Calls(fake.OpChat))
	}
}

func TestDeterministicLengthCap(t *testing.T) {
	e := fake.New(fake.WithReply("a b c d e"))
	s, err := e.Chat(context.Background(), engine.ChatRequest{Model: "fake-chat",
		Messages: []engine.Message{{Role: engine.RoleUser, Content: "x y"}}, NumCtx: 16, NumPredict: 3})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for {
		c, err := s.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		text += c.Content
		if c.Done && (c.DoneReason != "length" || c.PromptTokens != 2 || c.CompletionTokens != 3) {
			t.Fatalf("final = %+v", c)
		}
	}
	if text != "a b c" {
		t.Fatalf("text = %q", text)
	}
}

func TestLoadExpiryUsesClock(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := fake.New(fake.WithClock(func() time.Time { return now }))
	if err := e.Load(context.Background(), "fake-chat", time.Hour); err != nil {
		t.Fatal(err)
	}
	ls, _ := e.Loaded(context.Background())
	if len(ls) != 1 || !ls[0].ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("loaded = %+v", ls)
	}
}

func TestRecordsSampling(t *testing.T) {
	e := fake.New()
	ctx := context.Background()
	req := engine.ChatRequest{Model: "fake-chat", Messages: []engine.Message{{Role: engine.RoleUser, Content: "hi"}}, NumCtx: 32, NumPredict: 8}
	if _, ok := e.LastChatSampling(); ok {
		t.Fatal("sampling recorded before any call")
	}
	if _, err := e.Chat(ctx, req); err != nil {
		t.Fatal(err)
	}
	got, ok := e.LastChatSampling()
	if !ok || got.Temperature != nil || got.TopP != nil || got.Seed != nil || len(got.Stop) != 0 {
		t.Fatalf("unset sampling = %+v, %v", got, ok)
	}
	temp, topP, seed := 0.0, 0.9, 0
	req.Temperature, req.TopP, req.Seed, req.Stop = &temp, &topP, &seed, []string{"END"}
	if _, err := e.Chat(ctx, req); err != nil {
		t.Fatal(err)
	}
	got, _ = e.LastChatSampling()
	if got.Temperature == nil || *got.Temperature != 0 || got.TopP == nil || *got.TopP != 0.9 ||
		got.Seed == nil || *got.Seed != 0 || len(got.Stop) != 1 || got.Stop[0] != "END" {
		t.Fatalf("sampling = %+v", got)
	}
}
