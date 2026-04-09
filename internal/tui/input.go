package tui

import (
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SubmitMsg is sent to the parent model when the user presses Enter.
type SubmitMsg struct {
	Text string
}

const maxHistory = 100

// InputModel wraps a bubbles/textarea with input history and Blade Runner styling.
type InputModel struct {
	textarea textarea.Model
	disabled bool
	width    int

	// Input history (most recent last). Up/Down cycle through previous inputs
	// when the cursor is on a single-line input or at the first/last line.
	history    []string
	historyIdx int // points past the end when not browsing (-1 = not browsing)
	draft      string // saves current input when browsing history
}

// NewInputModel creates a styled input field.
func NewInputModel(width int) InputModel {
	ta := textarea.New()
	ta.Placeholder = "speak..."
	ta.ShowLineNumbers = false
	ta.MaxHeight = 5
	ta.SetWidth(width - 4) // account for border padding
	ta.SetHeight(3)
	ta.CharLimit = 0

	ta.FocusedStyle.Base = lipgloss.NewStyle().
		Foreground(ColorDimWhite)
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().
		Foreground(ColorDimGray)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle().
		Background(lipgloss.Color("#111122"))
	ta.BlurredStyle.Base = lipgloss.NewStyle().
		Foreground(ColorDimGray)
	ta.BlurredStyle.Placeholder = lipgloss.NewStyle().
		Foreground(ColorDimGray)

	ta.Focus()

	return InputModel{
		textarea:   ta,
		width:      width,
		historyIdx: -1,
	}
}

// SetWidth updates the input width.
func (m *InputModel) SetWidth(width int) {
	m.width = width
	m.textarea.SetWidth(width - 4)
}

// SetPlaceholder changes the placeholder text.
func (m *InputModel) SetPlaceholder(text string) {
	m.textarea.Placeholder = text
}

// Disable greys out the input and shows "thinking..." placeholder.
func (m *InputModel) Disable() {
	m.disabled = true
	m.textarea.Placeholder = "thinking..."
	m.textarea.Blur()
}

// Enable restores the input to active state.
func (m *InputModel) Enable() {
	m.disabled = false
	m.textarea.Placeholder = "speak..."
	m.textarea.Focus()
	m.textarea.Reset()
	m.historyIdx = -1
	m.draft = ""
}

// Height returns the total rendered height (textarea + borders).
func (m InputModel) Height() int {
	return m.textarea.Height() + 2
}

// Init satisfies tea.Model.
func (m InputModel) Init() tea.Cmd {
	return textarea.Blink
}

// isSingleLine returns true when the textarea content has no newlines
// (i.e. the user hasn't started a multi-line input with Alt+Enter).
func (m InputModel) isSingleLine() bool {
	v := m.textarea.Value()
	for i := 0; i < len(v); i++ {
		if v[i] == '\n' {
			return false
		}
	}
	return true
}

// Update handles key events. Returns a SubmitMsg when Enter is pressed.
// Up/Down cycle through input history when the input is a single line.
func (m InputModel) Update(msg tea.Msg) (InputModel, tea.Cmd) {
	if m.disabled {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			if msg.Alt {
				var cmd tea.Cmd
				m.textarea, cmd = m.textarea.Update(msg)
				return m, cmd
			}
			text := m.textarea.Value()
			if text == "" {
				return m, nil
			}
			// Save to history.
			m.history = append(m.history, text)
			if len(m.history) > maxHistory {
				m.history = m.history[len(m.history)-maxHistory:]
			}
			m.historyIdx = -1
			m.draft = ""
			return m, func() tea.Msg { return SubmitMsg{Text: text} }

		case tea.KeyUp:
			if m.isSingleLine() && len(m.history) > 0 {
				if m.historyIdx == -1 {
					// Start browsing: save current draft, go to last entry.
					m.draft = m.textarea.Value()
					m.historyIdx = len(m.history) - 1
				} else if m.historyIdx > 0 {
					m.historyIdx--
				}
				m.textarea.Reset()
				m.textarea.SetValue(m.history[m.historyIdx])
				return m, nil
			}

		case tea.KeyDown:
			if m.isSingleLine() && m.historyIdx >= 0 {
				if m.historyIdx < len(m.history)-1 {
					m.historyIdx++
					m.textarea.Reset()
					m.textarea.SetValue(m.history[m.historyIdx])
				} else {
					// Past the end: restore draft.
					m.historyIdx = -1
					m.textarea.Reset()
					m.textarea.SetValue(m.draft)
					m.draft = ""
				}
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

// View renders the input with a neon border.
func (m InputModel) View() string {
	inner := m.textarea.View()
	if m.disabled {
		return StyleInputBorderDisabled.Width(m.width - 2).Render(inner)
	}
	return StyleInputBorder.Width(m.width - 2).Render(inner)
}
