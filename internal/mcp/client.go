package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// ToolInfo describes a single tool advertised by an MCP server.
type ToolInfo struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// Client wraps an mcp-go client for one MCP server.
type Client struct {
	name   string
	config ServerConfig
	inner  *mcpclient.Client
}

// NewClient creates a Client for the given server configuration.
// Call Connect to establish the connection.
func NewClient(name string, cfg ServerConfig) *Client {
	return &Client{name: name, config: cfg}
}

// Connect establishes the transport connection and initialises the MCP session.
func (c *Client) Connect(ctx context.Context) error {
	transport := c.config.Transport
	if transport == "" {
		// Infer transport from what's configured.
		if c.config.Command != "" {
			transport = "stdio"
		} else if c.config.URL != "" {
			transport = "sse"
		} else {
			return fmt.Errorf("mcp server %q: no command or URL configured", c.name)
		}
	}

	var inner *mcpclient.Client
	var err error

	switch transport {
	case "stdio":
		if c.config.Command == "" {
			return fmt.Errorf("mcp server %q: stdio transport requires a command", c.name)
		}
		// Build env slice in KEY=VALUE form.
		env := make([]string, 0, len(c.config.Env))
		for k, v := range c.config.Env {
			env = append(env, k+"="+v)
		}
		inner, err = mcpclient.NewStdioMCPClient(c.config.Command, env, c.config.Args...)
		if err != nil {
			return fmt.Errorf("mcp server %q: start stdio client: %w", c.name, err)
		}
	case "sse":
		if c.config.URL == "" {
			return fmt.Errorf("mcp server %q: sse transport requires a URL", c.name)
		}
		inner, err = mcpclient.NewSSEMCPClient(c.config.URL)
		if err != nil {
			return fmt.Errorf("mcp server %q: create sse client: %w", c.name, err)
		}
		if err := inner.Start(ctx); err != nil {
			return fmt.Errorf("mcp server %q: start sse client: %w", c.name, err)
		}
	default:
		return fmt.Errorf("mcp server %q: unknown transport %q (want stdio or sse)", c.name, transport)
	}

	// Initialise the MCP session.
	_, err = inner.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "replicant",
				Version: "1.0",
			},
		},
	})
	if err != nil {
		_ = inner.Close()
		return fmt.Errorf("mcp server %q: initialize: %w", c.name, err)
	}

	c.inner = inner
	return nil
}

// ListTools returns the list of tools the server exposes.
func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	if c.inner == nil {
		return nil, fmt.Errorf("mcp server %q: not connected", c.name)
	}
	result, err := c.inner.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("mcp server %q: list tools: %w", c.name, err)
	}

	out := make([]ToolInfo, 0, len(result.Tools))
	for _, t := range result.Tools {
		schema := toolInputSchemaToMap(t)
		out = append(out, ToolInfo{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return out, nil
}

// CallTool invokes a tool by name with the given arguments.
// It returns the concatenated text from all TextContent blocks in the result.
func (c *Client) CallTool(ctx context.Context, toolName string, args map[string]any) (string, error) {
	if c.inner == nil {
		return "", fmt.Errorf("mcp server %q: not connected", c.name)
	}
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: args,
		},
	}
	result, err := c.inner.CallTool(ctx, req)
	if err != nil {
		return "", fmt.Errorf("mcp server %q: call tool %q: %w", c.name, toolName, err)
	}
	if result.IsError {
		// Collect error text from content.
		return "", fmt.Errorf("mcp server %q: tool %q returned error: %s",
			c.name, toolName, contentToString(result.Content))
	}
	return contentToString(result.Content), nil
}

// Close shuts down the underlying transport.
func (c *Client) Close() error {
	if c.inner == nil {
		return nil
	}
	return c.inner.Close()
}

// toolInputSchemaToMap converts a mcp.Tool's InputSchema into map[string]any
// suitable for use as a JSON Schema in tool definitions.
func toolInputSchemaToMap(t mcp.Tool) map[string]any {
	m := map[string]any{
		"type": "object",
	}
	if len(t.InputSchema.Properties) > 0 {
		m["properties"] = t.InputSchema.Properties
	}
	if len(t.InputSchema.Required) > 0 {
		m["required"] = t.InputSchema.Required
	}
	return m
}

// contentToString extracts text from a slice of mcp.Content items.
func contentToString(content []mcp.Content) string {
	var sb strings.Builder
	for _, c := range content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}
