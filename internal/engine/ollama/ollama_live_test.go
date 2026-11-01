//go:build ollama

package ollama_test

import (
	"os"
	"testing"

	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/engine/enginetest"
	"github.com/inCoderKaka/local-server/internal/engine/ollama"
)

// TestLive runs the shared contract suite against a real Ollama. It needs
// LS_OLLAMA_URL and two installed models: a small chat model named by
// LS_OLLAMA_CHAT_MODEL and an embedding model named by LS_OLLAMA_EMBED_MODEL.
// Each defaults to the enginetest name ("chat-test", "embed-test") when unset
// or empty, so copying a model to that name with `ollama cp` also works. It
// skips when LS_OLLAMA_URL is unset.
func TestLive(t *testing.T) {
	url := os.Getenv("LS_OLLAMA_URL")
	if url == "" {
		t.Skip("LS_OLLAMA_URL not set")
	}
	a := ollama.New(ollama.Config{BaseURL: url})
	t.Cleanup(a.Close)
	if err := a.CheckVersion(t.Context()); err != nil {
		t.Fatal(err)
	}
	enginetest.RunModels(t, func(*testing.T) engine.Engine { return a },
		envOr("LS_OLLAMA_CHAT_MODEL", enginetest.ChatModel),
		envOr("LS_OLLAMA_EMBED_MODEL", enginetest.EmbedModel))
}

// envOr returns the environment variable key, or def when it is unset or empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
