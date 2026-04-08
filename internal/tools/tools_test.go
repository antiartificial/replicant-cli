package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/antiartificial/replicant/internal/permission"
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

func toJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// Tool interface — Name / Description / Parameters / Risk
// ---------------------------------------------------------------------------

func TestToolInterface(t *testing.T) {
	tools := []Tool{
		&ReadFileTool{},
		&EditFileTool{},
		&ExecuteTool{},
		&GlobTool{},
		&GrepTool{},
	}

	for _, tool := range tools {
		t.Run(tool.Name(), func(t *testing.T) {
			if tool.Name() == "" {
				t.Error("Name() returned empty string")
			}
			if tool.Description() == "" {
				t.Error("Description() returned empty string")
			}
			params := tool.Parameters()
			if params == nil || len(params) == 0 {
				t.Error("Parameters() returned nil or empty map")
			}
			// Risk must be a known value.
			r := tool.Risk()
			s := r.String()
			if s == "" || s == "unknown" {
				t.Errorf("Risk().String() returned %q", s)
			}
		})
	}
}

func TestRiskLevels(t *testing.T) {
	if got := (&ReadFileTool{}).Risk(); got != permission.RiskNone {
		t.Errorf("read_file risk = %v, want RiskNone", got)
	}
	if got := (&GlobTool{}).Risk(); got != permission.RiskNone {
		t.Errorf("glob_files risk = %v, want RiskNone", got)
	}
	if got := (&GrepTool{}).Risk(); got != permission.RiskNone {
		t.Errorf("grep risk = %v, want RiskNone", got)
	}
	if got := (&EditFileTool{}).Risk(); got != permission.RiskLow {
		t.Errorf("edit_file risk = %v, want RiskLow", got)
	}
	if got := (&ExecuteTool{}).Risk(); got != permission.RiskHigh {
		t.Errorf("execute risk = %v, want RiskHigh", got)
	}
}

// ---------------------------------------------------------------------------
// ReadFileTool
// ---------------------------------------------------------------------------

func TestReadFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	mustWriteFile(t, path, "line one\nline two\nline three\n")

	tool := &ReadFileTool{}
	result, err := tool.Run(toJSON(t, map[string]any{"path": path}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain all three lines with line numbers.
	for _, want := range []string{"1\t", "2\t", "3\t", "line one", "line two", "line three"} {
		if !strings.Contains(result, want) {
			t.Errorf("result missing %q:\n%s", want, result)
		}
	}
}

func TestReadFile_WithOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nums.txt")
	mustWriteFile(t, path, "a\nb\nc\nd\n")

	tool := &ReadFileTool{}
	result, err := tool.Run(toJSON(t, map[string]any{"path": path, "offset": 3}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(result, "1\t") || strings.Contains(result, "2\t") {
		t.Error("result should not contain lines before offset 3")
	}
	if !strings.Contains(result, "c") || !strings.Contains(result, "d") {
		t.Errorf("result missing lines from offset 3 onward:\n%s", result)
	}
}

func TestReadFile_WithLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nums.txt")
	mustWriteFile(t, path, "a\nb\nc\nd\n")

	tool := &ReadFileTool{}
	result, err := tool.Run(toJSON(t, map[string]any{"path": path, "limit": 2}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d:\n%s", len(lines), result)
	}
}

func TestReadFile_OffsetAndLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nums.txt")
	mustWriteFile(t, path, "a\nb\nc\nd\ne\n")

	tool := &ReadFileTool{}
	result, err := tool.Run(toJSON(t, map[string]any{"path": path, "offset": 2, "limit": 2}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain lines 2 and 3 only.
	if !strings.Contains(result, "b") || !strings.Contains(result, "c") {
		t.Errorf("expected lines b and c:\n%s", result)
	}
	if strings.Contains(result, "a") || strings.Contains(result, "d") {
		t.Errorf("should not contain lines outside offset+limit:\n%s", result)
	}
}

func TestReadFile_MissingFile(t *testing.T) {
	tool := &ReadFileTool{}
	_, err := tool.Run(toJSON(t, map[string]any{"path": "/no/such/file.txt"}))
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestReadFile_OffsetBeyondEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "short.txt")
	mustWriteFile(t, path, "only one line\n")

	tool := &ReadFileTool{}
	_, err := tool.Run(toJSON(t, map[string]any{"path": path, "offset": 100}))
	if err == nil {
		t.Error("expected error when offset exceeds file length")
	}
}

func TestReadFile_InvalidJSON(t *testing.T) {
	tool := &ReadFileTool{}
	_, err := tool.Run("not json")
	if err == nil {
		t.Error("expected error for invalid JSON args")
	}
}

// ---------------------------------------------------------------------------
// EditFileTool
// ---------------------------------------------------------------------------

func TestEditFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	mustWriteFile(t, path, "hello world\n")

	tool := &EditFileTool{}
	result, err := tool.Run(toJSON(t, map[string]any{
		"path":       path,
		"old_string": "hello",
		"new_string": "goodbye",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "replaced 1 occurrence") {
		t.Errorf("unexpected result: %s", result)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "goodbye world") {
		t.Errorf("file not updated: %s", string(data))
	}
}

func TestEditFile_OldStringNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	mustWriteFile(t, path, "hello world\n")

	tool := &EditFileTool{}
	_, err := tool.Run(toJSON(t, map[string]any{
		"path":       path,
		"old_string": "NOTPRESENT",
		"new_string": "anything",
	}))
	if err == nil {
		t.Error("expected error when old_string not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestEditFile_NonUniqueOldString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	mustWriteFile(t, path, "foo bar foo\n")

	tool := &EditFileTool{}
	_, err := tool.Run(toJSON(t, map[string]any{
		"path":       path,
		"old_string": "foo",
		"new_string": "baz",
	}))
	if err == nil {
		t.Error("expected error when old_string appears more than once")
	}
	if !strings.Contains(err.Error(), "times") {
		t.Errorf("error should mention count, got: %v", err)
	}
}

func TestEditFile_MissingFile(t *testing.T) {
	tool := &EditFileTool{}
	_, err := tool.Run(toJSON(t, map[string]any{
		"path":       "/no/such/file.txt",
		"old_string": "x",
		"new_string": "y",
	}))
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestEditFile_InvalidJSON(t *testing.T) {
	tool := &EditFileTool{}
	_, err := tool.Run("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// ExecuteTool
// ---------------------------------------------------------------------------

func TestExecute_SimpleCommand(t *testing.T) {
	tool := &ExecuteTool{}
	result, err := tool.Run(toJSON(t, map[string]any{"command": "echo hello"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected 'hello' in output, got: %s", result)
	}
	if !strings.Contains(result, "exit_code: 0") {
		t.Errorf("expected exit_code 0, got: %s", result)
	}
}

func TestExecute_ExitCode(t *testing.T) {
	tool := &ExecuteTool{}
	result, err := tool.Run(toJSON(t, map[string]any{"command": "exit 42"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "exit_code: 42") {
		t.Errorf("expected exit_code 42, got: %s", result)
	}
}

func TestExecute_Timeout(t *testing.T) {
	start := time.Now()
	tool := &ExecuteTool{}
	result, err := tool.Run(toJSON(t, map[string]any{
		"command": "sleep 10",
		"timeout": 1,
	}))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Errorf("timed-out command took too long: %v (want <3s)", elapsed)
	}
	// Should have a non-zero exit code indicating timeout/kill.
	if strings.Contains(result, "exit_code: 0") {
		t.Errorf("expected non-zero exit code for timed-out command, got: %s", result)
	}
}

func TestExecute_CombinedOutput(t *testing.T) {
	tool := &ExecuteTool{}
	// Write to both stdout and stderr.
	result, err := tool.Run(toJSON(t, map[string]any{
		"command": "echo stdout_msg && echo stderr_msg >&2",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "stdout_msg") {
		t.Errorf("stdout missing: %s", result)
	}
	if !strings.Contains(result, "stderr_msg") {
		t.Errorf("stderr missing: %s", result)
	}
}

func TestExecute_MissingCommand(t *testing.T) {
	tool := &ExecuteTool{}
	_, err := tool.Run(toJSON(t, map[string]any{"command": ""}))
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestExecute_InvalidJSON(t *testing.T) {
	tool := &ExecuteTool{}
	_, err := tool.Run("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// GlobTool
// ---------------------------------------------------------------------------

func TestGlob_MatchPattern(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.go"), "")
	mustWriteFile(t, filepath.Join(dir, "b.go"), "")
	mustWriteFile(t, filepath.Join(dir, "c.txt"), "")

	tool := &GlobTool{}
	result, err := tool.Run(toJSON(t, map[string]any{"pattern": "*.go", "path": dir}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "a.go") || !strings.Contains(result, "b.go") {
		t.Errorf("expected .go files in result: %s", result)
	}
	if strings.Contains(result, "c.txt") {
		t.Errorf("unexpected .txt file in result: %s", result)
	}
}

func TestGlob_NoMatches(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.txt"), "")

	tool := &GlobTool{}
	result, err := tool.Run(toJSON(t, map[string]any{"pattern": "*.go", "path": dir}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "no matches") {
		t.Errorf("expected 'no matches', got: %s", result)
	}
}

func TestGlob_RespectsPath(t *testing.T) {
	dir := t.TempDir()
	subA := filepath.Join(dir, "a")
	subB := filepath.Join(dir, "b")
	if err := os.MkdirAll(subA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(subB, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(subA, "match.go"), "")
	mustWriteFile(t, filepath.Join(subB, "other.go"), "")

	tool := &GlobTool{}
	result, err := tool.Run(toJSON(t, map[string]any{"pattern": "*.go", "path": subA}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "match.go") {
		t.Errorf("expected match.go: %s", result)
	}
	if strings.Contains(result, "other.go") {
		t.Errorf("other.go from sibling dir should not appear: %s", result)
	}
}

func TestGlob_Recursive(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(dir, "top.go"), "")
	mustWriteFile(t, filepath.Join(sub, "nested.go"), "")

	tool := &GlobTool{}
	result, err := tool.Run(toJSON(t, map[string]any{"pattern": "*.go", "path": dir}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "top.go") || !strings.Contains(result, "nested.go") {
		t.Errorf("expected both top.go and nested.go: %s", result)
	}
}

func TestGlob_MissingPattern(t *testing.T) {
	tool := &GlobTool{}
	_, err := tool.Run(toJSON(t, map[string]any{}))
	if err == nil {
		t.Error("expected error for missing pattern")
	}
}

func TestGlob_InvalidJSON(t *testing.T) {
	tool := &GlobTool{}
	_, err := tool.Run("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// GrepTool
// ---------------------------------------------------------------------------

func TestGrep_FindPattern(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "file.txt"), "apple\nbanana\napricot\n")

	tool := &GrepTool{}
	result, err := tool.Run(toJSON(t, map[string]any{"pattern": "apple", "path": dir}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "apple") {
		t.Errorf("expected 'apple' in result: %s", result)
	}
}

func TestGrep_NoMatches(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "file.txt"), "hello world\n")

	tool := &GrepTool{}
	result, err := tool.Run(toJSON(t, map[string]any{"pattern": "NOTFOUND", "path": dir}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "no matches") {
		t.Errorf("expected 'no matches', got: %s", result)
	}
}

func TestGrep_IncludeFilter(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.go"), "package main // target\n")
	mustWriteFile(t, filepath.Join(dir, "b.txt"), "target in text\n")

	tool := &GrepTool{}
	result, err := tool.Run(toJSON(t, map[string]any{
		"pattern": "target",
		"path":    dir,
		"include": "*.go",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "a.go") {
		t.Errorf("expected a.go in result: %s", result)
	}
	if strings.Contains(result, "b.txt") {
		t.Errorf("b.txt should be excluded by include filter: %s", result)
	}
}

func TestGrep_MissingPattern(t *testing.T) {
	tool := &GrepTool{}
	_, err := tool.Run(toJSON(t, map[string]any{}))
	if err == nil {
		t.Error("expected error for missing pattern")
	}
}

func TestGrep_InvalidJSON(t *testing.T) {
	tool := &GrepTool{}
	_, err := tool.Run("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

func TestRegistry_NewRegistryHasAllBuiltins(t *testing.T) {
	r := NewRegistry()
	wantNames := []string{"read_file", "edit_file", "execute", "glob_files", "grep"}
	for _, name := range wantNames {
		if _, ok := r.Get(name); !ok {
			t.Errorf("registry missing built-in tool: %s", name)
		}
	}
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()
	tool, ok := r.Get("read_file")
	if !ok {
		t.Fatal("expected to find read_file")
	}
	if tool.Name() != "read_file" {
		t.Errorf("Name() = %q, want 'read_file'", tool.Name())
	}
}

func TestRegistry_GetMissing(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent_tool")
	if ok {
		t.Error("expected ok=false for missing tool")
	}
}

func TestRegistry_Resolve(t *testing.T) {
	r := NewRegistry()
	tools := r.Resolve([]string{"read_file", "grep", "nonexistent"})
	if len(tools) != 2 {
		t.Errorf("Resolve returned %d tools, want 2", len(tools))
	}
	names := make(map[string]bool)
	for _, t := range tools {
		names[t.Name()] = true
	}
	if !names["read_file"] || !names["grep"] {
		t.Errorf("unexpected resolved names: %v", names)
	}
}

func TestRegistry_ResolveEmpty(t *testing.T) {
	r := NewRegistry()
	tools := r.Resolve([]string{})
	if len(tools) != 0 {
		t.Errorf("expected 0 tools for empty names, got %d", len(tools))
	}
}

func TestRegistry_All(t *testing.T) {
	r := NewRegistry()
	all := r.All()
	if len(all) < 5 {
		t.Errorf("expected at least 5 tools from All(), got %d", len(all))
	}
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()

	// Create a trivial custom tool.
	custom := &customTestTool{name: "custom_tool"}
	r.Register(custom)

	got, ok := r.Get("custom_tool")
	if !ok {
		t.Fatal("expected to find custom_tool after Register")
	}
	if got.Name() != "custom_tool" {
		t.Errorf("Name() = %q, want 'custom_tool'", got.Name())
	}
}

func TestRegistry_RegisterOverwrites(t *testing.T) {
	r := NewRegistry()
	original, _ := r.Get("grep")
	r.Register(&customTestTool{name: "grep"})
	got, _ := r.Get("grep")
	if got == original {
		t.Error("Register should overwrite existing tool")
	}
}

// customTestTool is a minimal Tool for registry tests.
type customTestTool struct {
	name string
}

func (c *customTestTool) Name() string                  { return c.name }
func (c *customTestTool) Description() string           { return "test tool" }
func (c *customTestTool) Parameters() map[string]any    { return map[string]any{"type": "object"} }
func (c *customTestTool) Run(args string) (string, error) { return "ok", nil }
func (c *customTestTool) Risk() permission.RiskLevel    { return permission.RiskNone }

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestReadFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	mustWriteFile(t, path, "")

	tool := &ReadFileTool{}
	// An empty file has 0 lines; offset 1 (default) exceeds the file length.
	// The implementation returns an error in this case.
	_, err := tool.Run(toJSON(t, map[string]any{"path": path}))
	if err == nil {
		t.Error("expected error when reading empty file at default offset=1")
	}
	if !strings.Contains(err.Error(), "offset") {
		t.Errorf("error should mention 'offset', got: %v", err)
	}
}

func TestEditFile_ReplacePreservesRemainder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	content := "line1\nTARGET\nline3\n"
	mustWriteFile(t, path, content)

	tool := &EditFileTool{}
	_, err := tool.Run(toJSON(t, map[string]any{
		"path":       path,
		"old_string": "TARGET",
		"new_string": "REPLACED",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line3") {
		t.Errorf("surrounding lines lost after edit:\n%s", got)
	}
	if !strings.Contains(got, "REPLACED") {
		t.Errorf("replacement not applied:\n%s", got)
	}
	if strings.Contains(got, "TARGET") {
		t.Errorf("old_string still present:\n%s", got)
	}
}

func TestExecute_WorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	tool := &ExecuteTool{}
	result, err := tool.Run(toJSON(t, map[string]any{
		"command": fmt.Sprintf("ls %s", dir),
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "exit_code: 0") {
		t.Errorf("expected successful ls, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// Timeout / context / RunTool tests
// ---------------------------------------------------------------------------

// contextToolStub is a minimal ContextTool used in tests.
type contextToolStub struct {
	customTestTool
	runFn func(ctx context.Context, args string) (string, error)
}

func (c *contextToolStub) RunWithContext(ctx context.Context, args string) (string, error) {
	return c.runFn(ctx, args)
}

// TestExecute_ContextCancellation passes a pre-cancelled context to a
// ContextTool and verifies that the returned error is ctx.Err().
func TestExecute_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	tool := &contextToolStub{
		customTestTool: customTestTool{name: "stub"},
		runFn: func(ctx context.Context, args string) (string, error) {
			// Respect context cancellation.
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			default:
				return "ok", nil
			}
		},
	}

	_, err := tool.RunWithContext(ctx, "{}")
	if err == nil {
		t.Fatal("expected an error from a pre-cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

// slowPlainTool is a plain Tool (no ContextTool) that sleeps.
type slowPlainTool struct {
	customTestTool
	sleep time.Duration
}

func (s *slowPlainTool) Run(_ string) (string, error) {
	time.Sleep(s.sleep)
	return "finished", nil
}

// TestRunTool_ContextTimeout verifies that RunTool enforces context deadlines
// on plain (non-ContextTool) tools by returning context.DeadlineExceeded.
func TestRunTool_ContextTimeout(t *testing.T) {
	slow := &slowPlainTool{
		customTestTool: customTestTool{name: "slow"},
		sleep:          5 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := RunTool(ctx, slow, "{}")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error from deadline-exceeded context")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("RunTool took too long: %v (deadline was 100ms)", elapsed)
	}
}

// TestRunTool_ContextTool verifies RunTool calls RunWithContext when available.
func TestRunTool_ContextTool(t *testing.T) {
	var ctxReceived context.Context
	tool := &contextToolStub{
		customTestTool: customTestTool{name: "ctx_tool"},
		runFn: func(ctx context.Context, args string) (string, error) {
			ctxReceived = ctx
			return "ok", nil
		},
	}

	ctx := context.Background()
	result, err := RunTool(ctx, tool, "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
	if ctxReceived != ctx {
		t.Error("RunTool should pass the context to RunWithContext")
	}
}

// TestRunTool_PlainTool verifies RunTool wraps plain (non-ContextTool) tools.
func TestRunTool_PlainTool(t *testing.T) {
	tool := &customTestTool{name: "plain"}
	result, err := RunTool(context.Background(), tool, "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
}

// TestGetTimeout_Default verifies that tools without TimeoutTool get DefaultToolTimeout.
func TestGetTimeout_Default(t *testing.T) {
	tool := &customTestTool{name: "notimeout"}
	got := GetTimeout(tool)
	if got != DefaultToolTimeout {
		t.Errorf("expected DefaultToolTimeout (%v), got %v", DefaultToolTimeout, got)
	}
}

// timeoutTestTool is a tool that implements TimeoutTool.
type timeoutTestTool struct {
	customTestTool
	timeout time.Duration
}

func (t *timeoutTestTool) Timeout() time.Duration { return t.timeout }

// TestGetTimeout_Custom verifies that tools implementing TimeoutTool return their value.
func TestGetTimeout_Custom(t *testing.T) {
	want := 42 * time.Second
	tool := &timeoutTestTool{
		customTestTool: customTestTool{name: "withtimeout"},
		timeout:        want,
	}
	got := GetTimeout(tool)
	if got != want {
		t.Errorf("expected %v, got %v", want, got)
	}
}
