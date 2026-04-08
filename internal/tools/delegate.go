package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/antiartificial/replicant/internal/agent"
	"github.com/antiartificial/replicant/internal/permission"
	"github.com/antiartificial/replicant/internal/replicant"
)

const delegateTimeout = 5 * time.Minute

// DelegateTool lets a parent agent spawn a child agent with a specific
// replicant persona. The child runs its own ReAct loop to completion and
// returns its final text response as the tool result.
type DelegateTool struct {
	repRegistry     *replicant.Registry
	toolRegistry    *Registry
	providerFactory func(model string) (agent.Provider, string, error)
}

// NewDelegateTool constructs a DelegateTool.
//
// providerFactory is a closure (typically from main.go) that maps a prefixed
// model string like "anthropic/claude-sonnet-4-20250514" to a Provider and
// bare model name, mirroring the signature of agent.NewProvider.
func NewDelegateTool(
	rep *replicant.Registry,
	toolReg *Registry,
	providerFactory func(model string) (agent.Provider, string, error),
) *DelegateTool {
	return &DelegateTool{
		repRegistry:     rep,
		toolRegistry:    toolReg,
		providerFactory: providerFactory,
	}
}

func (d *DelegateTool) Name() string { return "delegate" }

func (d *DelegateTool) Description() string {
	return "Delegate a task to another replicant (child agent). " +
		"The child runs its own ReAct loop to completion and returns its final answer. " +
		"Use this to hand off specialised work — e.g. send implementation tasks to a coder replicant."
}

func (d *DelegateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"replicant": map[string]any{
				"type":        "string",
				"description": "Name of the replicant to delegate to (e.g. \"rachael\", \"deckard\").",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "The task description to give the child agent as its user message.",
			},
			"context": map[string]any{
				"type":        "string",
				"description": "Optional additional context prepended to the task.",
			},
		},
		"required": []string{"replicant", "task"},
	}
}

func (d *DelegateTool) Risk() permission.RiskLevel {
	// Spawning a child is low-risk at this level; the child's own tools carry
	// their own risk checks.
	return permission.RiskLow
}

// delegateInput is the decoded JSON input for the delegate tool.
type delegateInput struct {
	Replicant string `json:"replicant"`
	Task      string `json:"task"`
	Context   string `json:"context"`
}

// Run executes the delegation: looks up the child replicant, creates an agent,
// runs it to completion, and returns the final text response.
func (d *DelegateTool) Run(args string) (string, error) {
	var input delegateInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return "", fmt.Errorf("delegate: invalid args: %w", err)
	}
	if input.Replicant == "" {
		return "", fmt.Errorf("delegate: replicant name is required")
	}
	if input.Task == "" {
		return "", fmt.Errorf("delegate: task is required")
	}

	// Look up the child replicant definition.
	def, ok := d.repRegistry.Get(input.Replicant)
	if !ok {
		return "", fmt.Errorf("delegate: replicant %q not found", input.Replicant)
	}

	// Build the user message for the child.
	userMessage := input.Task
	if input.Context != "" {
		userMessage = input.Context + "\n\n" + input.Task
	}

	// Resolve the child's model via the provider factory.
	model := def.Model
	prov, bareModel, err := d.providerFactory(model)
	if err != nil {
		return "", fmt.Errorf("delegate: provider for %q: %w", input.Replicant, err)
	}

	// Resolve the child's tools — deliberately excluding "delegate" to
	// prevent infinite recursion.
	childToolNames := filterOut(def.Tools, "delegate")
	resolvedTools := d.toolRegistry.Resolve(childToolNames)

	toolDefs := make([]agent.ToolDef, len(resolvedTools))
	for i, t := range resolvedTools {
		toolDefs[i] = agent.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.Parameters(),
		}
	}

	toolRunner := func(name string, runArgs string) (string, error) {
		t, ok := d.toolRegistry.Get(name)
		if !ok {
			return "", fmt.Errorf("unknown tool: %s", name)
		}
		return t.Run(runArgs)
	}

	// Child agents run fully autonomously — no interactive permission checks.
	childPermFn := func(name, _ string) (bool, error) {
		t, ok := d.toolRegistry.Get(name)
		if !ok {
			return false, nil
		}
		// Auto-approve everything below RiskHigh for child agents.
		return t.Risk() < permission.RiskHigh, nil
	}

	childAgent := agent.NewAgent(prov,
		agent.WithSystemPrompt(def.SystemPrompt),
		agent.WithModel(bareModel),
		agent.WithMaxTokens(def.MaxTokens),
		agent.WithTemperature(def.Temperature),
		agent.WithTools(toolDefs),
		agent.WithToolRunner(toolRunner),
		agent.WithMaxTurns(def.MaxTurns),
		agent.WithPermissionFn(childPermFn),
	)

	// Run the child with a timeout.
	ctx, cancel := context.WithTimeout(context.Background(), delegateTimeout)
	defer cancel()

	events := make(chan agent.Event, 128)

	go func() {
		childAgent.Run(ctx, userMessage, nil, events)
		close(events)
	}()

	// Collect the child's final text and any errors.
	var textParts []string
	var runErr error

	for ev := range events {
		switch ev.Type {
		case agent.EventText:
			textParts = append(textParts, ev.Text)
		case agent.EventError:
			runErr = ev.Error
		}
	}

	if runErr != nil {
		// Return partial text (if any) alongside the error.
		partial := strings.Join(textParts, "")
		if partial != "" {
			return fmt.Sprintf("[child agent error: %v]\n\n%s", runErr, partial), nil
		}
		return "", fmt.Errorf("delegate (%s): %w", input.Replicant, runErr)
	}

	result := strings.Join(textParts, "")
	if result == "" {
		return fmt.Sprintf("(child agent %q completed with no text output)", input.Replicant), nil
	}
	return result, nil
}

// filterOut returns a copy of names with target removed.
func filterOut(names []string, target string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n != target {
			out = append(out, n)
		}
	}
	return out
}
