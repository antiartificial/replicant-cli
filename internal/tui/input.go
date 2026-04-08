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

// InputModel wraps a bubbles/textarea with Blade Runner styling.
type InputModel struct {
	textarea textarea.Model
	disabled bool
	width    int
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

	// Style the textarea itself (no extra border; we wrap it below)
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
		textarea: ta,
		width:    width,
	}
}

// SetWidth updates the input width.
func (m *InputModel) SetWidth(width int) {
	m.width = width
	m.textarea.SetWidth(width - 4)
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
}

// Height returns the total rendered height of the input component (textarea + borders).
func (m InputModel) Height() int {
	// textarea height + 2 for top/bottom border
	return m.textarea.Height() + 2
}

// Init satisfies tea.Model.
func (m InputModel) Init() tea.Cmd {
	return textarea.Blink
}

// Update handles key events. Returns a SubmitMsg when Enter is pressed without Shift.
func (m InputModel) Update(msg tea.Msg) (InputModel, tea.Cmd) {
	if m.disabled {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			// Shift+Enter inserts a newline; plain Enter submits.
			if msg.Alt {
				// alt-enter: insert newline
				var cmd tea.Cmd
				m.textarea, cmd = m.textarea.Update(msg)
				return m, cmd
			}
			text := m.textarea.Value()
			if text == "" {
				return m, nil
			}
			return m, func() tea.Msg { return SubmitMsg{Text: text} }
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
