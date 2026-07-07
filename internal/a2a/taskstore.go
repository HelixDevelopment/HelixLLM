package a2a

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// TaskStore is a minimal in-memory task registry. A2A tasks are first-class
// and async per the spec, but this proof's downstream call is synchronous
// (the coder responds in-request), so tasks are created already resolved to
// a terminal state; tasks/get still round-trips them by id (spec §1.3 / the
// design §4 bullet 4).
type TaskStore struct {
	mu    sync.RWMutex
	tasks map[string]Task
}

// NewTaskStore constructs an empty store.
func NewTaskStore() *TaskStore {
	return &TaskStore{tasks: make(map[string]Task)}
}

// New allocates a fresh Task id (a real UUID, not a guessed/incrementing
// counter) and stores it.
func (s *TaskStore) New(history []Message) Task {
	t := Task{
		ID:      uuid.NewString(),
		Status:  TaskStatus{State: TaskStateSubmitted, Timestamp: now()},
		History: history,
	}
	s.mu.Lock()
	s.tasks[t.ID] = t
	s.mu.Unlock()
	return t
}

// Update stores the given task by its own id, overwriting the previous
// state (used to record the submitted -> working -> completed/failed
// lifecycle transitions).
func (s *TaskStore) Update(t Task) {
	s.mu.Lock()
	s.tasks[t.ID] = t
	s.mu.Unlock()
}

// Get returns the task by id and whether it was found.
func (s *TaskStore) Get(id string) (Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	return t, ok
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
