package agent

import (
	"context"
	"fmt"
)

const compactPrompt = `Summarize the conversation so far into a concise context block.
Preserve:
- The user's original objective and any sub-goals
- Key decisions made and their rationale
- Current state of the work (what's done, what's pending)
- Important file paths, function names, or code snippets referenced
- Any errors encountered and how they were resolved

Be concise but complete. This summary replaces the full history, so don't lose critical context.`

// CompactHistory takes the current message history and produces a compacted
// version by asking the LLM to summarize the older messages, keeping the
// most recent N messages intact.
//
// keepRecent is the number of recent messages to preserve verbatim.
// The older messages are summarized into a single system-like user message.
func CompactHistory(ctx context.Context, provider Provider, model string, messages []Message, keepRecent int) ([]Message, Usage, error) {
	if len(messages) <= keepRecent {
		return messages, Usage{}, nil // nothing to compact
	}

	// Split into old (to summarize) and recent (to keep)
	oldMessages := messages[:len(messages)-keepRecent]
	recentMessages := messages[len(messages)-keepRecent:]

	// Ask the model to summarize the old messages
	summaryReq := &CompletionRequest{
		Model:  model,
		System: compactPrompt,
		Messages: append(oldMessages, Message{
			Role:    "user",
			Content: []ContentBlock{{Type: "text", Text: "Summarize the conversation above."}},
		}),
		MaxTokens:   4096,
		Temperature: 0.2,
	}

	resp, err := provider.Complete(ctx, summaryReq)
	if err != nil {
		return nil, Usage{}, fmt.Errorf("compact: summarize: %w", err)
	}

	// Extract the summary text
	var summaryText string
	for _, block := range resp.Content {
		if block.Type == "text" {
			summaryText += block.Text
		}
	}

	if summaryText == "" {
		return nil, resp.Usage, fmt.Errorf("compact: empty summary returned")
	}

	// Build the compacted history: summary as first user message + recent messages
	compacted := make([]Message, 0, 2+len(recentMessages))
	compacted = append(compacted, Message{
		Role: "user",
		Content: []ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("[Context Summary]\n\n%s\n\n[End Summary — conversation continues below]", summaryText),
		}},
	})
	// Need a brief assistant ack so the message alternation is valid
	compacted = append(compacted, Message{
		Role: "assistant",
		Content: []ContentBlock{{
			Type: "text",
			Text: "Understood, I have the context. Continuing.",
		}},
	})
	compacted = append(compacted, recentMessages...)

	return compacted, resp.Usage, nil
}

// EstimateTokens gives a rough token count for a message history.
// Uses the ~4 chars per token heuristic.
func EstimateTokens(messages []Message) int {
	total := 0
	for _, m := range messages {
		for _, b := range m.Content {
			switch b.Type {
			case "text":
				total += len(b.Text) / 4
			case "tool_use":
				total += len(b.Input) / 4
				total += len(b.Name) / 4
			case "tool_result":
				total += len(b.Content) / 4
			}
		}
	}
	return total
}
