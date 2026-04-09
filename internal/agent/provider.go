package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Message is a single turn in a conversation.
type Message struct {
	Role    string         `json:"role"`    // "user" or "assistant"
	Content []ContentBlock `json:"content"`
}

// ContentBlock is one typed piece of content inside a Message.
type ContentBlock struct {
	// Type is one of "text", "tool_use", or "tool_result".
	Type string `json:"type"`

	// text block
	Text string `json:"text,omitempty"`

	// tool_use block
	ID    string          `json:"id,omitempty"`    // tool invocation id
	Name  string          `json:"name,omitempty"`  // tool name
	Input json.RawMessage `json:"input,omitempty"` // JSON-encoded tool input

	// tool_result block
	ToolUseID string `json:"tool_use_id,omitempty"` // id of the tool_use this responds to
	Content   string `json:"content,omitempty"`     // result text (also used for text blocks when needed)
	IsError   bool   `json:"is_error,omitempty"`    // true when the tool returned an error
}

// ToolDef describes a tool that the model may invoke.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// CompletionRequest is the input to a single model call.
type CompletionRequest struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []ToolDef
	MaxTokens   int
	Temperature float64
}

// CompletionResponse is the output from a single model call.
type CompletionResponse struct {
	Content    []ContentBlock
	StopReason string // "end_turn", "tool_use", "max_tokens"
	Usage      Usage
}

// Usage tracks token consumption for a single model call.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Provider is the interface that wraps a single LLM completion call.
// Implementations must be safe for concurrent use.
type Provider interface {
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
}

// StreamEvent is a single event emitted during a streaming completion.
type StreamEvent struct {
	// Type is one of "text_delta", "tool_use_start", "tool_use_delta",
	// "tool_use_end", or "message_done".
	Type string

	// Text is the text fragment for "text_delta" events.
	Text string

	// ToolID is the tool invocation ID for tool_use_* events.
	ToolID string
	// ToolName is the tool name for "tool_use_start" events.
	ToolName string
	// ToolInput is the accumulated JSON input for "tool_use_end" events and
	// the partial JSON fragment for "tool_use_delta" events.
	ToolInput string

	// StopReason is set on "message_done" events.
	StopReason string
	// Usage is set on "message_done" events with the final token counts.
	Usage Usage
}

// StreamProvider is an optional extension of Provider that supports
// incremental streaming of completion events.
// Implementations must be safe for concurrent use.
type StreamProvider interface {
	Provider
	Stream(ctx context.Context, req *CompletionRequest, events chan<- StreamEvent) error
}

// NewProvider creates a Provider from a prefixed model string such as:
//
//	"anthropic/claude-sonnet-4-20250514"  -> AnthropicProvider
//	"openai/gpt-4o"                       -> OpenAIProvider
//	"xai/grok-3"                          -> OpenAIProvider (xAI base URL)
//
// It returns the provider, the bare model name (without the prefix), and any
// error. When no prefix is present the model is assumed to be Anthropic.
func NewProvider(model string, anthropicKey, openaiKey, xaiKey string) (Provider, string, error) {
	prefix, bare, found := strings.Cut(model, "/")
	if !found {
		// No prefix -- treat as Anthropic for backward compatibility.
		return NewAnthropicProvider(anthropicKey), model, nil
	}

	switch strings.ToLower(prefix) {
	case "anthropic":
		return NewAnthropicProvider(anthropicKey), bare, nil
	case "openai":
		return NewOpenAIProvider(openaiKey), bare, nil
	case "xai":
		// Use dedicated xAI key if set, fall back to OpenAI key.
		key := xaiKey
		if key == "" {
			key = openaiKey
		}
		return NewXAIProvider(key), bare, nil
	default:
		return nil, "", fmt.Errorf("unknown provider prefix %q in model %q", prefix, model)
	}
}
