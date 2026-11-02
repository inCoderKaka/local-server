// Package fake is a deterministic in-memory engine.Engine for tests of the
// gateway, scheduler and the adapter contract suite (req_001 D-31). It counts
// one token per whitespace-separated word, streams its reply one word per
// chunk, and supports load/unload, context overflow and error injection.
// It is not an adapter: any package may import it.
package fake

import (
	"context"
	"hash/fnv"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine"
)

// Op names an Engine method for error injection.
type Op string

// Injectable operations.
const (
	OpChat       Op = "chat"
	OpChatStream Op = "chat_stream" // fails the stream on its first Recv
	OpEmbed      Op = "embed"
	OpListModels Op = "list_models"
	OpModelInfo  Op = "model_info"
	OpLoad       Op = "load"
	OpUnload     Op = "unload"
	OpLoaded     Op = "loaded"
)

// ModelSpec declares a model the fake serves.
type ModelSpec struct {
	Name          string
	ContextLength int
	Embedding     bool  // an embedding model; otherwise a chat model
	Dim           int   // embedding dimension, default 8
	MemoryBytes   int64 // reported by Loaded, default 1 GiB
}

// DefaultReply is what a chat model answers unless WithReply is given.
const DefaultReply = "this is a deterministic reply from the fake engine"

// Engine is the fake. Create it with New.
type Engine struct {
	mu     sync.Mutex
	models map[string]ModelSpec
	order  []string
	loaded map[string]engine.LoadedModel
	reply  string
	now    func() time.Time
	inject map[Op][]error
	calls  map[Op]int
	last   *Sampling

	// fallbackFirst and fallbackIdle are the chat timeouts that apply when a call sets none, as an
	// adapter's configured timeouts do; zero means no limit. gaps are the delays before chunks.
	fallbackFirst, fallbackIdle time.Duration
	gaps                        map[int]time.Duration
}

// Option configures New.
type Option func(*Engine)

// WithModels replaces the default models.
func WithModels(specs ...ModelSpec) Option {
	return func(e *Engine) {
		e.models, e.order = map[string]ModelSpec{}, nil
		for _, s := range specs {
			e.add(s)
		}
	}
}

// WithReply sets the chat reply (streamed one word per chunk).
func WithReply(s string) Option { return func(e *Engine) { e.reply = s } }

// WithChatTimeouts sets the fake's own first-token and idle limits for a chat call that sets
// none in its ChatRequest, as an adapter's configured timeouts do. Zero means no limit. A call
// that sets FirstTokenTimeout or IdleTimeout uses its value instead (req_001 D-38, D-54).
func WithChatTimeouts(firstToken, idle time.Duration) Option {
	return func(e *Engine) { e.fallbackFirst, e.fallbackIdle = firstToken, idle }
}

// WithChunkGap makes the engine take d (real time) before it delivers chunk number index of every
// chat reply: 0 is the first chunk, 1 the second, and the last, Done chunk comes after one chunk
// per word. The wait ends early with the context's error when the call is cancelled. When d is
// longer than the call's limit (the first-token limit for chunk 0, else the idle limit; see
// WithChatTimeouts and ChatRequest), Recv fails with a timeout error once that limit has passed,
// as the Ollama adapter does.
func WithChunkGap(index int, d time.Duration) Option {
	return func(e *Engine) {
		if e.gaps == nil {
			e.gaps = map[int]time.Duration{}
		}
		e.gaps[index] = d
	}
}

// WithClock sets the clock used for load expiry.
func WithClock(now func() time.Time) Option { return func(e *Engine) { e.now = now } }

// New returns a fake with one chat model "fake-chat" and one embedding model
// "fake-embed" unless WithModels is given.
func New(opts ...Option) *Engine {
	e := &Engine{
		models: map[string]ModelSpec{}, loaded: map[string]engine.LoadedModel{},
		reply: DefaultReply, now: time.Now, inject: map[Op][]error{}, calls: map[Op]int{},
	}
	e.add(ModelSpec{Name: "fake-chat", ContextLength: 8192})
	e.add(ModelSpec{Name: "fake-embed", ContextLength: 2048, Embedding: true})
	for _, o := range opts {
		o(e)
	}
	return e
}

func (e *Engine) add(s ModelSpec) {
	if s.Dim == 0 {
		s.Dim = 8
	}
	if s.MemoryBytes == 0 {
		s.MemoryBytes = 1 << 30
	}
	if _, ok := e.models[s.Name]; !ok {
		e.order = append(e.order, s.Name)
	}
	e.models[s.Name] = s
}

// InjectError makes the next n calls of op fail with err (n <= 0 means 1).
// Injected errors are consumed in order across repeated calls.
func (e *Engine) InjectError(op Op, err error, n int) {
	if n <= 0 {
		n = 1
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := 0; i < n; i++ {
		e.inject[op] = append(e.inject[op], err)
	}
}

// Sampling is the optional sampling controls of a chat call; nil fields and an
// empty Stop were not set.
type Sampling struct {
	Temperature *float64
	TopP        *float64
	Seed        *int
	Stop        []string
}

// LastChatSampling returns the sampling controls of the last accepted Chat
// call (a copy), and false if there was none. Prompt content is not recorded.
func (e *Engine) LastChatSampling() (Sampling, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.last == nil {
		return Sampling{}, false
	}
	out := *e.last
	if out.Temperature != nil {
		v := *out.Temperature
		out.Temperature = &v
	}
	if out.TopP != nil {
		v := *out.TopP
		out.TopP = &v
	}
	if out.Seed != nil {
		v := *out.Seed
		out.Seed = &v
	}
	out.Stop = append([]string(nil), out.Stop...)
	return out, true
}

// Calls reports how many times op was called.
func (e *Engine) Calls(op Op) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls[op]
}

// enter counts the call and pops an injected error. Callers hold e.mu.
func (e *Engine) enter(op Op) error {
	e.calls[op]++
	if q := e.inject[op]; len(q) > 0 {
		e.inject[op] = q[1:]
		return q[0]
	}
	return nil
}

// CountTokens is the fake's tokenizer: one token per word.
func CountTokens(s string) int { return len(strings.Fields(s)) }

func (e *Engine) ListModels(ctx context.Context) ([]engine.Model, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.enter(OpListModels); err != nil {
		return nil, err
	}
	out := make([]engine.Model, 0, len(e.order))
	for _, n := range e.order {
		out = append(out, engine.Model{Name: n, SizeBytes: e.models[n].MemoryBytes, Digest: "sha256:" + n})
	}
	return out, nil
}

func (e *Engine) ModelInfo(ctx context.Context, name string) (engine.ModelInfo, error) {
	if err := ctx.Err(); err != nil {
		return engine.ModelInfo{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.enter(OpModelInfo); err != nil {
		return engine.ModelInfo{}, err
	}
	s, ok := e.models[name]
	if !ok {
		return engine.ModelInfo{}, notFound(name)
	}
	caps := []string{"completion"}
	if s.Embedding {
		caps = []string{engine.CapEmbedding}
	}
	return engine.ModelInfo{Name: name, Family: "fake", ParameterSize: "1B", Quantization: "Q4", ContextLength: s.ContextLength, Capabilities: caps}, nil
}

func (e *Engine) Load(ctx context.Context, name string, keepAlive time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.enter(OpLoad); err != nil {
		return err
	}
	s, ok := e.models[name]
	if !ok {
		return notFound(name)
	}
	e.load(s, keepAlive)
	return nil
}

// load marks s loaded. Callers hold e.mu.
func (e *Engine) load(s ModelSpec, keepAlive time.Duration) {
	m := engine.LoadedModel{Name: s.Name, SizeBytes: s.MemoryBytes, VRAMBytes: s.MemoryBytes, ContextLength: s.ContextLength}
	if keepAlive > 0 {
		m.ExpiresAt = e.now().Add(keepAlive)
	}
	e.loaded[s.Name] = m
}

func (e *Engine) Unload(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.enter(OpUnload); err != nil {
		return err
	}
	if _, ok := e.models[name]; !ok {
		return notFound(name)
	}
	delete(e.loaded, name)
	return nil
}

func (e *Engine) Loaded(ctx context.Context) ([]engine.LoadedModel, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.enter(OpLoaded); err != nil {
		return nil, err
	}
	var out []engine.LoadedModel
	for _, n := range e.order {
		if m, ok := e.loaded[n]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (e *Engine) Chat(ctx context.Context, req engine.ChatRequest) (engine.ChatStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.NumCtx <= 0 || req.NumPredict <= 0 || len(req.Messages) == 0 {
		return nil, engine.Errorf(engine.CodeInvalidRequest, nil, "chat needs messages, a positive num_ctx and a positive num_predict")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.enter(OpChat); err != nil {
		return nil, err
	}
	s, ok := e.models[req.Model]
	if !ok {
		return nil, notFound(req.Model)
	}
	msgs := req.Messages
	prompt := 0
	for _, m := range msgs {
		prompt += CountTokens(m.Content)
	}
	if prompt > req.NumCtx {
		if req.OnContextOverflow != engine.OverflowTruncate {
			return nil, engine.Errorf(engine.CodeContextTooLong, nil, "prompt exceeds num_ctx %d", req.NumCtx)
		}
		_, prompt = truncate(msgs, req.NumCtx)
	}
	e.load(s, req.KeepAlive)
	e.last = &Sampling{Temperature: copyPtr(req.Temperature), TopP: copyPtr(req.TopP),
		Seed: copyPtr(req.Seed), Stop: append([]string(nil), req.Stop...)}
	words := strings.Fields(e.reply)
	reason := "stop"
	if len(words) > req.NumPredict {
		words, reason = words[:req.NumPredict], "length"
	}
	st := &stream{ctx: ctx, words: words, prompt: prompt, reason: reason, gaps: e.gaps,
		first: positiveOr(req.FirstTokenTimeout, e.fallbackFirst), idle: positiveOr(req.IdleTimeout, e.fallbackIdle)}
	if q := e.inject[OpChatStream]; len(q) > 0 {
		st.fail, e.inject[OpChatStream] = q[0], q[1:]
	}
	e.calls[OpChatStream]++
	return st, nil
}

// positiveOr is call when it is positive, else fallback.
func positiveOr(call, fallback time.Duration) time.Duration {
	if call > 0 {
		return call
	}
	return fallback
}

func copyPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// truncate drops the oldest non-system messages until the prompt fits or only
// the last message is left, as Ollama does by default (req_001 D-48).
func truncate(msgs []engine.Message, numCtx int) ([]engine.Message, int) {
	msgs = append([]engine.Message(nil), msgs...)
	total := func() (n int) {
		for _, m := range msgs {
			n += CountTokens(m.Content)
		}
		return
	}
	for total() > numCtx && len(msgs) > 1 {
		drop := -1
		for i := 0; i < len(msgs)-1; i++ {
			if msgs[i].Role != engine.RoleSystem {
				drop = i
				break
			}
		}
		if drop < 0 {
			break
		}
		msgs = append(msgs[:drop], msgs[drop+1:]...)
	}
	return msgs, total()
}

type stream struct {
	ctx    context.Context
	words  []string
	prompt int
	reason string
	fail   error
	i      int
	done   bool
	closed bool

	gaps        map[int]time.Duration
	first, idle time.Duration // the limits for this call; zero means none
	sent        int           // chunks delivered, the Done chunk included
}

// wait takes the gap before the next chunk, if any, and fails it with a timeout error when the
// gap is longer than the limit that applies.
func (s *stream) wait() error {
	d := s.gaps[s.sent]
	if d <= 0 {
		return nil
	}
	limit := s.idle
	if s.sent == 0 {
		limit = s.first
	}
	timedOut := limit > 0 && d > limit
	if timedOut {
		d = limit
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
	if timedOut {
		s.fail = engine.Errorf(engine.CodeTimeout, nil, "the engine stopped answering in time")
		return s.fail
	}
	return nil
}

func (s *stream) Recv() (engine.ChatChunk, error) {
	if s.closed {
		return engine.ChatChunk{}, io.EOF
	}
	if err := s.ctx.Err(); err != nil {
		return engine.ChatChunk{}, err
	}
	if s.fail != nil {
		return engine.ChatChunk{}, s.fail
	}
	if s.done {
		return engine.ChatChunk{}, io.EOF
	}
	if err := s.wait(); err != nil {
		return engine.ChatChunk{}, err
	}
	s.sent++
	if s.i < len(s.words) {
		w := s.words[s.i]
		if s.i > 0 {
			w = " " + w
		}
		s.i++
		return engine.ChatChunk{Content: w}, nil
	}
	s.done = true
	return engine.ChatChunk{Done: true, DoneReason: s.reason, PromptTokens: s.prompt, CompletionTokens: len(s.words)}, nil
}

func (s *stream) Close() error { s.closed = true; return nil }

func (e *Engine) Embed(ctx context.Context, req engine.EmbedRequest) (engine.EmbedResult, error) {
	if err := ctx.Err(); err != nil {
		return engine.EmbedResult{}, err
	}
	if req.NumCtx <= 0 || len(req.Input) == 0 {
		return engine.EmbedResult{}, engine.Errorf(engine.CodeInvalidRequest, nil, "embed needs input and a positive num_ctx")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.enter(OpEmbed); err != nil {
		return engine.EmbedResult{}, err
	}
	s, ok := e.models[req.Model]
	if !ok {
		return engine.EmbedResult{}, notFound(req.Model)
	}
	var res engine.EmbedResult
	for _, in := range req.Input {
		n := CountTokens(in)
		if n > req.NumCtx {
			if req.OnContextOverflow != engine.OverflowTruncate {
				return engine.EmbedResult{}, engine.Errorf(engine.CodeContextTooLong, nil, "input exceeds num_ctx %d", req.NumCtx)
			}
			n = req.NumCtx
		}
		res.PromptTokens += n
		res.Embeddings = append(res.Embeddings, vector(in, s.Dim))
	}
	e.load(s, req.KeepAlive)
	return res, nil
}

// vector is a deterministic pseudo-embedding derived from the input hash.
func vector(in string, dim int) []float32 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(in))
	x := h.Sum64()
	v := make([]float32, dim)
	for i := range v {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		v[i] = float32(x%2000)/1000 - 1
	}
	return v
}

func notFound(name string) error {
	return engine.Errorf(engine.CodeModelNotFound, nil, "model %q is not installed", name)
}

var _ engine.Engine = (*Engine)(nil)
