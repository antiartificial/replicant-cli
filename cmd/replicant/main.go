package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

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
	resumeID := flag.String("resume", "", "session ID or path to resume")
	flag.StringVar(resumeID, "s", "", "session ID or path to resume (shorthand)")
	listSessions := flag.Bool("list-sessions", false, "list recent sessions and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("replicant", version)
		return
	}

	if *listSessions {
		if err := runListSessions(); err != nil {
			fmt.Fprintf(os.Stderr, "replicant: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := run(*replicantName, *modelOverride, *resumeID); err != nil {
		fmt.Fprintf(os.Stderr, "replicant: %v\n", err)
		os.Exit(1)
	}
}

// runListSessions prints the most recent sessions in a human-readable table.
func runListSessions() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sessions, err := agent.ListSessions(cfg.SessionDir, 20)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	if len(sessions) == 0 {
		fmt.Println("no sessions found in", cfg.SessionDir)
		return nil
	}
	fmt.Printf("%-38s  %-12s  %-30s  %s\n", "ID", "REPLICANT", "MODEL", "STARTED")
	fmt.Println(strings.Repeat("-", 100))
	for _, s := range sessions {
		fmt.Printf("%-38s  %-12s  %-30s  %s\n",
			s.ID,
			s.Replicant,
			s.Model,
			s.StartedAt.Local().Format("2006-01-02 15:04:05"),
		)
	}
	return nil
}

func run(replicantName, modelOverride, resumeID string) error {
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

	// Parse autonomy level from config. We keep a pointer so the command
	// handler and permFn always see the current value.
	autonomyLevelVal := parseAutonomyLevel(cfg.Autonomy)
	autonomyLevel := &autonomyLevelVal

	// Determine session: resume an existing one or create a new one.
	var (
		session       *agent.Session
		history       []agent.Message
		replayEntries []tui.ReplayEntry
	)

	if resumeID != "" {
		// Locate the session file.
		sessionPath, findErr := agent.FindSession(cfg.SessionDir, resumeID)
		if findErr != nil {
			return findErr
		}

		// Load all entries for history reconstruction and TUI replay.
		entries, loadErr := agent.LoadSession(sessionPath)
		if loadErr != nil {
			return fmt.Errorf("load session: %w", loadErr)
		}

		// Reconstruct agent history so the model has full context.
		history = agent.ReconstructHistory(entries)

		// Build TUI replay entries (skip session_start).
		for _, e := range entries {
			switch e.Type {
			case "message":
				replayEntries = append(replayEntries, tui.ReplayEntry{
					Type:    e.Role, // "user" or "assistant"
					Content: e.Content,
				})
			case "tool_call":
				replayEntries = append(replayEntries, tui.ReplayEntry{
					Type:     "tool_call",
					ToolName: e.ToolName,
					ToolArgs: e.ToolArgs,
					ToolID:   e.ToolID,
				})
			case "tool_result":
				replayEntries = append(replayEntries, tui.ReplayEntry{
					Type:    "tool_result",
					Content: e.Result,
					ToolID:  e.ToolID,
					IsError: e.IsError,
				})
			}
		}

		// Open the session file in append mode.
		session, err = agent.ResumeSession(sessionPath)
		if err != nil {
			return fmt.Errorf("resume session: %w", err)
		}
	} else {
		// Create a fresh session log.
		session, err = agent.NewSession(cfg.SessionDir, def.Name, model)
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
	}
	defer session.Close()

	// Set up tools.
	toolRegistry := tools.NewRegistry()

	// Wire up the delegate tool so replicants can spawn child agents.
	// The provider factory closure captures the API keys from config.
	delegateTool := tools.NewDelegateTool(reg, toolRegistry, func(model string) (agent.Provider, string, error) {
		if model == "" {
			model = cfg.DefaultModel
		}
		return agent.NewProvider(model, cfg.AnthropicKey, cfg.OpenAIKey)
	})
	toolRegistry.Register(delegateTool)

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
		if !t.Risk().NeedsConfirmation(*autonomyLevel) {
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
		agent.WithAutoCompact(100000),
	)

	// Build the TUI agent function that bridges agent events to bubbletea messages.
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
			case agent.EventCompact:
				_ = session.Append(agent.SessionEntry{
					Type:    "message",
					Role:    "assistant",
					Content: fmt.Sprintf("[context compacted: %d → %d messages]", ev.CompactedFrom, ev.CompactedTo),
				})
				events <- tui.StreamChunkMsg{Text: fmt.Sprintf("\n[compacting conversation: %d → %d messages]\n", ev.CompactedFrom, ev.CompactedTo)}
			case agent.EventError:
				events <- tui.StreamErrorMsg{Err: ev.Error}
			}
		}
	}

	// Build the command handler closure that handles slash commands typed by
	// the user. autonomyLevel is a pointer so changes here are immediately
	// visible to permFn above.
	cmdHandler := func(command, args string) string {
		switch command {
		case "auto", "autonomy":
			if args == "" {
				return fmt.Sprintf("autonomy: %s", autonomyLevelName(*autonomyLevel))
			}
			switch args {
			case "full":
				*autonomyLevel = permission.AutonomyFull
			case "high":
				*autonomyLevel = permission.AutonomyHigh
			case "normal":
				*autonomyLevel = permission.AutonomyNormal
			case "off":
				*autonomyLevel = permission.AutonomyOff
			default:
				return fmt.Sprintf("unknown autonomy level %q — valid: off, normal, high, full", args)
			}
			name := autonomyLevelName(*autonomyLevel)
			return fmt.Sprintf("autonomy set to: %s", name)

		case "help":
			return strings.Join([]string{
				"available commands:",
				"  /auto              — show current autonomy level",
				"  /auto <level>      — set autonomy (off|normal|high|full)",
				"  /model             — show current model",
				"  /session           — show current session path",
				"  /help              — show this help",
				"  /quit              — exit replicant",
			}, "\n")

		case "model":
			return fmt.Sprintf("model: %s", model)

		case "session":
			return fmt.Sprintf("session: %s", session.ID)

		default:
			return fmt.Sprintf("unknown command: /%s — type /help for a list", command)
		}
	}

	// Launch the TUI.
	return tui.Run(model, agentFn, tui.CommandHandler(cmdHandler), autonomyLevelName(*autonomyLevel), replayEntries)
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

// autonomyLevelName returns the canonical string for an AutonomyLevel.
func autonomyLevelName(l permission.AutonomyLevel) string {
	switch l {
	case permission.AutonomyFull:
		return "full"
	case permission.AutonomyHigh:
		return "high"
	case permission.AutonomyNormal:
		return "normal"
	default:
		return "off"
	}
}
