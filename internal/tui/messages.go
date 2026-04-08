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
type PermissionRequestMsg struct {
	ToolCallID string
	ToolName   string
	Args       string
	RiskLevel  permission.RiskLevel
}

// PermissionResponseMsg carries the user's decision for a permission request.
type PermissionResponseMsg struct {
	ToolCallID string
	Approved   bool
}
