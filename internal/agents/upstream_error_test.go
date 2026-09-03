// Guard for the two brain-backed agent routes, which live outside the
// gateway package but on the SAME gin engine and the same unauthenticated
// surface.
//
// The agent loop wraps brain.Complete with %w (agent.go), so its error
// carries the provider's own text — including the backend address when the
// provider was unreachable. Both handlers used to hand that to the client
// verbatim:
//
//	POST /v1/agents/chat -> 500
//	{"error":"agent: brain.Complete (turn 1): router: no available provider …"}
//
// A sibling route leaking what /v1/chat/completions had just stopped leaking
// would have made that fix cosmetic, so both now share the gateway's
// redaction funnel. This guard is what stops them drifting back apart.
package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// internalAddressPattern mirrors the gateway guard's pattern: a leak is any
// URL, IP literal, host:port authority, cluster-internal suffix, or dial
// diagnostic reaching the client.
var internalAddressPattern = regexp.MustCompile(
	`dial tcp|connection refused|connect: |` +
		`https?://|` +
		`127\.0\.0\.1|\[::1\]|\b(?:\d{1,3}\.){3}\d{1,3}\b|` +
		`\b[a-zA-Z0-9][a-zA-Z0-9.-]*:\d{2,5}\b|` +
		`\.internal\b|\.local\b|\.svc\b`)

// dialFailureText is the VERBATIM provider error the shipped binary
// produced when its backend was not listening.
const dialFailureText = `llamacpp: send request: Post ` +
	`"http://localhost:50052/v1/chat/completions": ` +
	`dial tcp 127.0.0.1:50052: connect: connection refused`

var errDialFailure = errors.New(dialFailureText)

// dialFailureProvider fails the way a real provider fails when nothing is
// listening on the other end. It is registered into a REAL brain.Brain so
// the error travels the production wrapping path (brain -> agent's %w wrap
// -> handler) rather than being injected at the handler boundary.
type dialFailureProvider struct{}

func (*dialFailureProvider) Complete(context.Context, *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	return nil, errDialFailure
}

func (*dialFailureProvider) CompleteStream(context.Context, *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	return nil, errDialFailure
}
func (*dialFailureProvider) Models() []string { return nil }
func (*dialFailureProvider) Name() string     { return "dialfail" }
func (*dialFailureProvider) Available() bool  { return true }

func dialFailureBrainFor() *brain.Brain {
	b := brain.New(brain.Config{DefaultProvider: "dialfail"})
	b.RegisterProvider("dialfail", &dialFailureProvider{})
	return b
}

func TestAgentRoutes_DoNotDiscloseInternalAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name string
		path string
		body string
	}{
		{"agents_chat", "/v1/agents/chat",
			`{"messages":[{"role":"user","content":"Hi"}]}`},
		{"agents_coordinate", "/v1/agents/coordinate",
			`{"task":"say hi"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			b := dialFailureBrainFor()
			RegisterAgentRoutesWithExtras(r,
				NewAgent(AgentConfig{Brain: b}), nil,
				NewCoordinator(CoordinatorConfig{Brain: b}),
				nil, nil, nil)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path,
				bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			body := w.Body.String()

			if m := internalAddressPattern.FindString(body); m != "" {
				t.Errorf("%s: client-visible body discloses internal topology (%q)\nbody: %s",
					tc.path, m, body)
			}
			// Redaction must not become silence: the caller is still owed a
			// truthful answer that the upstream failed.
			var got struct {
				Error struct {
					Message string `json:"message"`
					Type    string `json:"type"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("%s: response is not an error envelope: %v\nbody: %s",
					tc.path, err, body)
			}
			if got.Error.Message == "" {
				t.Errorf("%s: error.message is empty; the caller must still be told "+
					"the upstream failed, not merely told nothing", tc.path)
			}
			if got.Error.Type != "server_error" {
				t.Errorf("%s: error.type = %q, want %q",
					tc.path, got.Error.Type, "server_error")
			}
		})
	}
}
