package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// maxResultLines is the number of output lines shown before truncation.
const maxResultLines = 15

// maxArgValueLines is the number of lines shown per arg value before truncation.
const maxArgValueLines = 3

// ReplayEntry is a single item replayed into the conversation view when resuming
// a previous session. It mirrors SessionEntry but lives in the tui package so
// that tui has no import cycle dependency on agent.
type ReplayEntry struct {
	Type     string // "user", "assistant", "tool_call", "tool_result"
	Content  string
	ToolName string
	ToolArgs string
	ToolID   string
	IsError  bool
}

// messageKind classifies the role of a rendered conversation block.
type messageKind int

const (
	kindUser messageKind = iota
	kindAssistant
	kindToolCall
	kindToolResult
	kindBanner
	kindTaskGroup
)

// messageBlock is a single rendered unit in the conversation history.
type messageBlock struct {
	kind      messageKind
	rendered  string // final rendered string (may be multi-line)
	rawText   string // unstyled text content (for copy-to-clipboard)
	timestamp time.Time
	id        string // tool call ID for linking calls to results
}

// ConversationModel holds the scrollable conversation viewport and message history.
type ConversationModel struct {
	viewport viewport.Model
	blocks   []messageBlock

	// current streaming assistant block (index into blocks, -1 if none)
	streamingIdx int
	streamBuf    *strings.Builder

	replicantName string

	// activeTasks tracks running/completed tasks for the task group display.
	activeTasks []taskEntry
	// taskBlockIdx is the index of the kindTaskGroup block (-1 if none).
	taskBlockIdx int
	// taskFrame is the animation frame counter for spinning indicators.
	taskFrame int

	width  int
	height int
}

// NewConversationModel creates an empty conversation view.
// replicantName is shown in the assistant label (e.g. "deckard", "rachael").
func NewConversationModel(width, height int, replicantName string) ConversationModel {
	if replicantName == "" {
		replicantName = "assistant"
	}
	vp := viewport.New(width, height)
	vp.SetContent("")
	return ConversationModel{
		viewport:      vp,
		replicantName: replicantName,
		streamingIdx:  -1,
		taskBlockIdx:  -1,
		streamBuf:     &strings.Builder{},
		width:         width,
		height:        height,
	}
}

// SetSize resizes the viewport.
func (m *ConversationModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.viewport.Width = width
	m.viewport.Height = height
	m.rebuildViewport()
}

// AddBanner adds the startup banner as the first block.
func (m *ConversationModel) AddBanner(text string) {
	m.blocks = append(m.blocks, messageBlock{
		kind:      kindBanner,
		rendered:  StyleLogo.Render(text),
		timestamp: time.Now(),
	})
	m.rebuildViewport()
}

// AddUserMessage appends a completed user message block.
func (m *ConversationModel) AddUserMessage(text string) {
	label := StyleUserLabel.Render("▸ you")
	ts := StyleTimestamp.Render(time.Now().Format("15:04:05"))
	header := lipgloss.JoinHorizontal(lipgloss.Top, label, "  ", ts)
	body := StyleUserMessage.Width(m.width - 2).Render(text)
	rendered := header + "\n" + body + "\n"

	m.blocks = append(m.blocks, messageBlock{
		kind:      kindUser,
		rendered:  rendered,
		timestamp: time.Now(),
	})
	m.rebuildViewport()
}

// StartAssistantMessage opens a new streaming assistant block.
func (m *ConversationModel) StartAssistantMessage() {
	m.streamBuf.Reset()
	label := StyleAssistantLabel.Render("▸ " + m.replicantName)
	ts := StyleTimestamp.Render(time.Now().Format("15:04:05"))
	header := lipgloss.JoinHorizontal(lipgloss.Top, label, "  ", ts)

	m.blocks = append(m.blocks, messageBlock{
		kind:      kindAssistant,
		rendered:  header + "\n",
		timestamp: time.Now(),
	})
	m.streamingIdx = len(m.blocks) - 1
	m.rebuildViewport()
}

// AppendChunk appends streaming text to the current assistant block.
// If no assistant block is active (e.g. after a tool call cycle), a new
// one is started automatically so text isn't silently dropped.
func (m *ConversationModel) AppendChunk(text string) {
	if m.streamingIdx < 0 || m.streamingIdx >= len(m.blocks) {
		m.StartAssistantMessage()
	}
	m.streamBuf.WriteString(text)
	b := &m.blocks[m.streamingIdx]

	label := StyleAssistantLabel.Render("▸ " + m.replicantName)
	ts := StyleTimestamp.Render(b.timestamp.Format("15:04:05"))
	header := lipgloss.JoinHorizontal(lipgloss.Top, label, "  ", ts)
	b.rawText = m.streamBuf.String()
	body := StyleAssistantMessage.Width(m.width - 2).Render(b.rawText)
	b.rendered = header + "\n" + body + "\n"

	m.rebuildViewport()
}

// FinalizeAssistant closes the current streaming assistant block.
func (m *ConversationModel) FinalizeAssistant() {
	if m.streamingIdx >= 0 && m.streamingIdx < len(m.blocks) {
		b := &m.blocks[m.streamingIdx]
		// Append a trailing newline separator
		b.rendered += StyleSeparator.Render(strings.Repeat("─", m.width)) + "\n"
	}
	m.streamingIdx = -1
	m.streamBuf.Reset()
	m.rebuildViewport()
}

// AddToolCall appends a tool call block with formatted args.
func (m *ConversationModel) AddToolCall(id, name, args string) {
	label := StyleToolCallLabel.Render("◆ " + name)
	argsFormatted := formatToolArgs(name, args)
	argsRendered := StyleToolCallArgs.Width(m.width - 6).Render(argsFormatted)
	rendered := label + "\n" + argsRendered + "\n"

	m.blocks = append(m.blocks, messageBlock{
		kind:      kindToolCall,
		rendered:  rendered,
		timestamp: time.Now(),
		id:        id,
	})
	m.rebuildViewport()
}

// AddToolResult appends the result of a tool call with truncation and status.
func (m *ConversationModel) AddToolResult(id, result string, isError bool) {
	if isError {
		body := StyleToolResultError.Width(m.width - 6).Render("  error: " + result)
		m.blocks = append(m.blocks, messageBlock{
			kind:      kindToolResult,
			rendered:  body + "\n",
			timestamp: time.Now(),
			id:        id,
		})
		m.rebuildViewport()
		return
	}

	lines := strings.Split(result, "\n")
	totalLines := len(lines)

	var displayLines []string
	var meta string
	if totalLines > maxResultLines {
		displayLines = lines[:maxResultLines]
		hidden := totalLines - maxResultLines
		meta = fmt.Sprintf("[%d more lines]", hidden)
	} else {
		displayLines = lines
	}

	// Indent each line for visual hierarchy.
	indented := make([]string, len(displayLines))
	for i, l := range displayLines {
		indented[i] = "    " + l
	}
	content := strings.Join(indented, "\n")

	body := StyleToolResult.Width(m.width - 6).Render(content)
	rendered := body + "\n"
	if meta != "" {
		rendered += StyleToolResultMeta.Render(meta) + "\n"
	}

	m.blocks = append(m.blocks, messageBlock{
		kind:      kindToolResult,
		rendered:  rendered,
		timestamp: time.Now(),
		id:        id,
	})
	m.rebuildViewport()
}

// AppendToolProgress appends a partial output line to the most recent tool call
// block that matches the given ID. If no matching block is found it appends to
// the most recent tool call block regardless of ID.
func (m *ConversationModel) AppendToolProgress(id, output string) {
	// Find the most recent kindToolCall block with a matching id.
	idx := -1
	for i := len(m.blocks) - 1; i >= 0; i-- {
		if m.blocks[i].kind == kindToolCall {
			if m.blocks[i].id == id || idx == -1 {
				idx = i
			}
			if m.blocks[i].id == id {
				break
			}
		}
	}
	if idx < 0 {
		return
	}
	b := &m.blocks[idx]
	// Append the progress line inside the existing block rendered text.
	progress := StyleToolResult.Width(m.width - 6).Render("  » " + output)
	b.rendered += progress + "\n"
	m.rebuildViewport()
}

// ReplayHistory adds previously loaded session entries to the conversation view.
// It should be called once after the banner is shown, before the user's first input.
func (m *ConversationModel) ReplayHistory(entries []ReplayEntry) {
	inAssistant := false
	for _, e := range entries {
		switch e.Type {
		case "user":
			if inAssistant {
				m.FinalizeAssistant()
				inAssistant = false
			}
			m.AddUserMessage(e.Content)
		case "assistant":
			if !inAssistant {
				m.StartAssistantMessage()
				inAssistant = true
			}
			m.AppendChunk(e.Content)
		case "tool_call":
			if inAssistant {
				m.FinalizeAssistant()
				inAssistant = false
			}
			m.AddToolCall(e.ToolID, e.ToolName, e.ToolArgs)
		case "tool_result":
			m.AddToolResult(e.ToolID, e.Content, e.IsError)
		}
	}
	if inAssistant {
		m.FinalizeAssistant()
	}
}

// taskEntry tracks one active or completed tool/subtask.
type taskEntry struct {
	id     string
	name   string
	status string // "running", "completed", "failed", "waiting"
	detail string
}

// Spinner frames for running/waiting tasks (braille-style).
var runningFrames = []string{"/", "-", "\\", "|"}
var waitingFrames = []string{".  ", ".. ", "...", "   "}

// UpdateTask updates or adds a task in the active task group display.
func (m *ConversationModel) UpdateTask(id, name, status, detail string) {
	found := false
	for i := range m.activeTasks {
		if m.activeTasks[i].id == id {
			m.activeTasks[i].status = status
			if detail != "" {
				m.activeTasks[i].detail = detail
			}
			found = true
			break
		}
	}
	if !found {
		m.activeTasks = append(m.activeTasks, taskEntry{
			id: id, name: name, status: status, detail: detail,
		})
	}
	m.renderTaskGroup()
}

// AdvanceTaskFrame increments the animation frame and re-renders active tasks.
// Call this on every spinner tick when tasks are active.
func (m *ConversationModel) AdvanceTaskFrame() {
	if len(m.activeTasks) == 0 {
		return
	}
	m.taskFrame++
	m.renderTaskGroup()
}

// HasActiveTasks returns true if any tasks are running or waiting.
func (m *ConversationModel) HasActiveTasks() bool {
	for _, t := range m.activeTasks {
		if t.status == "running" || t.status == "waiting" {
			return true
		}
	}
	return false
}

func (m *ConversationModel) renderTaskGroup() {
	var sb strings.Builder
	for _, t := range m.activeTasks {
		var icon string
		var style lipgloss.Style

		switch t.status {
		case "running":
			icon = runningFrames[m.taskFrame%len(runningFrames)]
			style = StyleToolCallLabel
		case "waiting":
			icon = waitingFrames[m.taskFrame%len(waitingFrames)]
			style = StyleToolCallArgs
		case "completed":
			icon = "ok"
			style = StyleToolResult
		case "failed":
			icon = "!!"
			style = StyleToolResultError
		default:
			icon = "  "
			style = StyleToolCallArgs
		}

		line := fmt.Sprintf("  [%s] %s", icon, t.name)
		if t.detail != "" {
			line += " " + StyleTimestamp.Render(t.detail)
		}
		sb.WriteString(style.Render(line))
		sb.WriteByte('\n')
	}

	rendered := sb.String()

	if m.taskBlockIdx >= 0 && m.taskBlockIdx < len(m.blocks) {
		m.blocks[m.taskBlockIdx].rendered = rendered
	} else {
		m.blocks = append(m.blocks, messageBlock{
			kind:      kindTaskGroup,
			rendered:  rendered,
			timestamp: time.Now(),
		})
		m.taskBlockIdx = len(m.blocks) - 1
	}
	m.rebuildViewport()

	// Clear when all tasks are done.
	hasActive := false
	for _, t := range m.activeTasks {
		if t.status == "running" || t.status == "waiting" {
			hasActive = true
			break
		}
	}
	if !hasActive {
		m.activeTasks = nil
		m.taskBlockIdx = -1
	}
}

// LastAssistantText returns the raw (unstyled) text content of the most recent
// assistant message block. Returns empty string if there are no assistant blocks.
func (m *ConversationModel) LastAssistantText() string {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		if m.blocks[i].kind == kindAssistant {
			return m.blocks[i].rawText
		}
	}
	return ""
}

// rebuildViewport concatenates all rendered blocks and refreshes the viewport content,
// then scrolls to the bottom.
func (m *ConversationModel) rebuildViewport() {
	var sb strings.Builder
	for _, b := range m.blocks {
		sb.WriteString(b.rendered)
	}
	m.viewport.SetContent(sb.String())
	m.viewport.GotoBottom()
}

// Init satisfies tea.Model (no-op; viewport is managed by the parent).
func (m ConversationModel) Init() tea.Cmd {
	return nil
}

// Update handles viewport scroll keys.
func (m ConversationModel) Update(msg tea.Msg) (ConversationModel, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// View renders the conversation viewport.
func (m ConversationModel) View() string {
	return m.viewport.View()
}

// ── formatting helpers ────────────────────────────────────────────────────────

// formatToolArgs parses the JSON args and returns a human-readable key: value
// listing. Long values are truncated to maxArgValueLines lines.
func formatToolArgs(name, argsJSON string) string {
	if argsJSON == "" {
		return ""
	}

	// Try to decode as a flat object.
	var m map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		// Not a JSON object — show truncated raw string.
		return truncateArgValue(argsJSON)
	}

	if len(m) == 0 {
		return ""
	}

	var lines []string
	for k, v := range m {
		var valStr string
		switch tv := v.(type) {
		case string:
			valStr = tv
		case []any:
			parts := make([]string, 0, len(tv))
			for _, item := range tv {
				parts = append(parts, fmt.Sprintf("%v", item))
			}
			valStr = "[" + strings.Join(parts, ", ") + "]"
		default:
			b, _ := json.Marshal(v)
			valStr = string(b)
		}
		valStr = truncateArgValue(valStr)
		lines = append(lines, fmt.Sprintf("%s: %s", k, valStr))
	}

	return strings.Join(lines, "\n")
}

// truncateArgValue truncates a multi-line value to maxArgValueLines lines,
// appending a "[N more lines]" suffix when truncated.
func truncateArgValue(val string) string {
	lines := strings.Split(val, "\n")
	if len(lines) <= maxArgValueLines {
		return val
	}
	hidden := len(lines) - maxArgValueLines
	kept := strings.Join(lines[:maxArgValueLines], "\n")
	return fmt.Sprintf("%s\n[%d more lines]", kept, hidden)
}
