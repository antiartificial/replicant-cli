package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// messageKind classifies the role of a rendered conversation block.
type messageKind int

const (
	kindUser messageKind = iota
	kindAssistant
	kindToolCall
	kindToolResult
	kindBanner
)

// messageBlock is a single rendered unit in the conversation history.
type messageBlock struct {
	kind      messageKind
	rendered  string // final rendered string (may be multi-line)
	timestamp time.Time
	id        string // tool call ID for linking calls to results
}

// ConversationModel holds the scrollable conversation viewport and message history.
type ConversationModel struct {
	viewport viewport.Model
	blocks   []messageBlock

	// current streaming assistant block (index into blocks, -1 if none)
	streamingIdx int
	streamBuf    strings.Builder

	width  int
	height int
}

// NewConversationModel creates an empty conversation view.
func NewConversationModel(width, height int) ConversationModel {
	vp := viewport.New(width, height)
	vp.SetContent("")
	return ConversationModel{
		viewport:     vp,
		streamingIdx: -1,
		width:        width,
		height:       height,
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
	label := StyleAssistantLabel.Render("▸ deckard")
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
func (m *ConversationModel) AppendChunk(text string) {
	if m.streamingIdx < 0 || m.streamingIdx >= len(m.blocks) {
		return
	}
	m.streamBuf.WriteString(text)
	b := &m.blocks[m.streamingIdx]

	label := StyleAssistantLabel.Render("▸ deckard")
	ts := StyleTimestamp.Render(b.timestamp.Format("15:04:05"))
	header := lipgloss.JoinHorizontal(lipgloss.Top, label, "  ", ts)
	body := StyleAssistantMessage.Width(m.width - 2).Render(m.streamBuf.String())
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

// AddToolCall appends a tool call block.
func (m *ConversationModel) AddToolCall(id, name, args string) {
	label := StyleToolCallLabel.Render(fmt.Sprintf("◆ %s", name))
	argsRendered := StyleToolCallArgs.Width(m.width - 6).Render(args)
	rendered := label + "\n" + argsRendered + "\n"

	m.blocks = append(m.blocks, messageBlock{
		kind:      kindToolCall,
		rendered:  rendered,
		timestamp: time.Now(),
		id:        id,
	})
	m.rebuildViewport()
}

// AddToolResult appends the result of a tool call.
func (m *ConversationModel) AddToolResult(id, result string, isError bool) {
	var body string
	if isError {
		body = StyleToolResultError.Width(m.width - 6).Render("  error: " + result)
	} else {
		body = StyleToolResult.Width(m.width - 6).Render(result)
	}
	rendered := body + "\n"

	m.blocks = append(m.blocks, messageBlock{
		kind:      kindToolResult,
		rendered:  rendered,
		timestamp: time.Now(),
		id:        id,
	})
	m.rebuildViewport()
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
