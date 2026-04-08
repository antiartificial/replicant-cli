package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/antiartificial/replicant/internal/memory"
	"github.com/antiartificial/replicant/internal/permission"
)

// RememberTool lets the agent explicitly store a memory in contextdb.
type RememberTool struct {
	mem *memory.Memory
}

// NewRememberTool creates a remember tool backed by the given memory store.
func NewRememberTool(mem *memory.Memory) *RememberTool {
	return &RememberTool{mem: mem}
}

func (t *RememberTool) Name() string { return "remember" }

func (t *RememberTool) Description() string {
	return "Store a memory for future recall across sessions. " +
		"Use category to classify the importance and decay rate: " +
		"\"observation\" or \"task\" (hours/days), " +
		"\"error\" (hours/days), " +
		"\"decision\" or \"learning\" (weeks), " +
		"\"skill\" or \"procedure\" (months)."
}

func (t *RememberTool) Risk() permission.RiskLevel { return permission.RiskNone }

func (t *RememberTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "The memory content to store.",
			},
			"category": map[string]any{
				"type":        "string",
				"description": "Memory category: observation, task, error, decision, learning, skill, procedure.",
				"enum":        []string{"observation", "task", "error", "decision", "learning", "skill", "procedure"},
			},
		},
		"required": []string{"content", "category"},
	}
}

type rememberArgs struct {
	Content  string `json:"content"`
	Category string `json:"category"`
}

func (t *RememberTool) Run(args string) (string, error) {
	var a rememberArgs
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("remember: invalid arguments: %w", err)
	}
	if a.Content == "" {
		return "", fmt.Errorf("remember: content is required")
	}
	if a.Category == "" {
		a.Category = "observation"
	}

	if err := t.mem.Remember(context.Background(), a.Content, "replicant:agent", a.Category); err != nil {
		return "", fmt.Errorf("remember: %w", err)
	}

	return fmt.Sprintf("stored memory [%s]: %s", a.Category, a.Content), nil
}
