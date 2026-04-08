package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/antiartificial/replicant/internal/permission"
)

// ToolAdapter bridges an MCP tool to the tools.Tool and tools.ContextTool interfaces.
// The adapter's name follows the convention "mcp:<serverName>:<toolName>" so it
// can be selected by wildcard patterns such as "mcp:github:*".
type ToolAdapter struct {
	client     *Client
	serverName string
	toolName   string
	toolDesc   string
	schema     map[string]any
}

// newToolAdapter creates a ToolAdapter for a single MCP tool.
func newToolAdapter(client *Client, serverName, toolName, toolDesc string, schema map[string]any) *ToolAdapter {
	return &ToolAdapter{
		client:     client,
		serverName: serverName,
		toolName:   toolName,
		toolDesc:   toolDesc,
		schema:     schema,
	}
}

// Name returns the tool's identifier in the form "mcp:<server>:<tool>".
func (a *ToolAdapter) Name() string {
	return fmt.Sprintf("mcp:%s:%s", a.serverName, a.toolName)
}

// Description returns the tool's human-readable description from the MCP server.
func (a *ToolAdapter) Description() string {
	return a.toolDesc
}

// Parameters returns the JSON Schema for the tool's input arguments.
func (a *ToolAdapter) Parameters() map[string]any {
	return a.schema
}

// Risk returns RiskLow because MCP tools are treated as potentially side-effecting.
func (a *ToolAdapter) Risk() permission.RiskLevel {
	return permission.RiskLow
}

// Run executes the tool synchronously without a context.
func (a *ToolAdapter) Run(args string) (string, error) {
	return a.RunWithContext(context.Background(), args)
}

// RunWithContext executes the tool with the given context and JSON-encoded arguments.
func (a *ToolAdapter) RunWithContext(ctx context.Context, args string) (string, error) {
	var argMap map[string]any
	if args != "" && args != "null" && args != "{}" {
		if err := json.Unmarshal([]byte(args), &argMap); err != nil {
			return "", fmt.Errorf("mcp tool %s: parse args: %w", a.Name(), err)
		}
	}
	if argMap == nil {
		argMap = make(map[string]any)
	}
	return a.client.CallTool(ctx, a.toolName, argMap)
}
