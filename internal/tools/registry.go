package tools

// Registry holds all available tools by name.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates a Registry pre-populated with all built-in tools.
func NewRegistry() *Registry {
	r := &Registry{tools: make(map[string]Tool)}
	for _, t := range []Tool{
		&ReadFileTool{},
		&WriteFileTool{},
		&EditFileTool{},
		&ListDirTool{},
		&ExecuteTool{},
		&GlobTool{},
		&GrepTool{},
	} {
		r.tools[t.Name()] = t
	}
	return r
}

// Register adds or replaces a tool in the registry.
// This allows external code (e.g. main.go) to inject tools that require
// dependencies not available at construction time.
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Get returns a tool by name and whether it was found.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Resolve returns the Tool for each name in names, skipping unknown names.
// It delegates to ResolveWithWildcards so callers get wildcard expansion for
// free (e.g. "mcp:github:*").
func (r *Registry) Resolve(names []string) []Tool {
	return r.ResolveWithWildcards(names)
}

// ResolveWithWildcards resolves a list of tool names with optional wildcard
// support. Names that end with "*" are treated as prefix patterns: every
// registered tool whose name starts with the prefix (everything before the
// trailing "*") is included. Exact-match names behave identically to the
// original Resolve method.
func (r *Registry) ResolveWithWildcards(names []string) []Tool {
	out := make([]Tool, 0, len(names))
	for _, name := range names {
		if len(name) > 0 && name[len(name)-1] == '*' {
			prefix := name[:len(name)-1]
			for toolName, t := range r.tools {
				if len(toolName) >= len(prefix) && toolName[:len(prefix)] == prefix {
					out = append(out, t)
				}
			}
		} else {
			if t, ok := r.tools[name]; ok {
				out = append(out, t)
			}
		}
	}
	return out
}

// All returns every registered tool in an unspecified order.
func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}
