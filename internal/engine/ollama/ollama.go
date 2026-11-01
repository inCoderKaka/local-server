package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine"
)

// Adapter implements engine.Engine on Ollama's native API.
type Adapter struct {
	cfg  Config
	http *http.Client
	base string
}

var _ engine.Engine = (*Adapter)(nil)

// New returns an adapter. It does not contact the server; call CheckVersion.
func New(cfg Config) *Adapter {
	cfg = cfg.withDefaults()
	tr := &http.Transport{
		Proxy:               nil, // the engine is local; never route it through a proxy
		DialContext:         (&net.Dialer{Timeout: cfg.ConnectTimeout}).DialContext,
		MaxIdleConnsPerHost: 4,
	}
	return &Adapter{cfg: cfg, http: &http.Client{Transport: tr}, base: strings.TrimRight(cfg.BaseURL, "/")}
}

// Close drops idle connections.
func (a *Adapter) Close() { a.http.CloseIdleConnections() }

// timeoutOrCtx turns a failed call into the right typed error. parent is the
// caller's context, limited the derived one that carries our timeout.
func timeoutOrCtx(parent, limited context.Context, err error) error {
	if perr := parent.Err(); perr != nil {
		return perr
	}
	if limited.Err() != nil {
		return engine.Errorf(engine.CodeTimeout, err, "the engine did not answer in time")
	}
	return engine.Errorf(engine.CodeEngineUnavailable, err, "the engine cannot be reached")
}

// send performs one request and returns the response for a 2xx status. A
// non-2xx status is mapped to a typed error (the body text is never kept).
func (a *Adapter) send(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, engine.Errorf(engine.CodeInvalidRequest, err, "request cannot be encoded")
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, rd)
	if err != nil {
		return nil, engine.Errorf(engine.CodeInvalidRequest, err, "bad engine address")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		return nil, mapStatus(resp)
	}
	return resp, nil
}

// do runs a non-streaming call bounded by timeout and decodes the JSON reply.
func (a *Adapter) do(ctx context.Context, timeout time.Duration, method, path string, body, out any) error {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := a.send(cctx, method, path, body)
	if err != nil {
		var ee *engine.Error
		if errors.As(err, &ee) {
			return err
		}
		return timeoutOrCtx(ctx, cctx, err)
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, a.cfg.MaxResponseBytes))
		return nil
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, a.cfg.MaxResponseBytes))
	if err := dec.Decode(out); err != nil {
		if cerr := cctx.Err(); cerr != nil {
			return timeoutOrCtx(ctx, cctx, err)
		}
		return engine.Errorf(engine.CodeEngineError, err, "the engine sent an unreadable reply")
	}
	return nil
}

// mapStatus maps an error response to a typed error. Only the status and the
// recognised kind are kept: the body text is dropped so no content can leak.
func mapStatus(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	return classify(resp.StatusCode, e.Error)
}

// classify maps an Ollama failure (HTTP status plus its error text; status 0
// for an error inside a stream) to a typed error.
func classify(status int, msg string) error {
	low := strings.ToLower(msg)
	switch {
	case isOverflow(low):
		return engine.Errorf(engine.CodeContextTooLong, nil, "the input is longer than the context window")
	case status == http.StatusNotFound || (strings.Contains(low, "model") && strings.Contains(low, "not found")):
		return engine.Errorf(engine.CodeModelNotFound, nil, "the model is not installed")
	case status == http.StatusBadRequest:
		return engine.Errorf(engine.CodeInvalidRequest, nil, "the engine rejected the request (HTTP %d)", status)
	case status == http.StatusServiceUnavailable || status == http.StatusBadGateway || status == http.StatusGatewayTimeout:
		return engine.Errorf(engine.CodeEngineUnavailable, nil, "the engine is unavailable (HTTP %d)", status)
	}
	return engine.Errorf(engine.CodeEngineError, nil, "the engine failed (HTTP %d)", status)
}

// overflowMarkers are lower-case fragments of Ollama's oversize errors. The
// chat and embed texts are not in Ollama's published docs; the contract test
// pins them against a real server (req_001 D-48). Embed can also fail near
// the limit even when truncating (Ollama issue #14186), with errors about the
// KV cache or batch, so those count as overflow too.
var overflowMarkers = []string{
	"exceeds the context", "exceeds maximum context", "exceeded context", "exceeds context",
	"context length", "context window", "prompt too long", "input length", "input too long",
	"too long", "kv cache", "decode batch",
}

func isOverflow(low string) bool {
	for _, m := range overflowMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

func (a *Adapter) keepAliveSecs(d time.Duration) int64 {
	switch {
	case d == 0:
		d = a.cfg.DefaultKeepAlive
	case d < 0:
		return -1
	}
	if s := int64((d + time.Second - 1) / time.Second); s > 0 {
		return s
	}
	return 1
}

type options struct {
	NumCtx     int `json:"num_ctx"`
	NumPredict int `json:"num_predict,omitempty"`
	// Sampling controls are sent only when the caller set them.
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	Seed        *int     `json:"seed,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

// ListModels reads /api/tags.
func (a *Adapter) ListModels(ctx context.Context) ([]engine.Model, error) {
	var out struct {
		Models []struct {
			Name       string    `json:"name"`
			Size       int64     `json:"size"`
			Digest     string    `json:"digest"`
			ModifiedAt time.Time `json:"modified_at"`
		} `json:"models"`
	}
	if err := a.do(ctx, a.cfg.RequestTimeout, "GET", "/api/tags", nil, &out); err != nil {
		return nil, err
	}
	ms := make([]engine.Model, 0, len(out.Models))
	for _, m := range out.Models {
		ms = append(ms, engine.Model{Name: m.Name, SizeBytes: m.Size, Digest: m.Digest, ModifiedAt: m.ModifiedAt})
	}
	return ms, nil
}

// ModelInfo reads /api/show.
func (a *Adapter) ModelInfo(ctx context.Context, name string) (engine.ModelInfo, error) {
	var out struct {
		Details struct {
			Family        string `json:"family"`
			ParameterSize string `json:"parameter_size"`
			Quantization  string `json:"quantization_level"`
		} `json:"details"`
		ModelInfo    map[string]any `json:"model_info"`
		Capabilities []string       `json:"capabilities"`
	}
	if err := a.do(ctx, a.cfg.RequestTimeout, "POST", "/api/show", map[string]any{"model": name}, &out); err != nil {
		return engine.ModelInfo{}, err
	}
	info := engine.ModelInfo{Name: name, Family: out.Details.Family, ParameterSize: out.Details.ParameterSize,
		Quantization: out.Details.Quantization, Capabilities: out.Capabilities}
	for k, v := range out.ModelInfo {
		if strings.HasSuffix(k, ".context_length") {
			if f, ok := v.(float64); ok && f > 0 {
				info.ContextLength = int(f)
			}
		}
	}
	return info, nil
}

// Loaded reads /api/ps.
func (a *Adapter) Loaded(ctx context.Context) ([]engine.LoadedModel, error) {
	var out struct {
		Models []struct {
			Name          string    `json:"name"`
			Size          int64     `json:"size"`
			SizeVRAM      int64     `json:"size_vram"`
			ContextLength int       `json:"context_length"`
			ExpiresAt     time.Time `json:"expires_at"`
		} `json:"models"`
	}
	if err := a.do(ctx, a.cfg.RequestTimeout, "GET", "/api/ps", nil, &out); err != nil {
		return nil, err
	}
	ls := make([]engine.LoadedModel, 0, len(out.Models))
	for _, m := range out.Models {
		ls = append(ls, engine.LoadedModel{Name: m.Name, SizeBytes: m.Size, VRAMBytes: m.SizeVRAM, ContextLength: m.ContextLength, ExpiresAt: m.ExpiresAt})
	}
	return ls, nil
}

// Load preloads a model: an /api/generate call without a prompt, with an
// explicit num_ctx (Config.LoadNumCtx) and keep_alive. Embedding-only models
// reject generate, so they are loaded through an empty /api/embed instead.
func (a *Adapter) Load(ctx context.Context, name string, keepAlive time.Duration) error {
	return a.setResident(ctx, a.cfg.LoadTimeout, name, a.keepAliveSecs(keepAlive))
}

// Unload evicts a model (keep_alive 0).
func (a *Adapter) Unload(ctx context.Context, name string) error {
	return a.setResident(ctx, a.cfg.RequestTimeout, name, 0)
}

func (a *Adapter) setResident(ctx context.Context, timeout time.Duration, name string, secs int64) error {
	opts := options{NumCtx: a.cfg.LoadNumCtx}
	err := a.do(ctx, timeout, "POST", "/api/generate",
		map[string]any{"model": name, "keep_alive": secs, "stream": false, "options": opts}, nil)
	if engine.CodeOf(err) == engine.CodeInvalidRequest {
		// The model cannot generate (embedding-only): use the embed endpoint.
		err = a.do(ctx, timeout, "POST", "/api/embed",
			map[string]any{"model": name, "input": []string{}, "keep_alive": secs, "options": opts}, nil)
	}
	return err
}

// Embed calls /api/embed. With the refuse policy it sends truncate:false.
func (a *Adapter) Embed(ctx context.Context, req engine.EmbedRequest) (engine.EmbedResult, error) {
	if req.Model == "" || len(req.Input) == 0 || req.NumCtx <= 0 {
		return engine.EmbedResult{}, engine.Errorf(engine.CodeInvalidRequest, nil, "embed needs a model, input and a positive num_ctx")
	}
	body := struct {
		Model     string   `json:"model"`
		Input     []string `json:"input"`
		Truncate  *bool    `json:"truncate,omitempty"`
		KeepAlive int64    `json:"keep_alive"`
		Options   options  `json:"options"`
	}{Model: req.Model, Input: req.Input, KeepAlive: a.keepAliveSecs(req.KeepAlive), Options: options{NumCtx: req.NumCtx}}
	body.Truncate = truncateFlag(req.OnContextOverflow)
	var out struct {
		Embeddings      [][]float32 `json:"embeddings"`
		PromptEvalCount int         `json:"prompt_eval_count"`
	}
	if err := a.do(ctx, a.cfg.EmbedTimeout, "POST", "/api/embed", body, &out); err != nil {
		return engine.EmbedResult{}, err
	}
	if len(out.Embeddings) != len(req.Input) {
		return engine.EmbedResult{}, engine.Errorf(engine.CodeEngineError, nil, "the engine returned %d vectors for %d inputs", len(out.Embeddings), len(req.Input))
	}
	return engine.EmbedResult{Embeddings: out.Embeddings, PromptTokens: out.PromptEvalCount}, nil
}

// truncateFlag is the `truncate` field: false for refuse (the default), left
// out for truncate so Ollama's default behaviour applies (req_001 D-48).
func truncateFlag(p engine.OverflowPolicy) *bool {
	if p == engine.OverflowTruncate {
		return nil
	}
	f := false
	return &f
}
