package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/antiartificial/replicant/internal/permission"
)

// EditFileTool implements the edit_file tool.
type EditFileTool struct{}

func (t *EditFileTool) Name() string { return "edit_file" }

func (t *EditFileTool) Description() string {
	return "Perform an exact search-and-replace in a file. " +
		"old_string must appear exactly once; the tool replaces it with new_string."
}

func (t *EditFileTool) Risk() permission.RiskLevel { return permission.RiskLow }

func (t *EditFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path to the file to edit.",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "The exact string to find. Must appear exactly once in the file.",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "The string to replace old_string with.",
			},
		},
		"required": []string{"path", "old_string", "new_string"},
	}
}

type editFileArgs struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

func (t *EditFileTool) Run(args string) (string, error) {
	var a editFileArgs
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("edit_file: invalid arguments: %w", err)
	}
	if a.Path == "" {
		return "", fmt.Errorf("edit_file: path is required")
	}

	data, err := os.ReadFile(a.Path)
	if err != nil {
		return "", fmt.Errorf("edit_file: %w", err)
	}
	content := string(data)

	count := strings.Count(content, a.OldString)
	switch {
	case count == 0:
		return "", fmt.Errorf("edit_file: old_string not found in %s", a.Path)
	case count > 1:
		return "", fmt.Errorf("edit_file: old_string appears %d times in %s (must be unique)", count, a.Path)
	}

	updated := strings.Replace(content, a.OldString, a.NewString, 1)

	info, err := os.Stat(a.Path)
	if err != nil {
		return "", fmt.Errorf("edit_file: stat %s: %w", a.Path, err)
	}
	if err := os.WriteFile(a.Path, []byte(updated), info.Mode()); err != nil {
		return "", fmt.Errorf("edit_file: writing %s: %w", a.Path, err)
	}

	return fmt.Sprintf("edit_file: replaced 1 occurrence in %s", a.Path), nil
}
