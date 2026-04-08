package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/antiartificial/replicant/internal/agent"
	"github.com/antiartificial/replicant/internal/permission"
)

const spawnTimeout = 10 * time.Minute

// defaultSpawnTools is the tool set given to spawned child agents when the
// caller does not specify an explicit list. Excludes spawn and delegate to
// prevent infinite recursion.
var defaultSpawnTools = []string{
	"read_file",
	"write_file",
	"edit_file",
	"list_dir",
	"execute",
	"glob",
	"grep",
}

// SpawnTool creates an ad-hoc child agent with a custom system prompt.
// Unlike delegate (which uses predefined replicants), spawn lets the
// parent define the child's behavior inline — similar to how Factory's
// mission workers are created at runtime by an orchestrator.
type SpawnTool struct {
	toolRegistry    *Registry
	providerFactory func(model string) (agent.Provider, string, error)
	defaultModel    string
}

// NewSpawnTool constructs a SpawnTool.
//
// toolReg is used to resolve tool names for the child agent.
// providerFactory maps a model string to a Provider and bare model name.
// defaultModel is used when the caller omits the model field.
func NewSpawnTool(
	toolReg *Registry,
	providerFactory func(model string) (agent.Provider, string, error),
	defaultModel string,
) *SpawnTool {
	return &SpawnTool{
		toolRegistry:    toolReg,
		providerFactory: providerFactory,
		defaultModel:    defaultModel,
	}
}

func (s *SpawnTool) Name() string { return "spawn" }

func (s *SpawnTool) Description() string {
	return "Spawn an ad-hoc child agent with an inline system prompt. " +
		"Unlike delegate (which requires a named replicant), spawn lets you define " +
		"the child's persona and capabilities at runtime. Use this when you need a " +
		"specialised worker whose behaviour is determined by the task at hand."
}

func (s *SpawnTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Display name for the child agent (used in logging).",
			},
			"instruction": map[string]any{
				"type":        "string",
				"description": "System prompt that defines the child agent's behaviour and persona.",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "The user message to send to the child agent.",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Optional model override (e.g. \"anthropic/claude-haiku-4-20250514\"). Defaults to the parent's model.",
			},
			"tools": map[string]any{
				"type":        "array",
				"description": "Optional list of tool names to give the child. Defaults to [read_file, write_file, edit_file, list_dir, execute, glob, grep].",
				"items":       map[string]any{"type": "string"},
			},
			"max_turns": map[string]any{
				"type":        "integer",
				"description": "Maximum number of agent turns (default 30).",
			},
		},
		"required": []string{"name", "instruction", "task"},
	}
}

func (s *SpawnTool) Risk() permission.RiskLevel {
	// Spawning a child is low-risk at this level; the child's own tools carry
	// their own risk checks.
	return permission.RiskLow
}

// Timeout declares the max duration for a spawn call.
func (s *SpawnTool) Timeout() time.Duration { return spawnTimeout }

// spawnInput is the decoded JSON input for the spawn tool.
type spawnInput struct {
	Name        string   `json:"name"`
	Instruction string   `json:"instruction"`
	Task        string   `json:"task"`
	Model       string   `json:"model"`
	Tools       []string `json:"tools"`
	MaxTurns    int      `json:"max_turns"`
}

// Run executes the spawn without a parent context.
func (s *SpawnTool) Run(args string) (string, error) {
	return s.RunWithContext(context.Background(), args)
}

// RunWithContext executes the spawn with cancellation from the parent.
func (s *SpawnTool) RunWithContext(ctx context.Context, args string) (string, error) {
	var input spawnInput
	if err := json.Unmarshal([]byte(args), &input); err != nil {
		return "", fmt.Errorf("spawn: invalid args: %w", err)
	}
	if input.Name == "" {
		return "", fmt.Errorf("spawn: name is required")
	}
	if input.Instruction == "" {
		return "", fmt.Errorf("spawn: instruction is required")
	}
	if input.Task == "" {
		return "", fmt.Errorf("spawn: task is required")
	}

	// Resolve model.
	model := input.Model
	if model == "" {
		model = s.defaultModel
	}
	prov, bareModel, err := s.providerFactory(model)
	if err != nil {
		return "", fmt.Errorf("spawn: provider for %q: %w", model, err)
	}

	// Resolve tool list — exclude spawn and delegate to prevent recursion.
	toolNames := input.Tools
	if len(toolNames) == 0 {
		toolNames = defaultSpawnTools
	}
	toolNames = filterOut(filterOut(toolNames, "spawn"), "delegate")

	resolvedTools := s.toolRegistry.Resolve(toolNames)
	toolDefs := make([]agent.ToolDef, len(resolvedTools))
	for i, t := range resolvedTools {
		toolDefs[i] = agent.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.Parameters(),
		}
	}

	toolRunner := func(name string, runArgs string) (string, error) {
		t, ok := s.toolRegistry.Get(name)
		if !ok {
			return "", fmt.Errorf("unknown tool: %s", name)
		}
		return t.Run(runArgs)
	}

	// Child agents run fully autonomously — auto-approve everything below RiskHigh.
	childPermFn := func(name, _ string) (bool, error) {
		t, ok := s.toolRegistry.Get(name)
		if !ok {
			return false, nil
		}
		return t.Risk() < permission.RiskHigh, nil
	}

	maxTurns := input.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 30
	}

	childAgent := agent.NewAgent(prov,
		agent.WithSystemPrompt(input.Instruction),
		agent.WithModel(bareModel),
		agent.WithTools(toolDefs),
		agent.WithToolRunner(toolRunner),
		agent.WithMaxTurns(maxTurns),
		agent.WithPermissionFn(childPermFn),
	)

	// Run the child with a timeout derived from the parent context.
	ctx, cancel := context.WithTimeout(ctx, spawnTimeout)
	defer cancel()

	events := make(chan agent.Event, 128)

	go func() {
		childAgent.Run(ctx, input.Task, nil, events)
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
		partial := strings.Join(textParts, "")
		if partial != "" {
			return fmt.Sprintf("[child agent %q error: %v]\n\n%s", input.Name, runErr, partial), nil
		}
		return "", fmt.Errorf("spawn (%s): %w", input.Name, runErr)
	}

	result := strings.Join(textParts, "")
	if result == "" {
		return fmt.Sprintf("(child agent %q completed with no text output)", input.Name), nil
	}
	return result, nil
}
