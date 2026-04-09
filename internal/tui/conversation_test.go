package tui

import (
	"testing"
)

// TestConversationModel_CopySafety verifies that ConversationModel can be
// copied by value after streaming has started without panicking. Bubbletea
// copies the model on every Update call, so a strings.Builder (which panics
// on copy after write) must be a pointer.
func TestConversationModel_CopySafety(t *testing.T) {
	m := NewConversationModel(80, 24, "deckard")
	m.StartAssistantMessage()
	m.AppendChunk("hello ")

	// Simulate bubbletea's copy-on-update: copy the model by value.
	m2 := m

	// This would panic with "strings: illegal use of non-zero Builder
	// copied by value" if streamBuf were a value type.
	m2.AppendChunk("world")

	// The original should still work too.
	m.AppendChunk("!")

	// Verify both share the same buffer (pointer semantics).
	got := m.streamBuf.String()
	if got != "hello world!" {
		t.Errorf("streamBuf = %q, want %q", got, "hello world!")
	}
}

// TestConversationModel_LastAssistantText verifies that the raw text from
// the most recent assistant message is retrievable for clipboard copy.
func TestConversationModel_LastAssistantText(t *testing.T) {
	m := NewConversationModel(80, 24, "test")

	// No assistant message yet.
	if got := m.LastAssistantText(); got != "" {
		t.Errorf("LastAssistantText() = %q, want empty", got)
	}

	m.AddUserMessage("hello")
	m.StartAssistantMessage()
	m.AppendChunk("response text")
	m.FinalizeAssistant()

	if got := m.LastAssistantText(); got != "response text" {
		t.Errorf("LastAssistantText() = %q, want %q", got, "response text")
	}
}

// TestConversationModel_ReplicantName verifies the assistant label uses the
// configured replicant name.
func TestConversationModel_ReplicantName(t *testing.T) {
	m := NewConversationModel(80, 24, "zhora")
	m.StartAssistantMessage()
	m.AppendChunk("debugging...")
	m.FinalizeAssistant()

	// The rendered block should contain the replicant name.
	found := false
	for _, b := range m.blocks {
		if b.kind == kindAssistant {
			if containsSubstring(b.rendered, "zhora") {
				found = true
			}
		}
	}
	if !found {
		t.Error("assistant block does not contain replicant name 'zhora'")
	}
}

// TestConversationModel_EmptyName defaults to "assistant".
func TestConversationModel_EmptyName(t *testing.T) {
	m := NewConversationModel(80, 24, "")
	m.StartAssistantMessage()
	m.AppendChunk("hi")
	m.FinalizeAssistant()

	found := false
	for _, b := range m.blocks {
		if b.kind == kindAssistant {
			if containsSubstring(b.rendered, "assistant") {
				found = true
			}
		}
	}
	if !found {
		t.Error("assistant block does not contain default name 'assistant'")
	}
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
