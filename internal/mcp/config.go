package mcp

import (
	"encoding/json"
	"fmt"
	"os"
)

// ServerConfig holds connection parameters for a single MCP server.
type ServerConfig struct {
	Command   string            `json:"command,omitempty" yaml:"command,omitempty"`
	Args      []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	URL       string            `json:"url,omitempty" yaml:"url,omitempty"`
	Transport string            `json:"transport" yaml:"transport"` // "stdio" or "sse"
}

// Config is the top-level MCP configuration loaded from ~/.replicant/mcp.json.
type Config struct {
	Servers map[string]ServerConfig `json:"servers"`
}

// LoadConfig reads a JSON file at path and returns the parsed Config.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mcp config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse mcp config %s: %w", path, err)
	}
	if cfg.Servers == nil {
		cfg.Servers = make(map[string]ServerConfig)
	}
	return &cfg, nil
}
