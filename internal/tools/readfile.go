package tools

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/antiartificial/replicant/internal/permission"
)

const defaultReadLimit = 500

// ReadFileTool implements the read_file tool.
type ReadFileTool struct{}

func (t *ReadFileTool) Name() string { return "read_file" }

func (t *ReadFileTool) Description() string {
	return "Read the contents of a file, returning lines prefixed with line numbers (cat -n style). " +
		"Use offset and limit to read a specific range of lines."
}

func (t *ReadFileTool) Risk() permission.RiskLevel { return permission.RiskNone }

func (t *ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path to the file to read.",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "1-based line number to start reading from. Defaults to 1.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("Maximum number of lines to return. Defaults to %d.", defaultReadLimit),
			},
		},
		"required": []string{"path"},
	}
}

type readFileArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

func (t *ReadFileTool) Run(args string) (string, error) {
	var a readFileArgs
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("read_file: invalid arguments: %w", err)
	}
	if a.Path == "" {
		return "", fmt.Errorf("read_file: path is required")
	}
	if a.Offset <= 0 {
		a.Offset = 1
	}
	if a.Limit <= 0 {
		a.Limit = defaultReadLimit
	}

	f, err := os.Open(a.Path)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	scanner := bufio.NewScanner(f)
	lineNum := 0
	written := 0
	for scanner.Scan() {
		lineNum++
		if lineNum < a.Offset {
			continue
		}
		if written >= a.Limit {
			break
		}
		fmt.Fprintf(&sb, "%6d\t%s\n", lineNum, scanner.Text())
		written++
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read_file: reading %s: %w", a.Path, err)
	}
	if written == 0 && lineNum < a.Offset {
		return "", fmt.Errorf("read_file: offset %d exceeds file length (%d lines)", a.Offset, lineNum)
	}
	return sb.String(), nil
}
