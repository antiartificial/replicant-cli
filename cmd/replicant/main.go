package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/antiartificial/replicant/internal/agent"
	"github.com/antiartificial/replicant/internal/config"
	"github.com/antiartificial/replicant/internal/replicant"
	"github.com/antiartificial/replicant/internal/tools"
	"github.com/antiartificial/replicant/internal/tui"
)

func main() {
	replicantName := flag.String("r", "deckard", "replicant name to load")
	modelOverride := flag.String("m", "", "override model (e.g. claude-sonnet-4-20250514)")
	flag.Parse()

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
	// Strip provider prefix if present (e.g. "anthropic/claude-sonnet-4-20250514" -> "claude-sonnet-4-20250514")
	model = stripProviderPrefix(model)

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

	// Create the Anthropic provider.
	provider := agent.NewAnthropicProvider(cfg.AnthropicKey)

	// Create the agent.
	a := agent.NewAgent(provider,
		agent.WithSystemPrompt(def.SystemPrompt),
		agent.WithModel(model),
		agent.WithMaxTokens(def.MaxTokens),
		agent.WithTemperature(def.Temperature),
		agent.WithTools(toolDefs),
		agent.WithToolRunner(toolRunner),
		agent.WithMaxTurns(def.MaxTurns),
	)

	// Build the TUI agent function that bridges agent events to bubbletea messages.
	var history []agent.Message

	agentFn := func(ctx context.Context, message string, events chan<- tea.Msg) {
		agentEvents := make(chan agent.Event, 64)

		go func() {
			a.Run(ctx, message, history, agentEvents)
			close(agentEvents)
		}()

		for ev := range agentEvents {
			switch ev.Type {
			case agent.EventText:
				events <- tui.StreamChunkMsg{Text: ev.Text}
			case agent.EventToolCall:
				events <- tui.ToolCallMsg{
					ID:   ev.ToolID,
					Name: ev.ToolName,
					Args: ev.ToolArgs,
				}
			case agent.EventToolResult:
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

// stripProviderPrefix removes a "provider/" prefix from a model string.
func stripProviderPrefix(model string) string {
	for i := range model {
		if model[i] == '/' {
			return model[i+1:]
		}
	}
	return model
}
