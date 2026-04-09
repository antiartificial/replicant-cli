package agent

// ModelInfo describes a model's capabilities.
type ModelInfo struct {
	ContextWindow int // total tokens
	MaxOutput     int // max output tokens
}

// modelRegistry maps known model names (or prefixes) to their capabilities.
var modelRegistry = map[string]ModelInfo{
	// Anthropic
	"claude-opus-4-20250514":          {ContextWindow: 200000, MaxOutput: 32768},
	"claude-sonnet-4-20250514":        {ContextWindow: 200000, MaxOutput: 16384},
	"claude-haiku-4-5-20251001":       {ContextWindow: 200000, MaxOutput: 8192},

	// OpenAI
	"gpt-4o":                          {ContextWindow: 128000, MaxOutput: 16384},
	"gpt-4o-mini":                     {ContextWindow: 128000, MaxOutput: 16384},
	"gpt-4-turbo":                     {ContextWindow: 128000, MaxOutput: 4096},
	"o3":                              {ContextWindow: 200000, MaxOutput: 100000},
	"o4-mini":                         {ContextWindow: 200000, MaxOutput: 100000},

	// xAI
	"grok-3":                          {ContextWindow: 131072, MaxOutput: 16384},
	"grok-3-mini":                     {ContextWindow: 131072, MaxOutput: 16384},
	"grok-3-fast":                     {ContextWindow: 131072, MaxOutput: 16384},
	"grok-2":                          {ContextWindow: 131072, MaxOutput: 8192},
}

// LookupModel returns the ModelInfo for a model name. If not found, returns
// conservative defaults (128K context, 8K output).
func LookupModel(model string) ModelInfo {
	if info, ok := modelRegistry[model]; ok {
		return info
	}
	// Sensible defaults for unknown models.
	return ModelInfo{ContextWindow: 128000, MaxOutput: 8192}
}

// CompactThreshold returns the token count at which auto-compaction should
// trigger, set to 75% of the model's context window.
func CompactThreshold(model string) int {
	info := LookupModel(model)
	return info.ContextWindow * 3 / 4
}
