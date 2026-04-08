package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/antiartificial/replicant/internal/permission"
)

const maxGrepResults = 100

// GrepTool implements the grep tool.
type GrepTool struct{}

func (t *GrepTool) Name() string { return "grep" }

func (t *GrepTool) Description() string {
	return "Search for a regex pattern in files, returning matches in file:line:content format. " +
		"Uses ripgrep (rg) when available, falling back to grep -rn. " +
		fmt.Sprintf("Returns at most %d matching lines.", maxGrepResults)
}

func (t *GrepTool) Risk() permission.RiskLevel { return permission.RiskNone }

func (t *GrepTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regular expression pattern to search for.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory or file to search. Defaults to current working directory.",
			},
			"include": map[string]any{
				"type":        "string",
				"description": "Glob filter for file names to include (e.g. \"*.go\").",
			},
		},
		"required": []string{"pattern"},
	}
}

type grepArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Include string `json:"include"`
}

func (t *GrepTool) Run(args string) (string, error) {
	var a grepArgs
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("grep: invalid arguments: %w", err)
	}
	if a.Pattern == "" {
		return "", fmt.Errorf("grep: pattern is required")
	}
	if a.Path == "" {
		a.Path = "."
	}

	var cmd *exec.Cmd
	if rgPath, err := exec.LookPath("rg"); err == nil {
		rgArgs := []string{"--line-number", "--no-heading", "--color=never", a.Pattern, a.Path}
		if a.Include != "" {
			rgArgs = append([]string{"--glob", a.Include}, rgArgs...)
		}
		cmd = exec.Command(rgPath, rgArgs...)
	} else {
		grepArgs := []string{"-rn", "--color=never"}
		if a.Include != "" {
			grepArgs = append(grepArgs, "--include="+a.Include)
		}
		grepArgs = append(grepArgs, a.Pattern, a.Path)
		cmd = exec.Command("grep", grepArgs...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	// exit code 1 from grep/rg means no matches — not an error for us.
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "no matches found", nil
		}
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = runErr.Error()
		}
		return "", fmt.Errorf("grep: %s", errMsg)
	}

	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return "no matches found", nil
	}

	truncated := false
	if len(lines) > maxGrepResults {
		lines = lines[:maxGrepResults]
		truncated = true
	}

	result := strings.Join(lines, "\n")
	if truncated {
		result += fmt.Sprintf("\n[results truncated to %d lines]", maxGrepResults)
	}
	return result, nil
}
