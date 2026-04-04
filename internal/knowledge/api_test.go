package knowledge_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newTestRouter() (*gin.Engine, *knowledge.Pipeline) {
	p := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          knowledge.NewHashEmbedder(64),
		Store:             knowledge.NewMemoryStore(),
		Chunker:           knowledge.NewFixedSizeChunker(100, 10),
		DefaultCollection: "default",
		DefaultTopK:       5,
	})
	r := gin.New()
	knowledge.RegisterKnowledgeRoutes(r, p)
	return r, p
}

func postJSON(t *testing.T, r *gin.Engine, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func getJSON(r *gin.Engine, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------
// Ingest
// ---------------------------------------------------------------------------

func TestAPI_Ingest_Success(t *testing.T) {
	r, _ := newTestRouter()

	w := postJSON(t, r, "/internal/knowledge/ingest", knowledge.IngestRequest{
		Title:      "API Test Doc",
		Content:    "This is the content of the document used in the API test.",
		Collection: "api-test",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result knowledge.IngestResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.DocumentID == "" {
		t.Error("expected non-empty document_id")
	}
	if result.Chunks <= 0 {
		t.Errorf("expected chunks > 0, got %d", result.Chunks)
	}
	if result.Collection != "api-test" {
		t.Errorf("expected collection 'api-test', got %q", result.Collection)
	}
}

func TestAPI_Ingest_EmptyContent_Returns400(t *testing.T) {
	r, _ := newTestRouter()

	w := postJSON(t, r, "/internal/knowledge/ingest", knowledge.IngestRequest{
		Content:    "",
		Collection: "col",
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPI_Ingest_EmptyCollection_Returns400(t *testing.T) {
	r, _ := newTestRouter()

	w := postJSON(t, r, "/internal/knowledge/ingest", knowledge.IngestRequest{
		Content:    "Some content",
		Collection: "",
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPI_Ingest_InvalidJSON_Returns400(t *testing.T) {
	r, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodPost, "/internal/knowledge/ingest",
		bytes.NewBufferString("{invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Query
// ---------------------------------------------------------------------------

func TestAPI_Query_Success(t *testing.T) {
	r, _ := newTestRouter()

	// Ingest first
	postJSON(t, r, "/internal/knowledge/ingest", knowledge.IngestRequest{
		Content:    "The quick brown fox jumps over the lazy dog.",
		Collection: "query-test",
	})

	w := postJSON(t, r, "/internal/knowledge/query", knowledge.QueryRequest{
		Query:      "quick fox",
		Collection: "query-test",
		TopK:       3,
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result knowledge.QueryResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Query != "quick fox" {
		t.Errorf("unexpected query echo: %q", result.Query)
	}
	if len(result.Chunks) == 0 {
		t.Error("expected at least one chunk")
	}
}

func TestAPI_Query_EmptyQuery_Returns400(t *testing.T) {
	r, _ := newTestRouter()

	w := postJSON(t, r, "/internal/knowledge/query", knowledge.QueryRequest{
		Query:      "",
		Collection: "col",
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPI_Query_EmptyCollection_Returns400(t *testing.T) {
	r, _ := newTestRouter()

	w := postJSON(t, r, "/internal/knowledge/query", knowledge.QueryRequest{
		Query:      "something",
		Collection: "",
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPI_Query_InvalidJSON_Returns400(t *testing.T) {
	r, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodPost, "/internal/knowledge/query",
		bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Collections
// ---------------------------------------------------------------------------

func TestAPI_Collections_AfterIngest(t *testing.T) {
	r, _ := newTestRouter()

	postJSON(t, r, "/internal/knowledge/ingest", knowledge.IngestRequest{
		Content:    "Content for collections endpoint test.",
		Collection: "col-endpoint",
	})

	w := getJSON(r, "/internal/knowledge/collections")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cols []knowledge.Collection
	if err := json.NewDecoder(w.Body).Decode(&cols); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	found := false
	for _, c := range cols {
		if c.Name == "col-endpoint" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'col-endpoint' in collections list")
	}
}

func TestAPI_Collections_EmptyStore(t *testing.T) {
	r, _ := newTestRouter()

	w := getJSON(r, "/internal/knowledge/collections")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var cols []knowledge.Collection
	if err := json.NewDecoder(w.Body).Decode(&cols); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Empty store should return empty array, not null
	if cols == nil {
		t.Error("expected empty array, got null")
	}
}

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

func TestAPI_Stats_ReturnsTotals(t *testing.T) {
	r, _ := newTestRouter()

	postJSON(t, r, "/internal/knowledge/ingest", knowledge.IngestRequest{
		Content:    "First stats document content.",
		Collection: "stats-api",
	})
	postJSON(t, r, "/internal/knowledge/ingest", knowledge.IngestRequest{
		Content:    "Second stats document content with more text.",
		Collection: "stats-api",
	})

	w := getJSON(r, "/internal/knowledge/stats")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats knowledge.Stats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if stats.TotalDocs < 2 {
		t.Errorf("expected >= 2 total docs, got %d", stats.TotalDocs)
	}
	if stats.TotalChunks <= 0 {
		t.Errorf("expected total chunks > 0, got %d", stats.TotalChunks)
	}
}
