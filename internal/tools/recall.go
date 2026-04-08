package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/antiartificial/replicant/internal/memory"
	"github.com/antiartificial/replicant/internal/permission"
)

// RecallTool lets the agent search its memory for relevant past observations.
type RecallTool struct {
	mem *memory.Memory
}

// NewRecallTool creates a recall tool backed by the given memory store.
func NewRecallTool(mem *memory.Memory) *RecallTool {
	return &RecallTool{mem: mem}
}

func (t *RecallTool) Name() string { return "recall" }

func (t *RecallTool) Description() string {
	return "Search agent memory for information relevant to a query. " +
		"Returns past observations, decisions, learnings, and session summaries " +
		"ranked by relevance and recency. Use this before starting a task to " +
		"surface relevant prior context."
}

func (t *RecallTool) Risk() permission.RiskLevel { return permission.RiskNone }

func (t *RecallTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural-language query describing what you want to recall.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return. Defaults to 5.",
			},
		},
		"required": []string{"query"},
	}
}

type recallArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func (t *RecallTool) Run(args string) (string, error) {
	var a recallArgs
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("recall: invalid arguments: %w", err)
	}
	if a.Query == "" {
		return "", fmt.Errorf("recall: query is required")
	}
	if a.Limit <= 0 {
		a.Limit = 5
	}

	results, err := t.mem.Recall(context.Background(), a.Query, a.Limit)
	if err != nil {
		return "", fmt.Errorf("recall: %w", err)
	}

	if len(results) == 0 {
		return "no memories found matching your query", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "recalled %d memor%s:\n\n", len(results), pluralSuffix(len(results)))
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. [%s] (score: %.2f", i+1, r.Category, r.Score)
		if !r.CreatedAt.IsZero() {
			fmt.Fprintf(&sb, ", %s", r.CreatedAt.Format("2006-01-02 15:04"))
		}
		fmt.Fprintf(&sb, ")\n   %s\n\n", r.Content)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

func pluralSuffix(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
