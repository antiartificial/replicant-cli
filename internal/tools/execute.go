package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/antiartificial/replicant/internal/permission"
)

const (
	defaultExecTimeout = 30
	maxOutputLen       = 50000
)

// ExecuteTool implements the execute tool.
type ExecuteTool struct{}

func (t *ExecuteTool) Name() string { return "execute" }

func (t *ExecuteTool) Description() string {
	return "Run a shell command via sh -c and return its combined stdout+stderr output along with the exit code. " +
		"Output is truncated to 50000 characters."
}

func (t *ExecuteTool) Risk() permission.RiskLevel { return permission.RiskHigh }

func (t *ExecuteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute.",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("Timeout in seconds. Defaults to %d.", defaultExecTimeout),
			},
		},
		"required": []string{"command"},
	}
}

type executeArgs struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// Timeout declares the default max duration for execute. The LLM can override
// this per-call via the timeout parameter, but this is the ceiling used by the
// harness when no per-call value is set.
func (t *ExecuteTool) Timeout() time.Duration { return 10 * time.Minute }

func (t *ExecuteTool) Run(args string) (string, error) {
	return t.RunWithContext(context.Background(), args)
}

func (t *ExecuteTool) RunWithContext(ctx context.Context, args string) (string, error) {
	var a executeArgs
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("execute: invalid arguments: %w", err)
	}
	if a.Command == "" {
		return "", fmt.Errorf("execute: command is required")
	}
	if a.Timeout <= 0 {
		a.Timeout = defaultExecTimeout
	}

	// Use the tighter of: parent context deadline or per-call timeout.
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(a.Timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(callCtx, "sh", "-c", a.Command)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Run()

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			exitCode = -1
		} else {
			exitCode = -1
		}
	}

	output := buf.String()
	truncated := false
	if len(output) > maxOutputLen {
		output = output[:maxOutputLen]
		truncated = true
	}

	result := fmt.Sprintf("exit_code: %d\n%s", exitCode, output)
	if truncated {
		result += "\n[output truncated]"
	}
	return result, nil
}
