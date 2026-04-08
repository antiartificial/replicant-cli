package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/antiartificial/replicant/internal/permission"
)

// WriteFileTool implements the write_file tool. Creates or overwrites a file.
type WriteFileTool struct{}

func (t *WriteFileTool) Name() string { return "write_file" }

func (t *WriteFileTool) Description() string {
	return "Create a new file or overwrite an existing file with the given content. " +
		"Parent directories are created automatically if they don't exist."
}

func (t *WriteFileTool) Risk() permission.RiskLevel { return permission.RiskLow }

func (t *WriteFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to create or overwrite.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Full content to write to the file.",
			},
		},
		"required": []string{"path", "content"},
	}
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *WriteFileTool) Run(args string) (string, error) {
	var a writeFileArgs
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("write_file: invalid arguments: %w", err)
	}
	if a.Path == "" {
		return "", fmt.Errorf("write_file: path is required")
	}

	// Create parent directories if needed.
	dir := filepath.Dir(a.Path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("write_file: create directories: %w", err)
		}
	}

	if err := os.WriteFile(a.Path, []byte(a.Content), 0o644); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}

	return fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path), nil
}
