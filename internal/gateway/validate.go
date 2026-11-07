package gateway

import (
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/policy"
)

// This file is F1 step 4, validate (req_001 D-39): it refuses what the engine port cannot
// carry, caps max_tokens and pre-checks the prompt size. Nothing here logs or quotes request
// content; the answers are fixed text and field names from the spec.
//
// The OpenAI fields that api/openapi.yaml lists but engine.ChatRequest cannot carry yet
// (sampling options, stop, response_format other than text, tools, images) are refused with
// unsupported_parameter, never dropped (req_001 D-32). PLT-F-059 adds sampling options to the
// engine port; the refusals for the fields it carries then go away.

// chatInput is a chat request that passed validation.
type chatInput struct {
	model    string
	messages []engine.Message
	// stream is stream: true; includeUsage is stream_options.include_usage.
	stream, includeUsage bool
	// maxTokens is what the request asked for; 0 means it asked for nothing.
	maxTokens int64
	// promptBytes is the size of all message text, the base of the prompt-size estimate.
	promptBytes int64
}

// validateChat checks req against what the gateway and the engine port support.
func validateChat(req *api.ChatCompletionRequest) (chatInput, *apiError) {
	var in chatInput
	if strings.TrimSpace(req.Model) == "" {
		return in, invalidParam("model", "model is required.")
	}
	in.model = req.Model
	in.stream = req.Stream.IsSet() && !req.Stream.Null && req.Stream.Value
	if req.StreamOptions.IsSet() && !req.StreamOptions.Null {
		if !in.stream {
			return in, invalidParam("stream_options", "stream_options is only allowed with stream: true.")
		}
		in.includeUsage = req.StreamOptions.Value.IncludeUsage.Value
	}
	for _, f := range []struct {
		name string
		set  bool
	}{
		{"frequency_penalty", req.FrequencyPenalty.IsSet() && !req.FrequencyPenalty.Null},
		{"presence_penalty", req.PresencePenalty.IsSet() && !req.PresencePenalty.Null},
		{"seed", req.Seed.IsSet() && !req.Seed.Null},
		{"temperature", req.Temperature.IsSet() && !req.Temperature.Null},
		{"top_p", req.TopP.IsSet() && !req.TopP.Null},
		{"stop", req.Stop.IsSet()},
		{"tools", len(req.Tools) > 0},
	} {
		if f.set {
			return in, unsupportedParam(f.name, "This field is not supported by this host's model engine yet.")
		}
	}
	if rf := req.ResponseFormat; rf.IsSet() && rf.Value.Type != api.ResponseFormatTextResponseFormat {
		return in, unsupportedParam("response_format", "Only response_format {\"type\": \"text\"} is supported by this host's model engine yet.")
	}
	mt, mct := req.MaxTokens, req.MaxCompletionTokens
	hasMT, hasMCT := mt.IsSet() && !mt.Null, mct.IsSet() && !mct.Null
	switch {
	case hasMT && hasMCT:
		return in, invalidParam("max_completion_tokens", "Send max_tokens or max_completion_tokens, not both.")
	case hasMT:
		in.maxTokens = int64(mt.Value)
	case hasMCT:
		in.maxTokens = int64(mct.Value)
	}
	if in.maxTokens < 0 {
		return in, invalidParam("max_tokens", "max_tokens must be at least 1.")
	}
	if len(req.Messages) == 0 {
		return in, invalidParam("messages", "messages must hold at least one message.")
	}
	for _, m := range req.Messages {
		msg, e := flattenMessage(m)
		if e != nil {
			return in, e
		}
		in.messages = append(in.messages, msg)
		in.promptBytes += int64(len(msg.Content))
	}
	return in, nil
}

// flattenMessage turns one message into the engine's text-only form. Text parts are joined
// without a separator.
func flattenMessage(m api.ChatMessage) (engine.Message, *apiError) {
	notSupported := func(what string) (engine.Message, *apiError) {
		return engine.Message{}, unsupportedParam("messages", what+" are not supported by this host's model engine yet.")
	}
	switch m.Type {
	case api.SystemMessageChatMessage:
		return engine.Message{Role: engine.RoleSystem, Content: textOf(m.SystemMessage.Content)}, nil
	case api.UserMessageChatMessage:
		c := m.UserMessage.Content
		if c.Type == api.StringUserMessageContent {
			return engine.Message{Role: engine.RoleUser, Content: c.String}, nil
		}
		var sb strings.Builder
		for _, p := range c.UserContentPartArray {
			if p.Type != api.TextContentPartUserContentPart {
				return notSupported("Image parts")
			}
			sb.WriteString(p.TextContentPart.Text)
		}
		return engine.Message{Role: engine.RoleUser, Content: sb.String()}, nil
	case api.AssistantMessageChatMessage:
		a := m.AssistantMessage
		if len(a.ToolCalls) > 0 {
			return notSupported("Tool calls")
		}
		var text string
		if c, ok := a.Content.Get(); ok {
			switch c.Type {
			case api.StringAssistantMessageContent:
				text = c.String
			case api.TextContentPartArrayAssistantMessageContent:
				var sb strings.Builder
				for _, p := range c.TextContentPartArray {
					sb.WriteString(p.Text)
				}
				text = sb.String()
			}
		}
		return engine.Message{Role: engine.RoleAssistant, Content: text}, nil
	case api.ToolMessageChatMessage:
		return notSupported("Tool messages")
	}
	return engine.Message{}, invalidParam("messages", "A message has an unknown role.")
}

func textOf(c api.TextContent) string {
	if c.Type == api.StringTextContent {
		return c.String
	}
	var sb strings.Builder
	for _, p := range c.TextContentPartArray {
		sb.WriteString(p.Text)
	}
	return sb.String()
}

// embedInput is an embeddings request that passed validation.
type embedInput struct {
	model  string
	inputs []string
	base64 bool
	// maxInputBytes is the size of the largest input, the base of the prompt-size estimate.
	maxInputBytes int64
	// totalInputBytes is the size of all input texts, the base of the prompt count estimated for
	// an aborted call (req_001 D-108). Only this number is kept, never the text.
	totalInputBytes int64
}

func validateEmbed(req *api.EmbeddingRequest, maxInputs int64) (embedInput, *apiError) {
	var in embedInput
	if strings.TrimSpace(req.Model) == "" {
		return in, invalidParam("model", "model is required.")
	}
	in.model = req.Model
	if req.Dimensions.IsSet() {
		return in, unsupportedParam("dimensions", "This field is not supported by this host's model engine yet.")
	}
	in.base64 = req.EncodingFormat.IsSet() && req.EncodingFormat.Value == api.EmbeddingRequestEncodingFormatBase64
	switch req.Input.Type {
	case api.StringEmbeddingRequestInput:
		in.inputs = []string{req.Input.String}
	case api.StringArrayEmbeddingRequestInput:
		in.inputs = req.Input.StringArray
	}
	if len(in.inputs) == 0 {
		return in, invalidParam("input", "input must hold at least one text.")
	}
	if int64(len(in.inputs)) > maxInputs {
		return in, invalidParam("input", fmt.Sprintf("input may hold at most %d texts.", maxInputs))
	}
	for _, s := range in.inputs {
		if s == "" {
			return in, invalidParam("input", "A text in input is empty.")
		}
		in.maxInputBytes = max(in.maxInputBytes, int64(len(s)))
		in.totalInputBytes += int64(len(s))
	}
	return in, nil
}

// plan is what a validated request runs with.
type plan struct {
	numCtx     int
	numPredict int
	overflow   engine.OverflowPolicy
	// background is true when the policy decided the background priority class.
	background bool
}

// estimateTokens turns bytes of text into an estimated token count, rounding up.
func estimateTokens(bytes, charsPerToken int64) int64 {
	if bytes <= 0 {
		return 0
	}
	n := bytes / charsPerToken
	if bytes%charsPerToken != 0 {
		n++
	}
	return n
}

// abortedPromptTokens is the prompt count billed for an aborted call (req_001 D-108): the
// estimate of promptBytes, never more than limit, the most prompt tokens the engine could
// have processed (its context size; with on_context_overflow truncate it drops the rest).
// With refuse the pre-check already keeps the estimate within limit. limit <= 0 means no cap.
func abortedPromptTokens(promptBytes, charsPerToken, limit int64) int64 {
	t := estimateTokens(promptBytes, charsPerToken)
	if limit > 0 && t > limit {
		return limit
	}
	return t
}

// planCommon fixes the context size and the overflow policy, and pre-checks the prompt size
// (req_001 D-39 step 4, D-48): with overflow policy refuse, a prompt estimated to exceed the
// context cap is context_too_long; with truncate the engine may drop old messages, so there is
// nothing to check. The estimate is not exact; the engine's own count decides in the end.
func planCommon(cfg config, dec policy.Decision, overflow, param string, promptBytes int64) (plan, *apiError) {
	p := plan{overflow: engine.OverflowRefuse, background: dec.Priority == policy.Background}
	ctxCap := cfg.defaultContext
	if c := dec.Caps.ContextCap; c != nil && *c > 0 {
		ctxCap = *c
	}
	ctxCap = min(ctxCap, math.MaxInt32)
	p.numCtx = int(ctxCap)
	if overflow == string(engine.OverflowTruncate) {
		p.overflow = engine.OverflowTruncate
		return p, nil
	}
	if estimateTokens(promptBytes, cfg.charsPerToken) > ctxCap {
		return p, newErr(http.StatusBadRequest, typeInvalidRequest, codeContextTooLong, param,
			fmt.Sprintf("The prompt is longer than the context limit of %d tokens.", ctxCap))
	}
	return p, nil
}

// planChat adds the output cap: the request's max_tokens (or the default when it sent none),
// capped by the grant's and the rules' output cap, or by the host's ceiling when they set none.
func planChat(cfg config, dec policy.Decision, overflow string, in chatInput) (plan, *apiError) {
	p, e := planCommon(cfg, dec, overflow, "messages", in.promptBytes)
	if e != nil {
		return p, e
	}
	limit := cfg.maxTokensCeiling
	if c := dec.Caps.MaxOutputTokens; c != nil {
		limit = *c
	}
	want := in.maxTokens
	if want == 0 {
		want = cfg.defaultMaxTokens
	}
	p.numPredict = int(min(max(min(want, limit), 1), math.MaxInt32))
	return p, nil
}

// planEmbed checks every input against the context cap, by the size of the largest.
func planEmbed(cfg config, dec policy.Decision, overflow string, in embedInput) (plan, *apiError) {
	return planCommon(cfg, dec, overflow, "input", in.maxInputBytes)
}
