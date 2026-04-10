package models

import (
	"sync"
	"time"
)

// ModelStatus represents the lifecycle state of a model in the runtime.
type ModelStatus string

const (
	// StatusUnloaded means the model is known but not loaded into memory.
	StatusUnloaded ModelStatus = "unloaded"
	// StatusLoading means the model is currently being loaded.
	StatusLoading ModelStatus = "loading"
	// StatusLoaded means the model is loaded and ready to serve requests.
	StatusLoaded ModelStatus = "loaded"
	// StatusError means the model failed to load.
	StatusError ModelStatus = "error"
)

// RuntimeModel pairs a ModelDefinition with live runtime state.
type RuntimeModel struct {
	Definition ModelDefinition `json:"definition"`
	Status     ModelStatus     `json:"status"`
	LoadedAt   time.Time       `json:"loaded_at,omitempty"`
	LastUsed   time.Time       `json:"last_used,omitempty"`
	FilePath   string          `json:"file_path"`
	Downloaded bool            `json:"downloaded"`
}

// Registry is a thread-safe store of RuntimeModel instances, keyed by
// their Definition.ID.
type Registry struct {
	mu     sync.RWMutex
	models map[string]*RuntimeModel
}

// NewRegistry returns an initialised, empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		models: make(map[string]*RuntimeModel),
	}
}

// Add stores rm in the registry under rm.Definition.ID, replacing any
// existing entry with the same ID.
func (r *Registry) Add(rm RuntimeModel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := rm
	r.models[rm.Definition.ID] = &copy
}

// Get returns the RuntimeModel for the given id. Returns (model, true) when
// found, (zero value, false) otherwise.
func (r *Registry) Get(id string) (RuntimeModel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rm, ok := r.models[id]
	if !ok {
		return RuntimeModel{}, false
	}
	return *rm, true
}

// UpdateStatus changes the status of the model identified by id. When the
// new status is StatusLoaded, LoadedAt is set to the current time.
// The call is a no-op when id is not found.
func (r *Registry) UpdateStatus(id string, status ModelStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rm, ok := r.models[id]
	if !ok {
		return
	}
	rm.Status = status
	if status == StatusLoaded {
		rm.LoadedAt = time.Now()
	}
}

// MarkUsed records the current time as the LastUsed timestamp for the model
// identified by id. The call is a no-op when id is not found.
func (r *Registry) MarkUsed(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rm, ok := r.models[id]
	if !ok {
		return
	}
	rm.LastUsed = time.Now()
}

// BestAvailable returns the best loaded, non-embedding model for the
// requested tier following this fallback chain:
//
//   - TierPowerful → TierBalanced → TierFast
//   - TierBalanced → TierFast
//   - TierFast     → (no fallback)
//
// "Best" is defined as the model with the highest BFCLScore within the
// chosen tier. Returns (model, false) when nothing suitable is loaded.
func (r *Registry) BestAvailable(tier ModelTier) (RuntimeModel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tiers := fallbackChain(tier)
	for _, t := range tiers {
		if rm, ok := r.bestForTier(t); ok {
			return rm, true
		}
	}
	return RuntimeModel{}, false
}

// fallbackChain returns the ordered list of tiers to try, starting with the
// requested tier and descending toward TierFast.
func fallbackChain(tier ModelTier) []ModelTier {
	switch tier {
	case TierPowerful:
		return []ModelTier{TierPowerful, TierBalanced, TierFast}
	case TierBalanced:
		return []ModelTier{TierBalanced, TierFast}
	default: // TierFast
		return []ModelTier{TierFast}
	}
}

// bestForTier returns the loaded, non-embedding model with the highest
// BFCLScore in tier. Must be called with r.mu held (read or write).
func (r *Registry) bestForTier(tier ModelTier) (RuntimeModel, bool) {
	var best *RuntimeModel
	for _, rm := range r.models {
		if rm.Status != StatusLoaded {
			continue
		}
		if rm.Definition.IsEmbedding {
			continue
		}
		if rm.Definition.Tier != tier {
			continue
		}
		if best == nil || rm.Definition.BFCLScore > best.Definition.BFCLScore {
			best = rm
		}
	}
	if best == nil {
		return RuntimeModel{}, false
	}
	return *best, true
}

// LoadedModels returns a snapshot of all models currently in StatusLoaded.
func (r *Registry) LoadedModels() []RuntimeModel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []RuntimeModel
	for _, rm := range r.models {
		if rm.Status == StatusLoaded {
			result = append(result, *rm)
		}
	}
	return result
}

// All returns a snapshot of every model in the registry regardless of status.
func (r *Registry) All() []RuntimeModel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]RuntimeModel, 0, len(r.models))
	for _, rm := range r.models {
		result = append(result, *rm)
	}
	return result
}

// ModelNames returns the IDs of all loaded, non-embedding models.
func (r *Registry) ModelNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for _, rm := range r.models {
		if rm.Status == StatusLoaded && !rm.Definition.IsEmbedding {
			names = append(names, rm.Definition.ID)
		}
	}
	return names
}
