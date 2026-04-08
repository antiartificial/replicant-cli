package replicant

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadFromFile reads a replicant definition from a markdown file with YAML frontmatter.
func LoadFromFile(path string) (*ReplicantDef, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	def, err := LoadFromReader(f)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	def.SourcePath = path
	return def, nil
}

// LoadFromReader parses a replicant definition from a reader containing a
// markdown file with YAML frontmatter delimited by "---" lines.
func LoadFromReader(r io.Reader) (*ReplicantDef, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	frontmatter, body, err := splitFrontmatter(data)
	if err != nil {
		return nil, err
	}

	var def ReplicantDef
	if err := yaml.Unmarshal(frontmatter, &def); err != nil {
		return nil, fmt.Errorf("parse yaml frontmatter: %w", err)
	}

	def.SystemPrompt = strings.TrimSpace(body)
	def.ApplyDefaults()
	return &def, nil
}

// splitFrontmatter splits a document on "---" delimiters and returns the
// YAML frontmatter bytes and the markdown body string.
// The file must begin with "---" on its own line.
func splitFrontmatter(data []byte) (frontmatter []byte, body string, err error) {
	// Normalise line endings.
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))

	const delim = "---"

	// The file must start with the opening delimiter (optional leading newline
	// is tolerated).
	trimmed := bytes.TrimLeft(data, "\n")
	if !bytes.HasPrefix(trimmed, []byte(delim)) {
		return nil, "", fmt.Errorf("missing opening frontmatter delimiter '---'")
	}

	// Advance past the first "---\n".
	rest := trimmed[len(delim):]
	if len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	}

	// Find the closing "---".
	idx := bytes.Index(rest, []byte("\n"+delim))
	if idx == -1 {
		return nil, "", fmt.Errorf("missing closing frontmatter delimiter '---'")
	}

	frontmatter = rest[:idx]

	// Everything after the closing "---\n" is the body.
	after := rest[idx+1+len(delim):]
	if len(after) > 0 && after[0] == '\n' {
		after = after[1:]
	}
	body = string(after)

	return frontmatter, body, nil
}
