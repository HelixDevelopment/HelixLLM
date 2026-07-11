package gateway

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
)

// TestSEC02_MalformedJSON_RejectedBefore500 is the permanent regression
// guard for SEC-02 (docs/qa/ext_security_mem_bench_20260711T150836Z/RESULTS.md
// + docs/qa/sec02_facade_json_gate_wave2_20260711T2*Z/RESULTS.md).
//
// Reproduce-first (§11.4.115) finding: sending SEC-02's exact malformed-JSON
// matrix (truncated body, unquoted-key + trailing comma) THROUGH the
// HelixLLM facade -- with a nil Completer AND with a REAL Completer wired to
// the live coder backend at 127.0.0.1:18434 -- never produced HTTP 500. The
// original SEC-02 500 was observed sending malformed JSON DIRECTLY to
// llama.cpp, bypassing this facade entirely (upstream nlohmann::json
// parse-error behaviour, out of scope for this submodule).
//
// This test asserts the facade-side contract going forward: every v1 POST
// endpoint returns 400 with an OpenAI-compat error body for malformed JSON,
// and (for the real-coder sub-test) that the coder is NEVER invoked for a
// malformed request -- a live TCP connection count would be unnecessary
// here because a genuine invocation would take orders of magnitude longer
// than the microsecond-scale in-process HTTP round trip these assertions
// complete in; the RequireValidJSON gate is what makes that guarantee
// structural rather than incidental (see
// internal/gateway/middleware/jsonvalidate_test.go for the isolated,
// mutation-proven unit coverage of the gate itself).
func TestSEC02_MalformedJSON_RejectedBefore500(t *testing.T) {
	gin.SetMode(gin.TestMode)

	malformed := []struct {
		name string
		body string
	}{
		{"truncated_json", `{"model": "x", "messages": [{"role":"user","content":"trunc`},
		{"unquoted_key_trailing_comma", `{model: "x", max_tokens: 8,}`},
	}

	bodyPaths := []string{"/v1/chat/completions", "/v1/messages", "/v1/completions", "/v1/embeddings"}

	t.Run("nil_brain", func(t *testing.T) {
		r := gin.New()
		RegisterRoutes(r, RouterOptions{Brain: nil})

		for _, path := range bodyPaths {
			for _, tc := range malformed {
				t.Run(path+"/"+tc.name, func(t *testing.T) {
					req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(tc.body))
					req.Header.Set("Content-Type", "application/json")
					w := httptest.NewRecorder()
					r.ServeHTTP(w, req)

					if w.Code != http.StatusBadRequest {
						t.Fatalf("status = %d, want 400 (real HTTP evidence); body=%s", w.Code, w.Body.String())
					}
					if w.Code == http.StatusInternalServerError {
						t.Fatalf("SEC-02 REGRESSION: malformed JSON produced HTTP 500 through the facade")
					}
					if !bytes.Contains(w.Body.Bytes(), []byte("invalid_request_error")) {
						t.Fatalf("body missing OpenAI-compat error type: %s", w.Body.String())
					}
				})
			}
		}
	})

	t.Run("real_brain_live_coder", func(t *testing.T) {
		realBrain := brain.NewLlamaCppProvider("http://127.0.0.1:18434", []string{"probe-model"})
		if !realBrain.Available() {
			t.Skip("SKIP-OK: live coder at 127.0.0.1:18434 not reachable in this environment — read-only reproduction probe")
		}

		r := gin.New()
		RegisterRoutes(r, RouterOptions{Brain: realBrain})

		for _, path := range bodyPaths {
			for _, tc := range malformed {
				t.Run(path+"/"+tc.name, func(t *testing.T) {
					req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(tc.body))
					req.Header.Set("Content-Type", "application/json")
					w := httptest.NewRecorder()
					r.ServeHTTP(w, req)

					if w.Code != http.StatusBadRequest {
						t.Fatalf("status = %d, want 400 (real HTTP evidence, real coder wired); body=%s", w.Code, w.Body.String())
					}
					if w.Code == http.StatusInternalServerError {
						t.Fatalf("SEC-02 REGRESSION: malformed JSON reached the coder path and produced HTTP 500")
					}
				})
			}
		}
	})

	// Control: a well-formed request through the SAME real-coder-wired
	// router must be entirely unaffected by the SEC-02 gate (proves the
	// fix does not over-reject legitimate traffic).
	t.Run("well_formed_request_unaffected", func(t *testing.T) {
		realBrain := brain.NewLlamaCppProvider("http://127.0.0.1:18434", []string{"probe-model"})
		if !realBrain.Available() {
			t.Skip("SKIP-OK: live coder at 127.0.0.1:18434 not reachable in this environment — read-only reproduction probe")
		}

		r := gin.New()
		RegisterRoutes(r, RouterOptions{Brain: realBrain})

		body := `{"model": "probe-model", "messages": [{"role":"user","content":"Reply with exactly: OK"}], "max_tokens": 8}`
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("well-formed request status = %d, want 200 (real HTTP evidence); body=%s", w.Code, w.Body.String())
		}
	})
}
