package agent

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// EstimateTokens
// ---------------------------------------------------------------------------

func TestEstimateTokens_Empty(t *testing.T) {
	if got := EstimateTokens(nil); got != 0 {
		t.Errorf("EstimateTokens(nil) = %d, want 0", got)
	}
	if got := EstimateTokens([]Message{}); got != 0 {
		t.Errorf("EstimateTokens([]) = %d, want 0", got)
	}
}

func TestEstimateTokens_TextBlock(t *testing.T) {
	// 4 chars = ~1 token. We need at least 4 chars to get ≥1 token.
	msgs := []Message{{
		Role:    "user",
		Content: []ContentBlock{{Type: "text", Text: "abcd"}}, // 4 chars → 1 token
	}}
	got := EstimateTokens(msgs)
	if got != 1 {
		t.Errorf("EstimateTokens = %d, want 1", got)
	}
}

func TestEstimateTokens_MultipleBlocks(t *testing.T) {
	msgs := []Message{
		{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "aaaa"}, // 4 chars → 1 token
				{Type: "text", Text: "bbbb"}, // 4 chars → 1 token
			},
		},
		{
			Role: "assistant",
			Content: []ContentBlock{
				{Type: "text", Text: "cccccccc"}, // 8 chars → 2 tokens
			},
		},
	}
	got := EstimateTokens(msgs)
	if got != 4 {
		t.Errorf("EstimateTokens = %d, want 4", got)
	}
}

func TestEstimateTokens_ToolUseBlock(t *testing.T) {
	msgs := []Message{{
		Role: "assistant",
		Content: []ContentBlock{{
			Type:  "tool_use",
			Name:  "abcd",           // 4 chars → 1 token
			Input: []byte(`"abcd"`), // 6 chars → 1 token (6/4 = 1)
		}},
	}}
	got := EstimateTokens(msgs)
	// name: 4/4=1, input: 6/4=1 → total 2
	if got != 2 {
		t.Errorf("EstimateTokens = %d, want 2", got)
	}
}

func TestEstimateTokens_ToolResultBlock(t *testing.T) {
	msgs := []Message{{
		Role: "user",
		Content: []ContentBlock{{
			Type:    "tool_result",
			Content: "abcdefgh", // 8 chars → 2 tokens
		}},
	}}
	got := EstimateTokens(msgs)
	if got != 2 {
		t.Errorf("EstimateTokens = %d, want 2", got)
	}
}

func TestEstimateTokens_Proportional(t *testing.T) {
	// Verify the estimate grows with content length.
	short := []Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hi"}}}}
	long := []Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: strings.Repeat("a", 400)}}}}

	shortTok := EstimateTokens(short)
	longTok := EstimateTokens(long)

	if longTok <= shortTok {
		t.Errorf("expected longer message to have more tokens: short=%d long=%d", shortTok, longTok)
	}
}

// ---------------------------------------------------------------------------
// CompactHistory — no-op when messages ≤ keepRecent
// ---------------------------------------------------------------------------

func TestCompactHistory_NoOp(t *testing.T) {
	// 2 messages, keepRecent=10 → nothing to compact.
	msgs := []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []ContentBlock{{Type: "text", Text: "hi"}}},
	}

	prov := &MockProvider{}
	result, usage, err := CompactHistory(context.Background(), prov, "model", msgs, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != len(msgs) {
		t.Errorf("expected unchanged slice of length %d, got %d", len(msgs), len(result))
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Errorf("expected zero usage for no-op, got %+v", usage)
	}
	if prov.calls != 0 {
		t.Errorf("provider should not be called for no-op compact, got %d calls", prov.calls)
	}
}

// ---------------------------------------------------------------------------
// CompactHistory — summarises old messages
// ---------------------------------------------------------------------------

func TestCompactHistory_Summarizes(t *testing.T) {
	// Build a longer history so there is something to compact.
	var msgs []Message
	for i := 0; i < 6; i++ {
		msgs = append(msgs, Message{
			Role:    "user",
			Content: []ContentBlock{{Type: "text", Text: "user message"}},
		})
		msgs = append(msgs, Message{
			Role:    "assistant",
			Content: []ContentBlock{{Type: "text", Text: "assistant reply"}},
		})
	}
	// 12 messages total; keepRecent=4 → 8 old messages are summarised.

	prov := &MockProvider{
		responses: []mockResponse{
			textResponse("Summary of old conversation."),
		},
	}

	result, usage, err := CompactHistory(context.Background(), prov, "test-model", msgs, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Result should be: [summary_user, assistant_ack, ...4 recent messages]
	// = 2 + 4 = 6 messages.
	if len(result) != 6 {
		t.Errorf("expected 6 messages after compaction, got %d", len(result))
	}

	// First message should be the summary user message.
	if result[0].Role != "user" {
		t.Errorf("first message role = %q, want 'user'", result[0].Role)
	}
	if len(result[0].Content) == 0 || !strings.Contains(result[0].Content[0].Text, "Summary") {
		t.Errorf("first message should contain summary, got: %+v", result[0].Content)
	}

	// Second message should be the assistant ack.
	if result[1].Role != "assistant" {
		t.Errorf("second message role = %q, want 'assistant'", result[1].Role)
	}

	// Recent messages preserved at the end.
	for i := 2; i < 6; i++ {
		if result[i].Role == "" {
			t.Errorf("message %d has empty role", i)
		}
	}

	// Usage should be non-zero (from the summary call).
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		t.Error("expected non-zero usage from summary call")
	}
}

func TestCompactHistory_ProviderError(t *testing.T) {
	var msgs []Message
	for i := 0; i < 6; i++ {
		msgs = append(msgs, Message{
			Role:    "user",
			Content: []ContentBlock{{Type: "text", Text: "msg"}},
		})
	}

	prov := &MockProvider{
		responses: []mockResponse{{err: errTest}},
	}

	_, _, err := CompactHistory(context.Background(), prov, "model", msgs, 2)
	if err == nil {
		t.Error("expected error when provider fails during compaction")
	}
}

var errTest = &testError{"injected failure"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

func TestCompactHistory_EmptySummaryError(t *testing.T) {
	// Provider returns a response with no text blocks → empty summary.
	var msgs []Message
	for i := 0; i < 6; i++ {
		msgs = append(msgs, Message{
			Role:    "user",
			Content: []ContentBlock{{Type: "text", Text: "msg"}},
		})
	}

	prov := &MockProvider{
		responses: []mockResponse{{
			content:    []ContentBlock{{Type: "tool_use", ID: "x", Name: "y"}}, // no text
			stopReason: "end_turn",
		}},
	}

	_, _, err := CompactHistory(context.Background(), prov, "model", msgs, 2)
	if err == nil {
		t.Error("expected error when summary is empty")
	}
}

// ---------------------------------------------------------------------------
// EstimateTokens — unknown block types are ignored
// ---------------------------------------------------------------------------

func TestEstimateTokens_UnknownType(t *testing.T) {
	msgs := []Message{{
		Role: "user",
		Content: []ContentBlock{{Type: "unknown_type", Text: "some content"}},
	}}
	// unknown type → contributes 0.
	got := EstimateTokens(msgs)
	if got != 0 {
		t.Errorf("EstimateTokens for unknown block type = %d, want 0", got)
	}
}
