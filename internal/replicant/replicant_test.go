package replicant

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

const validDoc = `---
name: archer
description: A skilled test replicant
model: anthropic/claude-sonnet-4-20250514
tools:
  - read_file
  - grep
max_turns: 10
temperature: 0.7
max_tokens: 4096
---
You are Archer, a test replicant.
Do stuff well.
`

// ---------------------------------------------------------------------------
// LoadFromReader
// ---------------------------------------------------------------------------

func TestLoadFromReader_Valid(t *testing.T) {
	def, err := LoadFromReader(strings.NewReader(validDoc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if def.Name != "archer" {
		t.Errorf("Name = %q, want 'archer'", def.Name)
	}
	if def.Description != "A skilled test replicant" {
		t.Errorf("Description = %q", def.Description)
	}
	if def.Model != "anthropic/claude-sonnet-4-20250514" {
		t.Errorf("Model = %q", def.Model)
	}
	if len(def.Tools) != 2 || def.Tools[0] != "read_file" || def.Tools[1] != "grep" {
		t.Errorf("Tools = %v", def.Tools)
	}
	if def.MaxTurns != 10 {
		t.Errorf("MaxTurns = %d, want 10", def.MaxTurns)
	}
	if def.Temperature != 0.7 {
		t.Errorf("Temperature = %f, want 0.7", def.Temperature)
	}
	if def.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096", def.MaxTokens)
	}
	if !strings.Contains(def.SystemPrompt, "You are Archer") {
		t.Errorf("SystemPrompt missing expected content: %q", def.SystemPrompt)
	}
}

func TestLoadFromReader_NoFrontmatter(t *testing.T) {
	_, err := LoadFromReader(strings.NewReader("just plain text\nno frontmatter"))
	if err == nil {
		t.Error("expected error when frontmatter delimiter is missing")
	}
}

func TestLoadFromReader_MissingClosingDelimiter(t *testing.T) {
	doc := "---\nname: test\n"
	_, err := LoadFromReader(strings.NewReader(doc))
	if err == nil {
		t.Error("expected error when closing '---' is missing")
	}
}

func TestLoadFromReader_Defaults(t *testing.T) {
	// Only name is required; all other fields should get defaults.
	doc := "---\nname: minimal\n---\nA minimal system prompt.\n"
	def, err := LoadFromReader(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if def.MaxTurns != 50 {
		t.Errorf("default MaxTurns = %d, want 50", def.MaxTurns)
	}
	if def.Temperature != 0.3 {
		t.Errorf("default Temperature = %f, want 0.3", def.Temperature)
	}
	if def.MaxTokens != 8192 {
		t.Errorf("default MaxTokens = %d, want 8192", def.MaxTokens)
	}
}

func TestLoadFromReader_EmptyName_GetsDefault(t *testing.T) {
	// When name is omitted, ApplyDefaults sets it to "replicant".
	doc := "---\ndescription: nameless\n---\nPrompt here.\n"
	def, err := LoadFromReader(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.Name != "replicant" {
		t.Errorf("default Name = %q, want 'replicant'", def.Name)
	}
}

func TestLoadFromReader_SystemPromptTrimmed(t *testing.T) {
	doc := "---\nname: trimtest\n---\n\n\n  trimmed prompt  \n\n"
	def, err := LoadFromReader(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.SystemPrompt != "trimmed prompt" {
		t.Errorf("SystemPrompt = %q, want 'trimmed prompt'", def.SystemPrompt)
	}
}

func TestLoadFromReader_WindowsLineEndings(t *testing.T) {
	doc := "---\r\nname: wintest\r\n---\r\nSystem prompt here.\r\n"
	def, err := LoadFromReader(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("unexpected error with CRLF line endings: %v", err)
	}
	if def.Name != "wintest" {
		t.Errorf("Name = %q, want 'wintest'", def.Name)
	}
}

// ---------------------------------------------------------------------------
// LoadFromFile
// ---------------------------------------------------------------------------

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archer.md")
	mustWriteFile(t, path, validDoc)

	def, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.Name != "archer" {
		t.Errorf("Name = %q, want 'archer'", def.Name)
	}
	if def.SourcePath != path {
		t.Errorf("SourcePath = %q, want %q", def.SourcePath, path)
	}
}

func TestLoadFromFile_Missing(t *testing.T) {
	_, err := LoadFromFile("/no/such/file.md")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadFromFile_BadContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.md")
	mustWriteFile(t, path, "no frontmatter here")

	_, err := LoadFromFile(path)
	if err == nil {
		t.Error("expected error for file without frontmatter")
	}
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

func makeReplicantFile(t *testing.T, dir, name, description string) {
	t.Helper()
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\nSystem prompt for " + name + ".\n"
	mustWriteFile(t, filepath.Join(dir, name+".md"), content)
}

func TestRegistry_NewRegistry(t *testing.T) {
	dir := t.TempDir()
	makeReplicantFile(t, dir, "alpha", "First replicant")
	makeReplicantFile(t, dir, "beta", "Second replicant")

	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	def, ok := reg.Get("alpha")
	if !ok {
		t.Fatal("expected to find 'alpha'")
	}
	if def.Name != "alpha" {
		t.Errorf("Name = %q, want 'alpha'", def.Name)
	}

	if _, ok := reg.Get("beta"); !ok {
		t.Error("expected to find 'beta'")
	}
}

func TestRegistry_Get_Missing(t *testing.T) {
	reg, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Error("expected ok=false for missing replicant")
	}
}

func TestRegistry_List_Sorted(t *testing.T) {
	dir := t.TempDir()
	// Create in reverse alphabetical order to verify sorting.
	makeReplicantFile(t, dir, "zeta", "Last")
	makeReplicantFile(t, dir, "alpha", "First")
	makeReplicantFile(t, dir, "mu", "Middle")

	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list := reg.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 replicants, got %d", len(list))
	}
	if list[0].Name != "alpha" || list[1].Name != "mu" || list[2].Name != "zeta" {
		t.Errorf("List not sorted: [%s, %s, %s]", list[0].Name, list[1].Name, list[2].Name)
	}
}

func TestRegistry_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg.List()) != 0 {
		t.Errorf("expected empty registry, got %d entries", len(reg.List()))
	}
}

func TestRegistry_PriorityOrder(t *testing.T) {
	// First dir should win when same name appears in both.
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	content1 := "---\nname: shared\ndescription: from dir1\n---\nPrompt 1.\n"
	content2 := "---\nname: shared\ndescription: from dir2\n---\nPrompt 2.\n"
	mustWriteFile(t, filepath.Join(dir1, "shared.md"), content1)
	mustWriteFile(t, filepath.Join(dir2, "shared.md"), content2)

	reg, err := NewRegistry(dir1, dir2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	def, ok := reg.Get("shared")
	if !ok {
		t.Fatal("expected to find 'shared'")
	}
	if def.Description != "from dir1" {
		t.Errorf("expected dir1 to win priority, got description=%q", def.Description)
	}
}

func TestRegistry_IgnoresNonMdFiles(t *testing.T) {
	dir := t.TempDir()
	makeReplicantFile(t, dir, "valid", "Valid replicant")
	// Non-.md file should be ignored.
	mustWriteFile(t, filepath.Join(dir, "readme.txt"), "not a replicant")

	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg.List()) != 1 {
		t.Errorf("expected 1 replicant, got %d", len(reg.List()))
	}
}

func TestRegistry_BadFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	// Create an invalid .md file (no frontmatter).
	mustWriteFile(t, filepath.Join(dir, "invalid.md"), "no frontmatter")

	_, err := NewRegistry(dir)
	if err == nil {
		t.Error("expected error when a .md file has invalid content")
	}
}

// ---------------------------------------------------------------------------
// ApplyDefaults
// ---------------------------------------------------------------------------

func TestApplyDefaults_AllZero(t *testing.T) {
	def := &ReplicantDef{}
	def.ApplyDefaults()

	if def.MaxTurns != 50 {
		t.Errorf("MaxTurns = %d, want 50", def.MaxTurns)
	}
	if def.Temperature != 0.3 {
		t.Errorf("Temperature = %f, want 0.3", def.Temperature)
	}
	if def.MaxTokens != 8192 {
		t.Errorf("MaxTokens = %d, want 8192", def.MaxTokens)
	}
	if def.Name != "replicant" {
		t.Errorf("Name = %q, want 'replicant'", def.Name)
	}
}

func TestApplyDefaults_PreservesExisting(t *testing.T) {
	def := &ReplicantDef{
		Name:        "custom",
		MaxTurns:    100,
		Temperature: 0.9,
		MaxTokens:   2048,
	}
	def.ApplyDefaults()

	if def.Name != "custom" {
		t.Errorf("Name should not be overwritten: %q", def.Name)
	}
	if def.MaxTurns != 100 {
		t.Errorf("MaxTurns should not be overwritten: %d", def.MaxTurns)
	}
	if def.Temperature != 0.9 {
		t.Errorf("Temperature should not be overwritten: %f", def.Temperature)
	}
	if def.MaxTokens != 2048 {
		t.Errorf("MaxTokens should not be overwritten: %d", def.MaxTokens)
	}
}
