package replicant

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Registry holds a set of replicant definitions indexed by name.
// When the same name appears in multiple directories the definition from the
// directory listed earliest in the search order wins (local overrides home).
type Registry struct {
	defs map[string]*ReplicantDef
}

// DefaultDirs returns the standard search directories in priority order:
// ./replicants/ (local, highest priority) then ~/.replicant/replicants/.
func DefaultDirs() []string {
	dirs := []string{"./replicants/"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".replicant", "replicants"))
	}
	return dirs
}

// NewRegistry scans dirs for .md files and loads each as a ReplicantDef.
// Dirs are processed in order; the first definition found for a given name wins.
// If no dirs are provided the DefaultDirs() are used.
func NewRegistry(dirs ...string) (*Registry, error) {
	if len(dirs) == 0 {
		dirs = DefaultDirs()
	}

	r := &Registry{defs: make(map[string]*ReplicantDef)}

	for _, dir := range dirs {
		entries, err := filepath.Glob(filepath.Join(dir, "*.md"))
		if err != nil {
			// Glob only returns an error for a malformed pattern; treat as fatal.
			return nil, fmt.Errorf("glob %s: %w", dir, err)
		}
		for _, path := range entries {
			def, err := LoadFromFile(path)
			if err != nil {
				return nil, fmt.Errorf("load %s: %w", path, err)
			}
			// Earlier dirs (higher priority) win; do not overwrite.
			if _, exists := r.defs[def.Name]; !exists {
				r.defs[def.Name] = def
			}
		}
	}

	return r, nil
}

// Get returns the ReplicantDef with the given name, or (nil, false) if not found.
func (r *Registry) Get(name string) (*ReplicantDef, bool) {
	def, ok := r.defs[name]
	return def, ok
}

// List returns all loaded definitions sorted by name for deterministic output.
func (r *Registry) List() []*ReplicantDef {
	out := make([]*ReplicantDef, 0, len(r.defs))
	for _, def := range r.defs {
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}
