package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
