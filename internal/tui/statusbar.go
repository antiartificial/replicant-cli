package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// StatusBarModel tracks session statistics and renders the bottom bar.
type StatusBarModel struct {
	modelName    string
	autonomy     string
	tokenIn      int
	tokenOut     int
	startTime    time.Time
	width        int
	streaming    bool
	mouseEnabled bool
}

// NewStatusBarModel creates a status bar for the given model name.
func NewStatusBarModel(modelName string, width int) StatusBarModel {
	return StatusBarModel{
		modelName: modelName,
		autonomy:  "off",
		startTime: time.Now(),
		width:     width,
	}
}

// SetAutonomy updates the displayed autonomy level.
func (s *StatusBarModel) SetAutonomy(level string) {
	s.autonomy = level
}

// SetWidth updates the bar width.
func (s *StatusBarModel) SetWidth(width int) {
	s.width = width
}

// AddTokens adds to the running token counters.
func (s *StatusBarModel) AddTokens(in, out int) {
	s.tokenIn += in
	s.tokenOut += out
}

// SetTokens sets the token counters to absolute values (from cumulative usage).
func (s *StatusBarModel) SetTokens(in, out int) {
	s.tokenIn = in
	s.tokenOut = out
}

// SetStreaming marks whether the agent is currently streaming.
func (s *StatusBarModel) SetStreaming(v bool) {
	s.streaming = v
}

// SetMouse updates the mouse capture indicator.
func (s *StatusBarModel) SetMouse(enabled bool) {
	s.mouseEnabled = enabled
}

// View renders the single-line status bar.
func (s StatusBarModel) View() string {
	elapsed := time.Since(s.startTime).Round(time.Second)

	left := StyleStatusBar.Render(" REPLICANT")

	var streamIndicator string
	if s.streaming {
		streamIndicator = StyleStatusBarHighlight.Render(" ● ")
	} else {
		streamIndicator = StyleStatusBarDim.Render(" ○ ")
	}

	model := StyleStatusBarDim.Render(s.modelName)

	autonomy := StyleStatusBarDim.Render("auto:" + s.autonomy)

	tokens := StyleStatusBarDim.Render(
		fmt.Sprintf("in:%d out:%d", s.tokenIn, s.tokenOut),
	)

	var mouse string
	if s.mouseEnabled {
		mouse = StyleStatusBarDim.Render("mouse:on")
	} else {
		mouse = StyleStatusBarHighlight.Render("mouse:off(select)")
	}

	dur := StyleStatusBarDim.Render(elapsed.String())

	// Build center + right sections
	right := lipgloss.JoinHorizontal(lipgloss.Center,
		streamIndicator,
		StyleStatusBarDim.Render(" │ "),
		model,
		StyleStatusBarDim.Render(" │ "),
		autonomy,
		StyleStatusBarDim.Render(" │ "),
		mouse,
		StyleStatusBarDim.Render(" │ "),
		tokens,
		StyleStatusBarDim.Render(" │ "),
		dur,
		StyleStatusBarDim.Render(" "),
	)

	// Pad between left and right to fill the width
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	gap := s.width - leftWidth - rightWidth
	if gap < 0 {
		gap = 0
	}
	pad := StyleStatusBar.Render(strings.Repeat(" ", gap))

	return lipgloss.JoinHorizontal(lipgloss.Top, left, pad, right)
}
