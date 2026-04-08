package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SessionEntry is one line in the JSONL session log.
type SessionEntry struct {
	Type      string    `json:"type"`      // "session_start", "message", "tool_call", "tool_result"
	Timestamp time.Time `json:"timestamp"`

	// session_start fields
	ID        string `json:"id,omitempty"`
	Replicant string `json:"replicant,omitempty"`
	Model     string `json:"model,omitempty"`

	// message fields
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`

	// tool_call / tool_result fields
	ToolName string `json:"tool_name,omitempty"`
	ToolArgs string `json:"tool_args,omitempty"`
	ToolID   string `json:"tool_id,omitempty"`
	Result   string `json:"result,omitempty"`
	IsError  bool   `json:"is_error,omitempty"`
}

// Session is an append-only JSONL log of one agent conversation.
type Session struct {
	ID         string
	Replicant  string
	Model      string
	StartedAt  time.Time
	TotalUsage Usage

	mu   sync.Mutex
	file *os.File
}

// NewSession creates a new session log file under dir.
// The file is named <timestamp>-<replicantName>.jsonl and a session_start entry
// is written immediately.
func NewSession(dir, replicantName, model string) (*Session, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("session: mkdir %s: %w", dir, err)
	}

	now := time.Now().UTC()
	id := fmt.Sprintf("%s-%s", now.Format("20060102-150405"), sanitiseName(replicantName))
	filename := id + ".jsonl"
	path := filepath.Join(dir, filename)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("session: open %s: %w", path, err)
	}

	s := &Session{
		ID:        id,
		Replicant: replicantName,
		Model:     model,
		StartedAt: now,
		file:      f,
	}

	if err := s.Append(SessionEntry{
		Type:      "session_start",
		Timestamp: now,
		ID:        id,
		Replicant: replicantName,
		Model:     model,
	}); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("session: write start entry: %w", err)
	}

	return s, nil
}

// Append serialises entry as JSON and appends it as one line to the session file.
// It is safe to call from multiple goroutines.
func (s *Session) Append(entry SessionEntry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("session: marshal entry: %w", err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.file.Write(data); err != nil {
		return fmt.Errorf("session: write: %w", err)
	}
	return nil
}

// Close flushes and closes the underlying file.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}

// LoadSession reads a JSONL session file and returns all entries.
func LoadSession(path string) ([]SessionEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("session: open %s: %w", path, err)
	}
	defer f.Close()

	var entries []SessionEntry
	scanner := bufio.NewScanner(f)
	// Increase buffer for large tool results.
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry SessionEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			// Skip malformed lines rather than failing the whole load.
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("session: scan %s: %w", path, err)
	}
	return entries, nil
}

// ReconstructHistory rebuilds agent message history from session entries.
// It pairs tool_call and tool_result entries back into proper Message structures.
func ReconstructHistory(entries []SessionEntry) []Message {
	var messages []Message

	// We accumulate assistant text + tool_calls into one assistant message
	// until we see a tool_result (or another user message), then flush.
	var (
		inAssistant    bool
		assistantText  string
		assistantTools []ContentBlock // tool_use blocks
	)

	flushAssistant := func() {
		if !inAssistant {
			return
		}
		var blocks []ContentBlock
		if assistantText != "" {
			blocks = append(blocks, ContentBlock{Type: "text", Text: assistantText})
		}
		blocks = append(blocks, assistantTools...)
		if len(blocks) > 0 {
			messages = append(messages, Message{Role: "assistant", Content: blocks})
		}
		inAssistant = false
		assistantText = ""
		assistantTools = nil
	}

	// tool_result blocks accumulate into one user message.
	var pendingResults []ContentBlock

	flushResults := func() {
		if len(pendingResults) == 0 {
			return
		}
		messages = append(messages, Message{Role: "user", Content: pendingResults})
		pendingResults = nil
	}

	for _, e := range entries {
		switch e.Type {
		case "session_start":
			// Skip — metadata only.

		case "message":
			switch e.Role {
			case "user":
				// Before a new user message, flush any pending assistant content
				// and tool results.
				flushResults()
				flushAssistant()
				messages = append(messages, Message{
					Role:    "user",
					Content: []ContentBlock{{Type: "text", Text: e.Content}},
				})

			case "assistant":
				// Consecutive assistant text chunks get merged into one message.
				if !inAssistant {
					flushResults()
					inAssistant = true
					assistantText = ""
					assistantTools = nil
				}
				assistantText += e.Content
			}

		case "tool_call":
			// Tool calls belong to the current assistant message.
			if !inAssistant {
				inAssistant = true
				assistantText = ""
				assistantTools = nil
			}
			input := json.RawMessage(e.ToolArgs)
			if len(input) == 0 {
				input = json.RawMessage("{}")
			}
			assistantTools = append(assistantTools, ContentBlock{
				Type:  "tool_use",
				ID:    e.ToolID,
				Name:  e.ToolName,
				Input: input,
			})

		case "tool_result":
			// Flush the assistant message before appending results.
			flushAssistant()
			pendingResults = append(pendingResults, ContentBlock{
				Type:      "tool_result",
				ToolUseID: e.ToolID,
				Content:   e.Result,
				IsError:   e.IsError,
			})
		}
	}

	// Flush any remaining assistant content / results.
	flushResults()
	flushAssistant()

	return messages
}

// SessionInfo holds metadata about a discovered session file.
type SessionInfo struct {
	ID        string
	Path      string
	Replicant string
	Model     string
	StartedAt time.Time
}

// ListSessions returns available session files from dir, sorted most-recent first.
// It reads only the first line of each file to extract session_start metadata.
// At most limit entries are returned (0 = unlimited).
func ListSessions(dir string, limit int) ([]SessionInfo, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("session: glob %s: %w", dir, err)
	}

	var infos []SessionInfo
	for _, path := range matches {
		info, err := readSessionInfo(path)
		if err != nil {
			// Skip files we can't read.
			continue
		}
		infos = append(infos, info)
	}

	// Sort most-recent first.
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].StartedAt.After(infos[j].StartedAt)
	})

	if limit > 0 && len(infos) > limit {
		infos = infos[:limit]
	}
	return infos, nil
}

// readSessionInfo reads the first line of a JSONL file and returns metadata.
func readSessionInfo(path string) (SessionInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return SessionInfo{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return SessionInfo{}, fmt.Errorf("session: empty file %s", path)
	}
	var entry SessionEntry
	if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
		return SessionInfo{}, fmt.Errorf("session: parse first line of %s: %w", path, err)
	}
	id := entry.ID
	if id == "" {
		// Fall back to filename stem.
		base := filepath.Base(path)
		id = strings.TrimSuffix(base, ".jsonl")
	}
	return SessionInfo{
		ID:        id,
		Path:      path,
		Replicant: entry.Replicant,
		Model:     entry.Model,
		StartedAt: entry.Timestamp,
	}, nil
}

// FindSession resolves a session ID prefix or full path to a session file path.
// It searches dir for a matching *.jsonl file when query is not an existing path.
func FindSession(dir, query string) (string, error) {
	// Direct path check.
	if _, err := os.Stat(query); err == nil {
		return query, nil
	}

	// Try as a file inside dir.
	candidate := filepath.Join(dir, query)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	candidate += ".jsonl"
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	// Prefix match on session IDs.
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return "", fmt.Errorf("session: glob %s: %w", dir, err)
	}
	var found []string
	for _, p := range matches {
		base := strings.TrimSuffix(filepath.Base(p), ".jsonl")
		if strings.HasPrefix(base, query) {
			found = append(found, p)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("session: no session matching %q in %s", query, dir)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("session: ambiguous session ID %q matches %d files", query, len(found))
	}
}

// ResumeSession opens an existing session file in append mode and populates a
// Session from its first line (session_start entry).
func ResumeSession(path string) (*Session, error) {
	info, err := readSessionInfo(path)
	if err != nil {
		return nil, fmt.Errorf("session: read info from %s: %w", path, err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("session: open %s for append: %w", path, err)
	}

	return &Session{
		ID:        info.ID,
		Replicant: info.Replicant,
		Model:     info.Model,
		StartedAt: info.StartedAt,
		file:      f,
	}, nil
}

// sanitiseName replaces characters that are unsafe in file names.
func sanitiseName(name string) string {
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			out = append(out, c)
		} else {
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "session"
	}
	return string(out)
}
