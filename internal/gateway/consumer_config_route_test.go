package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
)

// A user must be able to OBTAIN their HelixCode and OpenCode configuration.
//
// `internal/naming` has produced both artefacts since the naming scheme landed,
// and both are covered by unit tests — but nothing outside that package has
// ever called ExportHelixCode, ExportOpenCode, MergeHelixCodeEnv or
// MergeOpenCode. The only instructions a user had were Go snippets in
// docs/guides/consumer_setup.md calling an `internal/` package, which is not
// importable from outside this module. So the artefacts existed and were
// unobtainable: the Claude Toolkit could read its models off `GET /v1/models`,
// and the other two consumers had no path at all.
//
// These tests exercise the USER-REACHABLE path — an HTTP request through the
// real router — precisely because testing the exporter function proved nothing
// about reachability. That was the whole finding.
//
// RED_MODE is the §11.4.115 polarity switch: RED_MODE=1 asserts the defect (no
// such route) on the pre-fix artifact; RED_MODE=0 is the standing guard.
func gatewayRedMode() bool { return os.Getenv("RED_MODE") == "1" }

// servingGateway builds a router whose Brain has one HelixLLM-served backend,
// backed by a real HTTP server that answers /health so the provider reports
// itself available.
func servingGateway(t *testing.T) (*gin.Engine, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
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

func getJSON(t *testing.T, r *gin.Engine, path string) (int, map[string]any, string) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	body := w.Body.String()
	var decoded map[string]any
	_ = json.Unmarshal([]byte(body), &decoded)
	return w.Code, decoded, body
}

func TestHelixCodeConfigIsObtainableOverHTTP(t *testing.T) {
	r, stop := servingGateway(t)
	defer stop()

	code, doc, body := getJSON(t, r, "/v1/config/helixcode")

	if gatewayRedMode() {
		if code != http.StatusNotFound {
			t.Fatalf("RED_MODE=1 expects no such route, but GET /v1/config/helixcode "+
				"returned %d — the defect is gone; re-run with RED_MODE=0", code)
		}
		return
	}

	if code != http.StatusOK {
		t.Fatalf("GET /v1/config/helixcode = %d, want 200\n%s", code, body)
	}

	env, _ := doc["env_file"].(string)
	if !strings.Contains(env, "HELIX_LLM_LOCAL_OPENAI_ENDPOINT=") {
		t.Errorf("the returned fragment does not set the one variable HelixCode's "+
			"live route reads:\n%s", env)
	}
	// The roster must name the identifier a user puts in `model`, and it must
	// be a real derived identifier, not the raw served name.
	if !strings.Contains(env, "helixllm-") {
		t.Errorf("the returned fragment carries no published identifier:\n%s", env)
	}

	models, _ := doc["models"].([]any)
	if len(models) == 0 {
		t.Fatalf("no models in the exported configuration:\n%s", body)
	}
	first, _ := models[0].(map[string]any)
	if wire, _ := first["wire_model"].(string); wire != "llama3:8b" {
		t.Errorf("wire_model = %q, want %q — the consumer needs the name the "+
			"instance itself answers to", wire, "llama3:8b")
	}
	if ident, _ := first["identity"].(string); !strings.HasPrefix(ident, "helixllm/") {
		t.Errorf("identity = %q, want a helixllm/<host>/<model> value", ident)
	}
}

func TestOpenCodeConfigIsObtainableOverHTTP(t *testing.T) {
	r, stop := servingGateway(t)
	defer stop()

	code, doc, body := getJSON(t, r, "/v1/config/opencode")

	if gatewayRedMode() {
		if code != http.StatusNotFound {
			t.Fatalf("RED_MODE=1 expects no such route, but GET /v1/config/opencode "+
				"returned %d — the defect is gone; re-run with RED_MODE=0", code)
		}
		return
	}

	if code != http.StatusOK {
		t.Fatalf("GET /v1/config/opencode = %d, want 200\n%s", code, body)
	}

	providerID, _ := doc["provider_id"].(string)
	if providerID == "" {
		t.Fatalf("no provider_id in the exported configuration:\n%s", body)
	}

	document, _ := doc["document"].(map[string]any)
	providers, _ := document["provider"].(map[string]any)
	entry, ok := providers[providerID].(map[string]any)
	if !ok {
		t.Fatalf("the document has no `provider.%s` entry:\n%s", providerID, body)
	}
	if npm, _ := entry["npm"].(string); npm != "@ai-sdk/openai-compatible" {
		t.Errorf("npm = %q, want the OpenAI-compatible adapter", npm)
	}
	options, _ := entry["options"].(map[string]any)
	baseURL, _ := options["baseURL"].(string)
	if !strings.HasSuffix(baseURL, "/v1") {
		t.Errorf("options.baseURL = %q — OpenCode's adapter appends no version "+
			"segment, so this one must carry it", baseURL)
	}
}

// The merge path must be reachable too: producing an artefact a user cannot
// apply to their existing file is only half a configuration.
func TestConfigMergeIsObtainableOverHTTP(t *testing.T) {
	r, stop := servingGateway(t)
	defer stop()

	existing := "# my own settings\nEDITOR=vim\n"
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/config/helixcode/merge",
		strings.NewReader(existing))
	req.Header.Set("Content-Type", "text/plain")
	r.ServeHTTP(w, req)

	if gatewayRedMode() {
		if w.Code != http.StatusNotFound {
			t.Fatalf("RED_MODE=1 expects no such route, but POST "+
				"/v1/config/helixcode/merge returned %d — re-run with RED_MODE=0", w.Code)
		}
		return
	}

	if w.Code != http.StatusOK {
		t.Fatalf("POST /v1/config/helixcode/merge = %d, want 200\n%s", w.Code, w.Body.String())
	}
	merged := w.Body.String()
	if !strings.Contains(merged, "EDITOR=vim") {
		t.Errorf("the merge dropped the operator's own lines:\n%s", merged)
	}
	if !strings.Contains(merged, "HELIX_LLM_LOCAL_OPENAI_ENDPOINT=") {
		t.Errorf("the merge did not add the managed block:\n%s", merged)
	}

	// Re-running must be a no-op — the managed block is delimited, so a second
	// merge replaces exactly what the first one wrote.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/v1/config/helixcode/merge",
		strings.NewReader(merged))
	req2.Header.Set("Content-Type", "text/plain")
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second merge = %d, want 200\n%s", w2.Code, w2.Body.String())
	}
	if w2.Body.String() != merged {
		t.Errorf("re-running the merge changed the file:\n--- first ---\n%s\n--- second ---\n%s",
			merged, w2.Body.String())
	}
}

// An unknown consumer must be refused, not silently answered with something.
func TestUnknownConsumerIsRefused(t *testing.T) {
	r, stop := servingGateway(t)
	defer stop()

	code, _, _ := getJSON(t, r, "/v1/config/emacs")
	if gatewayRedMode() {
		return
	}
	if code != http.StatusNotFound {
		t.Errorf("GET /v1/config/emacs = %d, want 404", code)
	}
}
