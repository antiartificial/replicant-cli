package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// agentState represents the current state of the agent interaction loop.
type agentState int

const (
	stateIdle                  agentState = iota
	stateWaitingForAgent                  // submitted, waiting for first chunk
	stateStreaming                        // receiving stream chunks
	stateWaitingForPermission             // blocked on a permission y/n prompt
)

// AgentFunc is the callback type for agent interactions. Implementations should
// push StreamChunkMsg, ToolCallMsg, ToolResultMsg, and finally StreamDoneMsg (or
// StreamErrorMsg) onto events, then return.
type AgentFunc func(ctx context.Context, message string, events chan<- tea.Msg)

// CommandHandler processes slash commands typed by the user. It receives the
// command name (without the leading slash) and any trailing arguments. It
// returns a response string that is shown as a system message in the
// conversation, or an empty string to show nothing.
type CommandHandler func(command string, args string) string

// banner shown at startup
const bannerText = " ╱╲  REPLICANT\n╱  ╲ v0.1.0"

// AppModel is the root bubbletea model for the replicant TUI.
type AppModel struct {
	conversation ConversationModel
	input        InputModel
	statusbar    StatusBarModel
	spinner      SpinnerModel

	state      agentState
	agentFn    AgentFunc
	cmdHandler CommandHandler
	cancelFn   context.CancelFunc

	// pendingPermission is set while we are waiting for the user to respond
	// to a permission request with y or n.
	pendingPermission chan<- bool
	// pendingEventsCh is the agent events channel to resume draining after
	// a permission response is sent.
	pendingEventsCh <-chan tea.Msg

	// replayEntries holds session history to replay after the first resize.
	replayEntries []ReplayEntry

	width  int
	height int
}

// NewAppModel constructs the root model. agentFn may be nil for a stub UI.
// cmdHandler may be nil; if provided, slash commands are dispatched to it
// instead of being sent to the agent. initialAutonomy is the autonomy level
// string displayed in the status bar at startup (e.g. "off", "normal").
// replicantName is displayed as the assistant label in the conversation view.
func NewAppModel(modelName string, agentFn AgentFunc, cmdHandler CommandHandler, initialAutonomy string, replicantName string) AppModel {
	// Start with a small default size; real size comes from WindowSizeMsg.
	const defaultW, defaultH = 80, 24

	sb := NewStatusBarModel(modelName, defaultW)
	if initialAutonomy != "" {
		sb.SetAutonomy(initialAutonomy)
	}

	m := AppModel{
		conversation: NewConversationModel(defaultW, defaultH-statusBarHeight-defaultInputHeight, replicantName),
		input:        NewInputModel(defaultW),
		statusbar:    sb,
		spinner:      NewSpinnerModel(),
		agentFn:      agentFn,
		cmdHandler:   cmdHandler,
		width:        defaultW,
		height:       defaultH,
	}
	return m
}

// WithReplayEntries returns a copy of m with the given replay entries set.
// They will be replayed into the conversation view after the first resize.
func (m AppModel) WithReplayEntries(entries []ReplayEntry) AppModel {
	m.replayEntries = entries
	return m
}

const (
	statusBarHeight    = 1
	defaultInputHeight = 5 // textarea(3) + border(2)
)

// Init is called once at startup. It returns a command that fires a
// WindowSizeMsg so the layout is initialised properly.
func (m AppModel) Init() tea.Cmd {
	return tea.Batch(
		m.input.Init(),
		// trigger an initial banner paint after the window size is known
	)
}

// Update handles all incoming messages.
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	// ── window resize ────────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.relayout()

		// Add banner once (only when blocks is empty) then replay history.
		if len(m.conversation.blocks) == 0 {
			m.conversation.AddBanner(bannerText)
			if len(m.replayEntries) > 0 {
				m.conversation.ReplayHistory(m.replayEntries)
				m.replayEntries = nil
			}
		}

	// ── keyboard ─────────────────────────────────────────────────────────────
	case tea.KeyMsg:
		// Handle permission prompt first so y/n is not forwarded to the input.
		if m.state == stateWaitingForPermission && m.pendingPermission != nil {
			switch msg.String() {
			case "y", "Y":
				m.pendingPermission <- true
				m.pendingPermission = nil
				m.state = stateStreaming
				m.statusbar.SetStreaming(true)
				if m.pendingEventsCh != nil {
					ch := m.pendingEventsCh
					m.pendingEventsCh = nil
					return m, drainChannel(ch)
				}
				return m, nil
			case "n", "N", "esc":
				m.pendingPermission <- false
				m.pendingPermission = nil
				m.state = stateStreaming
				m.statusbar.SetStreaming(true)
				if m.pendingEventsCh != nil {
					ch := m.pendingEventsCh
					m.pendingEventsCh = nil
					return m, drainChannel(ch)
				}
				return m, nil
			case "ctrl+c":
				if m.pendingPermission != nil {
					m.pendingPermission <- false
					m.pendingPermission = nil
				}
				if m.cancelFn != nil {
					m.cancelFn()
				}
				return m, tea.Quit
			default:
				// Ignore all other keys while waiting for permission.
				return m, nil
			}
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			if m.cancelFn != nil {
				m.cancelFn()
			}
			return m, tea.Quit

		case tea.KeyEsc:
			// Interrupt active streaming
			if m.state != stateIdle && m.cancelFn != nil {
				m.cancelFn()
				m.cancelFn = nil
				m.state = stateIdle
				m.spinner.Stop()
				m.conversation.FinalizeAssistant()
				m.input.Enable()
				m.statusbar.SetStreaming(false)
			}
			return m, nil
		}

	// ── user submitted text ──────────────────────────────────────────────────
	case SubmitMsg:
		if m.state != stateIdle {
			return m, nil
		}
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			return m, nil
		}

		// Check for slash commands — handle locally without sending to agent.
		if strings.HasPrefix(text, "/") {
			m.input.Enable()
			cmd, args, _ := strings.Cut(strings.TrimPrefix(text, "/"), " ")
			cmd = strings.ToLower(strings.TrimSpace(cmd))
			args = strings.TrimSpace(args)

			// Built-in /quit handled here directly.
			if cmd == "quit" || cmd == "q" {
				return m, tea.Quit
			}

			// Dispatch to handler if available.
			if m.cmdHandler != nil {
				response := m.cmdHandler(cmd, args)
				if response != "" {
					m.conversation.AddBanner(response)
				}
			} else {
				m.conversation.AddBanner(fmt.Sprintf("unknown command: /%s", cmd))
			}
			return m, nil
		}

		m.conversation.AddUserMessage(text)
		m.input.Disable()
		m.state = stateWaitingForAgent
		m.statusbar.SetStreaming(true)
		cmds = append(cmds, m.spinner.Start())

		if m.agentFn != nil {
			ctx, cancel := context.WithCancel(context.Background())
			m.cancelFn = cancel
			events := make(chan tea.Msg, 64)
			go func() {
				m.agentFn(ctx, text, events)
				close(events)
			}()
			cmds = append(cmds, drainChannel(events))
		} else {
			// Stub: echo the message back
			cmds = append(cmds, stubAgentCmd(text))
		}

	// ── agent stream events ──────────────────────────────────────────────────
	case streamChunkWithChanMsg:
		if m.state == stateWaitingForAgent {
			m.state = stateStreaming
			m.spinner.Stop()
			m.conversation.StartAssistantMessage()
		}
		m.conversation.AppendChunk(msg.Text)
		cmds = append(cmds, drainChannel(msg.ch))

	case toolCallWithChanMsg:
		if m.state == stateWaitingForAgent {
			m.state = stateStreaming
			m.spinner.Stop()
		}
		m.conversation.AddToolCall(msg.ID, msg.Name, msg.Args)
		cmds = append(cmds, drainChannel(msg.ch))

	case toolResultWithChanMsg:
		m.conversation.AddToolResult(msg.ID, msg.Result, msg.IsError)
		cmds = append(cmds, drainChannel(msg.ch))

	case toolProgressWithChanMsg:
		m.conversation.AppendToolProgress(msg.ID, msg.Output)
		cmds = append(cmds, drainChannel(msg.ch))

	// Plain versions (for external callers who don't use channel-draining)
	case StreamChunkMsg:
		if m.state == stateWaitingForAgent {
			m.state = stateStreaming
			m.spinner.Stop()
			m.conversation.StartAssistantMessage()
		}
		m.conversation.AppendChunk(msg.Text)

	case ToolCallMsg:
		if m.state == stateWaitingForAgent {
			m.state = stateStreaming
			m.spinner.Stop()
		}
		m.conversation.AddToolCall(msg.ID, msg.Name, msg.Args)

	case ToolResultMsg:
		m.conversation.AddToolResult(msg.ID, msg.Result, msg.IsError)

	case ToolProgressMsg:
		m.conversation.AppendToolProgress(msg.ID, msg.Output)

	case StreamDoneMsg:
		m.conversation.FinalizeAssistant()
		m.state = stateIdle
		m.spinner.Stop()
		m.input.Enable()
		m.statusbar.SetStreaming(false)
		if m.cancelFn != nil {
			m.cancelFn()
			m.cancelFn = nil
		}

	case StreamErrorMsg:
		errText := fmt.Sprintf("error: %v", msg.Err)
		m.conversation.AddToolResult("", errText, true)
		m.conversation.FinalizeAssistant()
		m.state = stateIdle
		m.spinner.Stop()
		m.input.Enable()
		m.statusbar.SetStreaming(false)
		if m.cancelFn != nil {
			m.cancelFn()
			m.cancelFn = nil
		}

	case PermissionRequestMsg:
		// Show a confirmation prompt and wait for the user to press y or n.
		prompt := fmt.Sprintf("[%s] wants to run: %s\nAllow? (y/n)", msg.ToolName, msg.Args)
		m.conversation.AddBanner(prompt)
		m.pendingPermission = msg.Response
		m.state = stateWaitingForPermission
		m.spinner.Stop()
		m.statusbar.SetStreaming(false)

	case AutonomyChangedMsg:
		m.statusbar.SetAutonomy(msg.Level)

	case permissionRequestWithChanMsg:
		// Same as PermissionRequestMsg but we also have the events channel so
		// we can resume draining once the user responds.
		prompt := fmt.Sprintf("[%s] wants to run: %s\nAllow? (y/n)", msg.ToolName, msg.Args)
		m.conversation.AddBanner(prompt)
		m.pendingPermission = msg.Response
		m.pendingEventsCh = msg.ch
		m.state = stateWaitingForPermission
		m.spinner.Stop()
		m.statusbar.SetStreaming(false)
	}

	// ── delegate to sub-models ───────────────────────────────────────────────
	{
		var cmd tea.Cmd
		m.conversation, cmd = m.conversation.Update(msg)
		cmds = append(cmds, cmd)
	}
	{
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}
	{
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// View renders the full terminal layout.
//
//	┌──────────────────────────────┐
//	│  conversation (fills space)  │
//	│                              │
//	│  [spinner if waiting]        │
//	├──────────────────────────────┤
//	│  input (3-5 lines + border)  │
//	├──────────────────────────────┤
//	│  status bar (1 line)         │
//	└──────────────────────────────┘
func (m AppModel) View() string {
	var sb strings.Builder

	// conversation (may include spinner line at bottom)
	convView := m.conversation.View()
	if m.spinner.Active() {
		convView += "\n" + m.spinner.View()
	}
	sb.WriteString(convView)
	sb.WriteByte('\n')

	// input box
	sb.WriteString(m.input.View())
	sb.WriteByte('\n')

	// status bar (always last line)
	sb.WriteString(m.statusbar.View())

	return sb.String()
}

// relayout recalculates component sizes after a resize.
func (m AppModel) relayout() AppModel {
	inputH := m.input.Height()
	convH := m.height - inputH - statusBarHeight
	if convH < 1 {
		convH = 1
	}

	m.conversation.SetSize(m.width, convH)
	m.input.SetWidth(m.width)
	m.statusbar.SetWidth(m.width)
	return m
}

// ── helpers ───────────────────────────────────────────────────────────────────

// Internal channel-aware message wrappers so the Update loop can re-schedule
// drainChannel after each event without the caller needing to know about it.

type streamChunkWithChanMsg struct {
	StreamChunkMsg
	ch <-chan tea.Msg
}

type toolCallWithChanMsg struct {
	ToolCallMsg
	ch <-chan tea.Msg
}

type toolResultWithChanMsg struct {
	ToolResultMsg
	ch <-chan tea.Msg
}

type permissionRequestWithChanMsg struct {
	PermissionRequestMsg
	ch <-chan tea.Msg
}

type toolProgressWithChanMsg struct {
	ToolProgressMsg
	ch <-chan tea.Msg
}

// drainChannel reads the next message from ch and wraps it so the Update
// loop knows to keep draining. Closes with StreamDoneMsg when ch is closed.
func drainChannel(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return StreamDoneMsg{}
		}
		switch m := msg.(type) {
		case StreamChunkMsg:
			return streamChunkWithChanMsg{StreamChunkMsg: m, ch: ch}
		case ToolCallMsg:
			return toolCallWithChanMsg{ToolCallMsg: m, ch: ch}
		case ToolResultMsg:
			return toolResultWithChanMsg{ToolResultMsg: m, ch: ch}
		case PermissionRequestMsg:
			return permissionRequestWithChanMsg{PermissionRequestMsg: m, ch: ch}
		case ToolProgressMsg:
			return toolProgressWithChanMsg{ToolProgressMsg: m, ch: ch}
		default:
			// StreamDoneMsg, StreamErrorMsg, etc. pass through
			return msg
		}
	}
}

// stubAgentCmd simulates an agent response for testing the UI without a real agent.
func stubAgentCmd(input string) tea.Cmd {
	return func() tea.Msg {
		return StreamChunkMsg{Text: fmt.Sprintf("I received: %q\n\nThis is a stub response — wire up a real AgentFunc to connect to the LLM.", input)}
	}
}

// Run is the entry point for launching the TUI program.
// replayEntries may be nil; when non-nil, the session history is replayed
// into the conversation view before the user's first input.
// replicantName is shown as the assistant label in the conversation view.
func Run(modelName string, agentFn AgentFunc, cmdHandler CommandHandler, initialAutonomy string, replayEntries []ReplayEntry, replicantName string) error {
	m := NewAppModel(modelName, agentFn, cmdHandler, initialAutonomy, replicantName)
	if len(replayEntries) > 0 {
		m = m.WithReplayEntries(replayEntries)
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
