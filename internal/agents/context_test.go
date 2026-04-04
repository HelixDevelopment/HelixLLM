package agents

import (
	"fmt"
	"sync"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func makeMsg(role types.Role, content string) types.InternalMessage {
	return types.InternalMessage{Role: role, Content: content}
}

func TestConversationContext_AddAndGet(t *testing.T) {
	ctx := NewConversationContext(10)
	msg := makeMsg(types.RoleUser, "hello")
	ctx.Add("s1", msg)

	got := ctx.Get("s1")
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if got[0].Content != "hello" {
		t.Errorf("unexpected content: %q", got[0].Content)
	}
}

func TestConversationContext_MultipleMessages(t *testing.T) {
	ctx := NewConversationContext(10)
	ctx.Add("s1", makeMsg(types.RoleUser, "first"))
	ctx.Add("s1", makeMsg(types.RoleAssistant, "second"))
	ctx.Add("s1", makeMsg(types.RoleUser, "third"))

	got := ctx.Get("s1")
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	if got[2].Content != "third" {
		t.Errorf("expected last message %q, got %q", "third", got[2].Content)
	}
}

func TestConversationContext_AddMultiple(t *testing.T) {
	ctx := NewConversationContext(10)
	msgs := []types.InternalMessage{
		makeMsg(types.RoleUser, "a"),
		makeMsg(types.RoleAssistant, "b"),
	}
	ctx.AddMultiple("s1", msgs)

	got := ctx.Get("s1")
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
}

func TestConversationContext_MaxHistoryEviction(t *testing.T) {
	maxHistory := 3
	ctx := NewConversationContext(maxHistory)

	for i := 0; i < 5; i++ {
		ctx.Add("s1", makeMsg(types.RoleUser, fmt.Sprintf("msg%d", i)))
	}

	got := ctx.Get("s1")
	if len(got) != maxHistory {
		t.Fatalf("expected %d messages after eviction, got %d", maxHistory, len(got))
	}
	// Should retain the last maxHistory messages.
	if got[0].Content != "msg2" {
		t.Errorf("expected oldest retained message to be %q, got %q", "msg2", got[0].Content)
	}
	if got[2].Content != "msg4" {
		t.Errorf("expected newest message to be %q, got %q", "msg4", got[2].Content)
	}
}

func TestConversationContext_AddMultipleEviction(t *testing.T) {
	ctx := NewConversationContext(2)
	msgs := []types.InternalMessage{
		makeMsg(types.RoleUser, "a"),
		makeMsg(types.RoleUser, "b"),
		makeMsg(types.RoleUser, "c"),
	}
	ctx.AddMultiple("s1", msgs)

	got := ctx.Get("s1")
	if len(got) != 2 {
		t.Fatalf("expected 2 messages after eviction, got %d", len(got))
	}
	if got[0].Content != "b" || got[1].Content != "c" {
		t.Errorf("unexpected retained messages: %v", got)
	}
}

func TestConversationContext_Clear(t *testing.T) {
	ctx := NewConversationContext(10)
	ctx.Add("s1", makeMsg(types.RoleUser, "hello"))
	ctx.Clear("s1")

	got := ctx.Get("s1")
	if got != nil {
		t.Errorf("expected nil after clear, got %v", got)
	}
}

func TestConversationContext_GetNonExistentSession(t *testing.T) {
	ctx := NewConversationContext(10)
	got := ctx.Get("does-not-exist")
	if got != nil {
		t.Errorf("expected nil for unknown session, got %v", got)
	}
}

func TestConversationContext_Sessions(t *testing.T) {
	ctx := NewConversationContext(10)
	ctx.Add("beta", makeMsg(types.RoleUser, "b"))
	ctx.Add("alpha", makeMsg(types.RoleUser, "a"))
	ctx.Add("gamma", makeMsg(types.RoleUser, "g"))

	sessions := ctx.Sessions()
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}
	// Sessions() returns sorted IDs.
	expected := []string{"alpha", "beta", "gamma"}
	for i, s := range sessions {
		if s != expected[i] {
			t.Errorf("session[%d]: expected %q, got %q", i, expected[i], s)
		}
	}
}

func TestConversationContext_GetReturnsCopy(t *testing.T) {
	ctx := NewConversationContext(10)
	ctx.Add("s1", makeMsg(types.RoleUser, "original"))

	got := ctx.Get("s1")
	// Mutate the returned slice.
	got[0].Content = "mutated"

	// Stored value must not be affected.
	stored := ctx.Get("s1")
	if stored[0].Content != "original" {
		t.Errorf("stored message was mutated: %q", stored[0].Content)
	}
}

func TestConversationContext_AddMultiple_Empty(t *testing.T) {
	// AddMultiple with an empty slice must be a no-op (early return).
	ctx := NewConversationContext(10)
	ctx.AddMultiple("s1", []types.InternalMessage{}) // empty — should not modify session
	got := ctx.Get("s1")
	if got != nil {
		t.Errorf("expected nil history after AddMultiple(empty), got %v", got)
	}
}

func TestConversationContext_UnlimitedHistory(t *testing.T) {
	ctx := NewConversationContext(0) // unlimited
	for i := 0; i < 20; i++ {
		ctx.Add("s1", makeMsg(types.RoleUser, fmt.Sprintf("msg%d", i)))
	}
	got := ctx.Get("s1")
	if len(got) != 20 {
		t.Errorf("expected 20 messages with unlimited history, got %d", len(got))
	}
}

func TestConversationContext_ConcurrentAccess(t *testing.T) {
	ctx := NewConversationContext(100)
	const goroutines = 50
	const msgsEach = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			sessionID := fmt.Sprintf("session-%d", id%5) // share 5 sessions
			for j := 0; j < msgsEach; j++ {
				ctx.Add(sessionID, makeMsg(types.RoleUser, fmt.Sprintf("g%d-m%d", id, j)))
				_ = ctx.Get(sessionID)
			}
		}(i)
	}
	wg.Wait()

	// Verify sessions list is non-empty and no panic occurred.
	sessions := ctx.Sessions()
	if len(sessions) == 0 {
		t.Error("expected at least one session after concurrent access")
	}
}
