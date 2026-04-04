package knowledge_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

// newQdrantTestServer spins up a minimal HTTP mock that satisfies the Qdrant
// REST API calls made by QdrantStore:
//
//   - GET  /          → health check (200)
//   - PUT  /collections/{name}        → create collection (200)
//   - PUT  /collections/{name}/points → upsert (200)
//   - POST /collections/{name}/points/search → search (200)
//   - POST /collections/{name}/points/delete → delete (200)
//   - GET  /collections               → list collections (200)
//   - DELETE /collections/{name}      → delete collection (200)
func newQdrantTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	})

	// List collections
	mux.HandleFunc("/collections", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			resp := map[string]any{
				"result": map[string]any{
					"collections": []map[string]any{
						{"name": "test-col"},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	})

	// Collection-specific endpoints
	mux.HandleFunc("/collections/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch {
		// PUT /collections/{name} - create collection
		case r.Method == http.MethodPut && !strings.Contains(path, "/points"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"result": true, "status": "ok"})

		// DELETE /collections/{name}
		case r.Method == http.MethodDelete && !strings.Contains(path, "/points"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"result": true, "status": "ok"})

		// PUT /collections/{name}/points - upsert
		case r.Method == http.MethodPut && strings.HasSuffix(path, "/points"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"status": "acknowledged"}})

		// POST /collections/{name}/points/search
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/points/search"):
			resp := map[string]any{
				"result": []map[string]any{
					{
						"id":    "chunk-1",
						"score": 0.95,
						"payload": map[string]any{
							"content":     "machine learning basics",
							"document_id": "doc-1",
							"position":    0,
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)

		// POST /collections/{name}/points/delete
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/points/delete"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"status": "acknowledged"}})

		default:
			http.NotFound(w, r)
		}
	})

	return httptest.NewServer(mux)
}

func TestQdrantStore_NewQdrantStore_ConnectFailure(t *testing.T) {
	// Port 1 is never open; we expect a connection error.
	_, err := knowledge.NewQdrantStore("127.0.0.1", 1)
	if err == nil {
		t.Fatal("expected error connecting to port 1, got nil")
	}
}

func TestQdrantStore_Upsert(t *testing.T) {
	srv := newQdrantTestServer(t)
	defer srv.Close()

	host, port := parseHostPort(t, srv.URL)
	store, err := knowledge.NewQdrantStore(host, port)
	if err != nil {
		t.Fatalf("NewQdrantStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	embedder := knowledge.NewHashEmbedder(16)
	v, _ := embedder.Embed("machine learning")

	chunks := []knowledge.Chunk{
		{ID: "chunk-1", DocumentID: "doc-1", Content: "machine learning basics", Embedding: v},
	}

	if err := store.Upsert("test-col", chunks); err != nil {
		t.Fatalf("Upsert error: %v", err)
	}
}

func TestQdrantStore_Upsert_Empty(t *testing.T) {
	srv := newQdrantTestServer(t)
	defer srv.Close()

	host, port := parseHostPort(t, srv.URL)
	store, err := knowledge.NewQdrantStore(host, port)
	if err != nil {
		t.Fatalf("NewQdrantStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Empty upsert should be a no-op.
	if err := store.Upsert("test-col", nil); err != nil {
		t.Fatalf("Upsert(nil) error: %v", err)
	}
	if err := store.Upsert("test-col", []knowledge.Chunk{}); err != nil {
		t.Fatalf("Upsert(empty) error: %v", err)
	}
}

func TestQdrantStore_Search(t *testing.T) {
	srv := newQdrantTestServer(t)
	defer srv.Close()

	host, port := parseHostPort(t, srv.URL)
	store, err := knowledge.NewQdrantStore(host, port)
	if err != nil {
		t.Fatalf("NewQdrantStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	embedder := knowledge.NewHashEmbedder(16)
	v, _ := embedder.Embed("machine learning")

	results, err := store.Search("test-col", v, 5)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "chunk-1" {
		t.Errorf("result ID = %q, want chunk-1", results[0].ID)
	}
	if results[0].Content != "machine learning basics" {
		t.Errorf("content = %q, want 'machine learning basics'", results[0].Content)
	}
	if results[0].Score < 0.9 {
		t.Errorf("score = %f, want >= 0.9", results[0].Score)
	}
}

func TestQdrantStore_Delete(t *testing.T) {
	srv := newQdrantTestServer(t)
	defer srv.Close()

	host, port := parseHostPort(t, srv.URL)
	store, err := knowledge.NewQdrantStore(host, port)
	if err != nil {
		t.Fatalf("NewQdrantStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.Delete("test-col", []string{"chunk-1"}); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
}

func TestQdrantStore_Delete_Empty(t *testing.T) {
	srv := newQdrantTestServer(t)
	defer srv.Close()

	host, port := parseHostPort(t, srv.URL)
	store, err := knowledge.NewQdrantStore(host, port)
	if err != nil {
		t.Fatalf("NewQdrantStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.Delete("test-col", nil); err != nil {
		t.Fatalf("Delete(nil) error: %v", err)
	}
}

func TestQdrantStore_Collections(t *testing.T) {
	srv := newQdrantTestServer(t)
	defer srv.Close()

	host, port := parseHostPort(t, srv.URL)
	store, err := knowledge.NewQdrantStore(host, port)
	if err != nil {
		t.Fatalf("NewQdrantStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	cols, err := store.Collections()
	if err != nil {
		t.Fatalf("Collections error: %v", err)
	}
	if len(cols) != 1 {
		t.Fatalf("expected 1 collection, got %d", len(cols))
	}
	if cols[0].Name != "test-col" {
		t.Errorf("collection name = %q, want test-col", cols[0].Name)
	}
}

func TestQdrantStore_DeleteCollection(t *testing.T) {
	srv := newQdrantTestServer(t)
	defer srv.Close()

	host, port := parseHostPort(t, srv.URL)
	store, err := knowledge.NewQdrantStore(host, port)
	if err != nil {
		t.Fatalf("NewQdrantStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.DeleteCollection("test-col"); err != nil {
		t.Fatalf("DeleteCollection error: %v", err)
	}
}

func TestQdrantStore_Stats(t *testing.T) {
	srv := newQdrantTestServer(t)
	defer srv.Close()

	host, port := parseHostPort(t, srv.URL)
	store, err := knowledge.NewQdrantStore(host, port)
	if err != nil {
		t.Fatalf("NewQdrantStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	stats, err := store.Stats()
	if err != nil {
		t.Fatalf("Stats error: %v", err)
	}
	if stats == nil {
		t.Fatal("Stats returned nil")
	}
	if len(stats.Collections) == 0 {
		t.Error("expected at least one collection in stats")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// parseHostPort extracts host and port integers from a URL like http://127.0.0.1:PORT.
func parseHostPort(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	// Strip scheme
	raw := strings.TrimPrefix(rawURL, "http://")
	raw = strings.TrimPrefix(raw, "https://")
	idx := strings.LastIndex(raw, ":")
	if idx < 0 {
		t.Fatalf("cannot parse host:port from %q", rawURL)
	}
	host := raw[:idx]
	var port int
	if _, err := func() (int, error) {
		n := 0
		for _, c := range raw[idx+1:] {
			if c < '0' || c > '9' {
				return 0, nil
			}
			n = n*10 + int(c-'0')
		}
		port = n
		return n, nil
	}(); err != nil {
		t.Fatalf("bad port in %q", rawURL)
	}
	return host, port
}
