package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/antiartificial/replicant/internal/agent"
	"github.com/antiartificial/replicant/internal/config"
	"github.com/antiartificial/replicant/internal/memory"
	internalmcp "github.com/antiartificial/replicant/internal/mcp"
	"github.com/antiartificial/replicant/internal/mission"
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

	// Wire up MCP servers. Failures are non-fatal: we log warnings and continue.
	mcpManager := internalmcp.NewManager()

	// Load global MCP config from ~/.replicant/mcp.json (optional).
	globalMCPPath := filepath.Join(os.Getenv("HOME"), ".replicant", "mcp.json")
	if _, statErr := os.Stat(globalMCPPath); statErr == nil {
		if loadErr := mcpManager.LoadConfig(globalMCPPath); loadErr != nil {
			fmt.Fprintf(os.Stderr, "replicant: mcp global config: %v\n", loadErr)
		}
	}

	// Add per-replicant MCP servers from the replicant definition.
	for name, srv := range def.MCPServers {
		addErr := mcpManager.AddServer(name, internalmcp.ServerConfig{
			Command:   srv.Command,
			Args:      srv.Args,
			Env:       srv.Env,
			URL:       srv.URL,
			Transport: srv.Transport,
		})
		if addErr != nil {
			fmt.Fprintf(os.Stderr, "replicant: mcp add server %q: %v\n", name, addErr)
		}
	}

	// Connect all MCP servers; warn about individual failures but keep going.
	connectCtx := context.Background()
	if connectErr := mcpManager.ConnectAll(connectCtx); connectErr != nil {
		fmt.Fprintf(os.Stderr, "replicant: mcp connect: %v\n", connectErr)
	}
	defer mcpManager.Close()

	// Register all MCP tool adapters so they are available to the agent.
	for _, adapter := range mcpManager.AllTools() {
		toolRegistry.Register(adapter)
	}

	// Open the agent memory store. Log a warning on error but don't abort —
	// the agent can still function without memory.
	mem, memErr := memory.New(cfg.MemoryDir)
	if memErr != nil {
		fmt.Fprintf(os.Stderr, "replicant: memory unavailable: %v\n", memErr)
	} else {
		defer mem.Close()
		toolRegistry.Register(tools.NewRememberTool(mem))
		toolRegistry.Register(tools.NewRecallTool(mem))
	}

	// Provider factory closure shared by delegate and spawn tools.
	providerFactory := func(m string) (agent.Provider, string, error) {
		if m == "" {
			m = cfg.DefaultModel
		}
		return agent.NewProvider(m, cfg.AnthropicKey, cfg.OpenAIKey)
	}

	// Wire up the delegate tool so replicants can spawn child agents from
	// predefined replicant definitions.
	delegateTool := tools.NewDelegateTool(reg, toolRegistry, providerFactory)
	toolRegistry.Register(delegateTool)

	// Wire up the spawn tool so replicants can create ad-hoc child agents
	// with inline system prompts at runtime.
	spawnTool := tools.NewSpawnTool(toolRegistry, providerFactory, model)
	toolRegistry.Register(spawnTool)

	// Wire up the mission tool so orchestrators can plan and run structured
	// multi-milestone missions with validation contracts.
	missionStore := mission.NewStore(cfg.MissionDir)
	missionEngine := mission.NewEngine(tools.NewRegistryAdapter(toolRegistry), providerFactory, model, missionStore)
	missionTool := tools.NewMissionTool(missionEngine, missionStore)
	toolRegistry.Register(missionTool)

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

	// Create the context-aware tool runner callback. This threads the agent's
	// context into tool execution so cancellation (Esc) and timeouts propagate.
	toolRunnerCtx := func(ctx context.Context, name string, args string) (string, error) {
		t, ok := toolRegistry.Get(name)
		if !ok {
			return "", fmt.Errorf("unknown tool: %s", name)
		}
		return tools.RunTool(ctx, t, args)
	}

	// toolStreamer is called for tools that support streaming output. It checks
	// whether the tool implements StreamingTool; if so, it runs it with a
	// progress channel and returns the result. Progress lines are sent back via
	// the channel for the agent to emit as EventToolProgress events.
	// When the tool does not support streaming it returns nil so the agent
	// falls through to toolRunnerCtx.
	toolStreamer := func(ctx context.Context, name, args string, progress chan<- string) *agent.ToolStreamResult {
		t, ok := toolRegistry.Get(name)
		if !ok {
			return &agent.ToolStreamResult{Err: fmt.Errorf("unknown tool: %s", name)}
		}
		st, ok := t.(tools.StreamingTool)
		if !ok {
			// Not a streaming tool — fall through to toolRunnerCtx.
			return nil
		}
		result, err := st.RunStreaming(ctx, args, progress)
		return &agent.ToolStreamResult{Result: result, Err: err}
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
		agent.WithToolRunnerCtx(toolRunnerCtx),
		agent.WithToolStreamer(toolStreamer),
		agent.WithMaxTurns(def.MaxTurns),
		agent.WithPermissionFn(permFn),
		agent.WithAutoCompact(agent.CompactThreshold(model)),
	)

	// Build the TUI agent function that bridges agent events to bubbletea messages.
	agentFn := func(ctx context.Context, message string, events chan<- tea.Msg) {
		// Expose the events channel to the permission function for this turn.
		currentEventsCh = events
		defer func() { currentEventsCh = nil }()

		// Track the last assistant reply so we can summarise the turn for memory.
		var lastAssistantText string

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
				lastAssistantText += ev.Text
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
				// Auto-store a brief session summary in agent memory.
				if mem != nil && lastAssistantText != "" {
					summary := buildSessionSummary(message, lastAssistantText)
					_ = mem.RememberSession(ctx, session.ID, def.Name, summary)
				}
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
	return tui.Run(model, agentFn, tui.CommandHandler(cmdHandler), autonomyLevelName(*autonomyLevel), replayEntries, def.Name)
}

// buildSessionSummary creates a brief summary of a single agent turn for
// storage in agent memory. It truncates both halves to keep the stored text
// manageable.
func buildSessionSummary(userMsg, assistantReply string) string {
	const maxLen = 300
	truncate := func(s string) string {
		s = strings.TrimSpace(s)
		if len(s) > maxLen {
			s = s[:maxLen] + "…"
		}
		return s
	}
	return fmt.Sprintf("User: %s\nAssistant: %s", truncate(userMsg), truncate(assistantReply))
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
