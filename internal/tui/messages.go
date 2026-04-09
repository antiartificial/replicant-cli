package tui

import "github.com/antiartificial/replicant/internal/permission"

// StreamChunkMsg carries a partial text chunk from the LLM stream.
type StreamChunkMsg struct {
	Text string
}

// ToolCallMsg signals that the agent wants to invoke a tool.
type ToolCallMsg struct {
	ID   string
	Name string
	Args string // raw JSON arguments
}

// ToolResultMsg carries the result of a tool execution.
type ToolResultMsg struct {
	ID      string
	Result  string
	IsError bool
}

// StreamDoneMsg signals that the agent has finished its response turn.
type StreamDoneMsg struct{}

// StreamErrorMsg carries an error that occurred during streaming.
type StreamErrorMsg struct {
	Err error
}

// PermissionRequestMsg asks the user to approve or deny a tool call.
// The caller blocks on Response; send true for approved, false for denied.
type PermissionRequestMsg struct {
	ToolCallID string
	ToolName   string
	Args       string
	RiskLevel  permission.RiskLevel
	Response   chan<- bool // send true=approved, false=denied
}

// PermissionResponseMsg carries the user's decision for a permission request.
type PermissionResponseMsg struct {
	ToolCallID string
	Approved   bool
}

// ToolProgressMsg carries partial output from a streaming tool.
type ToolProgressMsg struct {
	ID     string
	Output string
}

// CommandMsg is a local slash command that the TUI handles without sending to the agent.
type CommandMsg struct {
	Command string
	Args    string
}

// AutonomyChangedMsg notifies the TUI that the autonomy level was changed.
type AutonomyChangedMsg struct {
	Level string
}

// TokenUsageMsg updates the status bar token counters.
type TokenUsageMsg struct {
	InputTokens  int
	OutputTokens int
}

// TaskStatusMsg updates the active task display in the conversation.
type TaskStatusMsg struct {
	ID     string // unique task identifier
	Name   string // short display name (e.g. "implement auth middleware")
	Status string // "running", "completed", "failed", "waiting"
	Detail string // current activity (e.g. "reading src/auth.go", "running tests")
}
