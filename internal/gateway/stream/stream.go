package stream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/ledger"
)

// Result is what a finished call tells Options.Finish to meter.
type Result struct {
	// Outcome is ledger.OutcomeCompleted, OutcomeFailed or OutcomeAborted.
	Outcome string
	// PromptTokens is the engine's exact count on a completed call, else 0: an estimate is
	// never billed (req_001 D-38).
	PromptTokens int64
	// CompletionTokens is the engine's count on a completed call, else the chunks with text
	// that arrived, one token each on Ollama's stream.
	CompletionTokens int64
	// FinishReason is "stop" or "length" on a completed call.
	FinishReason string
}

// Options configures Open.
type Options struct {
	// ID is the completion id, the same on every chunk; Model and Created fill the chunks.
	ID      string
	Model   string
	Created int64
	// IncludeUsage is stream_options.include_usage.
	IncludeUsage bool
	// FirstToken, Idle and Total are the three timeouts (gateway.stream_*); 0 means none.
	FirstToken, Idle, Total time.Duration
	// Release frees the scheduler slot. It runs once: when the engine call ends, or when the total
	// timeout expires, whichever comes first, so a client that stops reading cannot hold the slot.
	Release func()
	// Finish meters the call and returns its signed receipt. It runs once, after the engine call
	// ended. For an aborted call the receipt is dropped. An error means no receipt could be
	// signed (req_001 D-16, D-51); the stream then ends with an error event and no receipt.
	Finish func(Result) (receipt string, err error)
	// ErrorEvent returns the JSON of the error envelope for err, an error that ended the stream.
	ErrorEvent func(err error) []byte
}

type state int

const (
	stRole state = iota
	stBody
	stFinish
	stEnd
)

// Stream is the SSE body of a streamed chat completion. Read it to its end, then Close it.
type Stream struct {
	o      Options
	parent context.Context
	ectx   context.Context // the engine call's context
	cancel context.CancelCauseFunc
	src    engine.ChatStream
	total  *time.Timer
	idle   *time.Timer

	pending *engine.ChatChunk // the first chunk, read by Open
	last    engine.ChatChunk
	chunks  int64
	state   state
	buf     []byte
	err     error // returned by Read once buf is drained

	ended, finalized, closed bool
	release                  sync.Once
}

// Open starts the engine call with open, under a context that the timeouts and the client's
// disconnect cancel, and waits for the first chunk. It returns the engine's error, a
// *TimeoutError, or the context's error if the client went away. After an error nothing is left
// to clean up: the slot is released and the engine call cancelled.
func Open(ctx context.Context, o Options, open func(context.Context) (engine.ChatStream, error)) (*Stream, error) {
	ectx, cancel := context.WithCancelCause(ctx)
	s := &Stream{o: o, parent: ctx, ectx: ectx, cancel: cancel}
	if o.Total > 0 {
		s.total = time.AfterFunc(o.Total, func() {
			cancel(&TimeoutError{Kind: TimeoutTotal})
			s.releaseSlot()
		})
	}
	if o.Idle > 0 {
		s.idle = time.AfterFunc(o.Idle, func() { cancel(&TimeoutError{Kind: TimeoutIdle}) })
		s.idle.Stop()
	}
	var first *time.Timer
	if o.FirstToken > 0 {
		first = time.AfterFunc(o.FirstToken, func() { cancel(&TimeoutError{Kind: TimeoutFirstToken}) })
	}
	src, err := open(ectx)
	var ch engine.ChatChunk
	if err == nil {
		s.src = src
		ch, err = src.Recv()
		if errors.Is(err, io.EOF) {
			err = engine.Errorf(engine.CodeEngineError, nil, "the stream ended without a final chunk")
		}
	}
	if first != nil && !first.Stop() && err == nil {
		err = &TimeoutError{Kind: TimeoutFirstToken} // fired as the first chunk arrived; ectx is already cancelled
	}
	if err != nil {
		err = s.reason(err)
		s.endEngine()
		return nil, err
	}
	s.pending = &ch
	return s, nil
}

// reason names why a call failed: the client's disconnect, a timeout, or err itself.
func (s *Stream) reason(err error) error {
	if e := s.parent.Err(); e != nil {
		return e
	}
	var te *TimeoutError
	if errors.As(context.Cause(s.ectx), &te) {
		return te
	}
	return err
}

// endEngine stops the timers, cancels and closes the engine call and releases the slot, once.
func (s *Stream) endEngine() {
	if s.ended {
		return
	}
	s.ended = true
	if s.total != nil {
		s.total.Stop()
	}
	if s.idle != nil {
		s.idle.Stop()
	}
	s.cancel(context.Canceled)
	if s.src != nil {
		_ = s.src.Close()
	}
	s.releaseSlot()
}

// releaseSlot runs Options.Release, once. The total timer calls it too, from its own goroutine.
func (s *Stream) releaseSlot() {
	if s.o.Release != nil {
		s.release.Do(s.o.Release)
	}
}

// Read implements io.Reader. One call returns at most one logical event group, so a caller
// that flushes after each Read (NewFlushWriter) delivers every chunk as it arrives.
func (s *Stream) Read(p []byte) (int, error) {
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	for len(s.buf) == 0 {
		if s.err != nil {
			return 0, s.err
		}
		if s.state == stEnd {
			return 0, io.EOF
		}
		s.advance()
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

// Close ends the engine call. If the stream was not read to its end the client has gone: the
// call is metered as aborted with the tokens so far (req_001 D-38).
func (s *Stream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	s.endEngine()
	if !s.finalized {
		s.finalized = true
		s.finish(Result{Outcome: ledger.OutcomeAborted, CompletionTokens: s.chunks})
	}
	return nil
}

func (s *Stream) finish(r Result) (string, error) {
	if s.o.Finish == nil {
		return "", nil
	}
	return s.o.Finish(r)
}

func (s *Stream) advance() {
	switch s.state {
	case stRole:
		s.buf = s.chunk(&delta{Role: "assistant", Content: ptr("")}, nil)
		s.state = stBody
	case stBody:
		s.body()
	case stFinish:
		s.conclude()
	}
}

// body handles the next engine chunk.
func (s *Stream) body() {
	if s.parent.Err() != nil {
		s.abort()
		return
	}
	ch, err := s.recv()
	if errors.Is(err, io.EOF) {
		err = engine.Errorf(engine.CodeEngineError, nil, "the stream ended without a final chunk")
	}
	if err != nil {
		err = s.reason(err)
		if s.parent.Err() != nil {
			s.abort()
			return
		}
		s.fail(err)
		return
	}
	if ch.Content != "" {
		s.chunks++
		s.buf = s.chunk(&delta{Content: &ch.Content}, nil)
	}
	if ch.Done {
		s.last = ch
		s.endEngine()
		reason := finishReason(ch.DoneReason)
		s.buf = append(s.buf, s.chunk(&delta{}, &reason)...)
		if s.o.IncludeUsage {
			s.buf = append(s.buf, s.usageChunk(ch)...)
		}
		s.state = stFinish
	}
}

// finishReason maps the engine's done reason to the OpenAI finish reason.
func finishReason(done string) string {
	if done == "length" {
		return "length"
	}
	return "stop"
}

func (s *Stream) recv() (engine.ChatChunk, error) {
	if s.pending != nil {
		ch := *s.pending
		s.pending = nil
		return ch, nil
	}
	if s.idle != nil {
		s.idle.Reset(s.o.Idle)
	}
	ch, err := s.src.Recv()
	if s.idle != nil {
		s.idle.Stop()
	}
	return ch, err
}

// abort ends a stream whose client is gone: meter what was served, write nothing.
func (s *Stream) abort() {
	s.endEngine()
	if !s.finalized {
		s.finalized = true
		s.finish(Result{Outcome: ledger.OutcomeAborted, CompletionTokens: s.chunks})
	}
	s.err = s.parent.Err()
}

// fail ends the stream with an error event, then the receipt of the failed call.
func (s *Stream) fail(err error) {
	s.endEngine()
	s.buf = append(s.buf, sse(s.o.ErrorEvent(err))...)
	s.finalized = true
	if rc, ferr := s.finish(Result{Outcome: ledger.OutcomeFailed, CompletionTokens: s.chunks}); ferr == nil {
		s.buf = append(s.buf, receiptLine(rc)...)
	}
	s.buf = append(s.buf, doneEvent...)
	s.state = stEnd
}

// conclude meters the completed call and writes the receipt and [DONE].
func (s *Stream) conclude() {
	s.finalized = true
	rc, err := s.finish(Result{Outcome: ledger.OutcomeCompleted, PromptTokens: int64(s.last.PromptTokens),
		CompletionTokens: int64(s.last.CompletionTokens), FinishReason: finishReason(s.last.DoneReason)})
	if err != nil { // no receipt can be signed: no answer goes out without one (D-16, D-51)
		s.buf = append(s.buf, sse(s.o.ErrorEvent(err))...)
	} else {
		s.buf = append(s.buf, receiptLine(rc)...)
	}
	s.buf = append(s.buf, doneEvent...)
	s.state = stEnd
}

// ---- wire format ----

var doneEvent = []byte("data: [DONE]\n\n")

func sse(payload []byte) []byte {
	out := make([]byte, 0, len(payload)+8)
	out = append(out, "data: "...)
	out = append(out, payload...)
	return append(out, '\n', '\n')
}

func receiptLine(jws string) []byte { return []byte(": ls-receipt " + jws + "\n\n") }

func ptr[T any](v T) *T { return &v }

type delta struct {
	Role    string  `json:"role,omitempty"`
	Content *string `json:"content,omitempty"`
}

type choice struct {
	Index        int     `json:"index"`
	Delta        delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

type usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// chunkJSON is ChatCompletionChunk of api/openapi.yaml; a golden test keeps them together
// (the api package generates no type for it yet, PLT-F-052).
type chunkJSON struct {
	ID      string          `json:"id"`
	Object  string          `json:"object"`
	Created int64           `json:"created"`
	Model   string          `json:"model"`
	Choices []choice        `json:"choices"`
	Usage   json.RawMessage `json:"usage,omitempty"`
}

func (s *Stream) frame(c chunkJSON) []byte {
	c.ID, c.Object, c.Created, c.Model = s.o.ID, "chat.completion.chunk", s.o.Created, s.o.Model
	if s.o.IncludeUsage && len(c.Usage) == 0 {
		c.Usage = json.RawMessage("null")
	}
	b, err := json.Marshal(c)
	if err != nil { // cannot happen: the fields are strings, numbers and nulls
		return nil
	}
	return sse(b)
}

func (s *Stream) chunk(d *delta, finish *string) []byte {
	return s.frame(chunkJSON{Choices: []choice{{Index: 0, Delta: *d, FinishReason: finish}}})
}

func (s *Stream) usageChunk(ch engine.ChatChunk) []byte {
	u, _ := json.Marshal(usage{PromptTokens: int64(ch.PromptTokens), CompletionTokens: int64(ch.CompletionTokens),
		TotalTokens: int64(ch.PromptTokens) + int64(ch.CompletionTokens)})
	return s.frame(chunkJSON{Choices: []choice{}, Usage: u})
}
