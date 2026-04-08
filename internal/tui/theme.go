package tui

import "github.com/charmbracelet/lipgloss"

// Blade Runner neon palette
var (
	ColorNeonCyan   = lipgloss.Color("#00FFFF")
	ColorNeonOrange = lipgloss.Color("#FF6600")
	ColorNeonPink   = lipgloss.Color("#FF1493")
	ColorDimWhite   = lipgloss.Color("#B0B0B0")
	ColorDarkBG     = lipgloss.Color("#0A0A0F")
	ColorGold       = lipgloss.Color("#FFD700")
	ColorDimGray    = lipgloss.Color("#555555")
	ColorRed        = lipgloss.Color("#FF3333")
	ColorGreen      = lipgloss.Color("#33FF33")
)

var (
	// UserMessage — "▸ you" label and user text
	StyleUserLabel = lipgloss.NewStyle().
			Foreground(ColorNeonCyan).
			Bold(true)

	StyleUserMessage = lipgloss.NewStyle().
				Foreground(ColorDimWhite).
				PaddingLeft(2)

	// AssistantMessage — "▸ deckard" label and assistant text
	StyleAssistantLabel = lipgloss.NewStyle().
				Foreground(ColorDimWhite).
				Bold(true)

	StyleAssistantMessage = lipgloss.NewStyle().
				Foreground(ColorDimWhite).
				PaddingLeft(2)

	// ToolCall — "◆ tool_name" and args block
	StyleToolCallLabel = lipgloss.NewStyle().
				Foreground(ColorNeonOrange).
				Bold(true)

	StyleToolCallArgs = lipgloss.NewStyle().
				Foreground(ColorDimGray).
				PaddingLeft(4)

	// ToolResult — output from a tool execution
	StyleToolResult = lipgloss.NewStyle().
			Foreground(ColorGreen).
			PaddingLeft(4)

	StyleToolResultError = lipgloss.NewStyle().
				Foreground(ColorRed).
				PaddingLeft(4)

	// StatusBar — bottom bar with model/token info
	StyleStatusBar = lipgloss.NewStyle().
			Background(ColorDarkBG).
			Foreground(ColorNeonPink).
			PaddingLeft(1).
			PaddingRight(1)

	StyleStatusBarDim = lipgloss.NewStyle().
				Background(ColorDarkBG).
				Foreground(ColorDimGray)

	StyleStatusBarHighlight = lipgloss.NewStyle().
				Background(ColorDarkBG).
				Foreground(ColorGold)

	// InputBorder — neon cyan border around the textarea
	StyleInputBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorNeonCyan).
				PaddingLeft(1).
				PaddingRight(1)

	StyleInputBorderDisabled = lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(ColorDimGray).
					PaddingLeft(1).
					PaddingRight(1)

	// ErrorText
	StyleErrorText = lipgloss.NewStyle().
			Foreground(ColorRed).
			Bold(true)

	// Logo / banner
	StyleLogo = lipgloss.NewStyle().
			Foreground(ColorNeonCyan).
			Bold(true)

	// Separator line
	StyleSeparator = lipgloss.NewStyle().
			Foreground(ColorDimGray)

	// Timestamp
	StyleTimestamp = lipgloss.NewStyle().
			Foreground(ColorDimGray).
			Italic(true)
)
