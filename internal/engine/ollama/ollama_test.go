package ollama_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/engine/enginetest"
	"github.com/inCoderKaka/local-server/internal/engine/ollama"
)

// fakeOllama is an httptest server that behaves like the parts of Ollama the
// adapter uses. It counts one token per word and records request bodies.
type fakeOllama struct {
	mu      sync.Mutex
	version string
	loaded  map[string]bool
	bodies  map[string][]map[string]any // path -> decoded request bodies
	// behaviour switches
	chatDelay time.Duration // wait before the first chat line
	midError  string        // error line sent after the first chat chunk
	embedFail string        // embed answers 500 with this error text
	srv       *httptest.Server
}

func newFakeOllama(t *testing.T) *fakeOllama {
	f := &fakeOllama{version: "0.13.0", loaded: map[string]bool{}, bodies: map[string][]map[string]any{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOllama) last(path string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.bodies[path]
	if len(b) == 0 {
		return nil
	}
	return b[len(b)-1]
}

func words(s string) int { return len(strings.Fields(s)) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func installed(name string) bool {
	return name == enginetest.ChatModel || name == enginetest.EmbedModel
}

func (f *fakeOllama) handle(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	f.mu.Lock()
	f.bodies[r.URL.Path] = append(f.bodies[r.URL.Path], body)
	f.mu.Unlock()
	model, _ := body["model"].(string)
	notFound := func() { writeJSON(w, 404, map[string]string{"error": fmt.Sprintf("model '%s' not found", model)}) }
	switch r.URL.Path {
	case "/api/version":
		writeJSON(w, 200, map[string]string{"version": f.version})
	case "/api/tags":
		writeJSON(w, 200, map[string]any{"models": []map[string]any{
			{"name": enginetest.ChatModel, "size": 100, "digest": "d1", "modified_at": time.Now()},
			{"name": enginetest.EmbedModel, "size": 50, "digest": "d2", "modified_at": time.Now()},
		}})
	case "/api/show":
		if !installed(model) {
			notFound()
			return
		}
		caps := []string{"completion"}
		if model == enginetest.EmbedModel {
			caps = []string{"embedding"}
		}
		writeJSON(w, 200, map[string]any{
			"details":      map[string]any{"family": "llama", "parameter_size": "1B", "quantization_level": "Q4"},
			"model_info":   map[string]any{"general.architecture": "llama", "llama.context_length": 4096},
			"capabilities": caps,
		})
	case "/api/ps":
		f.mu.Lock()
		var ms []map[string]any
		for n, on := range f.loaded {
			if on {
				ms = append(ms, map[string]any{"name": n, "size": 1000, "size_vram": 900, "context_length": 4096, "expires_at": time.Now().Add(time.Minute)})
			}
		}
		f.mu.Unlock()
		writeJSON(w, 200, map[string]any{"models": ms})
	case "/api/generate", "/api/embed":
		if !installed(model) {
			notFound()
			return
		}
		if r.URL.Path == "/api/generate" && model == enginetest.EmbedModel {
			writeJSON(w, 400, map[string]string{"error": "\"" + model + "\" does not support generate"})
			return
		}
		if r.URL.Path == "/api/embed" && model != enginetest.EmbedModel {
			writeJSON(w, 400, map[string]string{"error": "this model does not support embeddings"})
			return
		}
		if r.URL.Path == "/api/embed" && f.embedFail != "" {
			writeJSON(w, 500, map[string]string{"error": f.embedFail})
			return
		}
		input, _ := body["input"].([]any)
		if r.URL.Path == "/api/embed" && len(input) > 0 {
			f.embed(w, body, input)
			return
		}
		ka, _ := body["keep_alive"].(float64)
		f.mu.Lock()
		f.loaded[model] = ka != 0
		f.mu.Unlock()
		writeJSON(w, 200, map[string]any{"model": model, "done": true})
	case "/api/chat":
		f.chat(w, r, body)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeOllama) embed(w http.ResponseWriter, body map[string]any, input []any) {
	numCtx := int(body["options"].(map[string]any)["num_ctx"].(float64))
	truncate := true
	if v, ok := body["truncate"].(bool); ok {
		truncate = v
	}
	total := 0
	var vecs [][]float32
	for _, in := range input {
		n := words(in.(string))
		if n > numCtx {
			if !truncate {
				writeJSON(w, 400, map[string]string{"error": "the input length exceeds the context length"})
				return
			}
			n = numCtx
		}
		total += n
		vecs = append(vecs, []float32{0.1, 0.2, 0.3})
	}
	f.mu.Lock()
	f.loaded[body["model"].(string)] = true
	f.mu.Unlock()
	writeJSON(w, 200, map[string]any{"embeddings": vecs, "prompt_eval_count": total})
}

func (f *fakeOllama) chat(w http.ResponseWriter, r *http.Request, body map[string]any) {
	model := body["model"].(string)
	if !installed(model) {
		writeJSON(w, 404, map[string]string{"error": fmt.Sprintf("model '%s' not found", model)})
		return
	}
	numCtx := int(body["options"].(map[string]any)["num_ctx"].(float64))
	msgs, _ := body["messages"].([]any)
	var contents []string
	for _, m := range msgs {
		contents = append(contents, m.(map[string]any)["content"].(string))
	}
	total := func() (n int) {
		for _, c := range contents {
			n += words(c)
		}
		return
	}
	if total() > numCtx {
		if v, ok := body["truncate"].(bool); ok && !v {
			writeJSON(w, 400, map[string]string{"error": "prompt too long; exceeded context length"})
			return
		}
		for total() > numCtx && len(contents) > 1 {
			contents = contents[1:]
		}
	}
	f.mu.Lock()
	f.loaded[model] = true
	f.mu.Unlock()
	if f.chatDelay > 0 {
		select {
		case <-time.After(f.chatDelay):
		case <-r.Context().Done():
			return
		}
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	fl := w.(http.Flusher)
	line := func(v any) { _ = json.NewEncoder(w).Encode(v); fl.Flush() }
	reply := []string{"hello", " from", " fake", " ollama"}
	for i, tok := range reply {
		line(map[string]any{"model": model, "message": map[string]string{"role": "assistant", "content": tok}, "done": false})
		if i == 0 && f.midError != "" {
			line(map[string]any{"error": f.midError})
			return
		}
	}
	line(map[string]any{"model": model, "message": map[string]string{"role": "assistant", "content": ""}, "done": true,
		"done_reason": "stop", "prompt_eval_count": total(), "eval_count": len(reply)})
}

func newAdapter(f *fakeOllama, mod func(*ollama.Config)) *ollama.Adapter {
	cfg := ollama.Config{BaseURL: f.srv.URL}
	if mod != nil {
		mod(&cfg)
	}
	return ollama.New(cfg)
}

func TestContract(t *testing.T) {
	enginetest.Run(t, func(t *testing.T) engine.Engine {
		a := newAdapter(newFakeOllama(t), nil)
		t.Cleanup(a.Close)
		return a
	})
}

func TestContractUnavailable(t *testing.T) {
	enginetest.RunUnavailable(t, func(t *testing.T) engine.Engine {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close() // nothing listens any more
		return ollama.New(ollama.Config{BaseURL: url})
	})
}

func chatRequest(policy engine.OverflowPolicy) engine.ChatRequest {
	return engine.ChatRequest{Model: enginetest.ChatModel, Messages: []engine.Message{{Role: engine.RoleUser, Content: "hi there"}},
		NumCtx: 2048, NumPredict: 100, KeepAlive: 90 * time.Second, OnContextOverflow: policy}
}

func TestChatRequestBodyRefuseVersusTruncate(t *testing.T) {
	f := newFakeOllama(t)
	a := newAdapter(f, nil)
	defer a.Close()
	run := func(p engine.OverflowPolicy) map[string]any {
		s, err := a.Chat(context.Background(), chatRequest(p))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, readAll{s})
		s.Close()
		return f.last("/api/chat")
	}
	for _, p := range []engine.OverflowPolicy{"", engine.OverflowRefuse} {
		b := run(p)
		if b["truncate"] != false || b["shift"] != false {
			t.Fatalf("policy %q: body = %v, want truncate:false and shift:false", p, b)
		}
		opts := b["options"].(map[string]any)
		if opts["num_ctx"] != float64(2048) || opts["num_predict"] != float64(100) || b["keep_alive"] != float64(90) || b["stream"] != true {
			t.Fatalf("explicit options missing: %v", b)
		}
	}
	b := run(engine.OverflowTruncate)
	if _, ok := b["truncate"]; ok {
		t.Fatalf("truncate policy must leave truncate unset, body = %v", b)
	}
	if _, ok := b["shift"]; ok {
		t.Fatalf("truncate policy must leave shift unset, body = %v", b)
	}
}

type readAll struct{ s engine.ChatStream }

func (r readAll) Read([]byte) (int, error) {
	for {
		c, err := r.s.Recv()
		if err != nil || c.Done {
			return 0, io.EOF
		}
	}
}

func TestEmbedRequestBodyRefuseVersusTruncate(t *testing.T) {
	f := newFakeOllama(t)
	a := newAdapter(f, nil)
	defer a.Close()
	req := engine.EmbedRequest{Model: enginetest.EmbedModel, Input: []string{"a b"}, NumCtx: 128}
	if _, err := a.Embed(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	b := f.last("/api/embed")
	if b["truncate"] != false || b["options"].(map[string]any)["num_ctx"] != float64(128) || b["keep_alive"] != float64(300) {
		t.Fatalf("refuse body = %v, want truncate:false, num_ctx 128, default keep_alive 300", b)
	}
	req.OnContextOverflow = engine.OverflowTruncate
	if _, err := a.Embed(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.last("/api/embed")["truncate"]; ok {
		t.Fatalf("truncate policy must leave truncate unset")
	}
}

func TestEmbedFailureNearLimitIsTyped(t *testing.T) {
	f := newFakeOllama(t)
	f.embedFail = "failed to decode batch: could not find a kv cache slot"
	a := newAdapter(f, nil)
	defer a.Close()
	_, err := a.Embed(context.Background(), engine.EmbedRequest{Model: enginetest.EmbedModel, Input: []string{"a"},
		NumCtx: 64, OnContextOverflow: engine.OverflowTruncate})
	if !errors.Is(err, engine.ErrContextTooLong) {
		t.Fatalf("err = %v, want context_too_long", err)
	}
	f.embedFail = "something unrelated broke"
	_, err = a.Embed(context.Background(), engine.EmbedRequest{Model: enginetest.EmbedModel, Input: []string{"a"}, NumCtx: 64})
	if !errors.Is(err, engine.ErrEngineError) {
		t.Fatalf("err = %v, want engine_error", err)
	}
}

func TestVersionCheck(t *testing.T) {
	cases := []struct {
		server, min string
		wantErr     bool
	}{
		{"0.13.0", "", false},
		{"0.12.6", "", false},
		{"0.12.5", "", true},
		{"0.9.9", "", true},
		{"1.0.0", "", false},
		{"v0.12.6-rc1", "", false},
		{"0.12.6", "0.13.0", true}, // the minimum is a Config value
		{"garbage", "", true},
	}
	for _, c := range cases {
		f := newFakeOllama(t)
		f.version = c.server
		a := newAdapter(f, func(cfg *ollama.Config) { cfg.MinVersion = c.min })
		err := a.CheckVersion(context.Background())
		if (err != nil) != c.wantErr {
			t.Fatalf("server %q min %q: err = %v, wantErr %v", c.server, c.min, err, c.wantErr)
		}
		if err != nil && !errors.Is(err, engine.ErrUnsupportedVersion) {
			t.Fatalf("server %q: err = %v, want unsupported_engine_version", c.server, err)
		}
	}
}

func TestLoadAndUnloadSendExplicitSettings(t *testing.T) {
	f := newFakeOllama(t)
	a := newAdapter(f, func(c *ollama.Config) { c.LoadNumCtx = 1234 })
	defer a.Close()
	if err := a.Load(context.Background(), enginetest.ChatModel, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	b := f.last("/api/generate")
	if b["keep_alive"] != float64(120) || b["options"].(map[string]any)["num_ctx"] != float64(1234) {
		t.Fatalf("load body = %v", b)
	}
	if err := a.Unload(context.Background(), enginetest.ChatModel); err != nil {
		t.Fatal(err)
	}
	if f.last("/api/generate")["keep_alive"] != float64(0) {
		t.Fatalf("unload must send keep_alive 0")
	}
	// An embedding-only model rejects generate; the adapter falls back to embed.
	if err := a.Load(context.Background(), enginetest.EmbedModel, time.Minute); err != nil {
		t.Fatalf("embed model load: %v", err)
	}
	ls, _ := a.Loaded(context.Background())
	if len(ls) != 1 || ls[0].Name != enginetest.EmbedModel || ls[0].ContextLength != 4096 || ls[0].VRAMBytes != 900 {
		t.Fatalf("loaded = %+v", ls)
	}
}

func TestStreamErrorMidway(t *testing.T) {
	f := newFakeOllama(t)
	f.midError = "prompt too long; exceeded context length"
	a := newAdapter(f, nil)
	defer a.Close()
	s, err := a.Chat(context.Background(), chatRequest(""))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Recv(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recv(); !errors.Is(err, engine.ErrContextTooLong) {
		t.Fatalf("err = %v, want context_too_long", err)
	}
}

func TestFirstTokenTimeout(t *testing.T) {
	f := newFakeOllama(t)
	f.chatDelay = 2 * time.Second
	a := newAdapter(f, func(c *ollama.Config) { c.FirstTokenTimeout = 50 * time.Millisecond })
	defer a.Close()
	s, err := a.Chat(context.Background(), chatRequest(""))
	if err == nil {
		defer s.Close()
		_, err = s.Recv()
	}
	if !errors.Is(err, engine.ErrTimeout) {
		t.Fatalf("err = %v, want engine_timeout", err)
	}
}

func TestRequestTimeoutAndCallerCancel(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	a := ollama.New(ollama.Config{BaseURL: slow.URL, RequestTimeout: 50 * time.Millisecond})
	defer a.Close()
	if _, err := a.ListModels(context.Background()); !errors.Is(err, engine.ErrTimeout) {
		t.Fatalf("err = %v, want engine_timeout", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.ListModels(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestErrorsCarryNoContent(t *testing.T) {
	f := newFakeOllama(t)
	a := newAdapter(f, nil)
	defer a.Close()
	secret := strings.Repeat("topsecret ", 200)
	req := chatRequest("")
	req.NumCtx = 8
	req.Messages = []engine.Message{{Role: engine.RoleUser, Content: secret}}
	_, err := a.Chat(context.Background(), req)
	if !errors.Is(err, engine.ErrContextTooLong) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("error leaks prompt text: %v", err)
	}
}
