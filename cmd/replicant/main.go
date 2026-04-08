package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/antiartificial/replicant/internal/agent"
	"github.com/antiartificial/replicant/internal/config"
	"github.com/antiartificial/replicant/internal/permission"
	"github.com/antiartificial/replicant/internal/replicant"
	"github.com/antiartificial/replicant/internal/tools"
	"github.com/antiartificial/replicant/internal/tui"
)

var version = "dev"

func main() {
	replicantName := flag.String("r", "deckard", "replicant name to load")
	modelOverride := flag.String("m", "", "override model (e.g. claude-sonnet-4-20250514)")
	showVersion := flag.Bool("v", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("replicant", version)
		return
	}

	if err := run(*replicantName, *modelOverride); err != nil {
		fmt.Fprintf(os.Stderr, "replicant: %v\n", err)
		os.Exit(1)
	}
}

func run(replicantName, modelOverride string) error {
	// Load configuration from environment.
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Discover replicant definitions.
	reg, err := replicant.NewRegistry(replicant.DefaultDirs()...)
	if err != nil {
		return fmt.Errorf("load replicants: %w", err)
	}

	def, ok := reg.Get(replicantName)
	if !ok {
		available := reg.List()
		names := make([]string, len(available))
		for i, d := range available {
			names[i] = d.Name
		}
		return fmt.Errorf("replicant %q not found (available: %v)", replicantName, names)
	}

	// Resolve the model.
	model := def.Model
	if modelOverride != "" {
		model = modelOverride
	}
	if model == "" {
		model = cfg.DefaultModel
	}

	// Create the provider and strip any "provider/" prefix from the model name.
	provider, model, err := agent.NewProvider(model, cfg.AnthropicKey, cfg.OpenAIKey)
	if err != nil {
		return fmt.Errorf("provider: %w", err)
	}

	// Parse autonomy level from config.
	autonomyLevel := parseAutonomyLevel(cfg.Autonomy)

	// Create the session log.
	session, err := agent.NewSession(cfg.SessionDir, def.Name, model)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	defer session.Close()

	// Set up tools.
	toolRegistry := tools.NewRegistry()
	resolvedTools := toolRegistry.Resolve(def.Tools)

	// Build tool definitions for the agent.
	toolDefs := make([]agent.ToolDef, len(resolvedTools))
	for i, t := range resolvedTools {
		toolDefs[i] = agent.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.Parameters(),
		}
	}

	// Create the tool runner callback.
	toolRunner := func(name string, args string) (string, error) {
		t, ok := toolRegistry.Get(name)
		if !ok {
			return "", fmt.Errorf("unknown tool: %s", name)
		}
		return t.Run(args)
	}

	// currentEventsCh is set by agentFn on each invocation so the permission
	// function can forward PermissionRequestMsg into the TUI event stream.
	var currentEventsCh chan<- tea.Msg

	// permFn checks risk level and, when confirmation is needed, sends a
	// PermissionRequestMsg to the TUI and blocks until the user responds.
	permFn := func(name, args string) (bool, error) {
		t, found := toolRegistry.Get(name)
		if !found {
			// Unknown tool — deny to be safe.
			return false, nil
		}
		if !t.Risk().NeedsConfirmation(autonomyLevel) {
			return true, nil
		}
		responseCh := make(chan bool, 1)
		if currentEventsCh != nil {
			currentEventsCh <- tui.PermissionRequestMsg{
				ToolCallID: name,
				ToolName:   name,
				Args:       args,
				RiskLevel:  t.Risk(),
				Response:   responseCh,
			}
		}
		approved := <-responseCh
		return approved, nil
	}

	// Create the agent.
	a := agent.NewAgent(provider,
		agent.WithSystemPrompt(def.SystemPrompt),
		agent.WithModel(model),
		agent.WithMaxTokens(def.MaxTokens),
		agent.WithTemperature(def.Temperature),
		agent.WithTools(toolDefs),
		agent.WithToolRunner(toolRunner),
		agent.WithMaxTurns(def.MaxTurns),
		agent.WithPermissionFn(permFn),
	)

	// Build the TUI agent function that bridges agent events to bubbletea messages.
	var history []agent.Message

	agentFn := func(ctx context.Context, message string, events chan<- tea.Msg) {
		// Expose the events channel to the permission function for this turn.
		currentEventsCh = events
		defer func() { currentEventsCh = nil }()

		// Log the user message to the session.
		_ = session.Append(agent.SessionEntry{
			Type:    "message",
			Role:    "user",
			Content: message,
		})

		agentEvents := make(chan agent.Event, 64)

		go func() {
			a.Run(ctx, message, history, agentEvents)
			close(agentEvents)
		}()

		for ev := range agentEvents {
			switch ev.Type {
			case agent.EventText:
				_ = session.Append(agent.SessionEntry{
					Type:    "message",
					Role:    "assistant",
					Content: ev.Text,
				})
				events <- tui.StreamChunkMsg{Text: ev.Text}
			case agent.EventToolCall:
				_ = session.Append(agent.SessionEntry{
					Type:     "tool_call",
					ToolName: ev.ToolName,
					ToolArgs: ev.ToolArgs,
					ToolID:   ev.ToolID,
				})
				events <- tui.ToolCallMsg{
					ID:   ev.ToolID,
					Name: ev.ToolName,
					Args: ev.ToolArgs,
				}
			case agent.EventToolResult:
				_ = session.Append(agent.SessionEntry{
					Type:   "tool_result",
					ToolID: ev.ToolID,
					Result: ev.Result,
				})
				events <- tui.ToolResultMsg{
					ID:      ev.ToolID,
					Result:  ev.Result,
					IsError: ev.IsError,
				}
			case agent.EventDone:
				// Accumulate messages for multi-turn history.
				history = append(history,
					agent.Message{
						Role:    "user",
						Content: []agent.ContentBlock{{Type: "text", Text: message}},
					},
				)
				// StreamDoneMsg is sent by the TUI channel drainer.
			case agent.EventError:
				events <- tui.StreamErrorMsg{Err: ev.Error}
			}
		}
	}

	// Launch the TUI.
	return tui.Run(model, agentFn)
}

// parseAutonomyLevel converts a config string to a permission.AutonomyLevel.
func parseAutonomyLevel(s string) permission.AutonomyLevel {
	switch s {
	case "full":
		return permission.AutonomyFull
	case "high":
		return permission.AutonomyHigh
	case "normal":
		return permission.AutonomyNormal
	default:
		return permission.AutonomyOff
	}
}
