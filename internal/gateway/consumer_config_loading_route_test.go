package gateway_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
)

// Fetching your configuration during a restart must not cost you your models.
//
// WHAT THE USER SAW. llama.cpp answers /health with 503 while a model loads, so
// the provider reports itself unavailable — and it still lists the models it is
// configured with. The export withheld all of them, and both endpoints answered
// 200: the GET with a document whose `models` was `{}`, the merge with the
// caller's own file rewritten to hold that empty entry. The reasons existed the
// whole time, in a sibling `withheld` field neither the merge nor the caller's
// jq was looking at. Whether a user kept their models came down to when they
// happened to ask.
//
// These drive the USER-REACHABLE path — a real request through the real router
// against a real backend in the real loading state — because the state is
// produced by the health probe, and an exporter called directly cannot enter it.
//
// RED_MODE is the §11.4.115 polarity switch: RED_MODE=1 asserts the defect on
// the pre-fix tree, RED_MODE=0 is the standing guard.

// loadingGateway is servingGateway's backend mid-load: reachable, answering,
// and not yet ready — /health is 503, the model list is configured.
func loadingGateway(t *testing.T) (*gin.Engine, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			// llama.cpp's own "Loading model" answer.
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	b := brain.New(brain.Config{
		LlamaCppURL:    backend.URL,
		LlamaCppModels: []string{"llama3:8b"},
	})

	r := gin.New()
	gateway.RegisterRoutes(r, gateway.RouterOptions{ModelBrain: b})
	return r, backend.Close
}

// existingOpenCodeFile is the user's file from when the host was serving, keyed
// under the provider id this instance derives for itself.
func existingOpenCodeFile(t *testing.T, r *gin.Engine) ([]byte, string) {
	t.Helper()
	// Ask the running gateway which provider id it publishes, so the fixture
	// collides with the real key rather than a guessed one. While loading it
	// refuses, but the id is in the message; take it from a serving twin
	// instead — same host, same derivation.
	serving, stop := servingGateway(t)
	defer stop()
	code, doc, body := getJSON(t, serving, "/v1/config/opencode")
	if code != http.StatusOK {
		t.Fatalf("could not learn the provider id from a serving gateway: %d\n%s", code, body)
	}
	providerID, _ := doc["provider_id"].(string)
	if providerID == "" {
		t.Fatalf("serving gateway published no provider_id:\n%s", body)
	}
	_ = r

	file := map[string]any{
		"theme": "tokyonight",
		"provider": map[string]any{
			providerID: map[string]any{
				"npm":     "@ai-sdk/openai-compatible",
				"name":    "helixllm/host",
				"options": map[string]any{"baseURL": "http://host:8080/v1"},
				"models": map[string]any{
					"helixllm-host-llama3-8b-aaaaaaaaaaaa": map[string]any{
						"id": "llama3:8b", "name": "helixllm/host/llama3:8b"},
					"helixllm-host-mistral-7b-bbbbbbbbbbbb": map[string]any{
						"id": "mistral:7b", "name": "helixllm/host/mistral:7b"},
				},
			},
		},
	}
	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("building the user's existing file: %v", err)
	}
	return raw, providerID
}

func TestConfigGetWhileLoadingSaysSoInsteadOfAnsweringEmpty(t *testing.T) {
	r, stop := loadingGateway(t)
	defer stop()

	code, doc, body := getJSON(t, r, "/v1/config/opencode")

	if gatewayRedMode() {
		if code != http.StatusOK {
			t.Fatalf("RED_MODE=1 expects the pre-fix 200, got %d — the defect is "+
				"gone; re-run with RED_MODE=0\n%s", code, body)
			return
		}
		document, _ := doc["document"].(map[string]any)
		providers, _ := document["provider"].(map[string]any)
		for id, raw := range providers {
			entry, _ := raw.(map[string]any)
			models, _ := entry["models"].(map[string]any)
			if len(models) != 0 {
				t.Fatalf("RED_MODE=1 expects an empty `models` for %s, got %d", id, len(models))
			}
		}
		return
	}

	if code != http.StatusServiceUnavailable {
		t.Fatalf("GET /v1/config/opencode while loading = %d, want 503 — a document "+
			"naming no model is not a description of this host\n%s", code, body)
	}
	// It has to say what is wrong and what to do, or the caller is back to
	// reading an absence.
	msg := errorMessage(t, doc, body)
	for _, want := range []string{"provider-unavailable", "retry"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not mention %q: %s", want, msg)
		}
	}
	// The state is temporary, and the status has to say that rather than 404.
	if code == http.StatusNotFound {
		t.Error("a loading host was reported as absent")
	}
}

func TestConfigMergeWhileLoadingLeavesTheUsersFileAlone(t *testing.T) {
	r, stop := loadingGateway(t)
	defer stop()

	existing, providerID := existingOpenCodeFile(t, r)

	w := httptest.NewRecorder()
	w.Body = new(bytes.Buffer)
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost,
		"/v1/config/opencode/merge", bytes.NewReader(existing)))

	if gatewayRedMode() {
		if w.Code != http.StatusOK {
			t.Fatalf("RED_MODE=1 expects the pre-fix merge to succeed, got %d\n%s",
				w.Code, w.Body.String())
		}
		if n := countModels(t, w.Body.Bytes(), providerID); n != 0 {
			t.Fatalf("RED_MODE=1 expects the user's %d models to be erased, %d "+
				"survived — the defect is gone; re-run with RED_MODE=0", 2, n)
		}
		return
	}

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /v1/config/opencode/merge while loading = %d, want 503\n%s",
			w.Code, w.Body.String())
	}
	// The decisive one: nothing came back that a caller could write over their
	// two working models.
	if strings.Contains(w.Body.String(), "\"models\"") {
		t.Errorf("the refusal still returned a mergeable document:\n%s", w.Body.String())
	}
}

// The HelixCode arm is refused for the same reason: its managed block would
// carry an endpoint and a roster naming nothing the user could put in `model`.
func TestHelixCodeConfigWhileLoadingIsRefused(t *testing.T) {
	r, stop := loadingGateway(t)
	defer stop()

	code, _, body := getJSON(t, r, "/v1/config/helixcode")
	if gatewayRedMode() {
		if code != http.StatusOK {
			t.Fatalf("RED_MODE=1 expects the pre-fix 200, got %d\n%s", code, body)
		}
		return
	}
	if code != http.StatusServiceUnavailable {
		t.Errorf("GET /v1/config/helixcode while loading = %d, want 503\n%s", code, body)
	}
}

// VACUITY GUARD. An endpoint that refused everything would satisfy all three
// tests above while being useless, and the empty-document defect would be
// "fixed" by breaking the feature. A serving host must still be answered.
func TestConfigStillServedWhenTheBackendIsUp(t *testing.T) {
	r, stop := servingGateway(t)
	defer stop()

	code, doc, body := getJSON(t, r, "/v1/config/opencode")
	if gatewayRedMode() {
		return
	}
	if code != http.StatusOK {
		t.Fatalf("GET /v1/config/opencode from a serving host = %d, want 200\n%s", code, body)
	}
	document, _ := doc["document"].(map[string]any)
	providers, _ := document["provider"].(map[string]any)
	total := 0
	for _, raw := range providers {
		entry, _ := raw.(map[string]any)
		models, _ := entry["models"].(map[string]any)
		total += len(models)
	}
	if total == 0 {
		t.Errorf("a serving host produced a document naming no model:\n%s", body)
	}
}

// A server with no served hosts at all is a different answer, and refusing the
// loading case must not have swallowed it: there is nothing to wait for, so it
// stays the 404 it has always been. This is what keeps a genuinely empty server
// from being told to retry forever.
func TestServerWithNoServedHostsStillReports404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	gateway.RegisterRoutes(r, gateway.RouterOptions{ModelBrain: brain.New(brain.Config{})})

	code, _, body := getJSON(t, r, "/v1/config/opencode")
	if gatewayRedMode() {
		return
	}
	if code != http.StatusNotFound {
		t.Errorf("GET /v1/config/opencode with no served hosts = %d, want 404 — "+
			"nothing is loading, so there is nothing to retry for\n%s", code, body)
	}
}

func errorMessage(t *testing.T, doc map[string]any, body string) string {
	t.Helper()
	errObj, _ := doc["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	if msg == "" {
		t.Fatalf("no error message in the response:\n%s", body)
	}
	return msg
}

func countModels(t *testing.T, document []byte, providerID string) int {
	t.Helper()
	var root struct {
		Provider map[string]struct {
			Models map[string]any `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(document, &root); err != nil {
		t.Fatalf("merged document does not parse: %v\n%s", err, document)
	}
	return len(root.Provider[providerID].Models)
}
