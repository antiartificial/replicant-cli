package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

// EventType identifies the kind of event emitted by the agent loop.
type EventType int

const (
	// EventText carries a text fragment from the model.
	EventText EventType = iota
	// EventToolCall signals that the model wants to invoke a tool.
	EventToolCall
	// EventToolResult carries the output of a tool execution.
	EventToolResult
	// EventDone signals that the agent loop has completed normally.
	EventDone
	// EventError signals that a fatal error stopped the loop.
	EventError
)

// Event is emitted on the events channel during a Run call.
type Event struct {
	Type     EventType
	Text     string // EventText: model text content
	ToolName string // EventToolCall: name of the tool
	ToolArgs string // EventToolCall: JSON-encoded tool input
	ToolID   string // EventToolCall / EventToolResult: tool_use id
	Result   string // EventToolResult: tool output text
	IsError  bool   // EventToolResult / EventError: true when the operation failed
	Error    error  // EventError: the underlying error
	Usage    Usage  // cumulative token usage, updated after each turn
}

// AgentOption is a functional option for NewAgent.
type AgentOption func(*Agent)

// WithSystemPrompt sets the system prompt.
func WithSystemPrompt(prompt string) AgentOption {
	return func(a *Agent) { a.systemPrompt = prompt }
}

// WithModel overrides the model string.
func WithModel(model string) AgentOption {
	return func(a *Agent) { a.model = model }
}

// WithMaxTokens sets the per-turn token budget.
func WithMaxTokens(n int) AgentOption {
	return func(a *Agent) { a.maxTokens = n }
}

// WithTemperature sets the sampling temperature.
func WithTemperature(t float64) AgentOption {
	return func(a *Agent) { a.temperature = t }
}

// WithTools registers tools the model may call.
func WithTools(tools []ToolDef) AgentOption {
	return func(a *Agent) { a.tools = tools }
}

// WithToolRunner registers the callback that executes tool calls.
// name is the tool name; args is the JSON-encoded input; returns (result, error).
func WithToolRunner(fn func(name string, args string) (string, error)) AgentOption {
	return func(a *Agent) { a.toolRunner = fn }
}

// WithMaxTurns caps the number of ReAct loop iterations.
func WithMaxTurns(n int) AgentOption {
	return func(a *Agent) { a.maxTurns = n }
}

// Agent runs the ReAct (Reasoning + Acting) loop against a Provider.
type Agent struct {
	provider     Provider
	systemPrompt string
	model        string
	maxTokens    int
	temperature  float64
	tools        []ToolDef
	toolRunner   func(name string, args string) (string, error)
	maxTurns     int
}

// NewAgent constructs an Agent with the given provider and options.
// Defaults: model "claude-sonnet-4-20250514", maxTokens 8192, maxTurns 50.
func NewAgent(provider Provider, opts ...AgentOption) *Agent {
	a := &Agent{
		provider:  provider,
		model:     "claude-sonnet-4-20250514",
		maxTokens: 8192,
		maxTurns:  50,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Run executes the agent loop starting with userMessage appended to history.
//
// Events are sent to the events channel as they occur. The channel is not
// closed by Run; the caller should close it after Run returns if needed.
//
// The loop terminates when:
//   - The model returns stop_reason "end_turn"
//   - maxTurns is reached
//   - ctx is cancelled
//   - An unrecoverable error occurs
func (a *Agent) Run(ctx context.Context, userMessage string, history []Message, events chan<- Event) {
	// Build the initial message list.
	messages := make([]Message, len(history))
	copy(messages, history)
	messages = append(messages, Message{
		Role:    "user",
		Content: []ContentBlock{{Type: "text", Text: userMessage}},
	})

	var cumulativeUsage Usage

	for turn := 0; turn < a.maxTurns; turn++ {
		if ctx.Err() != nil {
			events <- Event{Type: EventError, IsError: true, Error: ctx.Err(), Usage: cumulativeUsage}
			return
		}

		req := &CompletionRequest{
			Model:       a.model,
			System:      a.systemPrompt,
			Messages:    messages,
			Tools:       a.tools,
			MaxTokens:   a.maxTokens,
			Temperature: a.temperature,
		}

		resp, err := a.provider.Complete(ctx, req)
		if err != nil {
			events <- Event{Type: EventError, IsError: true, Error: fmt.Errorf("turn %d: %w", turn, err), Usage: cumulativeUsage}
			return
		}

		// Accumulate usage.
		cumulativeUsage.InputTokens += resp.Usage.InputTokens
		cumulativeUsage.OutputTokens += resp.Usage.OutputTokens

		// Emit events for each content block and collect tool calls.
		var toolCalls []ContentBlock
		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				if block.Text != "" {
					events <- Event{
						Type:  EventText,
						Text:  block.Text,
						Usage: cumulativeUsage,
					}
				}
			case "tool_use":
				toolCalls = append(toolCalls, block)
				events <- Event{
					Type:     EventToolCall,
					ToolName: block.Name,
					ToolArgs: string(block.Input),
					ToolID:   block.ID,
					Usage:    cumulativeUsage,
				}
			}
		}

		// Append the assistant turn to history.
		messages = append(messages, Message{
			Role:    "assistant",
			Content: resp.Content,
		})

		// If the model is done, we're done.
		if resp.StopReason == "end_turn" || resp.StopReason == "stop_sequence" {
			events <- Event{Type: EventDone, Usage: cumulativeUsage}
			return
		}

		// If the model hit the token limit without tool calls, also stop.
		if resp.StopReason == "max_tokens" && len(toolCalls) == 0 {
			events <- Event{Type: EventDone, Usage: cumulativeUsage}
			return
		}

		// Execute tool calls and build the tool_result user turn.
		if len(toolCalls) == 0 {
			// No tool calls and no end_turn — treat as done to avoid spinning.
			events <- Event{Type: EventDone, Usage: cumulativeUsage}
			return
		}

		resultBlocks := make([]ContentBlock, 0, len(toolCalls))
		for _, tc := range toolCalls {
			result, toolErr := a.runTool(tc.Name, string(tc.Input))
			isErr := toolErr != nil

			var resultText string
			if toolErr != nil {
				resultText = toolErr.Error()
			} else {
				resultText = result
			}

			events <- Event{
				Type:     EventToolResult,
				ToolID:   tc.ID,
				ToolName: tc.Name,
				Result:   resultText,
				IsError:  isErr,
				Usage:    cumulativeUsage,
			}

			resultBlocks = append(resultBlocks, ContentBlock{
				Type:      "tool_result",
				ToolUseID: tc.ID,
				Content:   resultText,
				IsError:   isErr,
			})
		}

		// Feed tool results back as a user message.
		messages = append(messages, Message{
			Role:    "user",
			Content: resultBlocks,
		})
	}

	// maxTurns reached.
	events <- Event{
		Type:    EventError,
		IsError: true,
		Error:   fmt.Errorf("agent: max turns (%d) reached without end_turn", a.maxTurns),
		Usage:   cumulativeUsage,
	}
}

// runTool executes a single tool call. If no toolRunner is registered it
// returns an error so the model receives a graceful tool_result error.
func (a *Agent) runTool(name, args string) (string, error) {
	if a.toolRunner == nil {
		return "", fmt.Errorf("no tool runner registered (tool: %s)", name)
	}

	// Validate that args is well-formed JSON before passing to the runner.
	if args != "" && !json.Valid([]byte(args)) {
		return "", fmt.Errorf("tool %s: malformed JSON args: %s", name, args)
	}

	return a.toolRunner(name, args)
}
