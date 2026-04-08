package tools

import (
	"context"
	"time"

	"github.com/antiartificial/replicant/internal/permission"
)

// Tool is the interface all replicant tools implement.
type Tool interface {
	// Name returns the tool's identifier used in replicant definitions.
	Name() string

	// Description returns a human-readable description for the LLM.
	Description() string

	// Parameters returns the JSON Schema for the tool's input.
	Parameters() map[string]any

	// Run executes the tool with the given JSON arguments and returns the result.
	Run(args string) (string, error)

	// Risk returns the risk level for permission checks.
	Risk() permission.RiskLevel
}

// ContextTool is an optional extension of Tool that accepts a context for
// cancellation, deadline propagation, and long-running operations.
// Tools that may run for extended periods (execute, delegate, claude code)
// should implement this instead of relying solely on Run.
type ContextTool interface {
	Tool
	RunWithContext(ctx context.Context, args string) (string, error)
}

// ToolTimeout returns the maximum duration a tool should be allowed to run.
// Tools that implement TimeoutTool declare their own limit. All others get
// the default (2 minutes).
type TimeoutTool interface {
	Timeout() time.Duration
}

// DefaultToolTimeout is applied to tools that don't implement TimeoutTool.
const DefaultToolTimeout = 2 * time.Minute

// GetTimeout returns the timeout for a tool, checking TimeoutTool first.
func GetTimeout(t Tool) time.Duration {
	if tt, ok := t.(TimeoutTool); ok {
		return tt.Timeout()
	}
	return DefaultToolTimeout
}

// RunTool executes a tool with context and timeout support. If the tool
// implements ContextTool, it calls RunWithContext. Otherwise it calls Run
// in a goroutine with the context deadline enforced externally.
func RunTool(ctx context.Context, t Tool, args string) (string, error) {
	if ct, ok := t.(ContextTool); ok {
		return ct.RunWithContext(ctx, args)
	}

	// For tools that only implement Run, execute in a goroutine so
	// context cancellation still works.
	type result struct {
		out string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := t.Run(args)
		ch <- result{out, err}
	}()

	select {
	case r := <-ch:
		return r.out, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
