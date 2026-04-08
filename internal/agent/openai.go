package agent

import (
	"context"
	"encoding/json"
	"fmt"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"
)

const xaiBaseURL = "https://api.x.ai/v1"

// OpenAIProvider implements Provider using the official OpenAI Go SDK.
// It also supports xAI (Grok) by pointing the base URL at https://api.x.ai/v1.
type OpenAIProvider struct {
	client openai.Client
}

// OpenAIOption configures an OpenAIProvider.
type OpenAIOption func(*openaiConfig)

type openaiConfig struct {
	baseURL string
}

// WithBaseURL overrides the API base URL. Use this to point at xAI or any
// other OpenAI-compatible endpoint.
func WithBaseURL(url string) OpenAIOption {
	return func(c *openaiConfig) {
		c.baseURL = url
	}
}

// NewOpenAIProvider returns a Provider backed by the OpenAI Chat Completions API.
// If apiKey is empty the SDK falls back to the OPENAI_API_KEY environment variable.
func NewOpenAIProvider(apiKey string, opts ...OpenAIOption) *OpenAIProvider {
	cfg := &openaiConfig{}
	for _, o := range opts {
		o(cfg)
	}

	reqOpts := []option.RequestOption{}
	if apiKey != "" {
		reqOpts = append(reqOpts, option.WithAPIKey(apiKey))
	}
	if cfg.baseURL != "" {
		reqOpts = append(reqOpts, option.WithBaseURL(cfg.baseURL))
	}

	return &OpenAIProvider{
		client: openai.NewClient(reqOpts...),
	}
}

// NewXAIProvider returns an OpenAIProvider pre-configured to use the xAI API.
// If apiKey is empty the SDK falls back to the OPENAI_API_KEY environment variable.
func NewXAIProvider(apiKey string) *OpenAIProvider {
	return NewOpenAIProvider(apiKey, WithBaseURL(xaiBaseURL))
}

// Complete sends a non-streaming request to the OpenAI Chat Completions API
// and returns the response mapped to our internal types.
func (p *OpenAIProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	messages, err := toOpenAIMessages(req)
	if err != nil {
		return nil, fmt.Errorf("openai: build messages: %w", err)
	}

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(req.Model),
		Messages: messages,
	}

	if req.MaxTokens > 0 {
		params.MaxTokens = param.NewOpt(int64(req.MaxTokens))
	}

	if req.Temperature != 0 {
		params.Temperature = param.NewOpt(req.Temperature)
	}

	if len(req.Tools) > 0 {
		params.Tools = toOpenAITools(req.Tools)
	}

	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai: chat.completions.new: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai: empty choices in response")
	}

	choice := resp.Choices[0]
	out := &CompletionResponse{
		StopReason: mapFinishReason(choice.FinishReason),
		Usage: Usage{
			InputTokens:  int(resp.Usage.PromptTokens),
			OutputTokens: int(resp.Usage.CompletionTokens),
		},
	}

	// Collect text content, if any.
	if choice.Message.Content != "" {
		out.Content = append(out.Content, ContentBlock{
			Type: "text",
			Text: choice.Message.Content,
		})
	}

	// Collect tool calls, if any.
	for _, tc := range choice.Message.ToolCalls {
		out.Content = append(out.Content, ContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: json.RawMessage(tc.Function.Arguments),
		})
	}

	return out, nil
}

// toOpenAIMessages converts our Message slice (plus optional system prompt) to
// the OpenAI ChatCompletionMessageParamUnion slice.
func toOpenAIMessages(req *CompletionRequest) ([]openai.ChatCompletionMessageParamUnion, error) {
	var msgs []openai.ChatCompletionMessageParamUnion

	// System prompt goes as the first message.
	if req.System != "" {
		sys := openai.ChatCompletionSystemMessageParam{}
		sys.Content.OfString = param.NewOpt(req.System)
		msgs = append(msgs, openai.ChatCompletionMessageParamUnion{OfSystem: &sys})
	}

	for _, m := range req.Messages {
		converted, err := toOpenAIMessage(m)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, converted...)
	}

	return msgs, nil
}

// toOpenAIMessage converts a single internal Message to one or more OpenAI
// message params. A single internal message may expand to multiple OpenAI
// messages because tool results use a distinct "tool" role message per result.
func toOpenAIMessage(m Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	switch m.Role {
	case "assistant":
		return toOpenAIAssistantMessages(m.Content)
	default: // "user"
		return toOpenAIUserMessages(m.Content)
	}
}

// toOpenAIAssistantMessages converts assistant-role content blocks.
// Text blocks and tool_use blocks are merged into a single assistant message.
// tool_result blocks are silently skipped (they shouldn't appear on assistant
// turns; they come in on subsequent user turns).
func toOpenAIAssistantMessages(blocks []ContentBlock) ([]openai.ChatCompletionMessageParamUnion, error) {
	asst := openai.ChatCompletionAssistantMessageParam{}
	hasContent := false

	for _, b := range blocks {
		switch b.Type {
		case "text":
			asst.Content.OfString = param.NewOpt(b.Text)
			hasContent = true

		case "tool_use":
			tc := openai.ChatCompletionMessageToolCallParam{
				ID: b.ID,
				Function: openai.ChatCompletionMessageToolCallFunctionParam{
					Name:      b.Name,
					Arguments: string(b.Input),
				},
			}
			asst.ToolCalls = append(asst.ToolCalls, tc)
			hasContent = true
		}
	}

	if !hasContent {
		return nil, nil
	}
	return []openai.ChatCompletionMessageParamUnion{{OfAssistant: &asst}}, nil
}

// toOpenAIUserMessages converts user-role content blocks.
// Plain text blocks become a single user message with string content.
// tool_result blocks each become their own "tool" role message.
func toOpenAIUserMessages(blocks []ContentBlock) ([]openai.ChatCompletionMessageParamUnion, error) {
	var out []openai.ChatCompletionMessageParamUnion
	var textParts []string

	flushText := func() {
		if len(textParts) == 0 {
			return
		}
		combined := ""
		for _, t := range textParts {
			combined += t
		}
		user := openai.ChatCompletionUserMessageParam{}
		user.Content.OfString = param.NewOpt(combined)
		out = append(out, openai.ChatCompletionMessageParamUnion{OfUser: &user})
		textParts = nil
	}

	for _, b := range blocks {
		switch b.Type {
		case "text":
			textParts = append(textParts, b.Text)

		case "tool_result":
			// Flush any accumulated text first.
			flushText()

			toolMsg := openai.ChatCompletionToolMessageParam{
				ToolCallID: b.ToolUseID,
			}
			toolMsg.Content.OfString = param.NewOpt(b.Content)
			out = append(out, openai.ChatCompletionMessageParamUnion{OfTool: &toolMsg})
		}
	}

	flushText()
	return out, nil
}

// toOpenAITools converts our ToolDef slice to OpenAI ChatCompletionToolParam slice.
func toOpenAITools(tools []ToolDef) []openai.ChatCompletionToolParam {
	out := make([]openai.ChatCompletionToolParam, 0, len(tools))
	for _, t := range tools {
		fn := shared.FunctionDefinitionParam{
			Name:       t.Name,
			Parameters: shared.FunctionParameters(t.InputSchema),
		}
		if t.Description != "" {
			fn.Description = param.NewOpt(t.Description)
		}
		out = append(out, openai.ChatCompletionToolParam{
			Function: fn,
		})
	}
	return out
}

// mapFinishReason maps OpenAI finish reasons to our internal stop reason strings.
func mapFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return reason
	}
}
