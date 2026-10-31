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
// LS_OLLAMA_URL and two installed models named as in enginetest (copy any
// small chat model to "chat-test" and any embedding model to "embed-test"
// with `ollama cp`). It skips when LS_OLLAMA_URL is unset.
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
	enginetest.Run(t, func(*testing.T) engine.Engine { return a })
}
