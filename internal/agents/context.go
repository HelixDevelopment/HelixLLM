package agents

import (
	"sort"
	"sync"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// ConversationContext is an in-memory, thread-safe store for multi-turn
// conversation history. Each session is identified by a string ID and holds
// an ordered slice of messages. When the history for a session exceeds
// maxHistory, the oldest messages are evicted (FIFO).
type ConversationContext struct {
	mu         sync.RWMutex
	sessions   map[string][]types.InternalMessage
	maxHistory int
}

// NewConversationContext creates a ConversationContext that retains at most
// maxHistory messages per session. A maxHistory of zero or less means
// unlimited.
func NewConversationContext(maxHistory int) *ConversationContext {
	return &ConversationContext{
		sessions:   make(map[string][]types.InternalMessage),
		maxHistory: maxHistory,
	}
}

// Add appends a single message to the session's history. If the session does
// not exist it is created. Oldest messages are dropped when maxHistory is
// exceeded.
func (c *ConversationContext) Add(sessionID string, msg types.InternalMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.sessions[sessionID] = append(c.sessions[sessionID], msg)
	c.trim(sessionID)
}

// AddMultiple appends multiple messages to the session's history in order.
// Oldest messages are dropped when maxHistory is exceeded.
func (c *ConversationContext) AddMultiple(sessionID string, msgs []types.InternalMessage) {
	if len(msgs) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.sessions[sessionID] = append(c.sessions[sessionID], msgs...)
	c.trim(sessionID)
}

// Get returns a copy of the message history for the given session. Mutations
// to the returned slice do not affect the stored history. Returns nil if the
// session does not exist.
func (c *ConversationContext) Get(sessionID string) []types.InternalMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()

	msgs, ok := c.sessions[sessionID]
	if !ok {
		return nil
	}
	// Return a copy to prevent callers from mutating stored state.
	out := make([]types.InternalMessage, len(msgs))
	copy(out, msgs)
	return out
}

// Clear removes all messages for the given session.
func (c *ConversationContext) Clear(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.sessions, sessionID)
}

// Sessions returns a sorted list of all active session IDs.
func (c *ConversationContext) Sessions() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := make([]string, 0, len(c.sessions))
	for id := range c.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// trim drops the oldest messages so the session length does not exceed
// maxHistory. Must be called with c.mu held for writing.
func (c *ConversationContext) trim(sessionID string) {
	if c.maxHistory <= 0 {
		return
	}
	msgs := c.sessions[sessionID]
	if len(msgs) > c.maxHistory {
		c.sessions[sessionID] = msgs[len(msgs)-c.maxHistory:]
	}
}
