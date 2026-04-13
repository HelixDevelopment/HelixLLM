package fallback_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
)

// ---------------------------------------------------------------------------
// Test 1: BuffersAndFlushes — queue 2 entries, verify mock server receives them
// ---------------------------------------------------------------------------

func TestMemoryAdapter_BuffersAndFlushes(t *testing.T) {
	var received []fallback.MemoryEntry
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/memory/entities" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}
		var entries []fallback.MemoryEntry
		if err := json.Unmarshal(body, &entries); err != nil {
			http.Error(w, "decode error", http.StatusBadRequest)
			return
		}
		received = append(received, entries...)
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ma := fallback.NewMemoryAdapter(fallback.MemoryAdapterConfig{
		HelixMemoryURL: srv.URL,
		SyncInterval:   50 * time.Millisecond,
		Enabled:        true,
	})

	ma.Queue(fallback.MemoryEntry{Content: "user said hello", Type: "conversation", SessionID: "s1"})
	ma.Queue(fallback.MemoryEntry{Content: "user asked about Go", Type: "fact", SessionID: "s1"})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ma.Start(ctx)
	time.Sleep(200 * time.Millisecond) // let at least one flush tick
	cancel()
	ma.Wait()

	if len(received) != 2 {
		t.Errorf("expected 2 entries flushed to server, got %d", len(received))
	}
	if callCount.Load() == 0 {
		t.Error("expected at least one POST to /v1/memory/entities")
	}
}

// ---------------------------------------------------------------------------
// Test 2: CapsAt1000Entries — queue 1100, pending never exceeds 1000
// ---------------------------------------------------------------------------

func TestMemoryAdapter_CapsAt1000Entries(t *testing.T) {
	// Use a server that never succeeds so entries stay in the pending buffer
	// and we can measure the cap behaviour.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ma := fallback.NewMemoryAdapter(fallback.MemoryAdapterConfig{
		HelixMemoryURL: srv.URL,
		SyncInterval:   10 * time.Second, // prevent flush during queuing
		Enabled:        true,
	})

	for i := range 1100 {
		ma.Queue(fallback.MemoryEntry{
			Content:   "entry",
			Type:      "fact",
			SessionID: fmt.Sprintf("s%d", i),
		})
	}

	pending := ma.PendingCount()
	if pending > 1000 {
		t.Errorf("pending count must not exceed 1000, got %d", pending)
	}
	if pending == 0 {
		t.Error("expected pending entries, got 0")
	}
}

// ---------------------------------------------------------------------------
// Test 3: DisabledNoOp — enabled=false, Queue does nothing
// ---------------------------------------------------------------------------

func TestMemoryAdapter_DisabledNoOp(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ma := fallback.NewMemoryAdapter(fallback.MemoryAdapterConfig{
		HelixMemoryURL: srv.URL,
		SyncInterval:   20 * time.Millisecond,
		Enabled:        false,
	})

	ma.Queue(fallback.MemoryEntry{Content: "should be ignored", Type: "fact", SessionID: "s1"})
	ma.Queue(fallback.MemoryEntry{Content: "also ignored", Type: "fact", SessionID: "s2"})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Start must also be a no-op when disabled.
	ma.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	cancel()
	ma.Wait()

	if pending := ma.PendingCount(); pending != 0 {
		t.Errorf("disabled adapter: expected 0 pending entries, got %d", pending)
	}
	if n := callCount.Load(); n != 0 {
		t.Errorf("disabled adapter: expected 0 server calls, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// Test 4: GracefulDegradation — unreachable server, entries stay in buffer
// ---------------------------------------------------------------------------

func TestMemoryAdapter_GracefulDegradation_UnreachableServer(t *testing.T) {
	ma := fallback.NewMemoryAdapter(fallback.MemoryAdapterConfig{
		HelixMemoryURL: "http://127.0.0.1:19998", // nothing listening
		SyncInterval:   30 * time.Millisecond,
		Enabled:        true,
	})

	ma.Queue(fallback.MemoryEntry{Content: "keep me", Type: "fact", SessionID: "s1"})
	ma.Queue(fallback.MemoryEntry{Content: "keep me too", Type: "fact", SessionID: "s2"})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Must not panic or crash even when the server is unreachable.
	ma.Start(ctx)
	time.Sleep(150 * time.Millisecond)
	cancel()
	ma.Wait()

	// Entries must be retained in the buffer for the next attempt.
	if pending := ma.PendingCount(); pending != 2 {
		t.Errorf("unreachable server: expected 2 pending entries, got %d", pending)
	}
}
