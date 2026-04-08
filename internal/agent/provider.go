package agent

import (
	"context"
	"encoding/json"
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
