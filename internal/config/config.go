package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds all runtime configuration for replicant.
type Config struct {
	AnthropicKey string `json:"anthropic_api_key,omitempty"`
	OpenAIKey    string `json:"openai_api_key,omitempty"`
	DefaultModel string `json:"default_model,omitempty"`
	SessionDir   string `json:"session_dir,omitempty"`
	MemoryDir    string `json:"memory_dir,omitempty"`
	MissionDir   string `json:"mission_dir,omitempty"`
	Autonomy     string `json:"autonomy,omitempty"`
}

// Load reads configuration from multiple sources, in order of priority
// (later sources override earlier ones):
//
//  1. ~/.replicant/config.json  (non-secret settings)
//  2. ~/.replicant/.env         (API keys, overrides)
//  3. ./.env                    (project-local overrides)
//  4. Environment variables     (highest priority)
//
// Environment variable mapping:
//
//	ANTHROPIC_API_KEY  -> AnthropicKey
//	OPENAI_API_KEY     -> OpenAIKey
//	REPLICANT_MODEL    -> DefaultModel
//	REPLICANT_AUTONOMY -> Autonomy
//
// Directories default to ~/.replicant/{sessions,memory,missions}/ and are
// created if absent.
func Load() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("config: resolve home dir: %w", err)
	}

	replicantDir := filepath.Join(home, ".replicant")

	cfg := &Config{
		SessionDir: filepath.Join(replicantDir, "sessions"),
		MemoryDir:  filepath.Join(replicantDir, "memory"),
		MissionDir: filepath.Join(replicantDir, "missions"),
	}

	// 1. Load config.json (non-secret settings like model, autonomy, dirs).
	configPath := filepath.Join(replicantDir, "config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, cfg)
	}

	// 2. Load ~/.replicant/.env
	loadDotEnv(filepath.Join(replicantDir, ".env"))

	// 3. Load ./.env (project-local)
	loadDotEnv(".env")

	// 4. Environment variables override everything.
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		cfg.AnthropicKey = v
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.OpenAIKey = v
	}
	if v := os.Getenv("REPLICANT_MODEL"); v != "" {
		cfg.DefaultModel = v
	}
	if v := os.Getenv("REPLICANT_AUTONOMY"); v != "" {
		cfg.Autonomy = v
	}

	// Apply defaults for anything still unset.
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "claude-sonnet-4-20250514"
	}
	if cfg.Autonomy == "" {
		cfg.Autonomy = "normal"
	}
	if cfg.SessionDir == "" {
		cfg.SessionDir = filepath.Join(replicantDir, "sessions")
	}
	if cfg.MemoryDir == "" {
		cfg.MemoryDir = filepath.Join(replicantDir, "memory")
	}
	if cfg.MissionDir == "" {
		cfg.MissionDir = filepath.Join(replicantDir, "missions")
	}

	// Create data directories.
	for _, dir := range []string{cfg.SessionDir, cfg.MemoryDir, cfg.MissionDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("config: create dir %s: %w", dir, err)
		}
	}

	return cfg, nil
}

// loadDotEnv reads a .env file and sets environment variables for any keys
// that are not already set in the environment. This means real env vars
// always take precedence over .env files.
//
// Supports:
//   - KEY=VALUE
//   - KEY="VALUE"  (double-quoted, strips quotes)
//   - KEY='VALUE'  (single-quoted, strips quotes)
//   - export KEY=VALUE
//   - # comments
//   - blank lines
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // missing file is fine
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip blanks and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip optional "export " prefix.
		line = strings.TrimPrefix(line, "export ")

		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		// Strip matching quotes.
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}

		// Only set if not already in the environment.
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
}
