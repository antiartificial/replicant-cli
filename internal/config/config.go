package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config holds all runtime configuration for replicant.
type Config struct {
	AnthropicKey string
	OpenAIKey    string
	DefaultModel string
	SessionDir   string // defaults to ~/.replicant/sessions/
	Autonomy     string // "off", "normal", "high", "full"
}

// Load reads configuration from environment variables:
//
//	ANTHROPIC_API_KEY  — Anthropic API key
//	OPENAI_API_KEY     — OpenAI API key (optional, for future multi-provider)
//	REPLICANT_MODEL    — default model string (e.g. "claude-sonnet-4-20250514")
//	REPLICANT_AUTONOMY — autonomy level: "off", "normal", "high", "full"
//
// SessionDir defaults to ~/.replicant/sessions/ and is created if absent.
func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("config: resolve home dir: %w", err)
	}

	cfg := &Config{
		AnthropicKey: os.Getenv("ANTHROPIC_API_KEY"),
		OpenAIKey:    os.Getenv("OPENAI_API_KEY"),
		DefaultModel: os.Getenv("REPLICANT_MODEL"),
		SessionDir:   filepath.Join(home, ".replicant", "sessions"),
		Autonomy:     os.Getenv("REPLICANT_AUTONOMY"),
	}

	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "claude-sonnet-4-20250514"
	}

	if cfg.Autonomy == "" {
		cfg.Autonomy = "normal"
	}

	if err := os.MkdirAll(cfg.SessionDir, 0o700); err != nil {
		return nil, fmt.Errorf("config: create session dir %s: %w", cfg.SessionDir, err)
	}

	return cfg, nil
}
