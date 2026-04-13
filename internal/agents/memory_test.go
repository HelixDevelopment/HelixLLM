package agents

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// ---------------------------------------------------------------------------
// EpisodicMemory tests
// ---------------------------------------------------------------------------

func TestEpisodicMemory_AddAndList(t *testing.T) {
	em := newEpisodicMemory()
	em.add("s1", MemoryEntry{ID: "1", Content: "user prefers dark mode", Importance: 0.5, CreatedAt: time.Now()})
	em.add("s1", MemoryEntry{ID: "2", Content: "project deadline is Friday", Importance: 0.8, CreatedAt: time.Now()})

	entries := em.list("s1")
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestEpisodicMemory_Search_ContentMatch(t *testing.T) {
	em := newEpisodicMemory()
	em.add("s1", MemoryEntry{ID: "1", Content: "user prefers dark mode", Importance: 0.5, CreatedAt: time.Now()})
	em.add("s1", MemoryEntry{ID: "2", Content: "project deadline is Friday", Importance: 0.5, CreatedAt: time.Now()})

	results := em.search("s1", "dark mode", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "1" {
		t.Errorf("expected entry ID 1, got %q", results[0].ID)
	}
}

func TestEpisodicMemory_Search_TopK(t *testing.T) {
	em := newEpisodicMemory()
	for i := 0; i < 5; i++ {
		em.add("s1", MemoryEntry{ID: string(rune('0'+i)), Content: "common keyword here", Importance: 0.5, CreatedAt: time.Now()})
	}

	results := em.search("s1", "keyword", 3)
	if len(results) != 3 {
		t.Fatalf("expected 3 results (topK), got %d", len(results))
	}
}

func TestEpisodicMemory_HighImportancePersistsAcrossSessions(t *testing.T) {
	em := newEpisodicMemory()
	// High-importance entry added to session "s1"
	em.add("s1", MemoryEntry{ID: "hi", Content: "critical decision made", Importance: 0.9, CreatedAt: time.Now()})
	// Low-importance entry that should NOT appear in other sessions
	em.add("s1", MemoryEntry{ID: "lo", Content: "minor note", Importance: 0.3, CreatedAt: time.Now()})

	// Session "s2" has no entries, but should see the high-importance one via global
	results := em.search("s2", "critical decision", 10)
	if len(results) == 0 {
		t.Fatal("expected high-importance memory to persist across sessions")
	}
	found := false
	for _, r := range results {
		if r.ID == "hi" {
			found = true
		}
	}
	if !found {
		t.Error("high-importance entry not found in cross-session recall")
	}
}

func TestEpisodicMemory_LowImportanceDoesNotCrossSession(t *testing.T) {
	em := newEpisodicMemory()
	em.add("s1", MemoryEntry{ID: "lo", Content: "minor casual note", Importance: 0.3, CreatedAt: time.Now()})

	results := em.search("s2", "casual note", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 cross-session results for low-importance, got %d", len(results))
	}
}

func TestEpisodicMemory_Search_CaseInsensitive(t *testing.T) {
	em := newEpisodicMemory()
	em.add("s1", MemoryEntry{ID: "1", Content: "User Prefers Dark Mode", Importance: 0.5, CreatedAt: time.Now()})

	results := em.search("s1", "dark mode", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result (case-insensitive), got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// MemoryManager tests
// ---------------------------------------------------------------------------

func TestMemoryManager_RememberAndRecall(t *testing.T) {
	convCtx := NewConversationContext(50)
	mm := NewMemoryManager(convCtx, nil) // nil pipeline → no semantic tier

	ctx := context.Background()
	mm.Remember(ctx, "sess1", "deployment strategy decided: blue-green", 0.8)
	mm.Remember(ctx, "sess1", "use postgres for persistence", 0.6)

	results := mm.Recall(ctx, "sess1", "deployment", 5)
	if len(results) == 0 {
		t.Fatal("expected at least one recalled memory")
	}
	if results[0].Content != "deployment strategy decided: blue-green" {
		t.Errorf("unexpected recall content: %q", results[0].Content)
	}
}

func TestMemoryManager_Recall_ImportanceFilter(t *testing.T) {
	convCtx := NewConversationContext(50)
	mm := NewMemoryManager(convCtx, nil)
	ctx := context.Background()

	mm.Remember(ctx, "sess1", "critical architecture decision made", 0.9)
	mm.Remember(ctx, "sess2", "casual standup note from tuesday", 0.2)

	// sess2 query — should find its own low-importance entry directly...
	direct := mm.Recall(ctx, "sess2", "standup note", 5)
	if len(direct) == 0 {
		t.Fatal("expected to find session-local low importance entry")
	}

	// ...but should NOT find "casual standup note" from sess2 when querying sess3
	crossSession := mm.Recall(ctx, "sess3", "standup note", 5)
	if len(crossSession) != 0 {
		t.Errorf("low-importance memory should not cross to sess3, got %d results", len(crossSession))
	}
}

func TestMemoryManager_CrossSessionPersistence(t *testing.T) {
	convCtx := NewConversationContext(50)
	mm := NewMemoryManager(convCtx, nil)
	ctx := context.Background()

	// Store a high-importance memory in sess1
	mm.Remember(ctx, "sess1", "the system must use TLS everywhere", 0.9)

	// Recall from a completely different session
	results := mm.Recall(ctx, "sess-new", "TLS everywhere", 5)
	if len(results) == 0 {
		t.Fatal("high-importance memory should persist across sessions")
	}
}

func TestMemoryManager_GetContext_CombinesTiers(t *testing.T) {
	convCtx := NewConversationContext(50)
	mm := NewMemoryManager(convCtx, nil)
	ctx := context.Background()

	// Working tier: add a conversation message
	convCtx.Add("sess1", types.InternalMessage{Role: types.RoleUser, Content: "hello"})

	// Episodic tier: store a memory
	mm.Remember(ctx, "sess1", "user is an expert Go developer", 0.6)

	msgs := mm.GetContext("sess1")

	// We expect at least the episodic system summary + the user message
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages from GetContext, got %d", len(msgs))
	}

	// First message should be the episodic system summary
	if msgs[0].Role != types.RoleSystem {
		t.Errorf("expected first message to be system role, got %q", msgs[0].Role)
	}

	// Last message should be the working memory (user message)
	last := msgs[len(msgs)-1]
	if last.Role != types.RoleUser || last.Content != "hello" {
		t.Errorf("expected last message to be user 'hello', got role=%q content=%q", last.Role, last.Content)
	}
}

func TestMemoryManager_GetContext_NoMemories(t *testing.T) {
	convCtx := NewConversationContext(50)
	mm := NewMemoryManager(convCtx, nil)

	msgs := mm.GetContext("empty-session")
	// No messages and no episodic entries — should return empty or nil, not panic.
	if msgs == nil {
		msgs = []types.InternalMessage{}
	}
	if len(msgs) != 0 {
		t.Errorf("expected empty context for session with no history, got %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// PersistentSyncer wiring tests
// ---------------------------------------------------------------------------

// mockSyncer records every QueueMemory call for assertion in tests.
type mockSyncer struct {
	mu      sync.Mutex
	entries []struct{ content, memType, sessionID string }
}

func (ms *mockSyncer) QueueMemory(content, memType, sessionID string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.entries = append(ms.entries, struct{ content, memType, sessionID string }{content, memType, sessionID})
}

func (ms *mockSyncer) len() int {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return len(ms.entries)
}

func TestMemoryManager_Remember_HighImportance_CallsSyncer(t *testing.T) {
	convCtx := NewConversationContext(50)
	mm := NewMemoryManager(convCtx, nil)
	syncer := &mockSyncer{}
	mm.SetPersistentSyncer(syncer)

	ctx := context.Background()
	mm.Remember(ctx, "sess1", "critical architecture decision", 0.9)

	if syncer.len() != 1 {
		t.Fatalf("expected 1 syncer call for high-importance memory, got %d", syncer.len())
	}
	syncer.mu.Lock()
	e := syncer.entries[0]
	syncer.mu.Unlock()
	if e.content != "critical architecture decision" {
		t.Errorf("unexpected syncer content: %q", e.content)
	}
	if e.memType != "learning" {
		t.Errorf("unexpected syncer memType: %q", e.memType)
	}
	if e.sessionID != "sess1" {
		t.Errorf("unexpected syncer sessionID: %q", e.sessionID)
	}
}

func TestMemoryManager_Remember_LowImportance_DoesNotCallSyncer(t *testing.T) {
	convCtx := NewConversationContext(50)
	mm := NewMemoryManager(convCtx, nil)
	syncer := &mockSyncer{}
	mm.SetPersistentSyncer(syncer)

	ctx := context.Background()
	mm.Remember(ctx, "sess1", "minor casual note", 0.5)

	if syncer.len() != 0 {
		t.Errorf("expected 0 syncer calls for low-importance memory, got %d", syncer.len())
	}
}

func TestMemoryManager_Remember_ExactThreshold_CallsSyncer(t *testing.T) {
	convCtx := NewConversationContext(50)
	mm := NewMemoryManager(convCtx, nil)
	syncer := &mockSyncer{}
	mm.SetPersistentSyncer(syncer)

	ctx := context.Background()
	// Exactly at threshold (0.7) should trigger the syncer.
	mm.Remember(ctx, "sess1", "threshold memory", 0.7)

	if syncer.len() != 1 {
		t.Fatalf("expected 1 syncer call at exact threshold 0.7, got %d", syncer.len())
	}
}

func TestMemoryManager_Remember_NoSyncer_DoesNotPanic(t *testing.T) {
	convCtx := NewConversationContext(50)
	mm := NewMemoryManager(convCtx, nil) // no syncer set

	ctx := context.Background()
	// Must not panic even with high importance when no syncer is wired.
	mm.Remember(ctx, "sess1", "important but no syncer", 0.9)
}

func TestMemoryManager_SetPersistentSyncer_NilDisablesSyncer(t *testing.T) {
	convCtx := NewConversationContext(50)
	mm := NewMemoryManager(convCtx, nil)
	syncer := &mockSyncer{}
	mm.SetPersistentSyncer(syncer)

	// Disable syncer by passing nil.
	mm.SetPersistentSyncer(nil)

	ctx := context.Background()
	mm.Remember(ctx, "sess1", "important after disable", 0.9)

	if syncer.len() != 0 {
		t.Errorf("expected 0 syncer calls after nil disable, got %d", syncer.len())
	}
}

func TestMemoryManager_Recall_Deduplication(t *testing.T) {
	convCtx := NewConversationContext(50)
	mm := NewMemoryManager(convCtx, nil)
	ctx := context.Background()

	content := "unique piece of knowledge"
	mm.Remember(ctx, "sess1", content, 0.8)
	// Adding a second time should not cause duplicate results.
	mm.Remember(ctx, "sess1", content, 0.8)

	results := mm.Recall(ctx, "sess1", "unique piece", 10)
	count := 0
	for _, r := range results {
		if r.Content == content {
			count++
		}
	}
	if count > 1 {
		t.Errorf("expected at most 1 deduplicated result, got %d", count)
	}
}
