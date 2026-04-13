package agents

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
	"github.com/google/uuid"
)

// MemoryEntry is a single episodic memory item with importance scoring.
type MemoryEntry struct {
	ID         string    `json:"id"`
	Content    string    `json:"content"`
	Importance float64   `json:"importance"` // 0.0 – 1.0
	CreatedAt  time.Time `json:"created_at"`
	Tags       []string  `json:"tags,omitempty"`
}

// EpisodicMemory stores per-session facts/decisions with importance scores.
// Entries with Importance > highImportanceThreshold are considered persistent
// across sessions and are also stored in the "__global" bucket.
type EpisodicMemory struct {
	mu       sync.RWMutex
	memories map[string][]MemoryEntry // sessionID → entries
}

const highImportanceThreshold = 0.7
const globalSession = "__global"

// newEpisodicMemory creates an empty EpisodicMemory.
func newEpisodicMemory() *EpisodicMemory {
	return &EpisodicMemory{memories: make(map[string][]MemoryEntry)}
}

// add stores a MemoryEntry under sessionID. When the entry's Importance
// exceeds highImportanceThreshold it is also mirrored into the global bucket
// so that it survives across sessions.
func (e *EpisodicMemory) add(sessionID string, entry MemoryEntry) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.memories[sessionID] = append(e.memories[sessionID], entry)
	if entry.Importance > highImportanceThreshold {
		e.memories[globalSession] = append(e.memories[globalSession], entry)
	}
}

// search returns all entries from sessionID (plus global persistent entries)
// whose content contains any token from query (case-insensitive). The caller
// may limit the result with topK (0 = no limit).
func (e *EpisodicMemory) search(sessionID, query string, topK int) []MemoryEntry {
	e.mu.RLock()
	defer e.mu.RUnlock()

	tokens := strings.Fields(strings.ToLower(query))

	seen := make(map[string]bool)
	var out []MemoryEntry

	for _, bucket := range []string{sessionID, globalSession} {
		for _, entry := range e.memories[bucket] {
			if seen[entry.ID] {
				continue
			}
			lc := strings.ToLower(entry.Content)
			for _, tok := range tokens {
				if strings.Contains(lc, tok) {
					seen[entry.ID] = true
					out = append(out, entry)
					break
				}
			}
		}
	}

	if topK > 0 && len(out) > topK {
		out = out[:topK]
	}
	return out
}

// list returns all entries stored under sessionID.
func (e *EpisodicMemory) list(sessionID string) []MemoryEntry {
	e.mu.RLock()
	defer e.mu.RUnlock()

	src := e.memories[sessionID]
	out := make([]MemoryEntry, len(src))
	copy(out, src)
	return out
}

// SemanticMemory provides long-term storage backed by the RAG pipeline.
// Memories are ingested into the "memories" collection and retrieved via
// vector similarity search.
type SemanticMemory struct {
	pipeline   *knowledge.Pipeline
	collection string
}

const memoriesCollection = "memories"

// newSemanticMemory creates a SemanticMemory using the given pipeline.
func newSemanticMemory(pipeline *knowledge.Pipeline) *SemanticMemory {
	return &SemanticMemory{pipeline: pipeline, collection: memoriesCollection}
}

// store ingests a memory into the pipeline so it is available for vector search.
func (s *SemanticMemory) store(ctx context.Context, content string) error {
	if s.pipeline == nil {
		return nil
	}
	_, err := s.pipeline.Ingest(ctx, knowledge.IngestRequest{
		Title:      "memory",
		Content:    content,
		Collection: s.collection,
	})
	return err
}

// recall queries the pipeline for the topK most relevant memories.
func (s *SemanticMemory) recall(ctx context.Context, query string, topK int) ([]MemoryEntry, error) {
	if s.pipeline == nil {
		return nil, nil
	}
	res, err := s.pipeline.Query(ctx, knowledge.QueryRequest{
		Query:      query,
		Collection: s.collection,
		TopK:       topK,
	})
	if err != nil {
		return nil, err
	}

	entries := make([]MemoryEntry, 0, len(res.Chunks))
	for _, ch := range res.Chunks {
		entries = append(entries, MemoryEntry{
			ID:        uuid.NewString(),
			Content:   ch.Content,
			Importance: 0.5,
			CreatedAt: time.Now(),
		})
	}
	return entries, nil
}

// MemoryManager combines the three memory tiers.
//
//   - working   – current session conversation (ConversationContext)
//   - episodic  – important facts/decisions keyed by sessionID
//   - semantic  – long-term vector store via RAG pipeline
//
// An optional PersistentSyncer may be attached via SetPersistentSyncer; when
// set, high-importance memories (importance >= highImportanceThreshold) are
// also forwarded to it for cross-process persistence (e.g. HelixMemory).
type MemoryManager struct {
	working  *ConversationContext
	episodic *EpisodicMemory
	semantic *SemanticMemory

	syncerMu sync.RWMutex
	syncer   PersistentSyncer
}

// NewMemoryManager creates a MemoryManager.
// pipeline may be nil, in which case semantic memory is disabled.
func NewMemoryManager(working *ConversationContext, pipeline *knowledge.Pipeline) *MemoryManager {
	return &MemoryManager{
		working:  working,
		episodic: newEpisodicMemory(),
		semantic: newSemanticMemory(pipeline),
	}
}

// SetPersistentSyncer attaches a PersistentSyncer that receives high-importance
// memories from Remember().  Passing nil disables persistent sync.
// Safe to call concurrently.
func (m *MemoryManager) SetPersistentSyncer(syncer PersistentSyncer) {
	m.syncerMu.Lock()
	defer m.syncerMu.Unlock()
	m.syncer = syncer
}

// Remember stores content as a memory for sessionID. When importance > 0.7 the
// entry also persists to the global episodic bucket. The content is always
// ingested into semantic (vector) memory as well. If a PersistentSyncer has
// been set and importance >= highImportanceThreshold the memory is additionally
// forwarded to the syncer for cross-process persistence (e.g. HelixMemory).
func (m *MemoryManager) Remember(ctx context.Context, sessionID, content string, importance float64) {
	entry := MemoryEntry{
		ID:         uuid.NewString(),
		Content:    content,
		Importance: importance,
		CreatedAt:  time.Now(),
	}
	m.episodic.add(sessionID, entry)

	// Best-effort semantic store; ignore errors since we still have episodic.
	_ = m.semantic.store(ctx, content)

	// Forward high-importance memories to the persistent syncer (if wired).
	if importance >= highImportanceThreshold {
		m.syncerMu.RLock()
		s := m.syncer
		m.syncerMu.RUnlock()
		if s != nil {
			s.QueueMemory(content, "learning", sessionID)
		}
	}
}

// Recall retrieves up to topK memories related to query for sessionID.
// Results are merged from both episodic and semantic tiers, deduplicated by
// content, and capped at topK.
func (m *MemoryManager) Recall(ctx context.Context, sessionID, query string, topK int) []MemoryEntry {
	episodic := m.episodic.search(sessionID, query, topK)

	semantic, _ := m.semantic.recall(ctx, query, topK)

	// Deduplicate by content.
	seen := make(map[string]bool, len(episodic))
	merged := make([]MemoryEntry, 0, len(episodic)+len(semantic))
	for _, e := range episodic {
		if !seen[e.Content] {
			seen[e.Content] = true
			merged = append(merged, e)
		}
	}
	for _, e := range semantic {
		if !seen[e.Content] {
			seen[e.Content] = true
			merged = append(merged, e)
		}
	}

	if topK > 0 && len(merged) > topK {
		merged = merged[:topK]
	}
	return merged
}

// GetContext assembles all messages for sessionID from all three tiers into a
// single InternalMessage slice suitable for passing to Agent.Run.
//
// Order: episodic summaries → working history (actual conversation messages).
func (m *MemoryManager) GetContext(sessionID string) []types.InternalMessage {
	var msgs []types.InternalMessage

	// Attach episodic entries as a system summary.
	entries := m.episodic.list(sessionID)
	if len(entries) > 0 {
		var sb strings.Builder
		sb.WriteString("Relevant memories:\n")
		for _, e := range entries {
			sb.WriteString("- ")
			sb.WriteString(e.Content)
			sb.WriteString("\n")
		}
		msgs = append(msgs, types.InternalMessage{
			Role:    types.RoleSystem,
			Content: sb.String(),
		})
	}

	// Append working (conversation) history.
	if m.working != nil {
		msgs = append(msgs, m.working.Get(sessionID)...)
	}

	return msgs
}
