// Package engine is the model runtime port of the engine module (req_001 D-22):
// local-server owns this interface and each runtime (Ollama first) is an
// adapter behind it. The package holds only the port, its value types and the
// typed errors; adapters live in sub-packages and only cmd/ imports them.
//
// Prompts and responses are never logged or stored (req_001 D-31, D-40): no
// type here is a log value, and no error carries message content.
package engine

import (
	"context"
	"time"
)

// Engine is the model runtime port (req_001 D-22). Every method honours
// context cancellation. Failures are *Error values; test them with errors.Is
// against the Err* sentinels.
type Engine interface {
	// Chat starts a streaming chat. The caller must Close the stream.
	// ChatRequest.NumCtx is mandatory: the engine never relies on a runtime default.
	Chat(ctx context.Context, req ChatRequest) (ChatStream, error)
	// Embed returns one vector per input.
	Embed(ctx context.Context, req EmbedRequest) (EmbedResult, error)
	// ListModels lists the models installed in the runtime.
	ListModels(ctx context.Context) ([]Model, error)
	// ModelInfo describes one installed model; ErrModelNotFound if absent.
	ModelInfo(ctx context.Context, name string) (ModelInfo, error)
	// Load brings a model into memory and keeps it for keepAlive (negative: until unloaded).
	Load(ctx context.Context, name string, keepAlive time.Duration) error
	// Unload evicts a model from memory now.
	Unload(ctx context.Context, name string) error
	// Loaded lists the models in memory with their memory use.
	Loaded(ctx context.Context) ([]LoadedModel, error)
}

// OverflowPolicy is the grant's on_context_overflow setting (req_001 D-48).
type OverflowPolicy string

const (
	// OverflowRefuse rejects an over-long prompt with ErrContextTooLong. It is
	// the default, and the meaning of the zero value.
	OverflowRefuse OverflowPolicy = "refuse"
	// OverflowTruncate lets the runtime drop the oldest non-system messages
	// (chat) or cut the input (embed) to fit.
	OverflowTruncate OverflowPolicy = "truncate"
)

// Role of a chat message.
type Role string

// Message roles.
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one chat turn.
type Message struct {
	Role    Role
	Content string
}

// ChatRequest is one chat call. NumCtx and NumPredict are mandatory and
// positive (req_001 D-22, D-39 step 6).
type ChatRequest struct {
	Model      string
	Messages   []Message
	NumCtx     int
	NumPredict int
	// KeepAlive is how long the model stays loaded afterwards. Zero means the
	// adapter's configured default; negative means until unloaded.
	KeepAlive time.Duration
	// OnContextOverflow is zero (refuse) or one of the OverflowPolicy values.
	OnContextOverflow OverflowPolicy
}

// ChatChunk is one streamed piece. The last chunk has Done set and carries
// the native token counts (req_001 D-22).
type ChatChunk struct {
	Content          string
	Done             bool
	DoneReason       string // "stop", "length", ...; only on the last chunk
	PromptTokens     int    // only on the last chunk
	CompletionTokens int    // only on the last chunk
}

// ChatStream yields chunks until a Done chunk, then io.EOF.
type ChatStream interface {
	// Recv returns the next chunk. After the Done chunk it returns io.EOF.
	Recv() (ChatChunk, error)
	// Close releases the stream and cancels the engine call; safe to repeat.
	Close() error
}

// EmbedRequest is one embedding call.
type EmbedRequest struct {
	Model             string
	Input             []string
	NumCtx            int
	KeepAlive         time.Duration // as ChatRequest.KeepAlive
	OnContextOverflow OverflowPolicy
}

// EmbedResult holds one vector per input, in order.
type EmbedResult struct {
	Embeddings   [][]float32
	PromptTokens int
}

// Model is an installed model.
type Model struct {
	Name       string
	SizeBytes  int64
	Digest     string
	ModifiedAt time.Time
}

// ModelInfo describes an installed model.
type ModelInfo struct {
	Name          string
	Family        string
	ParameterSize string
	Quantization  string
	// ContextLength is the model's trained maximum, 0 if unknown.
	ContextLength int
	// Capabilities is the runtime's list, e.g. "completion", "embedding".
	Capabilities []string
}

// LoadedModel is a model in memory.
type LoadedModel struct {
	Name          string
	SizeBytes     int64 // total memory
	VRAMBytes     int64 // the part on the accelerator
	ContextLength int   // loaded context, 0 if unknown
	ExpiresAt     time.Time
}

// CapEmbedding is the capability name of an embedding model.
const CapEmbedding = "embedding"
