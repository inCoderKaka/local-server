package gateway_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/engine/fake"
	"github.com/inCoderKaka/local-server/internal/gateway"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/settings"
)

const streamChatBody = `{"model":"` + chatModel + `","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hello there"}]}`

// flushRecorder records the order of Write and Flush calls on a response.
type flushRecorder struct {
	*httptest.ResponseRecorder
	mu  sync.Mutex
	log []string
}

func (f *flushRecorder) Write(p []byte) (int, error) {
	f.mu.Lock()
	f.log = append(f.log, "write")
	f.mu.Unlock()
	return f.ResponseRecorder.Write(p)
}

func (f *flushRecorder) Flush() {
	f.mu.Lock()
	f.log = append(f.log, "flush")
	f.mu.Unlock()
	f.ResponseRecorder.Flush()
}

// sseEvents splits a body into its blank-line separated blocks.
func sseEvents(t *testing.T, body string) []string {
	t.Helper()
	if !strings.HasSuffix(body, "\n\n") {
		t.Fatalf("stream does not end with a blank line: %q", body)
	}
	return strings.Split(strings.TrimSuffix(body, "\n\n"), "\n\n")
}

const receiptPrefix = ": ls-receipt "

// TestStreamFlushAfterEveryWrite is PLT-B-004: the generated encoder copies the stream with
// io.Copy, so the gateway's writer must flush after the headers and every write.
func TestStreamFlushAfterEveryWrite(t *testing.T) {
	e := newEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(streamChatBody))
	req.RemoteAddr = "100.64.0.1:5555"
	req.Header.Set("Authorization", e.key)
	req.Header.Set("Content-Type", "application/json")
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status %d, content type %q: %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	writes := 0
	for i, c := range rec.log {
		if c != "write" {
			continue
		}
		writes++
		if i+1 >= len(rec.log) || rec.log[i+1] != "flush" {
			t.Fatalf("write %d is not followed by a flush: %v", writes, rec.log)
		}
	}
	if writes < 6 { // role, one per word, finish, usage, receipt, [DONE]
		t.Fatalf("only %d writes: %v", writes, rec.log)
	}
	if rec.log[0] != "flush" {
		t.Fatalf("the headers were not flushed first: %v", rec.log[:2])
	}
}

// TestStreamFlushDeliversChunksBeforeTheEngineFinishes reads a real HTTP response while the
// engine is stalled: the first chunks must arrive without waiting for the end of the stream.
func TestStreamFlushDeliversChunksBeforeTheEngineFinishes(t *testing.T) {
	e := newEnv(t)
	gate := make(chan struct{})
	e.eng.wrapChat = func(s engine.ChatStream) engine.ChatStream {
		return &gatedStream{ChatStream: s, after: 2, gate: gate, ctx: e.eng.chatCtx()}
	}
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	resp := postStream(t, context.Background(), srv.URL, e.key, streamChatBody)
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	for range 3 { // role and two words arrive while the engine waits on the gate
		if ev := readEvent(t, br); !strings.HasPrefix(ev, "data: {") {
			t.Fatalf("event = %q", ev)
		}
	}
	close(gate)
	var rest []string
	for {
		ev := readEvent(t, br)
		rest = append(rest, ev)
		if ev == "data: [DONE]" {
			break
		}
	}
	if n := len(rest); n < 3 || !strings.HasPrefix(rest[n-2], receiptPrefix) {
		t.Fatalf("tail = %q", rest)
	}
}

// gatedStream blocks the Recv after `after` chunks until gate closes or the call is cancelled.
type gatedStream struct {
	engine.ChatStream
	after, n int
	gate     chan struct{}
	ctx      context.Context
}

func (g *gatedStream) Recv() (engine.ChatChunk, error) {
	g.n++
	if g.n > g.after {
		select {
		case <-g.gate:
		case <-g.ctx.Done():
			return engine.ChatChunk{}, g.ctx.Err()
		}
	}
	return g.ChatStream.Recv()
}

// stalledStream yields `after` chunks and then waits for the call to be cancelled.
type stalledStream struct {
	engine.ChatStream
	after, n int
	ctx      context.Context
}

func (s *stalledStream) Recv() (engine.ChatChunk, error) {
	s.n++
	if s.n > s.after {
		<-s.ctx.Done()
		return engine.ChatChunk{}, s.ctx.Err()
	}
	return s.ChatStream.Recv()
}

// engineCtx wraps the next chat stream with mk, which gets the call's context.
func (e *env) wrapWithCtx(mk func(engine.ChatStream, context.Context) engine.ChatStream) {
	e.eng.wrapChat = func(s engine.ChatStream) engine.ChatStream { return mk(s, e.eng.chatCtx()) }
}

func postStream(t *testing.T, ctx context.Context, base, key, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readEvent(t *testing.T, br *bufio.Reader) string {
	t.Helper()
	var sb strings.Builder
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read event: %v (got %q)", err, sb.String())
		}
		if line == "\n" {
			return strings.TrimSuffix(sb.String(), "\n")
		}
		sb.WriteString(line)
	}
}

func chunkOf(t *testing.T, ev string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(ev, "data: ")), &m); err != nil {
		t.Fatalf("%v: %q", err, ev)
	}
	return m
}

type useRow struct {
	outcome            string
	prompt, completion int64
}

func (e *env) useRows() []useRow {
	e.t.Helper()
	rows, err := e.db.Reader().Query(context.Background(), `SELECT outcome, prompt_tokens, completion_tokens FROM use_record ORDER BY rowid`)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []useRow
	for rows.Next() {
		var r useRow
		if err := rows.Scan(&r.outcome, &r.prompt, &r.completion); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// waitRows waits for n usage rows: a disconnected stream is metered when the server notices.
func (e *env) waitRows(n int) []useRow {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rows := e.useRows(); len(rows) >= n {
			return rows
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatalf("usage rows did not reach %d", n)
	return nil
}

func TestStreamedChatEndsWithTheReceiptBeforeDone(t *testing.T) {
	e := newEnv(t)
	r := e.chat(streamChatBody)
	statusIs(t, r, http.StatusOK)
	evs := sseEvents(t, r.Body.String())
	n := len(evs)
	if evs[n-1] != "data: [DONE]" || !strings.HasPrefix(evs[n-2], receiptPrefix) {
		t.Fatalf("tail = %q", evs[n-2:])
	}
	if r.Header().Get("LS-Receipt") != "" {
		t.Fatal("a streamed response must not carry LS-Receipt")
	}
	rc := e.verify(strings.TrimPrefix(evs[n-2], receiptPrefix))
	if rc.Outcome != ledger.OutcomeCompleted || rc.Metering != ledger.Metered || rc.Operation != ledger.OpChatCompletions || rc.Model != chatModel {
		t.Fatalf("receipt = %+v", rc)
	}
	usage := chunkOf(t, evs[n-3])
	if len(usage["choices"].([]any)) != 0 {
		t.Fatalf("usage chunk = %v", usage)
	}
	u := usage["usage"].(map[string]any)
	if int64(u["prompt_tokens"].(float64)) != rc.PromptTokens || int64(u["completion_tokens"].(float64)) != rc.CompletionTokens || rc.CompletionTokens == 0 {
		t.Fatalf("usage %v differs from the receipt %+v", u, rc)
	}
	// Every chunk carries the id of the metered record.
	for _, ev := range evs[:n-2] {
		if id := chunkOf(t, ev)["id"]; id != "chatcmpl-"+rc.UsageID {
			t.Fatalf("chunk id = %v, want chatcmpl-%s", id, rc.UsageID)
		}
	}
	if rows := e.useRows(); len(rows) != 1 || rows[0].outcome != ledger.OutcomeCompleted {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestStreamedChatWithoutIncludeUsage(t *testing.T) {
	e := newEnv(t)
	r := e.chat(`{"model":"` + chatModel + `","stream":true,"messages":[{"role":"user","content":"hello there"}]}`)
	statusIs(t, r, http.StatusOK)
	if strings.Contains(r.Body.String(), `"usage"`) {
		t.Fatalf("usage in a stream that did not ask: %s", r.Body.String())
	}
}

// OpenAI publishes stream_options as nullable: null is the same as leaving it out.
func TestNullStreamOptionsIsTheSameAsOmitted(t *testing.T) {
	e := newEnv(t)
	r := e.chat(`{"model":"` + chatModel + `","stream":false,"stream_options":null,"messages":[{"role":"user","content":"hi"}]}`)
	statusIs(t, r, http.StatusOK)
	if strings.HasPrefix(r.Body.String(), "data:") || r.json()["object"] != "chat.completion" {
		t.Fatalf("not a plain completion: %s", r.Body.String())
	}
	r = e.chat(`{"model":"` + chatModel + `","stream":true,"stream_options":null,"messages":[{"role":"user","content":"hi"}]}`)
	statusIs(t, r, http.StatusOK)
	evs := sseEvents(t, r.Body.String())
	if n := len(evs); n < 3 || evs[n-1] != "data: [DONE]" {
		t.Fatalf("events = %q", evs)
	}
	if strings.Contains(r.Body.String(), `"usage"`) {
		t.Fatalf("usage chunk without include_usage: %s", r.Body.String())
	}
}

func TestStreamOptionsNeedStream(t *testing.T) {
	e := newEnv(t)
	for _, body := range []string{
		`{"model":"` + chatModel + `","stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"` + chatModel + `","stream":false,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`,
	} {
		r := e.chat(body)
		statusIs(t, r, http.StatusBadRequest)
		if eb := r.errBody(); eb["param"] != "stream_options" {
			t.Fatalf("error = %v", eb)
		}
	}
}

func TestStreamedChatMeteringFailureSendsTheUnmeteredReceipt(t *testing.T) {
	e := newEnv(t)
	e.meter.set(-1)
	r := e.chat(streamChatBody)
	statusIs(t, r, http.StatusOK)
	evs := sseEvents(t, r.Body.String())
	n := len(evs)
	if evs[n-1] != "data: [DONE]" || !strings.HasPrefix(evs[n-2], receiptPrefix) {
		t.Fatalf("tail = %q", evs[n-2:])
	}
	if rc := e.verify(strings.TrimPrefix(evs[n-2], receiptPrefix)); rc.Metering != ledger.Unmetered {
		t.Fatalf("receipt = %+v", rc)
	}
	if len(e.events("metering.failed")) != 1 {
		t.Fatal("metering.failed not emitted")
	}
}

func TestStreamedChatUnsignableReceiptEndsWithAnErrorAndNoReceipt(t *testing.T) {
	e := newEnv(t)
	e.meter.set(-1)
	e.meter.signFails = true
	r := e.chat(streamChatBody)
	statusIs(t, r, http.StatusOK)
	body := r.Body.String()
	if strings.Contains(body, "ls-receipt") || !strings.Contains(body, `"error"`) || !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Fatalf("body = %s", body)
	}
}

func TestStreamedChatEngineErrorBeforeTheFirstChunkIsAnHTTPError(t *testing.T) {
	e := newEnv(t)
	e.eng.InjectError(fake.OpChat, engineDown(), 1)
	r := e.chat(streamChatBody)
	statusIs(t, r, http.StatusServiceUnavailable)
	if r.Header().Get("Content-Type") == "text/event-stream" || e.useRecords() != 0 {
		t.Fatal("expected a plain error and no record")
	}
}

func TestStreamFirstTokenTimeout(t *testing.T) {
	e := newEnv(t)
	e.set(gateway.KeyStreamFirstToken, "50ms")
	e.wrapWithCtx(func(s engine.ChatStream, ctx context.Context) engine.ChatStream {
		return &stalledStream{ChatStream: s, ctx: ctx}
	})
	r := e.chat(streamChatBody)
	statusIs(t, r, http.StatusGatewayTimeout)
	if !e.eng.chatCtxDone() {
		t.Fatal("the engine call was not cancelled")
	}
	if e.useRecords() != 0 {
		t.Fatal("a call that served nothing was metered")
	}
}

func TestStreamIdleTimeout(t *testing.T) {
	e := newEnv(t)
	e.set(gateway.KeyStreamIdle, "50ms")
	e.wrapWithCtx(func(s engine.ChatStream, ctx context.Context) engine.ChatStream {
		return &stalledStream{ChatStream: s, after: 3, ctx: ctx}
	})
	r := e.chat(streamChatBody)
	statusIs(t, r, http.StatusOK)
	e.assertFailedStream(t, r.Body.String(), 3)
}

func TestStreamTotalTimeout(t *testing.T) {
	e := newEnv(t)
	e.set(gateway.KeyStreamTotal, "80ms")
	e.wrapWithCtx(func(s engine.ChatStream, ctx context.Context) engine.ChatStream {
		return &stalledStream{ChatStream: s, after: 3, ctx: ctx}
	})
	r := e.chat(streamChatBody)
	statusIs(t, r, http.StatusOK)
	e.assertFailedStream(t, r.Body.String(), 3)
}

// The next tests are PLT-B-039 (req_001 D-38, D-54). The fake engine stands in for an adapter whose
// own first-token and idle timeouts (the `fallback`) are shorter than the host's settings; the
// numbers are the ticket's 30 s fallback, 45 s engine gap and 60 s setting scaled by 1/100, because
// the stream package runs on real timers and has no clock seam.
const (
	adapterFallback = 300 * time.Millisecond // the engine adapter's own timeout: "30 s"
	engineGap       = 450 * time.Millisecond // how long the engine goes quiet: "45 s"
	settingLong     = 600 * time.Millisecond // the host setting above the gap: "60 s"
)

// defaultReplyWords is the token count of fake.DefaultReply; the gap sits before chunk 3, after
// three words.
var defaultReplyWords = int64(fake.CountTokens(fake.DefaultReply))

func withEngine(opts ...fake.Option) func(*envOpts) {
	return func(o *envOpts) { o.engine = opts }
}

// assertCompletedStream checks a stream that ran to its end and was metered as completed.
func (e *env) assertCompletedStream(t *testing.T, r resp) {
	t.Helper()
	statusIs(t, r, http.StatusOK)
	evs := sseEvents(t, r.Body.String())
	n := len(evs)
	if evs[n-1] != "data: [DONE]" || !strings.HasPrefix(evs[n-2], receiptPrefix) {
		t.Fatalf("tail = %q", evs[n-2:])
	}
	rc := e.verify(strings.TrimPrefix(evs[n-2], receiptPrefix))
	if rc.Outcome != ledger.OutcomeCompleted || rc.CompletionTokens != defaultReplyWords {
		t.Fatalf("receipt = %+v", rc)
	}
}

// A host stream_idle_timeout above the engine gap keeps the stream open even though the engine's
// own fallback is below the gap: the gateway sends its setting on the request.
func TestStreamIdleSettingAboveTheEngineGapKeepsTheStreamOpen(t *testing.T) {
	e := newEnv(t, withEngine(fake.WithChatTimeouts(adapterFallback, adapterFallback), fake.WithChunkGap(3, engineGap)))
	e.set(gateway.KeyStreamIdle, settingLong.String())
	r := e.chat(streamChatBody)
	e.assertCompletedStream(t, r)
	if len(e.eng.chats) != 1 || e.eng.chats[0].IdleTimeout != settingLong {
		t.Fatalf("engine requests = %+v, want IdleTimeout %v", e.eng.chats, settingLong)
	}
}

// The same gap with a stream_idle_timeout below it ends the stream with the idle timeout error.
func TestStreamIdleSettingBelowTheEngineGapEndsTheStream(t *testing.T) {
	e := newEnv(t, withEngine(fake.WithChatTimeouts(settingLong, settingLong), fake.WithChunkGap(3, engineGap)))
	e.set(gateway.KeyStreamIdle, adapterFallback.String())
	r := e.chat(streamChatBody)
	statusIs(t, r, http.StatusOK)
	e.assertFailedStream(t, r.Body.String(), 3)
	if e.eng.chats[0].IdleTimeout != adapterFallback {
		t.Fatalf("engine requests = %+v, want IdleTimeout %v", e.eng.chats, adapterFallback)
	}
}

// A host stream_first_token_timeout above the engine's wait for its first chunk keeps the call open.
func TestStreamFirstTokenSettingAboveTheEngineWaitKeepsTheStreamOpen(t *testing.T) {
	e := newEnv(t, withEngine(fake.WithChatTimeouts(adapterFallback, adapterFallback), fake.WithChunkGap(0, engineGap)))
	e.set(gateway.KeyStreamFirstToken, settingLong.String())
	r := e.chat(streamChatBody)
	e.assertCompletedStream(t, r)
	if len(e.eng.chats) != 1 || e.eng.chats[0].FirstTokenTimeout != settingLong {
		t.Fatalf("engine requests = %+v, want FirstTokenTimeout %v", e.eng.chats, settingLong)
	}
}

// A non-streamed call is bounded by gateway.engine_timeout alone: the engine's own first-token and
// idle fallback must not cut it, and the request carries engine_timeout as both limits.
func TestNonStreamedChatIsBoundedByTheEngineTimeoutAlone(t *testing.T) {
	e := newEnv(t, withEngine(fake.WithChatTimeouts(adapterFallback, adapterFallback),
		fake.WithChunkGap(0, engineGap), fake.WithChunkGap(3, engineGap)))
	e.set(gateway.KeyEngineTimeout, (2 * settingLong).String())
	r := e.chat(okChat)
	statusIs(t, r, http.StatusOK)
	got := e.eng.chats[0]
	if got.FirstTokenTimeout != 2*settingLong || got.IdleTimeout != 2*settingLong {
		t.Fatalf("engine request limits = %v and %v, want engine_timeout %v", got.FirstTokenTimeout, got.IdleTimeout, 2*settingLong)
	}
}

// ... and it still ends when engine_timeout is shorter than the engine's wait.
func TestNonStreamedChatEndsAtTheEngineTimeout(t *testing.T) {
	e := newEnv(t, withEngine(fake.WithChunkGap(3, engineGap)))
	e.set(gateway.KeyEngineTimeout, adapterFallback.String())
	statusIs(t, e.chat(okChat), http.StatusGatewayTimeout)
}

// ... and when the engine returns the context's own deadline error, that is an engine timeout
// logged at Warn, not the host failing.
func TestNonStreamedChatEngineDeadlineIsAnEngineTimeout(t *testing.T) {
	e := newEnv(t)
	e.set(gateway.KeyEngineTimeout, "80ms")
	e.wrapWithCtx(func(s engine.ChatStream, ctx context.Context) engine.ChatStream {
		return &stalledStream{ChatStream: s, after: 1, ctx: ctx}
	})
	r := e.chat(okChat)
	statusIs(t, r, http.StatusGatewayTimeout)
	if !strings.Contains(r.Body.String(), "The model engine did not answer in time.") {
		t.Fatalf("body = %q, want the engine timeout message", r.Body.String())
	}
	logs := e.tel.logText()
	if !strings.Contains(logs, "gateway: model engine call failed") || !strings.Contains(logs, "engine_timeout") ||
		strings.Contains(logs, "gateway: request failed") {
		t.Fatalf("logs = %q, want a Warn engine call failure and no request failure", logs)
	}
}

// assertFailedStream checks a stream the gateway ended with a 504 error event, the receipt of
// the failed call and [DONE], after `words` words.
func (e *env) assertFailedStream(t *testing.T, body string, words int64) {
	t.Helper()
	evs := sseEvents(t, body)
	n := len(evs)
	if evs[n-1] != "data: [DONE]" || !strings.HasPrefix(evs[n-2], receiptPrefix) {
		t.Fatalf("tail = %q", evs[n-2:])
	}
	var env struct {
		Error struct{ Message, Type string }
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(evs[n-3], "data: ")), &env); err != nil || env.Error.Type != "server_error" {
		t.Fatalf("error event = %q (%v)", evs[n-3], err)
	}
	rc := e.verify(strings.TrimPrefix(evs[n-2], receiptPrefix))
	if rc.Outcome != ledger.OutcomeFailed || rc.CompletionTokens != words || rc.PromptTokens != 0 {
		t.Fatalf("receipt = %+v", rc)
	}
	if !e.eng.chatCtxDone() {
		t.Fatal("the engine call was not cancelled")
	}
	if rows := e.useRows(); len(rows) != 1 || rows[0] != (useRow{ledger.OutcomeFailed, 0, words}) {
		t.Fatalf("rows = %+v", rows)
	}
}

func (r *recEngine) chatCtxDone() bool {
	select {
	case <-r.chatCtx().Done():
		return true
	default:
		return false
	}
}

// D-38: a client that disconnects mid-stream cancels the engine call and its tokens so far are
// metered as aborted. (Its receipt has nowhere to go: PLT-B-026.)
func TestStreamClientDisconnectCancelsTheEngineAndMetersAborted(t *testing.T) {
	e := newEnv(t)
	e.wrapWithCtx(func(s engine.ChatStream, ctx context.Context) engine.ChatStream {
		return &stalledStream{ChatStream: s, after: 2, ctx: ctx}
	})
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	resp := postStream(t, ctx, srv.URL, e.key, streamChatBody)
	br := bufio.NewReader(resp.Body)
	role := readEvent(t, br)
	for range 2 { // two words
		readEvent(t, br)
	}
	cancel()
	resp.Body.Close()
	rows := e.waitRows(1)
	if len(rows) != 1 || rows[0] != (useRow{ledger.OutcomeAborted, 0, 2}) {
		t.Fatalf("rows = %+v, want one aborted row with 2 completion tokens", rows)
	}
	var rowID string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT id FROM use_record`).Scan(&rowID); err != nil {
		t.Fatal(err)
	}
	if id := chunkOf(t, role)["id"]; id != "chatcmpl-"+rowID {
		t.Fatalf("chunk id = %v, want chatcmpl-%s (the aborted record's id)", id, rowID)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !e.eng.chatCtxDone() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !e.eng.chatCtxDone() {
		t.Fatal("the engine call was not cancelled")
	}
	// The slot is free again.
	e.eng.wrapChat = nil
	if r := e.chat(okChat); r.Code != http.StatusOK {
		t.Fatalf("after the disconnect: %d %s", r.Code, r.Body.String())
	}
	srv.Close() // waits for the handlers, so every log line of the disconnected request is in
	if logs := e.tel.logText(); strings.Contains(logs, "response could not be written") {
		t.Fatalf("a client disconnect was logged as a write failure: %s", logs)
	}
}

func TestStreamSettingsAreValidated(t *testing.T) {
	e := newEnv(t)
	for _, key := range []string{gateway.KeyStreamFirstToken, gateway.KeyStreamIdle, gateway.KeyStreamTotal} {
		for _, bad := range []string{"0s", "-1s", "soon"} {
			if err := e.st.Set(context.Background(), settings.Host, key, "", bad); err == nil {
				t.Errorf("%s accepted %q", key, bad)
			}
		}
		e.set(key, "5s")
	}
}
