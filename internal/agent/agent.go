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
	// EventCompact signals that the conversation history was compacted.
	EventCompact
	// EventToolProgress carries a partial output line from a streaming tool.
	EventToolProgress
)

// Event is emitted on the events channel during a Run call.
type Event struct {
	Type     EventType
	Text     string // EventText: model text content
	ToolName string // EventToolCall: name of the tool
	ToolArgs string // EventToolCall: JSON-encoded tool input
	ToolID   string // EventToolCall / EventToolResult: tool_use id
	Result        string // EventToolResult: tool output text
	IsError       bool   // EventToolResult / EventError: true when the operation failed
	Error         error  // EventError: the underlying error
	Usage         Usage  // cumulative token usage, updated after each turn
	CompactedFrom int    // EventCompact: number of messages before compaction
	CompactedTo   int    // EventCompact: number of messages after compaction
	ProgressOutput string // EventToolProgress: partial output line from a streaming tool
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

// WithToolRunnerCtx registers a context-aware tool runner. When set, it takes
// priority over the plain WithToolRunner callback. The context carries the
// agent loop's cancellation and can be used for per-tool timeouts.
func WithToolRunnerCtx(fn func(ctx context.Context, name string, args string) (string, error)) AgentOption {
	return func(a *Agent) { a.toolRunnerCtx = fn }
}

// WithMaxTurns caps the number of ReAct loop iterations.
func WithMaxTurns(n int) AgentOption {
	return func(a *Agent) { a.maxTurns = n }
}

// WithPermissionFn registers a callback that is called before each tool
// execution. If the callback returns (false, nil) the tool call is denied and
// the model receives a tool_result error. If it returns a non-nil error the
// agent loop is stopped with that error.
func WithPermissionFn(fn func(name, args string) (bool, error)) AgentOption {
	return func(a *Agent) { a.permissionFn = fn }
}

// WithAutoCompact enables automatic context compaction when the estimated
// token count of the message history exceeds threshold. A threshold of 0
// disables auto-compaction.
func WithAutoCompact(threshold int) AgentOption {
	return func(a *Agent) {
		a.autoCompact = threshold > 0
		a.compactThreshold = threshold
	}
}

// ToolStreamResult is the return type of a ToolStreamer callback.
type ToolStreamResult struct {
	Result string
	Err    error
}

// WithToolStreamer registers a callback that checks if a tool supports
// streaming and runs it with progress output. When set and the callback
// returns a non-nil ToolStreamResult, it is used instead of toolRunnerCtx.
// Progress channel items are emitted as EventToolProgress events.
//
// The callback should return nil (zero value) when the named tool does not
// support streaming, allowing the agent to fall through to toolRunnerCtx.
func WithToolStreamer(fn func(ctx context.Context, name, args string, progress chan<- string) *ToolStreamResult) AgentOption {
	return func(a *Agent) { a.toolStreamer = fn }
}

// Agent runs the ReAct (Reasoning + Acting) loop against a Provider.
type Agent struct {
	provider         Provider
	systemPrompt     string
	model            string
	maxTokens        int
	temperature      float64
	tools            []ToolDef
	toolRunner       func(name string, args string) (string, error)
	toolRunnerCtx    func(ctx context.Context, name string, args string) (string, error)
	toolStreamer      func(ctx context.Context, name, args string, progress chan<- string) *ToolStreamResult
	permissionFn     func(name string, args string) (bool, error)
	maxTurns         int
	autoCompact      bool
	compactThreshold int
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
//
// When the provider implements StreamProvider, Run uses streaming so that
// EventText events are emitted character-by-character as the model generates
// them. Otherwise it falls back to the non-streaming Complete path.
func (a *Agent) Run(ctx context.Context, userMessage string, history []Message, events chan<- Event) {
	// Build the initial message list.
	messages := make([]Message, len(history))
	copy(messages, history)
	messages = append(messages, Message{
		Role:    "user",
		Content: []ContentBlock{{Type: "text", Text: userMessage}},
	})

	// Prefer streaming when the provider supports it.
	if sp, ok := a.provider.(StreamProvider); ok {
		a.runStreaming(ctx, sp, messages, events)
		return
	}
	a.runNonStreaming(ctx, messages, events)
}

// maybeCompact checks whether the message history exceeds the compaction
// threshold and, if so, compacts it in-place. It returns the (possibly new)
// message slice. A non-nil error is fatal; callers should emit EventError and
// return.
func (a *Agent) maybeCompact(ctx context.Context, messages []Message, cumulativeUsage Usage, events chan<- Event) ([]Message, Usage, bool) {
	if !a.autoCompact || EstimateTokens(messages) < a.compactThreshold {
		return messages, cumulativeUsage, false
	}

	from := len(messages)
	// Keep the 10 most recent messages verbatim.
	compacted, compactUsage, err := CompactHistory(ctx, a.provider, a.model, messages, 10)
	if err != nil {
		events <- Event{Type: EventError, IsError: true, Error: fmt.Errorf("auto-compact: %w", err), Usage: cumulativeUsage}
		return messages, cumulativeUsage, true
	}

	cumulativeUsage.InputTokens += compactUsage.InputTokens
	cumulativeUsage.OutputTokens += compactUsage.OutputTokens

	events <- Event{
		Type:          EventCompact,
		CompactedFrom: from,
		CompactedTo:   len(compacted),
		Usage:         cumulativeUsage,
	}

	return compacted, cumulativeUsage, false
}

// runNonStreaming runs the ReAct loop using the non-streaming Complete path.
func (a *Agent) runNonStreaming(ctx context.Context, messages []Message, events chan<- Event) {
	var cumulativeUsage Usage

	for turn := 0; turn < a.maxTurns; turn++ {
		if ctx.Err() != nil {
			events <- Event{Type: EventError, IsError: true, Error: ctx.Err(), Usage: cumulativeUsage}
			return
		}

		// Auto-compact if the history is getting too large.
		var stop bool
		messages, cumulativeUsage, stop = a.maybeCompact(ctx, messages, cumulativeUsage, events)
		if stop {
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

		resultBlocks, stop := a.executeToolCalls(ctx, toolCalls, cumulativeUsage, events)
		if stop {
			return
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

// runStreaming runs the ReAct loop using the streaming Stream path.
//
// Each streaming turn:
//  1. Opens a stream via sp.Stream, which emits StreamEvents on a local channel.
//  2. Text deltas are forwarded as EventText events immediately.
//  3. Tool use events are collected; on tool_use_end the full call is emitted
//     as an EventToolCall and recorded so it can be appended to history.
//  4. When "message_done" arrives, the accumulated content blocks are appended
//     to the conversation history and the loop decides whether to continue.
func (a *Agent) runStreaming(ctx context.Context, sp StreamProvider, messages []Message, events chan<- Event) {
	var cumulativeUsage Usage

	for turn := 0; turn < a.maxTurns; turn++ {
		if ctx.Err() != nil {
			events <- Event{Type: EventError, IsError: true, Error: ctx.Err(), Usage: cumulativeUsage}
			return
		}

		// Auto-compact if the history is getting too large.
		var stop bool
		messages, cumulativeUsage, stop = a.maybeCompact(ctx, messages, cumulativeUsage, events)
		if stop {
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

		// Buffers accumulated during this stream turn.
		var (
			textBuf    []byte
			toolCalls  []ContentBlock
			stopReason string
			turnUsage  Usage
		)

		// pendingTool is the tool_use block currently being streamed.
		type pendingToolState struct {
			id       string
			name     string
			inputBuf []byte
		}
		var pending *pendingToolState

		// Run the stream in a goroutine so we can iterate over the channel.
		streamCh := make(chan StreamEvent, 64)
		streamErrCh := make(chan error, 1)

		go func() {
			streamErrCh <- sp.Stream(ctx, req, streamCh)
			close(streamCh)
		}()

		for se := range streamCh {
			switch se.Type {
			case "text_delta":
				textBuf = append(textBuf, se.Text...)
				events <- Event{
					Type:  EventText,
					Text:  se.Text,
					Usage: cumulativeUsage,
				}

			case "tool_use_start":
				pending = &pendingToolState{
					id:   se.ToolID,
					name: se.ToolName,
				}

			case "tool_use_delta":
				if pending != nil {
					pending.inputBuf = append(pending.inputBuf, se.ToolInput...)
				}

			case "tool_use_end":
				if pending != nil {
					inputJSON := json.RawMessage(pending.inputBuf)
					if len(inputJSON) == 0 {
						inputJSON = json.RawMessage("{}")
					}
					tc := ContentBlock{
						Type:  "tool_use",
						ID:    pending.id,
						Name:  pending.name,
						Input: inputJSON,
					}
					toolCalls = append(toolCalls, tc)
					events <- Event{
						Type:     EventToolCall,
						ToolName: tc.Name,
						ToolArgs: string(tc.Input),
						ToolID:   tc.ID,
						Usage:    cumulativeUsage,
					}
					pending = nil
				}

			case "message_done":
				stopReason = se.StopReason
				turnUsage = se.Usage
			}
		}

		if err := <-streamErrCh; err != nil {
			events <- Event{
				Type:    EventError,
				IsError: true,
				Error:   fmt.Errorf("turn %d: %w", turn, err),
				Usage:   cumulativeUsage,
			}
			return
		}

		// Accumulate usage from this turn.
		cumulativeUsage.InputTokens += turnUsage.InputTokens
		cumulativeUsage.OutputTokens += turnUsage.OutputTokens

		// Build the assistant content blocks for history.
		var assistantContent []ContentBlock
		if len(textBuf) > 0 {
			assistantContent = append(assistantContent, ContentBlock{
				Type: "text",
				Text: string(textBuf),
			})
		}
		assistantContent = append(assistantContent, toolCalls...)

		// Append the assistant turn to history.
		messages = append(messages, Message{
			Role:    "assistant",
			Content: assistantContent,
		})

		// Decide whether to continue.
		if stopReason == "end_turn" || stopReason == "stop_sequence" {
			events <- Event{Type: EventDone, Usage: cumulativeUsage}
			return
		}

		if stopReason == "max_tokens" && len(toolCalls) == 0 {
			events <- Event{Type: EventDone, Usage: cumulativeUsage}
			return
		}

		if len(toolCalls) == 0 {
			// No tool calls and no end_turn — treat as done to avoid spinning.
			events <- Event{Type: EventDone, Usage: cumulativeUsage}
			return
		}

		// Execute tool calls and build the tool_result user turn.
		resultBlocks, stop := a.executeToolCalls(ctx, toolCalls, cumulativeUsage, events)
		if stop {
			return
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

// executeToolCalls runs permission checks and executes each tool call.
// It returns the result content blocks and a stop flag. When stop is true
// the caller must return immediately (a fatal error was emitted to events).
func (a *Agent) executeToolCalls(ctx context.Context, toolCalls []ContentBlock, cumulativeUsage Usage, events chan<- Event) ([]ContentBlock, bool) {
	resultBlocks := make([]ContentBlock, 0, len(toolCalls))
	for _, tc := range toolCalls {
		// Check permission before running the tool.
		if a.permissionFn != nil {
			approved, permErr := a.permissionFn(tc.Name, string(tc.Input))
			if permErr != nil {
				events <- Event{Type: EventError, IsError: true, Error: fmt.Errorf("permission check: %w", permErr), Usage: cumulativeUsage}
				return nil, true
			}
			if !approved {
				const deniedMsg = "Tool call denied by user"
				events <- Event{
					Type:     EventToolResult,
					ToolID:   tc.ID,
					ToolName: tc.Name,
					Result:   deniedMsg,
					IsError:  true,
					Usage:    cumulativeUsage,
				}
				resultBlocks = append(resultBlocks, ContentBlock{
					Type:      "tool_result",
					ToolUseID: tc.ID,
					Content:   deniedMsg,
					IsError:   true,
				})
				continue
			}
		}

		result, toolErr := a.runTool(ctx, tc.Name, tc.ID, string(tc.Input), cumulativeUsage, events)
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
	return resultBlocks, false
}

// runTool executes a single tool call. When toolStreamer is set it is tried
// first; if it returns nil (tool doesn't support streaming) the call falls
// through to toolRunnerCtx / toolRunner. Progress lines from streaming tools
// are emitted as EventToolProgress events.
func (a *Agent) runTool(ctx context.Context, name, id, args string, cumulativeUsage Usage, events chan<- Event) (string, error) {
	if a.toolRunnerCtx == nil && a.toolRunner == nil && a.toolStreamer == nil {
		return "", fmt.Errorf("no tool runner registered (tool: %s)", name)
	}

	// Validate that args is well-formed JSON before passing to the runner.
	if args != "" && !json.Valid([]byte(args)) {
		return "", fmt.Errorf("tool %s: malformed JSON args: %s", name, args)
	}

	// Try the streaming path first.
	if a.toolStreamer != nil {
		progress := make(chan string, 32)
		// Drain progress in a goroutine that emits EventToolProgress events.
		progressDone := make(chan struct{})
		go func() {
			defer close(progressDone)
			for line := range progress {
				events <- Event{
					Type:           EventToolProgress,
					ToolID:         id,
					ToolName:       name,
					ProgressOutput: line,
					Usage:          cumulativeUsage,
				}
			}
		}()

		streamRes := a.toolStreamer(ctx, name, args, progress)
		close(progress)
		<-progressDone

		if streamRes != nil {
			return streamRes.Result, streamRes.Err
		}
		// streamRes == nil means tool doesn't support streaming; fall through.
	}

	if a.toolRunnerCtx != nil {
		return a.toolRunnerCtx(ctx, name, args)
	}
	return a.toolRunner(name, args)
}
