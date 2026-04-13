package fallback

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// maxPendingMemories is the maximum number of MemoryEntry values held in the
// pending buffer.  When the buffer is full, the oldest entry is dropped to make
// room for the new one.
const maxPendingMemories = 1000

// MemoryEntry is a single memory record queued for persistence in HelixMemory.
type MemoryEntry struct {
	Content   string `json:"content"`
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
}

// MemoryAdapterConfig holds configuration for MemoryAdapter.
type MemoryAdapterConfig struct {
	// HelixMemoryURL is the base URL of the HelixMemory service
	// (e.g. "http://localhost:9200").
	HelixMemoryURL string

	// SyncInterval controls how often the flush loop posts buffered entries.
	// Defaults to 30 seconds when zero.
	SyncInterval time.Duration

	// Enabled controls whether the adapter is active.  When false, Queue and
	// Start are both no-ops.
	Enabled bool
}

// MemoryAdapter buffers MemoryEntry values in memory and periodically flushes
// them to HelixMemory via a single batch POST request.
//
// On flush failure the entries are retained in the buffer so they are retried
// on the next tick — no data is lost as long as the process stays alive.
// The buffer is capped at maxPendingMemories to bound memory usage; when full,
// the oldest entry is evicted.
type MemoryAdapter struct {
	url          string
	syncInterval time.Duration
	enabled      bool
	httpClient   *http.Client

	mu      sync.Mutex
	pending []MemoryEntry

	wg sync.WaitGroup
}

// NewMemoryAdapter creates a MemoryAdapter from cfg.  Defaults applied:
//   - SyncInterval defaults to 30 seconds when zero.
//   - HTTP client timeout defaults to 10 seconds.
func NewMemoryAdapter(cfg MemoryAdapterConfig) *MemoryAdapter {
	interval := cfg.SyncInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &MemoryAdapter{
		url:          cfg.HelixMemoryURL,
		syncInterval: interval,
		enabled:      cfg.Enabled,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Queue appends entry to the pending buffer.  It is a no-op when the adapter
// is disabled.  When the buffer is already at maxPendingMemories the oldest
// entry is dropped to make room (FIFO eviction).
func (ma *MemoryAdapter) Queue(entry MemoryEntry) {
	if !ma.enabled {
		return
	}

	ma.mu.Lock()
	defer ma.mu.Unlock()

	if len(ma.pending) >= maxPendingMemories {
		// Drop the oldest entry (index 0) to stay within the cap.
		ma.pending = ma.pending[1:]
	}
	ma.pending = append(ma.pending, entry)
}

// PendingCount returns the number of entries currently in the buffer.
// It is safe to call concurrently and is primarily used by tests.
func (ma *MemoryAdapter) PendingCount() int {
	ma.mu.Lock()
	defer ma.mu.Unlock()
	return len(ma.pending)
}

// Start launches a background goroutine that flushes the pending buffer every
// syncInterval.  It is a no-op when the adapter is disabled.  The goroutine
// exits when ctx is cancelled.  Call Wait() to block until it has exited.
func (ma *MemoryAdapter) Start(ctx context.Context) {
	if !ma.enabled {
		return
	}

	ma.wg.Add(1)
	go func() {
		defer ma.wg.Done()

		ticker := time.NewTicker(ma.syncInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ma.flush(ctx)
			}
		}
	}()
}

// Wait blocks until the flush loop goroutine (if any) has exited.
func (ma *MemoryAdapter) Wait() {
	ma.wg.Wait()
}

// flush copies the current pending buffer, POSTs it to HelixMemory, and on a
// successful 2xx response removes the flushed entries from the pending slice.
// On any failure the entries are kept for the next attempt.
func (ma *MemoryAdapter) flush(ctx context.Context) {
	ma.mu.Lock()
	if len(ma.pending) == 0 {
		ma.mu.Unlock()
		return
	}
	// Snapshot the entries to send.
	snapshot := make([]MemoryEntry, len(ma.pending))
	copy(snapshot, ma.pending)
	ma.mu.Unlock()

	body, err := json.Marshal(snapshot)
	if err != nil {
		slog.Warn("memory_adapter: failed to marshal entries", "error", err)
		return
	}

	url := ma.url + "/v1/memory/entities"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		bytes.NewReader(body))
	if err != nil {
		slog.Warn("memory_adapter: failed to build request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ma.httpClient.Do(req)
	if err != nil {
		slog.Warn("memory_adapter: flush request failed, retaining entries",
			"url", url, "error", err, "count", len(snapshot))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("memory_adapter: non-2xx response, retaining entries",
			"url", url, "status", resp.StatusCode, "count", len(snapshot))
		return
	}

	// Success — remove the flushed entries from the pending slice.
	ma.mu.Lock()
	defer ma.mu.Unlock()
	// Only remove the entries we actually sent (the slice may have grown while
	// the HTTP round-trip was in flight).
	if len(ma.pending) >= len(snapshot) {
		ma.pending = ma.pending[len(snapshot):]
	} else {
		ma.pending = ma.pending[:0]
	}

	slog.Debug("memory_adapter: flushed entries", "count", len(snapshot))
}
