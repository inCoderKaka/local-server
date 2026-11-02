package ollama

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine"
)

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Chat streams /api/chat with an explicit num_ctx, num_predict and keep_alive.
// With the refuse policy (the default) it sends truncate:false and
// shift:false so an oversized prompt fails instead of being cut (req_001 D-48).
// The first-token and idle timers take ChatRequest.FirstTokenTimeout and
// IdleTimeout when they are set, else Config.FirstTokenTimeout and
// Config.StreamIdleTimeout.
func (a *Adapter) Chat(ctx context.Context, req engine.ChatRequest) (engine.ChatStream, error) {
	if req.Model == "" || len(req.Messages) == 0 || req.NumCtx <= 0 || req.NumPredict <= 0 {
		return nil, engine.Errorf(engine.CodeInvalidRequest, nil, "chat needs a model, messages, a positive num_ctx and a positive num_predict")
	}
	msgs := make([]chatMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = chatMessage{Role: string(m.Role), Content: m.Content}
	}
	body := struct {
		Model     string        `json:"model"`
		Messages  []chatMessage `json:"messages"`
		Stream    bool          `json:"stream"`
		Truncate  *bool         `json:"truncate,omitempty"`
		Shift     *bool         `json:"shift,omitempty"`
		KeepAlive int64         `json:"keep_alive"`
		Options   options       `json:"options"`
	}{Model: req.Model, Messages: msgs, Stream: true, KeepAlive: a.keepAliveSecs(req.KeepAlive),
		Options: options{NumCtx: req.NumCtx, NumPredict: req.NumPredict,
			Temperature: req.Temperature, TopP: req.TopP, Seed: req.Seed, Stop: req.Stop}}
	body.Truncate = truncateFlag(req.OnContextOverflow)
	body.Shift = body.Truncate

	sctx, cancel := context.WithCancel(ctx)
	s := &chatStream{parent: ctx, cancel: cancel, idle: callOr(req.IdleTimeout, a.cfg.StreamIdleTimeout)}
	s.timer = time.AfterFunc(callOr(req.FirstTokenTimeout, a.cfg.FirstTokenTimeout), s.expire)
	resp, err := a.send(sctx, "POST", "/api/chat", body)
	if err != nil {
		s.stop()
		var ee *engine.Error
		if errors.As(err, &ee) {
			return nil, err
		}
		return nil, s.wrap(err)
	}
	s.body = resp.Body
	s.sc = bufio.NewScanner(resp.Body)
	s.sc.Buffer(make([]byte, 0, 64<<10), a.cfg.MaxStreamLineBytes)
	return s, nil
}

// callOr is the per-call timeout when the caller set one (positive), else the
// adapter's configured fallback (req_001 D-38, D-54: the caller's host setting
// wins over the adapter's default).
func callOr(call, fallback time.Duration) time.Duration {
	if call > 0 {
		return call
	}
	return fallback
}

type chatStream struct {
	parent  context.Context
	cancel  context.CancelFunc
	timer   *time.Timer
	idle    time.Duration
	expired atomic.Bool
	body    io.ReadCloser
	sc      *bufio.Scanner

	once sync.Once
	done bool
}

func (s *chatStream) expire() { s.expired.Store(true); s.cancel() }

func (s *chatStream) stop() { s.timer.Stop(); s.cancel() }

// wrap turns a read or transport failure into a typed error.
func (s *chatStream) wrap(err error) error {
	if perr := s.parent.Err(); perr != nil {
		return perr
	}
	if s.expired.Load() {
		return engine.Errorf(engine.CodeTimeout, err, "the engine stopped answering in time")
	}
	return engine.Errorf(engine.CodeEngineUnavailable, err, "the engine cannot be reached")
}

func (s *chatStream) Recv() (engine.ChatChunk, error) {
	for {
		if s.done {
			return engine.ChatChunk{}, io.EOF
		}
		if s.body == nil { // closed
			return engine.ChatChunk{}, io.EOF
		}
		if err := s.parent.Err(); err != nil {
			return engine.ChatChunk{}, err
		}
		if !s.sc.Scan() {
			err := s.sc.Err()
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			if errors.Is(err, bufio.ErrTooLong) {
				return engine.ChatChunk{}, engine.Errorf(engine.CodeEngineError, err, "the engine sent an oversized line")
			}
			return engine.ChatChunk{}, s.wrap(err)
		}
		line := s.sc.Bytes()
		if len(line) == 0 {
			continue
		}
		s.timer.Reset(s.idle)
		var m struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done            bool   `json:"done"`
			DoneReason      string `json:"done_reason"`
			PromptEvalCount int    `json:"prompt_eval_count"`
			EvalCount       int    `json:"eval_count"`
			Error           string `json:"error"`
		}
		if err := json.Unmarshal(line, &m); err != nil {
			return engine.ChatChunk{}, engine.Errorf(engine.CodeEngineError, nil, "the engine sent an unreadable line")
		}
		if m.Error != "" {
			return engine.ChatChunk{}, classify(http.StatusInternalServerError, m.Error)
		}
		if m.Done {
			s.done = true
			s.stop()
			return engine.ChatChunk{Content: m.Message.Content, Done: true, DoneReason: m.DoneReason,
				PromptTokens: m.PromptEvalCount, CompletionTokens: m.EvalCount}, nil
		}
		if m.Message.Content == "" {
			continue // e.g. a thinking-only chunk
		}
		return engine.ChatChunk{Content: m.Message.Content}, nil
	}
}

func (s *chatStream) Close() error {
	s.once.Do(func() {
		s.stop()
		if s.body != nil {
			_ = s.body.Close()
			s.body = nil
		}
	})
	return nil
}
