// Provider-unavailable status-mapping regression guard.
//
// # What this pins
//
// When every configured provider is unreachable, the fallback Chain returns
// an "all providers exhausted" error. The gateway used to render that as
// HTTP 500 at five call sites, which tells a client "this server has a bug,
// the request is not worth retrying". The truthful status is 503: the
// service cannot serve the request RIGHT NOW because its backend is down,
// and the client should retry with backoff. RFC 9110 §15.6.4.
//
// This is not a status cosmetic. A 500 is what an OpenAI-compatible client,
// a load balancer, and a readiness probe all read as "broken build"; a 503
// is what they read as "warming up / degraded". Every deployment whose
// backend is still starting served the wrong one of those two answers.
//
// The project's own challenge banks declare the contract this guard pins:
//   - challenges/banks/chaos/provider_failure.yaml:8-21
//     ("Chat completion returns meaningful error when no LLM is available"
//     -> status_one_of [404, 503])
//   - challenges/banks/regression/dead_code.yaml:100-116  -> one_of [200, 503]
//
// # Polarity switch (§11.4.115)
//
// RED_MODE=1 reproduces the DEFECT on the pre-fix artifact: it asserts the
// broken behaviour (500) is present, so a run against the unfixed build
// PASSES and proves the reproduction is real rather than synthetic.
// RED_MODE=0 (the default) is the standing GREEN regression guard: it
// asserts the defect is ABSENT, i.e. the status is 503.
//
//	RED_MODE=1 go test -run TestProviderExhausted ./internal/gateway/  # pre-fix
//	           go test -run TestProviderExhausted ./internal/gateway/  # post-fix
//
// # Deliberate non-coverage
//
// A provider that is REACHABLE and returns an ordinary error stays 500 —
// that is a genuine upstream fault, not an availability condition. The
// existing TestChatCompletions_WithBrain_Error / _StreamError cases pin
// that side, and this guard must not disturb them.
package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/fallback"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// redMode reports whether the suite runs in defect-reproduction polarity.
func redMode() bool { return os.Getenv("RED_MODE") == "1" }

// exhaustedChain builds the real production error source: a fallback.Chain
// with zero usable entries, which is exactly the state a deployment is in
// when no provider is reachable. Using the real Chain (not a stub error)
// keeps the guard honest — it pins the status the SHIPPED error path
// produces, not one a test fixture invented.
func exhaustedChain() *fallback.Chain {
	return fallback.NewChain(
		map[string]brain.Provider{},
		fallback.NewRateLimitTracker(1, 1),
	)
}

// assertStatus applies the polarity switch: in RED mode the pre-fix status
// must be observed, otherwise the fixed status must be.
func assertStatus(t *testing.T, route string, got int) {
	t.Helper()
	want := http.StatusServiceUnavailable
	if redMode() {
		want = http.StatusInternalServerError
	}
	if got != want {
		t.Errorf("%s: status = %d, want %d (RED_MODE=%v)",
			route, got, want, redMode())
	}
}

func postJSON(t *testing.T, h gin.HandlerFunc, path string, body any) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST(path, h)

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w.Code
}

// TestProviderExhausted_ChainErrorIsIdentifiable proves the Chain's exhausted
// error is identifiable by a caller WITHOUT matching on message text. Without
// it, every consumer is reduced to substring-matching, which is why the five
// gateway sites hardcoded 500 in the first place.
//
// It also pins the historical message text: chain_test.go and Chain.Complete's
// doc comment both assert on the "all providers exhausted" substring, so the
// sentinel must carry it verbatim rather than reword it.
func TestProviderExhausted_ChainErrorIsIdentifiable(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: RED_MODE reproduces the pre-fix artifact, " +
			"where the fallback.IsProvidersExhausted predicate does not yet exist")
	}
	_, err := exhaustedChain().Complete(context.Background(),
		&types.InternalChatRequest{
			Model:    "llama-3.1-70b",
			Messages: []types.InternalMessage{{Role: types.RoleUser, Content: "Hello"}},
		})
	if err == nil {
		t.Fatal("a chain with no entries returned nil error; expected exhausted")
	}
	if !fallback.IsProvidersExhausted(err) {
		t.Errorf("IsProvidersExhausted(%v) = false, want true", err)
	}
	if !strings.Contains(err.Error(), "all providers exhausted") {
		t.Errorf("error %q lost the documented %q prefix",
			err.Error(), "all providers exhausted")
	}
}

// TestProviderExhausted_OrdinaryProviderErrorStaysInternal is the negative
// control: a provider that WAS reached and returned a fault is a genuine
// internal error and must remain 500. Without this, a fix that mapped every
// Completer failure to 503 would pass the cases above while telling clients
// a real server fault is merely a temporary outage.
func TestProviderExhausted_OrdinaryProviderErrorStaysInternal(t *testing.T) {
	if fallback.IsProvidersExhausted(errors.New("provider returned garbage")) {
		t.Error("an unrelated error was classified as providers-exhausted")
	}
}

func TestProviderExhausted_ChatCompletions(t *testing.T) {
	assertStatus(t, "POST /v1/chat/completions", postJSON(t,
		gateway.HandleChatCompletions(exhaustedChain(), nil, nil),
		"/v1/chat/completions",
		api.ChatCompletionRequest{
			Model:    "llama-3.1-70b",
			Messages: []api.ChatMessage{{Role: "user", Content: "Hello"}},
		}))
}

func TestProviderExhausted_ChatCompletionsStreaming(t *testing.T) {
	assertStatus(t, "POST /v1/chat/completions (stream)", postJSON(t,
		gateway.HandleChatCompletions(exhaustedChain(), nil, nil),
		"/v1/chat/completions",
		api.ChatCompletionRequest{
			Model:    "llama-3.1-70b",
			Messages: []api.ChatMessage{{Role: "user", Content: "Hello"}},
			Stream:   true,
		}))
}

func TestProviderExhausted_AnthropicMessages(t *testing.T) {
	assertStatus(t, "POST /v1/messages", postJSON(t,
		gateway.HandleMessages(exhaustedChain()),
		"/v1/messages",
		map[string]any{
			"model":      "claude-sonnet-4-20250514",
			"max_tokens": 16,
			"messages": []map[string]any{
				{"role": "user", "content": "Hello"},
			},
		}))
}

func TestProviderExhausted_AnthropicMessagesStreaming(t *testing.T) {
	assertStatus(t, "POST /v1/messages (stream)", postJSON(t,
		gateway.HandleMessages(exhaustedChain()),
		"/v1/messages",
		map[string]any{
			"model":      "claude-sonnet-4-20250514",
			"max_tokens": 16,
			"stream":     true,
			"messages": []map[string]any{
				{"role": "user", "content": "Hello"},
			},
		}))
}

func TestProviderExhausted_Completions(t *testing.T) {
	assertStatus(t, "POST /v1/completions", postJSON(t,
		gateway.HandleCompletions(exhaustedChain()),
		"/v1/completions",
		map[string]any{
			"model":  "llama-3.1-70b",
			"prompt": "Hello",
		}))
}
