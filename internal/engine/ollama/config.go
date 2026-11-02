// Package ollama is the Ollama adapter of the engine port (req_001 D-22,
// D-48). It speaks Ollama's native API: /api/chat, /api/embed, /api/tags,
// /api/show, /api/ps, /api/generate (load and unload) and /api/version. Only
// cmd/ imports it. It uses the standard library only and never logs or stores
// prompts or responses (req_001 D-31, D-40).
package ollama

import "time"

// DefaultMinVersion is the oldest Ollama release the adapter accepts. Chat
// `truncate` and `shift` arrived in 0.12.6 per Nojoin PR #343 (req_001 D-48
// lists that release number as unsourced); the host can override it in Config.
const DefaultMinVersion = "0.12.6"

// Defaults for the zero fields of Config. They are documented defaults, not
// logic constants: the caller (the runtime settings, req_001 D-38) owns every
// value and passes it in Config.
const (
	DefaultBaseURL            = "http://127.0.0.1:11434"
	DefaultKeepAlive          = 5 * time.Minute
	DefaultLoadNumCtx         = 4096
	DefaultConnectTimeout     = 5 * time.Second
	DefaultRequestTimeout     = 30 * time.Second
	DefaultEmbedTimeout       = 2 * time.Minute
	DefaultLoadTimeout        = 5 * time.Minute
	DefaultFirstTokenTimeout  = 2 * time.Minute
	DefaultStreamIdleTimeout  = 30 * time.Second
	DefaultMaxResponseBytes   = 64 << 20
	DefaultMaxStreamLineBytes = 4 << 20
)

// Config holds every timeout and limit of the adapter. A zero or negative
// field takes its documented default. The total time of a chat is bounded by
// the caller's context deadline (req_001 D-39 step 6).
type Config struct {
	// BaseURL of the Ollama server. Default DefaultBaseURL.
	BaseURL string
	// MinVersion is the minimum supported Ollama version, checked by
	// CheckVersion. Default DefaultMinVersion.
	MinVersion string
	// DefaultKeepAlive applies when a request's KeepAlive is zero.
	DefaultKeepAlive time.Duration
	// LoadNumCtx is the explicit num_ctx sent when Load preloads a model, so
	// Ollama's own default is never relied on (req_001 D-22).
	LoadNumCtx int
	// ConnectTimeout bounds dialing the server.
	ConnectTimeout time.Duration
	// RequestTimeout bounds ListModels, ModelInfo, Loaded, Unload and the version read.
	RequestTimeout time.Duration
	// EmbedTimeout bounds one Embed call (it may load the model).
	EmbedTimeout time.Duration
	// LoadTimeout bounds Load.
	LoadTimeout time.Duration
	// FirstTokenTimeout bounds the wait for the first chat chunk (includes model
	// load). It is the fallback for a chat call that sets no
	// ChatRequest.FirstTokenTimeout.
	FirstTokenTimeout time.Duration
	// StreamIdleTimeout bounds the gap between chat chunks. It is the fallback
	// for a chat call that sets no ChatRequest.IdleTimeout.
	StreamIdleTimeout time.Duration
	// MaxResponseBytes bounds a non-streaming response body.
	MaxResponseBytes int64
	// MaxStreamLineBytes bounds one streamed JSON line.
	MaxStreamLineBytes int
}

func (c Config) withDefaults() Config {
	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	if c.MinVersion == "" {
		c.MinVersion = DefaultMinVersion
	}
	d := func(v *time.Duration, def time.Duration) {
		if *v <= 0 {
			*v = def
		}
	}
	d(&c.DefaultKeepAlive, DefaultKeepAlive)
	d(&c.ConnectTimeout, DefaultConnectTimeout)
	d(&c.RequestTimeout, DefaultRequestTimeout)
	d(&c.EmbedTimeout, DefaultEmbedTimeout)
	d(&c.LoadTimeout, DefaultLoadTimeout)
	d(&c.FirstTokenTimeout, DefaultFirstTokenTimeout)
	d(&c.StreamIdleTimeout, DefaultStreamIdleTimeout)
	if c.LoadNumCtx <= 0 {
		c.LoadNumCtx = DefaultLoadNumCtx
	}
	if c.MaxResponseBytes <= 0 {
		c.MaxResponseBytes = DefaultMaxResponseBytes
	}
	if c.MaxStreamLineBytes <= 0 {
		c.MaxStreamLineBytes = DefaultMaxStreamLineBytes
	}
	return c
}
