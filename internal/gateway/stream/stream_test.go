package stream_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ogen-go/ogen"

	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/gateway/stream"
	"github.com/inCoderKaka/local-server/internal/ledger"
)

// item is one thing a scripted engine stream yields.
type item struct {
	ch  engine.ChatChunk
	err error
}

// script is an engine.ChatStream the test feeds by hand. Recv blocks until the test sends an
// item or the engine call's context ends, as a real engine adapter does.
type script struct {
	ctx    context.Context
	in     chan item
	closed atomic.Bool
}

func newScript() *script { return &script{in: make(chan item, 16)} }

func (s *script) open(ctx context.Context) (engine.ChatStream, error) { s.ctx = ctx; return s, nil }

func (s *script) Recv() (engine.ChatChunk, error) {
	select {
	case <-s.ctx.Done():
		return engine.ChatChunk{}, s.ctx.Err()
	case it, ok := <-s.in:
		if !ok {
			return engine.ChatChunk{}, io.EOF
		}
		return it.ch, it.err
	}
}

func (s *script) Close() error { s.closed.Store(true); return nil }

func (s *script) word(w string) { s.in <- item{ch: engine.ChatChunk{Content: w}} }
func (s *script) done(prompt, completion int, reason string) {
	s.in <- item{ch: engine.ChatChunk{Done: true, DoneReason: reason, PromptTokens: prompt, CompletionTokens: completion}}
}

// engineCancelled reports whether the engine call's context ended.
func (s *script) engineCancelled() bool {
	select {
	case <-s.ctx.Done():
		return true
	default:
		return false
	}
}

// harness records what the stream tells its owner.
type harness struct {
	mu       sync.Mutex
	released int
	results  []stream.Result
	finishFn func(stream.Result) (string, error)
}

func (h *harness) opts() stream.Options {
	return stream.Options{ID: "chatcmpl-u1", Model: "m", Created: 100, IncludeUsage: true,
		Release: func() { h.mu.Lock(); h.released++; h.mu.Unlock() },
		Finish: func(r stream.Result) (string, error) {
			h.mu.Lock()
			h.results = append(h.results, r)
			fn := h.finishFn
			h.mu.Unlock()
			if fn != nil {
				return fn(r)
			}
			return "JWS." + r.Outcome, nil
		},
		ErrorEvent: func(err error) []byte {
			var te *stream.TimeoutError
			if errors.As(err, &te) {
				return []byte(`{"error":{"message":"timeout","type":"server_error","kind":"` + te.Kind + `"}}`)
			}
			return []byte(`{"error":{"message":"failed","type":"server_error"}}`)
		}}
}

func (h *harness) snapshot() (int, []stream.Result) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.released, slices.Clone(h.results)
}

// events splits an SSE body into its blocks.
func events(t *testing.T, body string) []string {
	t.Helper()
	if !strings.HasSuffix(body, "\n\n") {
		t.Fatalf("stream does not end with a blank line: %q", body)
	}
	return strings.Split(strings.TrimSuffix(body, "\n\n"), "\n\n")
}

func readAll(t *testing.T, s *stream.Stream) string {
	t.Helper()
	b, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b)
}

// noLeak fails the test if goroutines started during it are still running at its end.
func noLeak(t *testing.T) {
	t.Helper()
	before := runtime.NumGoroutine()
	t.Cleanup(func() {
		deadline := time.Now().Add(2 * time.Second)
		for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if n := runtime.NumGoroutine(); n > before {
			buf := make([]byte, 1<<16)
			t.Errorf("goroutines: %d before, %d after\n%s", before, n, buf[:runtime.Stack(buf, true)])
		}
	})
}

func TestStreamEndsWithReceiptBeforeDone(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	sc.word("hello")
	sc.word("there")
	sc.done(7, 2, "stop")
	st, err := stream.Open(context.Background(), h.opts(), sc.open)
	if err != nil {
		t.Fatal(err)
	}
	evs := events(t, readAll(t, st))
	// role, 2 deltas, finish, usage, receipt, [DONE]
	if len(evs) != 7 {
		t.Fatalf("events = %d: %q", len(evs), evs)
	}
	if evs[len(evs)-1] != "data: [DONE]" || evs[len(evs)-2] != ": ls-receipt JWS.completed" {
		t.Fatalf("tail = %q", evs[len(evs)-2:])
	}
	usage := decode(t, evs[4])
	if len(usage["choices"].([]any)) != 0 || usage["usage"].(map[string]any)["total_tokens"] != float64(9) {
		t.Fatalf("usage chunk = %v", usage)
	}
	if fin := decode(t, evs[3])["choices"].([]any)[0].(map[string]any); fin["finish_reason"] != "stop" {
		t.Fatalf("finish chunk = %v", fin)
	}
	if role := decode(t, evs[0])["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any); role["role"] != "assistant" {
		t.Fatalf("first chunk = %v", role)
	}
	var text string
	for _, e := range evs[1:3] {
		text += decode(t, e)["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["content"].(string)
	}
	if text != "hellothere" {
		t.Fatalf("text = %q", text)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	rel, res := h.snapshot()
	want := []stream.Result{{Outcome: ledger.OutcomeCompleted, PromptTokens: 7, CompletionTokens: 2, FinishReason: "stop"}}
	if rel != 1 || !slices.Equal(res, want) {
		t.Fatalf("released %d, results %+v; want 1, %+v", rel, res, want)
	}
	if !sc.closed.Load() {
		t.Fatal("engine stream not closed")
	}
}

func TestStreamWithoutIncludeUsageHasNoUsageChunk(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	sc.word("a")
	sc.done(1, 1, "length")
	o := h.opts()
	o.IncludeUsage = false
	st, err := stream.Open(context.Background(), o, sc.open)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, st)
	evs := events(t, body)
	if len(evs) != 5 { // role, delta, finish, receipt, [DONE]
		t.Fatalf("events = %q", evs)
	}
	if strings.Contains(body, `"usage"`) {
		t.Fatalf("usage in a stream that did not ask: %s", body)
	}
	if !strings.Contains(evs[2], `"finish_reason":"length"`) {
		t.Fatalf("finish = %s", evs[2])
	}
	st.Close()
}

func TestUsageIsNullOnEveryChunkButTheLast(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	sc.word("a")
	sc.done(1, 1, "stop")
	st, _ := stream.Open(context.Background(), h.opts(), sc.open)
	evs := events(t, readAll(t, st))
	for _, e := range evs[:3] {
		if v, ok := decode(t, e)["usage"]; !ok || v != nil {
			t.Fatalf("usage should be null on %s", e)
		}
	}
	st.Close()
}

func TestFirstTokenTimeoutCancelsTheEngineAndReturnsTypedError(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	o := h.opts()
	o.FirstToken = 30 * time.Millisecond
	st, err := stream.Open(context.Background(), o, sc.open)
	var te *stream.TimeoutError
	if st != nil || !errors.As(err, &te) || te.Kind != stream.TimeoutFirstToken {
		t.Fatalf("Open = %v, %v; want a first_token timeout", st, err)
	}
	if !sc.engineCancelled() || !sc.closed.Load() {
		t.Fatal("engine call not cancelled and closed")
	}
	if rel, res := h.snapshot(); rel != 1 || len(res) != 0 {
		t.Fatalf("released %d, results %v; want 1, none (nothing was served)", rel, res)
	}
}

func TestIdleTimeoutEndsTheStreamWithAnErrorEventAndAFailedReceipt(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	o := h.opts()
	o.Idle = 40 * time.Millisecond
	sc.word("one")
	st, err := stream.Open(context.Background(), o, sc.open)
	if err != nil {
		t.Fatal(err)
	}
	sc.word("two") // then nothing more
	evs := events(t, readAll(t, st))
	// role, one, two, error, receipt, [DONE]
	if len(evs) != 6 || !strings.Contains(evs[3], `"kind":"idle"`) || evs[4] != ": ls-receipt JWS.failed" || evs[5] != "data: [DONE]" {
		t.Fatalf("events = %q", evs)
	}
	if !sc.engineCancelled() || !sc.closed.Load() {
		t.Fatal("engine call not cancelled and closed")
	}
	rel, res := h.snapshot()
	if rel != 1 || !slices.Equal(res, []stream.Result{{Outcome: ledger.OutcomeFailed, CompletionTokens: 2}}) {
		t.Fatalf("released %d, results %+v", rel, res)
	}
	st.Close()
	if _, res := h.snapshot(); len(res) != 1 {
		t.Fatalf("Close after the end metered again: %+v", res)
	}
}

func TestIdleTimeoutIsResetByEveryChunk(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	o := h.opts()
	o.Idle = 400 * time.Millisecond
	sc.word("a")
	st, err := stream.Open(context.Background(), o, sc.open)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range 6 {
			time.Sleep(100 * time.Millisecond)
			sc.word("w")
		}
		sc.done(1, 7, "stop")
	}()
	if body := readAll(t, st); !strings.Contains(body, ": ls-receipt JWS.completed") {
		t.Fatalf("a slow but steady stream was cut: %s", body)
	}
	st.Close()
}

func TestTotalTimeoutEndsAStreamThatKeepsTalking(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	o := h.opts()
	o.Total = 120 * time.Millisecond
	o.Idle = time.Second
	sc.word("a")
	st, err := stream.Open(context.Background(), o, sc.open)
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
				select {
				case sc.in <- item{ch: engine.ChatChunk{Content: "w"}}:
				default:
				}
			}
		}
	}()
	body := readAll(t, st)
	evs := events(t, body)
	n := len(evs)
	if !strings.Contains(evs[n-3], `"kind":"total"`) || evs[n-2] != ": ls-receipt JWS.failed" || evs[n-1] != "data: [DONE]" {
		t.Fatalf("tail = %q", evs[n-3:])
	}
	if !sc.engineCancelled() {
		t.Fatal("engine call not cancelled")
	}
	_, res := h.snapshot()
	if len(res) != 1 || res[0].Outcome != ledger.OutcomeFailed || res[0].CompletionTokens < 2 {
		t.Fatalf("results = %+v", res)
	}
	st.Close()
}

func TestTotalTimeoutReleasesTheSlotOfAClientThatStopsReading(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	o := h.opts()
	o.Total = 100 * time.Millisecond
	sc.word("a")
	st, err := stream.Open(context.Background(), o, sc.open)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if rel, _ := h.snapshot(); rel == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slot not released after the total timeout although nothing was read")
		}
		time.Sleep(5 * time.Millisecond)
	}
	st.Close()
	rel, res := h.snapshot()
	if rel != 1 || len(res) != 1 || res[0].Outcome != ledger.OutcomeAborted {
		t.Fatalf("released %d, results %+v; want 1, aborted", rel, res)
	}
}

func TestEngineErrorMidStream(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	sc.word("a")
	st, err := stream.Open(context.Background(), h.opts(), sc.open)
	if err != nil {
		t.Fatal(err)
	}
	sc.in <- item{err: engine.Errorf(engine.CodeEngineError, nil, "broke")}
	evs := events(t, readAll(t, st))
	if n := len(evs); n != 5 || !strings.Contains(evs[2], `"message":"failed"`) || evs[3] != ": ls-receipt JWS.failed" {
		t.Fatalf("events = %q", evs)
	}
	st.Close()
}

func TestEngineErrorBeforeTheFirstChunkIsReturnedByOpen(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	boom := engine.Errorf(engine.CodeModelNotFound, nil, "nope")
	sc.in <- item{err: boom}
	st, err := stream.Open(context.Background(), h.opts(), sc.open)
	if st != nil || !errors.Is(err, boom) {
		t.Fatalf("Open = %v, %v", st, err)
	}
	if rel, res := h.snapshot(); rel != 1 || len(res) != 0 {
		t.Fatalf("released %d, results %v", rel, res)
	}
}

func TestStreamEndingWithoutADoneChunkFails(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	sc.word("a")
	close(sc.in)
	st, err := stream.Open(context.Background(), h.opts(), sc.open)
	if err != nil {
		t.Fatal(err)
	}
	if body := readAll(t, st); !strings.Contains(body, `"message":"failed"`) || !strings.Contains(body, "JWS.failed") {
		t.Fatalf("body = %s", body)
	}
	st.Close()
}

func TestFinishFailureEndsWithAnErrorAndNoReceipt(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	h.finishFn = func(r stream.Result) (string, error) { return "", errors.New("cannot sign") }
	sc.word("a")
	sc.done(1, 1, "stop")
	st, _ := stream.Open(context.Background(), h.opts(), sc.open)
	body := readAll(t, st)
	if strings.Contains(body, "ls-receipt") || !strings.Contains(body, `"message":"failed"`) || !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Fatalf("body = %s", body)
	}
	st.Close()
}

func TestEngineErrorMidStreamWithAnUnsignableReceiptEndsWithNoReceipt(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	h.finishFn = func(r stream.Result) (string, error) { return "", errors.New("cannot sign") }
	sc.word("a")
	st, err := stream.Open(context.Background(), h.opts(), sc.open)
	if err != nil {
		t.Fatal(err)
	}
	sc.in <- item{err: engine.Errorf(engine.CodeEngineError, nil, "broke")}
	body := readAll(t, st)
	if strings.Contains(body, "ls-receipt") || strings.Count(body, `"message":"failed"`) != 1 || !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Fatalf("body = %s", body)
	}
	rel, res := h.snapshot()
	if rel != 1 || !slices.Equal(res, []stream.Result{{Outcome: ledger.OutcomeFailed, CompletionTokens: 1}}) {
		t.Fatalf("released %d, results %+v", rel, res)
	}
	st.Close()
}

func TestClientDisconnectWhileWaitingCancelsTheEngineAndMetersAborted(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sc.word("a")
	sc.word("b")
	st, err := stream.Open(ctx, h.opts(), sc.open)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	for range 3 { // role, a, b
		if _, err := st.Read(buf); err != nil {
			t.Fatal(err)
		}
	}
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err = st.Read(buf) // blocks in the engine until the client goes
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Read = %v, want context.Canceled", err)
	}
	st.Close()
	if !sc.engineCancelled() || !sc.closed.Load() {
		t.Fatal("engine call not cancelled and closed")
	}
	rel, res := h.snapshot()
	if rel != 1 || !slices.Equal(res, []stream.Result{{Outcome: ledger.OutcomeAborted, CompletionTokens: 2}}) {
		t.Fatalf("released %d, results %+v", rel, res)
	}
}

func TestClosingAnUnfinishedStreamMetersAborted(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	sc.word("a")
	sc.word("b")
	sc.word("c")
	st, _ := stream.Open(context.Background(), h.opts(), sc.open)
	buf := make([]byte, 4096)
	for range 3 { // role, a, b
		st.Read(buf)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st.Close() // safe to repeat
	if !sc.engineCancelled() {
		t.Fatal("engine call not cancelled")
	}
	rel, res := h.snapshot()
	if rel != 1 || !slices.Equal(res, []stream.Result{{Outcome: ledger.OutcomeAborted, CompletionTokens: 2}}) {
		t.Fatalf("released %d, results %+v", rel, res)
	}
	if _, err := st.Read(buf); err == nil {
		t.Fatal("Read after Close succeeded")
	}
}

func TestClientGoneBeforeTheFirstChunkIsReturnedByOpen(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	st, err := stream.Open(ctx, h.opts(), sc.open)
	if st != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Open = %v, %v", st, err)
	}
	if rel, _ := h.snapshot(); rel != 1 {
		t.Fatalf("released %d", rel)
	}
}

func TestSmallReadsLoseNothing(t *testing.T) {
	noLeak(t)
	h, sc := &harness{}, newScript()
	sc.word("hello")
	sc.done(1, 1, "stop")
	st, _ := stream.Open(context.Background(), h.opts(), sc.open)
	var out bytes.Buffer
	buf := make([]byte, 7)
	for {
		n, err := st.Read(buf)
		out.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(events(t, out.String())) != 6 {
		t.Fatalf("body = %q", out.String())
	}
	st.Close()
}

// ---- the flushing writer ----

// logWriter records the order of Write and Flush calls.
type logWriter struct {
	*httptest.ResponseRecorder
	log []string
}

func (l *logWriter) Write(p []byte) (int, error) {
	l.log = append(l.log, "write")
	return l.ResponseRecorder.Write(p)
}
func (l *logWriter) Flush() { l.log = append(l.log, "flush"); l.ResponseRecorder.Flush() }
func (l *logWriter) WriteHeader(c int) {
	l.log = append(l.log, "header")
	l.ResponseRecorder.WriteHeader(c)
}

func TestFlushWriterFlushesHeadersAndAfterEveryWrite(t *testing.T) {
	lw := &logWriter{ResponseRecorder: httptest.NewRecorder()}
	w := stream.NewFlushWriter(lw)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	w.Write([]byte("a"))
	w.Write([]byte("b"))
	want := []string{"header", "flush", "write", "flush", "write", "flush"}
	if !slices.Equal(lw.log, want) {
		t.Fatalf("calls = %v, want %v", lw.log, want)
	}
	if lw.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal("Cache-Control not set on an event stream")
	}
}

func TestFlushWriterLeavesOtherResponsesAlone(t *testing.T) {
	lw := &logWriter{ResponseRecorder: httptest.NewRecorder()}
	w := stream.NewFlushWriter(lw)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write([]byte("{}"))
	if slices.Contains(lw.log, "flush") || lw.Header().Get("Cache-Control") != "" {
		t.Fatalf("calls = %v", lw.log)
	}
}

func TestFlushWriterWithoutAFlusherDoesNotFail(t *testing.T) {
	var w http.ResponseWriter = stream.NewFlushWriter(bareWriter{httptest.NewRecorder()})
	w.Header().Set("Content-Type", "text/event-stream")
	if _, err := w.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
}

type bareWriter struct{ r *httptest.ResponseRecorder }

func (b bareWriter) Header() http.Header         { return b.r.Header() }
func (b bareWriter) Write(p []byte) (int, error) { return b.r.Write(p) }
func (b bareWriter) WriteHeader(c int)           { b.r.WriteHeader(c) }

// ---- the wire format against api/openapi.yaml ----

func decode(t *testing.T, event string) map[string]any {
	t.Helper()
	payload, ok := strings.CutPrefix(event, "data: ")
	if !ok {
		t.Fatalf("not a data event: %q", event)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatalf("%v: %q", err, payload)
	}
	return m
}

// TestChunkMatchesOpenAPISchema keeps the chunk this package writes equal to the
// ChatCompletionChunk schema until PLT-F-052 generates the type: only listed properties, every
// required one present, the enum values the schema allows.
func TestChunkMatchesOpenAPISchema(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ogen.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	props := func(name string) (map[string]bool, []string) {
		s := spec.Components.Schemas[name]
		if s == nil {
			t.Fatalf("%s missing from the spec", name)
		}
		m := map[string]bool{}
		for _, p := range s.Properties {
			m[p.Name] = true
		}
		return m, s.Required
	}
	chunkProps, chunkReq := props("ChatCompletionChunk")
	choiceProps, choiceReq := props("ChatCompletionChunkChoice")
	if c := spec.Components.Schemas["ChatCompletionChunk"].Properties; len(c) == 0 {
		t.Fatal("no properties")
	}
	check := func(what string, obj map[string]any, allowed map[string]bool, required []string) {
		for k := range obj {
			if !allowed[k] {
				t.Errorf("%s has %q, which the schema does not list", what, k)
			}
		}
		for _, k := range required {
			if _, ok := obj[k]; !ok {
				t.Errorf("%s lacks the required %q", what, k)
			}
		}
	}
	h, sc := &harness{}, newScript()
	sc.word("hi")
	sc.done(3, 1, "stop")
	st, _ := stream.Open(context.Background(), h.opts(), sc.open)
	evs := events(t, readAll(t, st))
	st.Close()
	for _, e := range evs[:len(evs)-2] {
		c := decode(t, e)
		check("chunk", c, chunkProps, chunkReq)
		if c["object"] != "chat.completion.chunk" || c["id"] != "chatcmpl-u1" || c["model"] != "m" || c["created"] != float64(100) {
			t.Errorf("chunk header = %v", c)
		}
		for _, ch := range c["choices"].([]any) {
			cm := ch.(map[string]any)
			check("choice", cm, choiceProps, choiceReq)
			d := cm["delta"].(map[string]any)
			for k := range d {
				if k != "role" && k != "content" && k != "tool_calls" {
					t.Errorf("delta has %q", k)
				}
			}
		}
	}
}
