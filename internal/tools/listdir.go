package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/antiartificial/replicant/internal/permission"
)

// ListDirTool implements the list_dir tool.
type ListDirTool struct{}

func (t *ListDirTool) Name() string { return "list_dir" }

func (t *ListDirTool) Description() string {
	return "List the contents of a directory, showing file names, sizes, and whether each entry is a file or directory."
}

func (t *ListDirTool) Risk() permission.RiskLevel { return permission.RiskNone }

func (t *ListDirTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the directory to list. Defaults to current directory.",
			},
		},
		"required": []string{},
	}
}

type listDirArgs struct {
	Path string `json:"path"`
}

func (t *ListDirTool) Run(args string) (string, error) {
	var a listDirArgs
	if args != "" {
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			return "", fmt.Errorf("list_dir: invalid arguments: %w", err)
		}
	}
	if a.Path == "" {
		a.Path = "."
	}

	entries, err := os.ReadDir(a.Path)
	if err != nil {
		return "", fmt.Errorf("list_dir: %w", err)
	}

	var sb strings.Builder
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		kind := "file"
		if entry.IsDir() {
			kind = "dir "
		}
		fmt.Fprintf(&sb, "%s  %8d  %s\n", kind, info.Size(), entry.Name())
	}

	if sb.Len() == 0 {
		return "(empty directory)", nil
	}
	return sb.String(), nil
}
