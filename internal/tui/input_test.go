package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInputModel_History(t *testing.T) {
	m := NewInputModel(80)

	// Submit three messages to build history.
	for _, text := range []string{"first", "second", "third"} {
		m.textarea.SetValue(text)
		var cmd tea.Cmd
		m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatalf("expected SubmitMsg cmd for %q", text)
		}
		// Simulate what Enable() does after the agent responds.
		m.Enable()
	}

	if len(m.history) != 3 {
		t.Fatalf("history length = %d, want 3", len(m.history))
	}

	// Press Up: should show "third" (most recent).
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.textarea.Value(); got != "third" {
		t.Errorf("after 1x Up: value = %q, want %q", got, "third")
	}

	// Press Up again: "second".
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.textarea.Value(); got != "second" {
		t.Errorf("after 2x Up: value = %q, want %q", got, "second")
	}

	// Press Up again: "first".
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.textarea.Value(); got != "first" {
		t.Errorf("after 3x Up: value = %q, want %q", got, "first")
	}

	// Press Up at the top: stays at "first".
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.textarea.Value(); got != "first" {
		t.Errorf("Up past top: value = %q, want %q", got, "first")
	}

	// Press Down: "second".
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.textarea.Value(); got != "second" {
		t.Errorf("Down from first: value = %q, want %q", got, "second")
	}

	// Press Down twice more to get back to empty draft.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.textarea.Value(); got != "" {
		t.Errorf("Down past end: value = %q, want empty", got)
	}
}

func TestInputModel_HistoryPreservesDraft(t *testing.T) {
	m := NewInputModel(80)

	// Submit one message.
	m.textarea.SetValue("old message")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Enable()

	// Type a partial draft.
	m.textarea.SetValue("partial dra")

	// Press Up to browse history.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.textarea.Value(); got != "old message" {
		t.Errorf("Up: value = %q, want %q", got, "old message")
	}

	// Press Down to return to draft.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.textarea.Value(); got != "partial dra" {
		t.Errorf("Down to draft: value = %q, want %q", got, "partial dra")
	}
}
