package agents

// PersistentSyncer receives memories that should persist across sessions via an
// external store (e.g. HelixMemory).  Implementations must be safe for
// concurrent use.
type PersistentSyncer interface {
	// QueueMemory enqueues content for async persistence.
	// content is the memory text, memType is a label (e.g. "learning"),
	// sessionID identifies the originating session.
	QueueMemory(content, memType, sessionID string)
}
