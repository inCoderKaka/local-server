package gateway

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/policy"
)

// Fuzz targets for the untrusted request bodies of chat and embeddings (req_001 D-60). They run
// the real generated decoder, validation and planning, and the gateway's own mapping of every
// error, over arbitrary bytes. Plain `go test` runs the seeds, in f.Add and in
// testdata/fuzz/<Target>/.
//
// Invariants: nothing panics; a request is accepted or answered with a well-formed OpenAI
// envelope; the param is null or a short identifier; no run of ten or more bytes of the request
// reaches the answer unless the same run is fixed text in this package's source.

type fuzzHandler struct {
	api.UnimplementedHandler
}

func (fuzzHandler) HandleAccessKeyAuth(ctx context.Context, _ api.OperationName, _ api.AccessKeyAuth) (context.Context, error) {
	return ctx, nil
}
func (fuzzHandler) HandleAdminSessionAuth(ctx context.Context, _ api.OperationName, _ api.AdminSessionAuth) (context.Context, error) {
	return ctx, nil
}
func (fuzzHandler) HandleConsumerSessionAuth(ctx context.Context, _ api.OperationName, _ api.ConsumerSessionAuth) (context.Context, error) {
	return ctx, nil
}

var fuzzCfg = config{defaultMaxTokens: 1024, maxTokensCeiling: 8192, defaultContext: 4096, charsPerToken: 4, maxEmbeddingInputs: 128}

func (fuzzHandler) CreateChatCompletion(_ context.Context, req *api.ChatCompletionRequest, _ api.CreateChatCompletionParams) (api.CreateChatCompletionRes, error) {
	in, e := validateChat(req)
	if e != nil {
		return nil, e
	}
	if _, e := planChat(fuzzCfg, policy.Decision{}, "refuse", in); e != nil {
		return nil, e
	}
	return &api.ChatCompletionHeaders{Response: api.ChatCompletion{ID: "x", Object: "chat.completion"}}, nil
}

func (fuzzHandler) CreateEmbedding(_ context.Context, req *api.EmbeddingRequest, _ api.CreateEmbeddingParams) (*api.EmbeddingListHeaders, error) {
	in, e := validateEmbed(req, fuzzCfg.maxEmbeddingInputs)
	if e != nil {
		return nil, e
	}
	if _, e := planEmbed(fuzzCfg, policy.Decision{}, "refuse", in); e != nil {
		return nil, e
	}
	return &api.EmbeddingListHeaders{LSReceipt: "a.b.c", Response: api.EmbeddingList{Object: "list"}}, nil
}

func (fuzzHandler) NewError(_ context.Context, err error) *api.ErrorStatusCodeWithHeaders {
	e, _ := classify(err, time.Time{})
	return e.response()
}

func fuzzServer(t testing.TB) http.Handler {
	t.Helper()
	h := fuzzHandler{}
	srv, err := api.NewServer(h, h, api.WithErrorHandler(func(_ context.Context, w http.ResponseWriter, _ *http.Request, err error) {
		writeError(w, decodeError(err))
	}))
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// sourceText is every string literal of this package's non-test source: the fixed text an answer
// may be made of.
func sourceText(t testing.TB) string {
	t.Helper()
	fset := token.NewFileSet()
	var sb strings.Builder
	for _, name := range []string{"errors.go", "validate.go"} {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil {
					sb.WriteString(s)
					sb.WriteByte('\n')
				}
			}
			return true
		})
	}
	return sb.String()
}

// quotesRequest reports whether answer holds a run of ten bytes of body that is not fixed text.
func quotesRequest(answer, body, fixed string) (string, bool) {
	const run = 10
	for i := 0; i+run <= len(body); i++ {
		w := body[i : i+run]
		if strings.Contains(answer, w) && !strings.Contains(fixed, w) {
			return w, true
		}
	}
	return "", false
}

func checkAnswer(t *testing.T, srv http.Handler, fixed, path string, body []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer x")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req) // a panic fails the test
	if rec.Code == http.StatusOK {
		return
	}
	var env struct {
		Error struct {
			Message string  `json:"message"`
			Type    string  `json:"type"`
			Param   *string `json:"param"`
			Code    *string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Error.Type == "" || env.Error.Message == "" {
		t.Fatalf("status %d: not an error envelope: %q", rec.Code, rec.Body.String())
	}
	if rec.Code < 400 || rec.Code > 499 {
		t.Fatalf("a bad body answered %d", rec.Code)
	}
	if p := env.Error.Param; p != nil && !safeName(*p) {
		t.Fatalf("param %q is not a short identifier", *p)
	}
	if !utf8.ValidString(rec.Body.String()) {
		t.Fatal("answer is not UTF-8")
	}
	// The param is a field name the contract says to echo, checked above; the message is the
	// only free text of the envelope.
	if w, bad := quotesRequest(env.Error.Message, string(body), fixed); bad {
		t.Fatalf("the message quotes request bytes %q: %s", w, rec.Body.String())
	}
}

var chatSeeds = []string{
	`{"model":"m","messages":[{"role":"user","content":"hello"}]}`,
	`{"model":"m","messages":[{"role":"system","content":"be brief"},{"role":"user","content":[{"type":"text","text":"hi"}]}],"max_tokens":16}`,
	`{"model":"m","messages":[{"role":"user","content":"hi"}],"n":2}`,
	`{"model":"m","messages":[{"role":"user","content":"hi"}],"logit_bias":{"1":2},"tool_choice":"auto"}`,
	`{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":true,"stream_options":{"include_usage":true}}`,
	`{"model":"m","messages":[{"role":"user","content":"hi"}],"temperature":9,"top_p":"x"}`,
	`{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]}`,
	`{"model":"m","messages":[{"role":"tool","content":"x","tool_call_id":"1"},{"role":"assistant","content":null,"tool_calls":[{"id":"1","type":"function","function":{"name":"f","arguments":"{}"}}]}]}`,
	`{"model":"m","messages":[{"role":"assistant"}],"max_tokens":0,"max_completion_tokens":-1}`,
	`{"model":"","messages":[]}`,
	`{"model":"m","messages":[{"role":"user","content":"a"}],"response_format":{"type":"json_schema","json_schema":{"name":"s"}}}`,
	`[]`, `null`, `{`, ``, `{"model":1}`, "{\"model\":\"m\xff\",\"messages\":[]}",
}

func FuzzChatRequest(f *testing.F) {
	srv, fixed := fuzzServer(f), sourceText(f)
	for _, s := range chatSeeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, body []byte) { checkAnswer(t, srv, fixed, "/v1/chat/completions", body) })
}

var embedSeeds = []string{
	`{"model":"m","input":"hello"}`,
	`{"model":"m","input":["a","b c"],"encoding_format":"base64"}`,
	`{"model":"m","input":[1,2,3]}`,
	`{"model":"m","input":"hi","dimensions":2,"user":"u"}`,
	`{"model":"m","input":[]}`,
	`{"model":"m","input":[""]}`,
	`{"model":"m","input":"hi","encoding_format":"hex"}`,
	`{"input":"hi"}`,
	`null`, `{`, ``, "{\"model\":\"m\",\"input\":\"\xff\"}",
}

func FuzzEmbeddingRequest(f *testing.F) {
	srv, fixed := fuzzServer(f), sourceText(f)
	for _, s := range embedSeeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, body []byte) { checkAnswer(t, srv, fixed, "/v1/embeddings", body) })
}
