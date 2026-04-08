package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// MockProvider — non-streaming, returns canned responses.
// ---------------------------------------------------------------------------

// mockResponse describes one pre-programmed response the mock provider returns.
type mockResponse struct {
	content    []ContentBlock
	stopReason string
	err        error
}

// MockProvider implements Provider by returning responses from a queue.
// When the queue is exhausted it returns a generic end_turn response.
type MockProvider struct {
	responses []mockResponse
	calls     int
}

func (m *MockProvider) Complete(_ context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	idx := m.calls
	m.calls++
	if idx < len(m.responses) {
		r := m.responses[idx]
		if r.err != nil {
			return nil, r.err
		}
		return &CompletionResponse{
			Content:    r.content,
			StopReason: r.stopReason,
			Usage:      Usage{InputTokens: 10, OutputTokens: 5},
		}, nil
	}
	// Fallback: end the conversation.
	return &CompletionResponse{
		Content:    []ContentBlock{{Type: "text", Text: "done"}},
		StopReason: "end_turn",
		Usage:      Usage{InputTokens: 5, OutputTokens: 2},
	}, nil
}

// textResponse creates a simple end_turn text response.
func textResponse(text string) mockResponse {
	return mockResponse{
		content:    []ContentBlock{{Type: "text", Text: text}},
		stopReason: "end_turn",
	}
}

// toolUseResponse creates a tool_use response (stops with "tool_use").
func toolUseResponse(id, name, input string) mockResponse {
	return mockResponse{
		content: []ContentBlock{{
			Type:  "tool_use",
			ID:    id,
			Name:  name,
			Input: json.RawMessage(input),
		}},
		stopReason: "tool_use",
	}
}

// collectEvents drains the events channel into a slice.
func collectEvents(ch <-chan Event) []Event {
	var events []Event
	for e := range ch {
		events = append(events, e)
	}
	return events
}

// runAgent runs the agent and collects events (channel closed on return).
func runAgent(a *Agent, ctx context.Context, msg string) []Event {
	ch := make(chan Event, 64)
	go func() {
		a.Run(ctx, msg, nil, ch)
		close(ch)
	}()
	return collectEvents(ch)
}

// ---------------------------------------------------------------------------
// TestAgent_SimpleResponse
// ---------------------------------------------------------------------------

func TestAgent_SimpleResponse(t *testing.T) {
	prov := &MockProvider{
		responses: []mockResponse{textResponse("Hello, world!")},
	}
	a := NewAgent(prov)

	events := runAgent(a, context.Background(), "hi")

	var texts []string
	var gotDone bool
	for _, e := range events {
		switch e.Type {
		case EventText:
			texts = append(texts, e.Text)
		case EventDone:
			gotDone = true
		case EventError:
			t.Errorf("unexpected EventError: %v", e.Error)
		}
	}

	if !gotDone {
		t.Error("expected EventDone")
	}
	combined := strings.Join(texts, "")
	if !strings.Contains(combined, "Hello, world!") {
		t.Errorf("expected text 'Hello, world!', got: %q", combined)
	}
}

// ---------------------------------------------------------------------------
// TestAgent_ToolUseLoop
// ---------------------------------------------------------------------------

func TestAgent_ToolUseLoop(t *testing.T) {
	toolCalled := false
	var calledName, calledArgs string

	prov := &MockProvider{
		responses: []mockResponse{
			// First turn: request tool call.
			toolUseResponse("tool-1", "my_tool", `{"x":1}`),
			// Second turn: end normally.
			textResponse("All done."),
		},
	}

	a := NewAgent(prov,
		WithToolRunner(func(name, args string) (string, error) {
			toolCalled = true
			calledName = name
			calledArgs = args
			return "tool result", nil
		}),
	)

	events := runAgent(a, context.Background(), "use a tool")

	if !toolCalled {
		t.Error("expected tool runner to be called")
	}
	if calledName != "my_tool" {
		t.Errorf("expected tool name 'my_tool', got %q", calledName)
	}
	if calledArgs != `{"x":1}` {
		t.Errorf("expected args '{\"x\":1}', got %q", calledArgs)
	}

	var gotToolCall, gotToolResult, gotDone bool
	for _, e := range events {
		switch e.Type {
		case EventToolCall:
			gotToolCall = true
			if e.ToolName != "my_tool" {
				t.Errorf("EventToolCall name = %q, want 'my_tool'", e.ToolName)
			}
		case EventToolResult:
			gotToolResult = true
			if e.Result != "tool result" {
				t.Errorf("EventToolResult result = %q, want 'tool result'", e.Result)
			}
		case EventDone:
			gotDone = true
		}
	}

	if !gotToolCall {
		t.Error("expected EventToolCall")
	}
	if !gotToolResult {
		t.Error("expected EventToolResult")
	}
	if !gotDone {
		t.Error("expected EventDone")
	}
}

// ---------------------------------------------------------------------------
// TestAgent_MaxTurns
// ---------------------------------------------------------------------------

func TestAgent_MaxTurns(t *testing.T) {
	// Provider always returns a tool call so the loop never ends naturally.
	prov := &MockProvider{}
	for i := 0; i < 10; i++ {
		prov.responses = append(prov.responses, toolUseResponse("id", "mytool", `{}`))
	}

	a := NewAgent(prov,
		WithMaxTurns(3),
		WithToolRunner(func(name, args string) (string, error) {
			return "ok", nil
		}),
	)

	events := runAgent(a, context.Background(), "go forever")

	var gotError bool
	for _, e := range events {
		if e.Type == EventError {
			gotError = true
			if !strings.Contains(e.Error.Error(), "max turns") {
				t.Errorf("error should mention 'max turns': %v", e.Error)
			}
		}
	}
	if !gotError {
		t.Error("expected EventError when max turns reached")
	}
}

// ---------------------------------------------------------------------------
// TestAgent_ContextCancellation
// ---------------------------------------------------------------------------

func TestAgent_ContextCancellation(t *testing.T) {
	// Use a provider that blocks until the context is cancelled.
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately before Run begins.
	cancel()

	prov := &MockProvider{
		responses: []mockResponse{textResponse("never")},
	}
	a := NewAgent(prov)

	events := runAgent(a, ctx, "hello")

	var gotError bool
	for _, e := range events {
		if e.Type == EventError {
			gotError = true
		}
	}
	if !gotError {
		t.Error("expected EventError when context is cancelled")
	}
}

// ---------------------------------------------------------------------------
// TestAgent_PermissionDenied
// ---------------------------------------------------------------------------

func TestAgent_PermissionDenied(t *testing.T) {
	prov := &MockProvider{
		responses: []mockResponse{
			toolUseResponse("tool-1", "dangerous_tool", `{}`),
			textResponse("ok"),
		},
	}

	a := NewAgent(prov,
		WithPermissionFn(func(name, args string) (bool, error) {
			// Deny all tools.
			return false, nil
		}),
		WithToolRunner(func(name, args string) (string, error) {
			t.Error("tool runner should not be called when permission is denied")
			return "never", nil
		}),
	)

	events := runAgent(a, context.Background(), "do something")

	var gotDenied bool
	for _, e := range events {
		if e.Type == EventToolResult && e.IsError {
			gotDenied = true
			if !strings.Contains(e.Result, "denied") {
				t.Errorf("expected 'denied' in result, got: %q", e.Result)
			}
		}
	}
	if !gotDenied {
		t.Error("expected a denied tool result event")
	}
}

// ---------------------------------------------------------------------------
// TestAgent_PermissionError
// ---------------------------------------------------------------------------

func TestAgent_PermissionError(t *testing.T) {
	prov := &MockProvider{
		responses: []mockResponse{
			toolUseResponse("tool-1", "atool", `{}`),
		},
	}

	permErr := errors.New("permission check exploded")

	a := NewAgent(prov,
		WithPermissionFn(func(name, args string) (bool, error) {
			return false, permErr
		}),
	)

	events := runAgent(a, context.Background(), "go")

	var gotError bool
	for _, e := range events {
		if e.Type == EventError {
			gotError = true
			if !errors.Is(e.Error, permErr) && !strings.Contains(e.Error.Error(), permErr.Error()) {
				t.Errorf("error should wrap permErr, got: %v", e.Error)
			}
		}
	}
	if !gotError {
		t.Error("expected EventError when permission fn returns error")
	}
}

// ---------------------------------------------------------------------------
// TestAgent_ProviderError
// ---------------------------------------------------------------------------

func TestAgent_ProviderError(t *testing.T) {
	provErr := errors.New("network timeout")
	prov := &MockProvider{
		responses: []mockResponse{{err: provErr}},
	}

	a := NewAgent(prov)
	events := runAgent(a, context.Background(), "hello")

	var gotError bool
	for _, e := range events {
		if e.Type == EventError {
			gotError = true
		}
	}
	if !gotError {
		t.Error("expected EventError when provider returns error")
	}
}

// ---------------------------------------------------------------------------
// TestAgent_NoToolRunner
// ---------------------------------------------------------------------------

func TestAgent_NoToolRunner(t *testing.T) {
	// When no tool runner is registered, tool calls should return an error
	// result to the model (not crash the agent).
	prov := &MockProvider{
		responses: []mockResponse{
			toolUseResponse("t1", "some_tool", `{}`),
			textResponse("done"),
		},
	}

	a := NewAgent(prov) // no WithToolRunner

	events := runAgent(a, context.Background(), "call a tool")

	var gotToolResult bool
	for _, e := range events {
		if e.Type == EventToolResult && e.IsError {
			gotToolResult = true
		}
	}
	if !gotToolResult {
		t.Error("expected error tool result when no runner is registered")
	}
}

// ---------------------------------------------------------------------------
// TestAgent_MultipleToolsInOneTurn
// ---------------------------------------------------------------------------

func TestAgent_MultipleToolsInOneTurn(t *testing.T) {
	var toolNames []string

	prov := &MockProvider{
		responses: []mockResponse{
			{
				content: []ContentBlock{
					{Type: "tool_use", ID: "t1", Name: "tool_a", Input: json.RawMessage(`{}`)},
					{Type: "tool_use", ID: "t2", Name: "tool_b", Input: json.RawMessage(`{}`)},
				},
				stopReason: "tool_use",
			},
			textResponse("done"),
		},
	}

	a := NewAgent(prov,
		WithToolRunner(func(name, args string) (string, error) {
			toolNames = append(toolNames, name)
			return "ok", nil
		}),
	)

	events := runAgent(a, context.Background(), "use multiple tools")

	if len(toolNames) != 2 {
		t.Errorf("expected 2 tool calls, got %d: %v", len(toolNames), toolNames)
	}

	var gotDone bool
	for _, e := range events {
		if e.Type == EventDone {
			gotDone = true
		}
	}
	if !gotDone {
		t.Error("expected EventDone")
	}
}

// ---------------------------------------------------------------------------
// TestAgent_UsageAccumulation
// ---------------------------------------------------------------------------

func TestAgent_UsageAccumulation(t *testing.T) {
	prov := &MockProvider{
		responses: []mockResponse{textResponse("ok")},
	}
	a := NewAgent(prov)

	events := runAgent(a, context.Background(), "hello")

	for _, e := range events {
		if e.Type == EventDone {
			if e.Usage.InputTokens == 0 && e.Usage.OutputTokens == 0 {
				t.Error("expected non-zero usage on EventDone")
			}
			return
		}
	}
	t.Error("no EventDone found")
}

// ---------------------------------------------------------------------------
// TestNewAgent_Defaults
// ---------------------------------------------------------------------------

func TestNewAgent_Defaults(t *testing.T) {
	prov := &MockProvider{}
	a := NewAgent(prov)

	if a.model != "claude-sonnet-4-20250514" {
		t.Errorf("default model = %q", a.model)
	}
	if a.maxTokens != 8192 {
		t.Errorf("default maxTokens = %d", a.maxTokens)
	}
	if a.maxTurns != 50 {
		t.Errorf("default maxTurns = %d", a.maxTurns)
	}
}

// ---------------------------------------------------------------------------
// TestNewAgent_Options
// ---------------------------------------------------------------------------

func TestNewAgent_Options(t *testing.T) {
	prov := &MockProvider{}
	a := NewAgent(prov,
		WithModel("gpt-4o"),
		WithMaxTokens(1024),
		WithMaxTurns(5),
		WithSystemPrompt("Be helpful"),
		WithTemperature(0.5),
	)

	if a.model != "gpt-4o" {
		t.Errorf("model = %q, want 'gpt-4o'", a.model)
	}
	if a.maxTokens != 1024 {
		t.Errorf("maxTokens = %d, want 1024", a.maxTokens)
	}
	if a.maxTurns != 5 {
		t.Errorf("maxTurns = %d, want 5", a.maxTurns)
	}
	if a.systemPrompt != "Be helpful" {
		t.Errorf("systemPrompt = %q", a.systemPrompt)
	}
	if a.temperature != 0.5 {
		t.Errorf("temperature = %f, want 0.5", a.temperature)
	}
}

// ---------------------------------------------------------------------------
// TestAgent_ToolTimeout
// ---------------------------------------------------------------------------

// TestAgent_ToolTimeout verifies that when the agent's context is cancelled
// while a slow tool is running, the agent emits EventError with the context
// error.
func TestAgent_ToolTimeout(t *testing.T) {
	prov := &MockProvider{
		responses: []mockResponse{
			// First turn: request a tool call.
			toolUseResponse("t1", "slow_tool", `{}`),
			// Never reached: the context is cancelled during the tool call.
			textResponse("done"),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	a := NewAgent(prov,
		WithMaxTurns(5),
		WithToolRunnerCtx(func(ctx context.Context, name, args string) (string, error) {
			// Simulate a slow tool that respects the context.
			select {
			case <-time.After(5 * time.Second):
				return "finished", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}),
	)

	// Cancel after a short delay so the tool is mid-execution.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	events := runAgent(a, ctx, "call slow tool")

	var gotError bool
	for _, e := range events {
		if e.Type == EventError {
			gotError = true
			if e.Error == nil {
				t.Error("EventError should carry a non-nil error")
			}
		}
	}
	if !gotError {
		t.Error("expected EventError when context is cancelled during tool execution")
	}
}

// ---------------------------------------------------------------------------
// TestAgent_ToolRunnerCtx
// ---------------------------------------------------------------------------

// TestAgent_ToolRunnerCtx verifies that WithToolRunnerCtx is preferred over
// WithToolRunner when both are registered.
func TestAgent_ToolRunnerCtx(t *testing.T) {
	var ctxRunnerCalled bool
	var plainRunnerCalled bool

	prov := &MockProvider{
		responses: []mockResponse{
			toolUseResponse("t1", "my_tool", `{}`),
			textResponse("done"),
		},
	}

	a := NewAgent(prov,
		WithToolRunner(func(name, args string) (string, error) {
			plainRunnerCalled = true
			return "plain result", nil
		}),
		WithToolRunnerCtx(func(ctx context.Context, name, args string) (string, error) {
			ctxRunnerCalled = true
			return "ctx result", nil
		}),
	)

	events := runAgent(a, context.Background(), "call a tool")

	if !ctxRunnerCalled {
		t.Error("expected WithToolRunnerCtx to be called")
	}
	if plainRunnerCalled {
		t.Error("WithToolRunner should NOT be called when WithToolRunnerCtx is set")
	}

	var gotResult bool
	for _, e := range events {
		if e.Type == EventToolResult && e.Result == "ctx result" {
			gotResult = true
		}
	}
	if !gotResult {
		t.Error("expected EventToolResult with 'ctx result'")
	}
}
