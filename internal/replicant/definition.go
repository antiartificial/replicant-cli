package replicant

// ReplicantDef defines an agent persona loaded from a markdown file.
// The YAML frontmatter provides configuration; the markdown body is the system prompt.
type ReplicantDef struct {
	// From YAML frontmatter
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Model       string   `yaml:"model"`       // e.g. "anthropic/claude-sonnet-4-20250514"
	Tools       []string `yaml:"tools"`        // tool names to enable
	MaxTurns    int      `yaml:"max_turns"`    // ReAct loop iteration limit (default 50)
	Temperature float64  `yaml:"temperature"`  // sampling temperature
	MaxTokens   int      `yaml:"max_tokens"`   // max output tokens per turn

	// From markdown body (after frontmatter)
	SystemPrompt string `yaml:"-"`

	// Source file path
	SourcePath string `yaml:"-"`
}

// Default values for optional fields.
func (d *ReplicantDef) ApplyDefaults() {
	if d.MaxTurns == 0 {
		d.MaxTurns = 50
	}
	if d.Temperature == 0 {
		d.Temperature = 0.3
	}
	if d.MaxTokens == 0 {
		d.MaxTokens = 8192
	}
	if d.Name == "" {
		d.Name = "replicant"
	}
}
