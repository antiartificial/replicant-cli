package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// StatusBarModel tracks session statistics and renders the two-line footer.
type StatusBarModel struct {
	modelName    string
	contextLimit int // model's context window in tokens
	autonomy     string
	tokenIn      int
	tokenOut     int
	startTime    time.Time
	width        int
	streaming    bool
	mouseEnabled bool
	replicant    string

	// Tool call counters by name.
	toolCounts map[string]int
}

// NewStatusBarModel creates a status bar for the given model and context limit.
func NewStatusBarModel(modelName string, contextLimit int, replicantName string, width int) StatusBarModel {
	return StatusBarModel{
		modelName:    modelName,
		contextLimit: contextLimit,
		autonomy:     "off",
		startTime:    time.Now(),
		width:        width,
		mouseEnabled: true,
		replicant:    replicantName,
		toolCounts:   make(map[string]int),
	}
}

// SetAutonomy updates the displayed autonomy level.
func (s *StatusBarModel) SetAutonomy(level string) { s.autonomy = level }

// SetWidth updates the bar width.
func (s *StatusBarModel) SetWidth(width int) { s.width = width }

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
func (s *StatusBarModel) SetStreaming(v bool) { s.streaming = v }

// SetMouse updates the mouse capture indicator.
func (s *StatusBarModel) SetMouse(enabled bool) { s.mouseEnabled = enabled }

// RecordToolCall increments the counter for a tool.
func (s *StatusBarModel) RecordToolCall(name string) {
	if s.toolCounts == nil {
		s.toolCounts = make(map[string]int)
	}
	s.toolCounts[name]++
}

// View renders the two-line status footer.
//
// Line 1: [Model (context)] progress_bar pct% | replicant | auto:level | elapsed
// Line 2: tool counts | autonomy cycle hint
func (s StatusBarModel) View() string {
	sep := StyleStatusBarDim.Render(" | ")

	// -- line 1 --
	var l1 strings.Builder

	// Stream indicator
	if s.streaming {
		l1.WriteString(StyleStatusBarHighlight.Render(" * "))
	} else {
		l1.WriteString(StyleStatusBarDim.Render("   "))
	}

	// Model + context label
	contextK := s.contextLimit / 1000
	modelLabel := fmt.Sprintf("[%s (%dk context)]", s.modelName, contextK)
	l1.WriteString(StyleStatusBar.Render(modelLabel))

	// Context usage progress bar
	totalTokens := s.tokenIn + s.tokenOut
	pct := 0
	if s.contextLimit > 0 {
		pct = totalTokens * 100 / s.contextLimit
		if pct > 100 {
			pct = 100
		}
	}
	bar := renderProgressBar(10, pct)
	l1.WriteString(" ")
	l1.WriteString(bar)
	l1.WriteString(StyleStatusBarDim.Render(fmt.Sprintf(" %d%%", pct)))

	l1.WriteString(sep)
	l1.WriteString(StyleStatusBar.Render(s.replicant))

	l1.WriteString(sep)

	// Elapsed
	elapsed := time.Since(s.startTime).Round(time.Second)
	l1.WriteString(StyleStatusBarDim.Render(elapsed.String()))

	// Pad line 1 to full width
	line1Content := l1.String()
	line1Width := lipgloss.Width(line1Content)
	if line1Width < s.width {
		line1Content += StyleStatusBar.Render(strings.Repeat(" ", s.width-line1Width))
	}

	// -- separator --
	sepLine := StyleSeparator.Render(strings.Repeat("-", s.width))

	// -- line 2 --
	var l2 strings.Builder

	// Tool call counts
	if len(s.toolCounts) > 0 {
		first := true
		// Show in a predictable order: common tools first.
		for _, name := range []string{"read_file", "write_file", "edit_file", "list_dir", "execute", "glob", "grep", "remember", "recall", "delegate", "spawn", "mission"} {
			if count, ok := s.toolCounts[name]; ok {
				if !first {
					l2.WriteString(StyleStatusBarDim.Render(" | "))
				}
				l2.WriteString(StyleToolResult.Render("v"))
				l2.WriteString(StyleStatusBarDim.Render(fmt.Sprintf(" %s x%d", shortToolName(name), count)))
				first = false
			}
		}
		// Any remaining tools not in the list above.
		for name, count := range s.toolCounts {
			if !isKnownTool(name) {
				if !first {
					l2.WriteString(StyleStatusBarDim.Render(" | "))
				}
				l2.WriteString(StyleToolResult.Render("v"))
				l2.WriteString(StyleStatusBarDim.Render(fmt.Sprintf(" %s x%d", shortToolName(name), count)))
				first = false
			}
		}
	} else {
		l2.WriteString(StyleStatusBarDim.Render("  no tool calls yet"))
	}

	// Right-align the autonomy cycle hint on line 2.
	var autoHint string
	switch s.autonomy {
	case "full":
		autoHint = ">> auto-approve all (tab to cycle)"
	case "high":
		autoHint = "> auto-approve most (tab to cycle)"
	case "normal":
		autoHint = "~ confirm edits+shell (tab to cycle)"
	default:
		autoHint = "# confirm all (tab to cycle)"
	}
	autoHintStyled := StyleStatusBarHighlight.Render(autoHint)

	line2Left := l2.String()
	line2LeftWidth := lipgloss.Width(line2Left)
	autoHintWidth := lipgloss.Width(autoHintStyled)
	gap := s.width - line2LeftWidth - autoHintWidth
	if gap < 2 {
		gap = 2
	}
	line2 := line2Left + strings.Repeat(" ", gap) + autoHintStyled

	return line1Content + "\n" + sepLine + "\n" + line2
}

// renderProgressBar renders a text progress bar like: |####------|
func renderProgressBar(width, pct int) string {
	filled := width * pct / 100
	if filled > width {
		filled = width
	}
	empty := width - filled

	var barStyle lipgloss.Style
	if pct > 75 {
		barStyle = lipgloss.NewStyle().Foreground(ColorRed)
	} else if pct > 50 {
		barStyle = lipgloss.NewStyle().Foreground(ColorGold)
	} else {
		barStyle = lipgloss.NewStyle().Foreground(ColorGreen)
	}

	bar := barStyle.Render(strings.Repeat("#", filled)) +
		StyleStatusBarDim.Render(strings.Repeat("-", empty))
	return bar
}

func shortToolName(name string) string {
	// Shorten common names for compact display.
	switch name {
	case "read_file":
		return "Read"
	case "write_file":
		return "Write"
	case "edit_file":
		return "Edit"
	case "list_dir":
		return "List"
	case "execute":
		return "Exec"
	case "glob":
		return "Glob"
	case "grep":
		return "Grep"
	case "remember":
		return "Mem+"
	case "recall":
		return "Mem?"
	case "delegate":
		return "Deleg"
	case "spawn":
		return "Spawn"
	case "mission":
		return "Mission"
	default:
		if len(name) > 8 {
			return name[:8]
		}
		return name
	}
}

var knownTools = map[string]bool{
	"read_file": true, "write_file": true, "edit_file": true,
	"list_dir": true, "execute": true, "glob": true, "grep": true,
	"remember": true, "recall": true, "delegate": true, "spawn": true,
	"mission": true,
}

func isKnownTool(name string) bool {
	return knownTools[name]
}
