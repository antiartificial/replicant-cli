package mcp

import (
	"context"
	"fmt"
)

// Manager manages connections to multiple MCP servers and vends ToolAdapters
// for all of the tools they expose.
type Manager struct {
	clients map[string]*Client
}

// NewManager creates an empty Manager. Use LoadConfig and/or AddServer to
// populate it, then call ConnectAll before calling AllTools.
func NewManager() *Manager {
	return &Manager{clients: make(map[string]*Client)}
}

// LoadConfig reads a Config from path and registers each server it describes.
// Servers whose names are already registered are skipped (first writer wins).
func (m *Manager) LoadConfig(path string) error {
	cfg, err := LoadConfig(path)
	if err != nil {
		return err
	}
	for name, srv := range cfg.Servers {
		if err := m.AddServer(name, srv); err != nil {
			return err
		}
	}
	return nil
}

// AddServer registers a server by name. It returns an error if a server with
// that name is already registered.
func (m *Manager) AddServer(name string, cfg ServerConfig) error {
	if _, exists := m.clients[name]; exists {
		return fmt.Errorf("mcp: server %q already registered", name)
	}
	m.clients[name] = NewClient(name, cfg)
	return nil
}

// ConnectAll establishes connections to all registered servers. Failures are
// collected and returned as a combined error; successfully connected servers
// remain usable even when others fail.
func (m *Manager) ConnectAll(ctx context.Context) error {
	var errs []error
	for name, c := range m.clients {
		if err := c.Connect(ctx); err != nil {
			errs = append(errs, fmt.Errorf("connect %q: %w", name, err))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	// Combine all errors into one.
	combined := errs[0]
	for _, e := range errs[1:] {
		combined = fmt.Errorf("%w; %w", combined, e)
	}
	return combined
}

// AllTools queries every connected server for its tool list and returns a
// ToolAdapter for each tool. Servers that are not yet connected or that fail
// to list tools are skipped silently (the caller should log warnings using
// ConnectAll's error output).
func (m *Manager) AllTools() []*ToolAdapter {
	ctx := context.Background()
	var adapters []*ToolAdapter
	for name, c := range m.clients {
		if c.inner == nil {
			// Not connected — skip.
			continue
		}
		tools, err := c.ListTools(ctx)
		if err != nil {
			// Non-fatal: log via the caller.
			_ = err
			continue
		}
		for _, t := range tools {
			adapters = append(adapters, newToolAdapter(c, name, t.Name, t.Description, t.InputSchema))
		}
	}
	return adapters
}

// Close shuts down all client connections.
func (m *Manager) Close() error {
	var errs []error
	for _, c := range m.clients {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	combined := errs[0]
	for _, e := range errs[1:] {
		combined = fmt.Errorf("%w; %w", combined, e)
	}
	return combined
}
