package a2a_test

// Unit tests for the A2A server surface. Per CLAUDE.md §4.4 / §11.4.27(A),
// mocks/fakes are permitted ONLY here (unit tests) — the httptest server
// below stands in for the downstream coder so these tests are hermetic and
// fast; the real end-to-end proof against the LIVE coder lives in
// docs/qa/phase3_a2a_20260707/harness (integration/full-automation layer,
// no mocks, §11.4.50 CONST-050(A)).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/a2a"
)

func fakeCoder(t *testing.T, modelID, replyText string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"` + modelID + `"}]}`))
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": replyText}},
			},
		}
		b, _ := json.Marshal(body)
		_, _ = w.Write(b)
	})
	return httptest.NewServer(mux)
}

func newTestServer(t *testing.T, replyText string) (*gin.Engine, *a2a.Server) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	coder := fakeCoder(t, "fake-model", replyText)
	t.Cleanup(coder.Close)

	down := a2a.NewDownstream(coder.URL, "", 5*time.Second)
	modelID, err := down.DiscoverModelID(context.Background())
	if err != nil {
		t.Fatalf("DiscoverModelID: %v", err)
	}
	down.ModelID = modelID

	card := a2a.BuildAgentCard(a2a.CardConfig{
		PublicURL:         "http://localhost:18441",
		BasePath:          "/a2a",
		DownstreamModelID: modelID,
		BearerConfigured:  true,
	})
	srv := a2a.NewServer(card, down, 128)

	r := gin.New()
	a2a.RegisterRoutes(r, srv, a2a.RouterOptions{BasePath: "/a2a", BearerKeys: "test-token"})
	return r, srv
}

func TestAgentCardHasRequiredFields(t *testing.T) {
	r, _ := newTestServer(t, "irrelevant")
	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var card map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	for _, field := range []string{"name", "description", "version", "url", "skills", "capabilities"} {
		if _, ok := card[field]; !ok {
			t.Errorf("agent card missing required field %q", field)
		}
	}
	skills, _ := card["skills"].([]any)
	if len(skills) == 0 {
		t.Errorf("agent card skills[] must be non-empty")
	}

	// Bluff-audit regression guard (docs/qa/a2a_live_e2e_20260711T134958Z
	// §2.4 Finding A / RED-baseline evidence in
	// docs/qa/a2a_wireshape_fix_*): the real a2a-go SDK's a2aclient.NewFromCard
	// dispatches JSON-RPC directly to card.URL literally, so card.URL MUST
	// include the actual JSON-RPC dispatch mount path RegisterRoutes uses
	// below ("/a2a") -- never just the bare public host:port.
	url, _ := card["url"].(string)
	if !strings.HasSuffix(url, "/a2a") {
		t.Errorf("agent card url = %q, want suffix %q (must include the JSON-RPC dispatch base path so a spec-faithful client that trusts card.URL literally does not 404)", url, "/a2a")
	}
}

func TestMessageSendHappyPath(t *testing.T) {
	r, _ := newTestServer(t, "func Fibonacci(n int) int { return n }")
	body := `{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"role":"user","parts":[{"kind":"text","text":"write fib"}]}}}`
	req := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in response: %s", w.Body.String())
	}
	status, _ := result["status"].(map[string]any)
	if state, _ := status["state"].(string); state != "completed" {
		t.Errorf("task state = %q, want completed", state)
	}

	// Bluff-audit regression guard (docs/qa/a2a_live_e2e_20260711T134958Z
	// §2.4 Finding B): the real a2a-go SDK's polymorphic result decoder
	// (a2a.UnmarshalEventJSON) requires a top-level "kind" discriminator to
	// type this object as a Task rather than a Message. Without it, a
	// spec-faithful client's typed SendMessage() call fails to decode even
	// though the HTTP transaction itself succeeded.
	if kind, _ := result["kind"].(string); kind != "task" {
		t.Errorf("message/send result[\"kind\"] = %q, want %q (required for the real a2a-go SDK's polymorphic Task/Message decode)", kind, "task")
	}
}

func TestMessageSendRejectsMissingAuth(t *testing.T) {
	r, _ := newTestServer(t, "func Fibonacci(n int) int { return n }")
	body := `{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"role":"user","parts":[{"kind":"text","text":"write fib"}]}}}`
	req := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if result, ok := resp["result"].(map[string]any); ok {
			if status, _ := result["status"].(map[string]any); status != nil {
				if state, _ := status["state"].(string); state == "completed" {
					t.Fatalf("unauthorized request was PROCESSED to completion: %s", w.Body.String())
				}
			}
		}
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestDispatchRejectsMalformedRequest(t *testing.T) {
	r, _ := newTestServer(t, "irrelevant")
	// Missing "method" entirely.
	body := `{"jsonrpc":"2.0","id":1,"params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, hasErr := resp["error"]; !hasErr {
		t.Fatalf("expected JSON-RPC error for malformed request, got: %s", w.Body.String())
	}
	if _, hasResult := resp["result"]; hasResult {
		t.Fatalf("malformed request must never produce a result: %s", w.Body.String())
	}
}

func TestTasksGetRoundTrip(t *testing.T) {
	r, _ := newTestServer(t, "func Reverse(s string) string { return s }")
	sendBody := `{"jsonrpc":"2.0","id":1,"method":"message/send","params":{"message":{"role":"user","parts":[{"kind":"text","text":"write reverse"}]}}}`
	req := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(sendBody))
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var sendResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &sendResp)
	result := sendResp["result"].(map[string]any)
	taskID := result["id"].(string)
	if taskID == "" {
		t.Fatalf("empty task id")
	}

	getBody := `{"jsonrpc":"2.0","id":2,"method":"tasks/get","params":{"id":"` + taskID + `"}}`
	req2 := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(getBody))
	req2.Header.Set("Authorization", "Bearer test-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var getResp map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &getResp)
	result2, ok := getResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tasks/get did not return a result: %s", w2.Body.String())
	}
	if result2["id"] != taskID {
		t.Errorf("tasks/get id mismatch: got %v want %v", result2["id"], taskID)
	}
	if kind, _ := result2["kind"].(string); kind != "task" {
		t.Errorf("tasks/get result[\"kind\"] = %q, want %q (kind discriminator regression guard)", kind, "task")
	}
}
