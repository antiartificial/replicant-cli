package tui

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SpinnerModel wraps bubbles/spinner with Blade Runner styling.
type SpinnerModel struct {
	spinner spinner.Model
	active  bool
}

// NewSpinnerModel creates a styled thinking spinner.
func NewSpinnerModel() SpinnerModel {
	s := spinner.New()
	s.Spinner = spinner.Spinner{
		Frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		FPS:    80 * 1000 * 1000, // 80ms per frame
	}
	s.Style = lipgloss.NewStyle().Foreground(ColorNeonCyan)
	return SpinnerModel{spinner: s}
}

// Start activates the spinner and returns the tick command.
func (m *SpinnerModel) Start() tea.Cmd {
	m.active = true
	return m.spinner.Tick
}

// Stop deactivates the spinner.
func (m *SpinnerModel) Stop() {
	m.active = false
}

// Active reports whether the spinner is currently running.
func (m SpinnerModel) Active() bool {
	return m.active
}

// Init satisfies tea.Model.
func (m SpinnerModel) Init() tea.Cmd {
	return nil
}

// Update advances the spinner animation.
func (m SpinnerModel) Update(msg tea.Msg) (SpinnerModel, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

// View renders the spinner + label, or empty string when inactive.
func (m SpinnerModel) View() string {
	if !m.active {
		return ""
	}
	frame := m.spinner.View()
	label := StyleAssistantLabel.Render(" deckard is thinking")
	return frame + label
}
