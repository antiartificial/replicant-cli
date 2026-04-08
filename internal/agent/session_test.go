package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustNewSession(t *testing.T, dir, name, model string) *Session {
	t.Helper()
	s, err := NewSession(dir, name, model)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// ---------------------------------------------------------------------------
// NewSession
// ---------------------------------------------------------------------------

func TestNewSession_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	s := mustNewSession(t, dir, "testbot", "claude-3")

	if s.ID == "" {
		t.Error("session ID should not be empty")
	}
	if s.Replicant != "testbot" {
		t.Errorf("Replicant = %q, want 'testbot'", s.Replicant)
	}
	if s.Model != "claude-3" {
		t.Errorf("Model = %q, want 'claude-3'", s.Model)
	}

	// File must exist.
	pattern := filepath.Join(dir, "*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		t.Fatal("expected at least one .jsonl file in session dir")
	}
}

func TestNewSession_WritesSessionStart(t *testing.T) {
	dir := t.TempDir()
	s := mustNewSession(t, dir, "starter", "model-x")
	_ = s.Close()

	pattern := filepath.Join(dir, "*.jsonl")
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		t.Fatal("no session file found")
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		t.Fatal("session file is empty")
	}

	var entry SessionEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("parse first line: %v", err)
	}

	if entry.Type != "session_start" {
		t.Errorf("first entry type = %q, want 'session_start'", entry.Type)
	}
	if entry.Replicant != "starter" {
		t.Errorf("first entry Replicant = %q, want 'starter'", entry.Replicant)
	}
	if entry.Model != "model-x" {
		t.Errorf("first entry Model = %q, want 'model-x'", entry.Model)
	}
	if entry.ID == "" {
		t.Error("first entry ID should not be empty")
	}
}

func TestNewSession_CreatesMissingDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "sessions", "sub")

	s, err := NewSession(dir, "bot", "model")
	if err != nil {
		t.Fatalf("NewSession with missing dirs: %v", err)
	}
	defer s.Close()

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("expected dir to be created: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Append
// ---------------------------------------------------------------------------

func TestSession_Append(t *testing.T) {
	dir := t.TempDir()
	s := mustNewSession(t, dir, "appendbot", "m")

	entries := []SessionEntry{
		{Type: "message", Role: "user", Content: "hello"},
		{Type: "message", Role: "assistant", Content: "hi there"},
		{Type: "tool_call", ToolName: "read_file", ToolArgs: `{"path":"/tmp/x"}`, ToolID: "tc1"},
	}
	for _, e := range entries {
		if err := s.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	_ = s.Close()

	// Find the session file.
	matches, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// 1 session_start + 3 appended = 4 lines.
	if len(lines) != 4 {
		t.Errorf("expected 4 lines, got %d", len(lines))
	}

	// Verify the appended entries.
	for i, want := range entries {
		var got SessionEntry
		if err := json.Unmarshal([]byte(lines[i+1]), &got); err != nil {
			t.Fatalf("parse line %d: %v", i+1, err)
		}
		if got.Type != want.Type {
			t.Errorf("line %d Type = %q, want %q", i+1, got.Type, want.Type)
		}
		if got.Content != want.Content && got.ToolName != want.ToolName {
			t.Errorf("line %d content mismatch", i+1)
		}
	}
}

func TestSession_Append_SetsTimestamp(t *testing.T) {
	dir := t.TempDir()
	s := mustNewSession(t, dir, "ts", "m")

	before := time.Now().UTC().Add(-time.Second)
	entry := SessionEntry{Type: "message", Role: "user", Content: "hi"}
	if err := s.Append(entry); err != nil {
		t.Fatalf("Append: %v", err)
	}
	_ = s.Close()

	matches, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	data, _ := os.ReadFile(matches[0])
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	var got SessionEntry
	if err := json.Unmarshal([]byte(lines[1]), &got); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !got.Timestamp.After(before) {
		t.Errorf("Timestamp %v not after %v", got.Timestamp, before)
	}
}

// ---------------------------------------------------------------------------
// LoadSession
// ---------------------------------------------------------------------------

func TestLoadSession(t *testing.T) {
	dir := t.TempDir()
	s := mustNewSession(t, dir, "loadbot", "mod")
	_ = s.Append(SessionEntry{Type: "message", Role: "user", Content: "q1"})
	_ = s.Append(SessionEntry{Type: "message", Role: "assistant", Content: "a1"})
	_ = s.Close()

	matches, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	entries, err := LoadSession(matches[0])
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}

	// 1 session_start + 2 messages = 3 entries.
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Type != "session_start" {
		t.Errorf("entries[0].Type = %q", entries[0].Type)
	}
	if entries[1].Content != "q1" {
		t.Errorf("entries[1].Content = %q, want 'q1'", entries[1].Content)
	}
	if entries[2].Content != "a1" {
		t.Errorf("entries[2].Content = %q, want 'a1'", entries[2].Content)
	}
}

func TestLoadSession_Missing(t *testing.T) {
	_, err := LoadSession("/no/such/file.jsonl")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadSession_SkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.jsonl")
	content := `{"type":"session_start","timestamp":"2024-01-01T00:00:00Z","id":"x"}` + "\n" +
		`NOT VALID JSON` + "\n" +
		`{"type":"message","role":"user","content":"hello"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	// Malformed line is skipped; 2 valid lines remain.
	if len(entries) != 2 {
		t.Errorf("expected 2 entries after skipping malformed line, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// ReconstructHistory
// ---------------------------------------------------------------------------

func TestReconstructHistory_Empty(t *testing.T) {
	msgs := ReconstructHistory(nil)
	if msgs != nil && len(msgs) != 0 {
		t.Errorf("expected empty result, got %v", msgs)
	}
}

func TestReconstructHistory_UserAssistant(t *testing.T) {
	entries := []SessionEntry{
		{Type: "session_start", ID: "s1"},
		{Type: "message", Role: "user", Content: "Hello"},
		{Type: "message", Role: "assistant", Content: "Hi"},
	}
	msgs := ReconstructHistory(entries)

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content[0].Text != "Hello" {
		t.Errorf("msgs[0] = %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content[0].Text != "Hi" {
		t.Errorf("msgs[1] = %+v", msgs[1])
	}
}

func TestReconstructHistory_ToolCallAndResult(t *testing.T) {
	entries := []SessionEntry{
		{Type: "session_start", ID: "s1"},
		{Type: "message", Role: "user", Content: "Use a tool"},
		{Type: "tool_call", ToolID: "tc1", ToolName: "read_file", ToolArgs: `{"path":"/tmp/x"}`},
		{Type: "tool_result", ToolID: "tc1", Result: "file contents", IsError: false},
		{Type: "message", Role: "assistant", Content: "Done."},
	}
	msgs := ReconstructHistory(entries)

	// Expected structure:
	// 1. user message
	// 2. assistant (with tool_use block)
	// 3. user (with tool_result block)
	// 4. assistant text
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d: %+v", len(msgs), msgs)
	}

	// First: user message.
	if msgs[0].Role != "user" {
		t.Errorf("msgs[0].Role = %q, want 'user'", msgs[0].Role)
	}

	// Second: assistant with tool_use.
	if msgs[1].Role != "assistant" {
		t.Errorf("msgs[1].Role = %q, want 'assistant'", msgs[1].Role)
	}
	found := false
	for _, b := range msgs[1].Content {
		if b.Type == "tool_use" && b.Name == "read_file" {
			found = true
		}
	}
	if !found {
		t.Errorf("msgs[1] should contain tool_use block: %+v", msgs[1].Content)
	}

	// Third: user with tool_result.
	if msgs[2].Role != "user" {
		t.Errorf("msgs[2].Role = %q, want 'user'", msgs[2].Role)
	}
	found = false
	for _, b := range msgs[2].Content {
		if b.Type == "tool_result" && b.Content == "file contents" {
			found = true
		}
	}
	if !found {
		t.Errorf("msgs[2] should contain tool_result block: %+v", msgs[2].Content)
	}

	// Fourth: assistant text.
	if msgs[3].Role != "assistant" {
		t.Errorf("msgs[3].Role = %q, want 'assistant'", msgs[3].Role)
	}
}

func TestReconstructHistory_SkipsSessionStart(t *testing.T) {
	entries := []SessionEntry{
		{Type: "session_start", ID: "s1"},
		{Type: "session_start", ID: "s1"}, // duplicate, should be skipped
		{Type: "message", Role: "user", Content: "hi"},
	}
	msgs := ReconstructHistory(entries)
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// ListSessions
// ---------------------------------------------------------------------------

func TestListSessions(t *testing.T) {
	dir := t.TempDir()

	// Use distinct replicant names to get distinct filenames even within the
	// same second (the filename includes the sanitised replicant name).
	s1 := mustNewSession(t, dir, "bot-alpha", "m")
	_ = s1.Close()
	s2 := mustNewSession(t, dir, "bot-beta", "m")
	_ = s2.Close()

	infos, err := ListSessions(dir, 0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(infos) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(infos))
	}
	// Most recent first — both have the same or very close timestamps; just
	// verify the sort doesn't panic and both are present.
	names := make(map[string]bool)
	for _, info := range infos {
		names[info.Replicant] = true
	}
	if !names["bot-alpha"] || !names["bot-beta"] {
		t.Errorf("unexpected replicant names: %v", names)
	}
}

func TestListSessions_WithLimit(t *testing.T) {
	dir := t.TempDir()

	// Use distinct names to guarantee distinct filenames.
	botNames := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	for _, name := range botNames {
		s := mustNewSession(t, dir, name, "m")
		_ = s.Close()
	}

	infos, err := ListSessions(dir, 3)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(infos) != 3 {
		t.Errorf("expected 3 sessions with limit=3, got %d", len(infos))
	}
}

func TestListSessions_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	infos, err := ListSessions(dir, 0)
	if err != nil {
		t.Fatalf("ListSessions on empty dir: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(infos))
	}
}

// ---------------------------------------------------------------------------
// FindSession
// ---------------------------------------------------------------------------

func TestFindSession_ExactPath(t *testing.T) {
	dir := t.TempDir()
	s := mustNewSession(t, dir, "find", "m")
	_ = s.Close()

	matches, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	path, err := FindSession(dir, matches[0])
	if err != nil {
		t.Fatalf("FindSession with exact path: %v", err)
	}
	if path != matches[0] {
		t.Errorf("path = %q, want %q", path, matches[0])
	}
}

func TestFindSession_ByPrefix(t *testing.T) {
	dir := t.TempDir()
	s := mustNewSession(t, dir, "prefixtest", "m")
	id := s.ID
	_ = s.Close()

	// Use first 8 chars as prefix.
	prefix := id[:8]
	path, err := FindSession(dir, prefix)
	if err != nil {
		t.Fatalf("FindSession by prefix: %v", err)
	}
	if !strings.HasSuffix(path, ".jsonl") {
		t.Errorf("expected .jsonl path, got: %q", path)
	}
}

func TestFindSession_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := FindSession(dir, "nonexistent-prefix")
	if err == nil {
		t.Error("expected error for no matching session")
	}
}

func TestFindSession_AmbiguousPrefix(t *testing.T) {
	dir := t.TempDir()

	// Write two files that share a common prefix manually.
	// Use a fixed prefix that both share.
	content := `{"type":"session_start","timestamp":"2024-01-01T00:00:00Z","id":"abc-one","replicant":"r"}` + "\n"
	f1 := filepath.Join(dir, "abc-one.jsonl")
	f2 := filepath.Join(dir, "abc-two.jsonl")
	if err := os.WriteFile(f1, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	content2 := `{"type":"session_start","timestamp":"2024-01-01T00:00:00Z","id":"abc-two","replicant":"r"}` + "\n"
	if err := os.WriteFile(f2, []byte(content2), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := FindSession(dir, "abc")
	if err == nil {
		t.Error("expected ambiguous error for prefix matching multiple sessions")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error should say 'ambiguous': %v", err)
	}
}

// ---------------------------------------------------------------------------
// sanitiseName
// ---------------------------------------------------------------------------

func TestSanitiseName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"with space", "with_space"},
		{"with/slash", "with_slash"},
		{"CamelCase", "CamelCase"},
		{"123", "123"},
		{"", "session"},
		{"!@#$%", "_____"},
	}
	for _, tt := range tests {
		got := sanitiseName(tt.input)
		if got != tt.want {
			t.Errorf("sanitiseName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ResumeSession
// ---------------------------------------------------------------------------

func TestResumeSession(t *testing.T) {
	dir := t.TempDir()
	s := mustNewSession(t, dir, "resume", "model-r")
	_ = s.Append(SessionEntry{Type: "message", Role: "user", Content: "first"})
	_ = s.Close()

	matches, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	resumed, err := ResumeSession(matches[0])
	if err != nil {
		t.Fatalf("ResumeSession: %v", err)
	}
	defer resumed.Close()

	if resumed.Replicant != "resume" {
		t.Errorf("Replicant = %q, want 'resume'", resumed.Replicant)
	}
	if resumed.Model != "model-r" {
		t.Errorf("Model = %q, want 'model-r'", resumed.Model)
	}

	// Append more entries.
	if err := resumed.Append(SessionEntry{Type: "message", Role: "user", Content: "resumed"}); err != nil {
		t.Fatalf("Append after resume: %v", err)
	}
	_ = resumed.Close()

	entries, err := LoadSession(matches[0])
	if err != nil {
		t.Fatalf("LoadSession after resume: %v", err)
	}
	// session_start + first + resumed = 3 entries.
	if len(entries) != 3 {
		t.Errorf("expected 3 entries after resume, got %d", len(entries))
	}
}
