// Package memory provides an agent memory store backed by contextdb in
// embedded mode. It is designed to give replicants episodic, semantic, and
// procedural memory that persists across sessions.
package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/antiartificial/contextdb/pkg/client"
)

// Memory wraps a contextdb DB and exposes a simple agent-memory API.
// A single embedded database holds all namespaces. All methods are safe
// for concurrent use.
type Memory struct {
	db *client.DB
	ns *client.NamespaceHandle
}

// New opens (or creates) a persistent memory store at dataDir.
// Use ~/.replicant/memory/ as the conventional location.
func New(dataDir string) (*Memory, error) {
	db, err := client.Open(client.Options{
		Mode:    client.ModeEmbedded,
		DataDir: dataDir,
	})
	if err != nil {
		return nil, fmt.Errorf("memory: open contextdb: %w", err)
	}

	ns := db.Namespace("replicant:sessions", client.NSModeAgentMemory)
	return &Memory{db: db, ns: ns}, nil
}

// Close shuts down the underlying database. Call this when the process
// is about to exit.
func (m *Memory) Close() error {
	return m.db.Close()
}

// Remember stores a memory from the current session.
//
// category controls the memory type (and therefore its decay rate):
//   - "observation", "task", "error" → MemEpisodic  (hours/days)
//   - "decision", "learning"         → MemSemantic  (weeks)
//   - "skill", "procedure"           → MemProcedural (months)
//
// Unknown categories fall back to MemEpisodic.
func (m *Memory) Remember(ctx context.Context, content, source, category string) error {
	_, err := m.ns.Write(ctx, client.WriteRequest{
		Content:    content,
		SourceID:   source,
		Labels:     []string{"Memory", labelForCategory(category)},
		Confidence: 0.9,
		MemType:    memTypeForCategory(category),
		Properties: map[string]any{
			"category": category,
		},
	})
	if err != nil {
		return fmt.Errorf("memory: remember: %w", err)
	}
	return nil
}

// Recall retrieves memories relevant to a query.
// limit caps the number of results returned (default 5 if ≤ 0).
func (m *Memory) Recall(ctx context.Context, query string, limit int) ([]MemoryResult, error) {
	if limit <= 0 {
		limit = 5
	}
	results, err := m.ns.Retrieve(ctx, client.RetrieveRequest{
		Text: query,
		TopK: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("memory: recall: %w", err)
	}

	out := make([]MemoryResult, 0, len(results))
	for _, r := range results {
		category, _ := r.Node.Properties["category"].(string)
		source := ""
		if r.Node.Properties != nil {
			if s, ok := r.Node.Properties["source_id"].(string); ok {
				source = s
			}
		}
		text, _ := r.Node.Properties["text"].(string)
		out = append(out, MemoryResult{
			Content:   text,
			Source:    source,
			Score:     r.Score,
			Category:  category,
			CreatedAt: r.Node.TxTime,
		})
	}
	return out, nil
}

// RememberSession stores a session summary for cross-session recall.
// sessionID is the session UUID, replicant is the replicant name, and
// summary is a brief plain-text description of what happened.
func (m *Memory) RememberSession(ctx context.Context, sessionID, replicant, summary string) error {
	_, err := m.ns.Write(ctx, client.WriteRequest{
		Content:    summary,
		SourceID:   fmt.Sprintf("replicant:%s", replicant),
		Labels:     []string{"Memory", "Session"},
		Confidence: 0.85,
		MemType:    client.MemSemantic,
		Properties: map[string]any{
			"category":   "session",
			"session_id": sessionID,
			"replicant":  replicant,
		},
	})
	if err != nil {
		return fmt.Errorf("memory: remember session: %w", err)
	}
	return nil
}

// MemoryResult is a single item returned by Recall.
type MemoryResult struct {
	Content   string
	Source    string
	Score     float64
	Category  string
	CreatedAt time.Time
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func memTypeForCategory(category string) client.MemoryType {
	switch strings.ToLower(category) {
	case "decision", "learning":
		return client.MemSemantic
	case "skill", "procedure":
		return client.MemProcedural
	default:
		// "observation", "task", "error", and anything unknown
		return client.MemEpisodic
	}
}

func labelForCategory(category string) string {
	switch strings.ToLower(category) {
	case "observation":
		return "Observation"
	case "decision":
		return "Decision"
	case "task":
		return "Task"
	case "error":
		return "Error"
	case "learning":
		return "Learning"
	case "skill", "procedure":
		return "Skill"
	default:
		return "Episodic"
	}
}
