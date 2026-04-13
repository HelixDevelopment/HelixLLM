package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
)

// TestMemoryAdapter_Integration_QueueAndFlush stands up a mock httptest server,
// queues a MemoryEntry, and verifies the adapter flushes it to the server.
// This test always runs — it uses only httptest and does not need live services.
func TestMemoryAdapter_Integration_QueueAndFlush(t *testing.T) {
	received := make(chan []fallback.MemoryEntry, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/memory/entities" || r.Method != http.MethodPost {
			http.Error(w, "unexpected path/method", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusInternalServerError)
			return
		}
		var entries []fallback.MemoryEntry
		if err := json.Unmarshal(body, &entries); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		select {
		case received <- entries:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	adapter := fallback.NewMemoryAdapter(fallback.MemoryAdapterConfig{
		HelixMemoryURL: srv.URL,
		SyncInterval:   50 * time.Millisecond, // fast tick for tests
		Enabled:        true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	adapter.Start(ctx)

	adapter.Queue(fallback.MemoryEntry{
		Content:   "integration test memory",
		Type:      "fact",
		SessionID: "test-session-001",
	})

	select {
	case entries := <-received:
		if len(entries) == 0 {
			t.Fatal("expected at least one flushed entry, got zero")
		}
		if entries[0].Content != "integration test memory" {
			t.Errorf("content = %q, want %q", entries[0].Content, "integration test memory")
		}
		if entries[0].SessionID != "test-session-001" {
			t.Errorf("session_id = %q, want %q", entries[0].SessionID, "test-session-001")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for memory flush")
	}
}
