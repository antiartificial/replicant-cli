package tools

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/antiartificial/replicant/internal/permission"
)

const maxGlobResults = 200

// GlobTool implements the glob_files tool.
type GlobTool struct{}

func (t *GlobTool) Name() string { return "glob_files" }

func (t *GlobTool) Description() string {
	return "Recursively walk a directory and return file paths that match a glob pattern. " +
		fmt.Sprintf("Returns at most %d results, sorted alphabetically.", maxGlobResults)
}

func (t *GlobTool) Risk() permission.RiskLevel { return permission.RiskNone }

func (t *GlobTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Glob pattern to match filenames against (e.g. \"*.go\", \"**/*.ts\").",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Root directory to search. Defaults to current working directory.",
			},
		},
		"required": []string{"pattern"},
	}
}

type globArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

func (t *GlobTool) Run(args string) (string, error) {
	var a globArgs
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return "", fmt.Errorf("glob_files: invalid arguments: %w", err)
	}
	if a.Pattern == "" {
		return "", fmt.Errorf("glob_files: pattern is required")
	}
	if a.Path == "" {
		a.Path = "."
	}

	// Extract the bare filename/extension pattern for matching — strip any
	// leading directory components from the pattern so that filepath.Match
	// works against just the filename portion.
	matchPart := filepath.Base(a.Pattern)

	var matches []string
	err := filepath.WalkDir(a.Path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		matched, merr := filepath.Match(matchPart, name)
		if merr != nil {
			return merr
		}
		if matched {
			matches = append(matches, p)
			if len(matches) >= maxGlobResults {
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("glob_files: walking %s: %w", a.Path, err)
	}

	sort.Strings(matches)

	if len(matches) == 0 {
		return "no matches found", nil
	}
	return strings.Join(matches, "\n"), nil
}
