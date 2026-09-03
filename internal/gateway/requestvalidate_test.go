// Semantic request-validation + upstream-error-disclosure regression guard.
//
// # What this pins
//
// Two defects that shared one cause: between ShouldBindJSON (which catches
// only JSON *syntax* errors) and provider dispatch, the gateway checked
// nothing. A request that was valid JSON but semantic nonsense — no
// messages, a negative token budget, a model name that is not a model name
// — went straight to the backend and came back as whatever the backend
// said.
//
//  1. STATUS. The caller who sent a bad request was told the SERVER had
//     failed (500, then 503 once the provider-unavailable mapping landed),
//     not that THEIR request was bad. 503 is the worse of the two here: it
//     tells a client to retry, and a retry of `messages: []` can never
//     succeed, so a correct client backs off and retries forever.
//
//  2. DISCLOSURE. The error body was the raw provider error, which named
//     the internal backend endpoint. Measured against the shipped binary
//     before this guard existed:
//
//     $ curl -sS .../v1/chat/completions -d '{"model":"llama-3.1-70b","messages":[]}'
//     HTTP=503 {"error":{"message":"brain error: all providers exhausted,
//     last error: llamacpp: send request: Post
//     \"http://localhost:50052/v1/chat/completions\":
//     dial tcp 127.0.0.1:50052: connect: connection refused", ...}}
//
//     A caller must learn that the upstream failed, not the address it
//     failed to reach. The operator still gets the full error — it moves to
//     the server log rather than being deleted.
//
// # The specification these cases encode
//
// Every rejection below cites an authority; none is invented here.
//
//	messages non-empty      OpenAI OpenAPI CreateChatCompletionRequest
//	                        .messages: `minItems: 1`; bank
//	                        challenges/banks/regression/known_bugs.yaml
//	                        CH-REG-001 asserts 400.
//	role non-empty          bank CH-REG-006 (`"role": null` -> 400). The
//	                        OpenAI message union discriminates on `role`, so
//	                        an absent role selects no variant at all.
//	max_tokens >= 1 (chat)  banks CH-REG-007 (-1 -> 400) and CH-REG-004
//	                        (0 -> 400). OpenAI's chat schema declares no
//	                        minimum (the field is `deprecated`), so the
//	                        project's own banks govern this endpoint.
//	max_tokens >= 0 (legacy OpenAI OpenAPI CreateCompletionRequest
//	  completions, and     .max_tokens: `minimum: 0`; Anthropic Messages
//	  Anthropic messages)  `max_tokens` `minimum: 0` (0 pre-warms the
//	                        prompt cache). 0 is LEGAL on both — only the
//	                        chat endpoint rejects it, per its own banks.
//	model well-formed       bank CH-REG-010 (330-char name -> 400),
//	                        CH-REG-002 (non-ASCII -> 400), and
//	                        challenges/banks/security/owasp.yaml CH-SEC-007
//	                        (`../../etc/passwd` -> 400).
//
// # Deliberate non-coverage
//
//   - Message CONTENT is never required to be non-empty: an assistant
//     message carrying `tool_calls` legitimately has empty content, and
//     rejecting it would break a correct client.
//   - The role ENUM is not enforced, only non-emptiness. OpenAI has
//     `developer` and `function` beyond this project's four roles and keeps
//     adding more; rejecting an unknown-but-present role would reject
//     requests a real client legitimately sends to a newer API.
//   - `n`, `temperature`, `top_p`, and the penalties are left alone. OpenAI
//     declares ranges for them, but no bank speaks and no defect was
//     reported, so tightening them is an unforced behaviour change.
//   - Model EXISTENCE is not checked. A well-formed but unknown model still
//     reaches dispatch; "requested resource does not exist" is 404 in
//     OpenAI's error-codes guide, which is what /v1/models/:id already
//     returns. This guard rejects names that are not model names, never
//     names that are merely unrecognised.
//   - AUTHENTICATION is untouched. An empty or malformed bearer token is a
//     401 answered by middleware.APIKeyAuth; this validator never reads the
//     Authorization header and must never turn an auth failure into a 400.
//
// # Polarity switch (§11.4.115)
//
// RED_MODE=1 reproduces the DEFECTS on the pre-fix artifact: bad requests
// are asserted to be answered with a non-400 upstream status, and the
// upstream error body is asserted to CONTAIN the internal address. A run
// against the unfixed build therefore PASSES, proving the reproduction is
// real rather than synthetic. RED_MODE=0 (the default) is the standing
// GREEN guard: bad requests must be 400, and no body may name the address.
//
//	RED_MODE=1 go test -run TestRequestValidation ./internal/gateway/   # pre-fix
//	           go test -run TestRequestValidation ./internal/gateway/   # post-fix
package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// internalAddressPattern matches the shapes an internal backend endpoint
// takes in an error string. Any of them in a client-visible body is the
// disclosure this guard forbids.
//
// It deliberately reaches beyond the loopback address the original leak
// happened to carry. A compose or Kubernetes deployment names its backend
// by SERVICE hostname, so the realistic leak shape is
// "llamacpp-backend:50052" or "backend.internal:8080" with no 127.0.0.1
// and no "dial tcp" in sight — a partial redaction that stripped only the
// literal loopback would otherwise sail past this guard.
var internalAddressPattern = regexp.MustCompile(
	`dial tcp|connection refused|connect: |` + // transport diagnostics
		`https?://|` + // any URL at all
		`127\.0\.0\.1|\[::1\]|\b(?:\d{1,3}\.){3}\d{1,3}\b|` + // IP literals
		`\b[a-zA-Z0-9][a-zA-Z0-9.-]*:\d{2,5}\b|` + // host:port authority
		`\.internal\b|\.local\b|\.svc\b`) // cluster-internal suffixes

// dialFailureCompleter is the production-shaped error source for the
// disclosure half: a Completer that fails the way a real provider fails
// when its backend is not listening. The message is the VERBATIM string
// the shipped binary produced (captured by curl against :8445, quoted in
// the file header), so the guard pins the real leak rather than a
// convenient stand-in.
type dialFailureCompleter struct{}

// dialFailureText is the VERBATIM provider error the shipped binary
// produced, captured by curl against the running server. Reproducing the
// leak from the real string keeps this guard from passing on a sanitised
// stand-in that never contained an address in the first place.
const dialFailureText = `llamacpp: send request: Post ` +
	`"http://localhost:50052/v1/chat/completions": ` +
	`dial tcp 127.0.0.1:50052: connect: connection refused`

var errDialFailure = errors.New(dialFailureText)

func (dialFailureCompleter) Complete(context.Context, *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	return nil, errDialFailure
}

func (dialFailureCompleter) CompleteStream(context.Context, *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	return nil, errDialFailure
}

// postRaw drives a handler with a literal body so a case can send shapes Go
// structs cannot express (a null role, a duplicated key), and returns both
// halves of the answer the client actually receives.
func postRaw(t *testing.T, h gin.HandlerFunc, path, body string) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST(path, h)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// invalidCase is one semantically-invalid request and the field it offends.
type invalidCase struct {
	name  string
	route string
	body  string
	// param is the OpenAI `error.param` the response must name, so a
	// client can point at the offending field rather than guess.
	param string
	// cite records the authority that makes this a 400, so a future
	// reader can check the rule rather than trust the test.
	cite string
}

func chatCases() []invalidCase {
	return []invalidCase{
		{
			name:  "empty_messages_array",
			route: "/v1/chat/completions",
			body:  `{"model":"llama-3.1-70b","messages":[]}`,
			param: "messages",
			cite:  "OpenAI minItems:1; bank CH-REG-001",
		},
		{
			name:  "missing_messages_field",
			route: "/v1/chat/completions",
			body:  `{"model":"llama-3.1-70b"}`,
			param: "messages",
			cite:  "OpenAI minItems:1; bank CH-REG-001",
		},
		{
			name:  "null_role_in_message",
			route: "/v1/chat/completions",
			body:  `{"model":"llama-3.1-70b","messages":[{"role":null,"content":"Hi"}]}`,
			param: "messages[0].role",
			cite:  "bank CH-REG-006",
		},
		{
			name:  "empty_role_in_second_message",
			route: "/v1/chat/completions",
			body: `{"model":"llama-3.1-70b","messages":[` +
				`{"role":"user","content":"Hi"},{"role":"","content":"there"}]}`,
			param: "messages[1].role",
			cite:  "bank CH-REG-006",
		},
		{
			name:  "negative_max_tokens",
			route: "/v1/chat/completions",
			body: `{"model":"llama-3.1-70b","messages":[{"role":"user","content":"Hi"}],` +
				`"max_tokens":-1}`,
			param: "max_tokens",
			cite:  "bank CH-REG-007",
		},
		{
			name:  "zero_max_tokens_streaming",
			route: "/v1/chat/completions",
			body: `{"model":"llama-3.1-70b","messages":[{"role":"user","content":"Hi"}],` +
				`"stream":true,"max_tokens":0}`,
			param: "max_tokens",
			cite:  "bank CH-REG-004",
		},
		{
			name:  "non_ascii_model_name",
			route: "/v1/chat/completions",
			body: `{"model":"llama-3.1-70b-中文",` +
				`"messages":[{"role":"user","content":"Hi"}]}`,
			param: "model",
			cite:  "bank CH-REG-002",
		},
		{
			name: "excessively_long_model_name",
			body: `{"model":"` + strings.Repeat("a", 330) + `",` +
				`"messages":[{"role":"user","content":"Hi"}]}`,
			route: "/v1/chat/completions",
			param: "model",
			cite:  "bank CH-REG-010",
		},
		{
			name:  "path_traversal_model_name",
			route: "/v1/chat/completions",
			body:  `{"model":"../../etc/passwd","messages":[{"role":"user","content":"Hi"}]}`,
			param: "model",
			cite:  "bank CH-SEC-007",
		},
		{
			// One byte over the cap. Paired with
			// "model_name_exactly_at_the_cap" in validRequests(), this pins
			// the boundary from both sides so the ceiling cannot drift
			// silently in either direction.
			name: "model_name_one_over_the_cap",
			body: `{"model":"` + strings.Repeat("m", 256) + `",` +
				`"messages":[{"role":"user","content":"Hi"}]}`,
			route: "/v1/chat/completions",
			param: "model",
			cite:  "modelNameMaxLen boundary",
		},
	}
}

func completionsCases() []invalidCase {
	return []invalidCase{
		{
			name:  "negative_max_tokens",
			route: "/v1/completions",
			body:  `{"model":"llama-3.1-70b","prompt":"Hi","max_tokens":-5}`,
			param: "max_tokens",
			cite:  "OpenAI CreateCompletionRequest minimum:0",
		},
		{
			name:  "path_traversal_model_name",
			route: "/v1/completions",
			body:  `{"model":"../../etc/passwd","prompt":"Hi"}`,
			param: "model",
			cite:  "bank CH-SEC-007 (same rule, sibling endpoint)",
		},
	}
}

func messagesCases() []invalidCase {
	return []invalidCase{
		{
			name:  "empty_messages_array",
			route: "/v1/messages",
			body:  `{"model":"claude-sonnet-4-20250514","messages":[],"max_tokens":16}`,
			param: "messages",
			cite:  "Anthropic Messages requires at least one message",
		},
		{
			name:  "negative_max_tokens",
			route: "/v1/messages",
			body: `{"model":"claude-sonnet-4-20250514",` +
				`"messages":[{"role":"user","content":"Hi"}],"max_tokens":-1}`,
			param: "max_tokens",
			cite:  "Anthropic Messages max_tokens minimum:0",
		},
		{
			name:  "non_ascii_model_name",
			route: "/v1/messages",
			body: `{"model":"claude-中文",` +
				`"messages":[{"role":"user","content":"Hi"}],"max_tokens":16}`,
			param: "model",
			cite:  "bank CH-REG-002 (same rule, sibling endpoint)",
		},
		{
			// Anthropic, verbatim: "A `max_tokens: 0` request is rejected
			// with an `invalid_request_error` if any of the following are
			// set: `stream: true` ...". A zero token budget produces no
			// stream, so the combination is nonsense even though each half
			// is individually legal.
			name:  "zero_max_tokens_with_stream",
			route: "/v1/messages",
			body: `{"model":"claude-sonnet-4-20250514",` +
				`"messages":[{"role":"user","content":"Hi"}],` +
				`"max_tokens":0,"stream":true}`,
			param: "max_tokens",
			cite:  "Anthropic prompt-caching: max_tokens 0 excludes stream:true",
		},
	}
}

// assertRejected applies the polarity switch to one invalid request.
//
// GREEN: the status is 400, the body names the offending param, and the
// body does not disclose an internal address.
// RED:   the status is NOT 400, because the pre-fix artifact forwarded the
// request to dispatch and answered with whatever the backend said.
func assertRejected(t *testing.T, tc invalidCase, status int, body string) {
	t.Helper()
	if redMode() {
		if status == http.StatusBadRequest {
			t.Errorf("%s %s [%s]: status = 400 on the PRE-FIX artifact; "+
				"the reproduction is not reproducing anything",
				tc.route, tc.name, tc.cite)
		}
		return
	}
	if status != http.StatusBadRequest {
		t.Errorf("%s %s [%s]: status = %d, want 400\nbody: %s",
			tc.route, tc.name, tc.cite, status, body)
		return
	}
	var got struct {
		Error struct {
			Message string  `json:"message"`
			Type    string  `json:"type"`
			Param   *string `json:"param"`
			Code    *string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("%s %s: response is not an error envelope: %v\nbody: %s",
			tc.route, tc.name, err, body)
	}
	if got.Error.Type != "invalid_request_error" {
		t.Errorf("%s %s: error.type = %q, want %q",
			tc.route, tc.name, got.Error.Type, "invalid_request_error")
	}
	if got.Error.Param == nil || *got.Error.Param != tc.param {
		t.Errorf("%s %s: error.param = %v, want %q — a client cannot fix "+
			"a field the error does not name",
			tc.route, tc.name, got.Error.Param, tc.param)
	}
	if got.Error.Message == "" {
		t.Errorf("%s %s: error.message is empty", tc.route, tc.name)
	}
	if got.Error.Code == nil || *got.Error.Code == "" {
		t.Errorf("%s %s: error.code is absent; a client library switching on "+
			"code sees nothing to switch on", tc.route, tc.name)
	}
	assertNoInternalAddress(t, tc.route+" "+tc.name, body)
}

// assertNoInternalAddress is the disclosure half, applied to every body this
// suite sees. Without it the status half could be satisfied by a 400 whose
// body still names the backend.
func assertNoInternalAddress(t *testing.T, where, body string) {
	t.Helper()
	if m := internalAddressPattern.FindString(body); m != "" {
		t.Errorf("%s: client-visible body discloses internal topology (%q)\nbody: %s",
			where, m, body)
	}
}

func TestRequestValidation_ChatCompletions(t *testing.T) {
	h := gateway.HandleChatCompletions(exhaustedChain(), nil, nil)
	for _, tc := range chatCases() {
		t.Run(tc.name, func(t *testing.T) {
			status, body := postRaw(t, h, tc.route, tc.body)
			assertRejected(t, tc, status, body)
		})
	}
}

func TestRequestValidation_Completions(t *testing.T) {
	h := gateway.HandleCompletions(exhaustedChain())
	for _, tc := range completionsCases() {
		t.Run(tc.name, func(t *testing.T) {
			status, body := postRaw(t, h, tc.route, tc.body)
			assertRejected(t, tc, status, body)
		})
	}
}

func TestRequestValidation_AnthropicMessages(t *testing.T) {
	h := gateway.HandleMessages(exhaustedChain())
	for _, tc := range messagesCases() {
		t.Run(tc.name, func(t *testing.T) {
			status, body := postRaw(t, h, tc.route, tc.body)
			assertRejected(t, tc, status, body)
		})
	}
}

// --- Positive controls -----------------------------------------------------
//
// Without these, every case above could be satisfied by a validator that
// rejects everything. These prove the validator lets a correct request
// THROUGH to dispatch — the request reaches the Completer and comes back
// with the Completer's answer (503 from an exhausted chain), never 400.

// validRequests are the shapes a real client legitimately sends, including
// the ones the deliberate-non-coverage list above promises not to reject.
func validRequests() []struct {
	name  string
	route string
	body  string
	h     func() gin.HandlerFunc
} {
	chat := func() gin.HandlerFunc { return gateway.HandleChatCompletions(exhaustedChain(), nil, nil) }
	comp := func() gin.HandlerFunc { return gateway.HandleCompletions(exhaustedChain()) }
	msgs := func() gin.HandlerFunc { return gateway.HandleMessages(exhaustedChain()) }

	return []struct {
		name  string
		route string
		body  string
		h     func() gin.HandlerFunc
	}{
		{
			name:  "plain_chat_request",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"llama-3.1-70b","messages":[{"role":"user","content":"Hi"}]}`,
		},
		{
			// The model-name charset must admit the identifier shapes this
			// project's own configuration uses: an org/name pair, an
			// Ollama tag, and a registry-qualified name.
			name:  "hf_style_model_name",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"meta-llama/Llama-3.3-70B-Instruct-Turbo-Free",` +
				`"messages":[{"role":"user","content":"Hi"}]}`,
		},
		{
			name:  "ollama_tag_model_name",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"llama3:8b","messages":[{"role":"user","content":"Hi"}]}`,
		},
		{
			name:  "registry_qualified_model_name",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"ghcr.io/ggml-org/llama.cpp:server-cuda",` +
				`"messages":[{"role":"user","content":"Hi"}]}`,
		},
		{
			name:  "digest_pinned_model_name",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"llama3@sha256:0123456789abcdef",` +
				`"messages":[{"role":"user","content":"Hi"}]}`,
		},
		{
			// Deliberate non-coverage: an assistant turn carrying tool_calls
			// has empty content by design. Requiring non-empty content would
			// reject a correct agent conversation.
			name:  "assistant_tool_call_with_empty_content",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"llama-3.1-70b","messages":[` +
				`{"role":"user","content":"list files"},` +
				`{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function",` +
				`"function":{"name":"Bash","arguments":"{}"}}]},` +
				`{"role":"tool","tool_call_id":"c1","content":"a.txt"}]}`,
		},
		{
			// Deliberate non-coverage: the role ENUM is not enforced, so a
			// role this project does not model (OpenAI's `developer`) still
			// passes.
			name:  "developer_role_not_in_project_enum",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"llama-3.1-70b","messages":[` +
				`{"role":"developer","content":"be terse"},` +
				`{"role":"user","content":"Hi"}]}`,
		},
		{
			// Deliberate non-coverage: `n` is out of OpenAI's declared
			// 1..128 range but is not validated here.
			name:  "out_of_range_n_is_not_rejected",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"llama-3.1-70b","messages":[{"role":"user","content":"Hi"}],"n":9999}`,
		},
		{
			// A well-formed model name this server does not serve is NOT a
			// 400 — unknown-resource is 404 territory and belongs to
			// /v1/models/:id, not to request validation.
			name:  "wellformed_but_unknown_model",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"totally-unknown-model-v9","messages":[{"role":"user","content":"Hi"}]}`,
		},
		{
			// A Bedrock foundation-model ARN: colons, slashes, dots.
			name:  "bedrock_arn_model_name",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"arn:aws:bedrock:us-east-1:123456789012:` +
				`foundation-model/anthropic.claude-3-5-sonnet-20240620-v1:0",` +
				`"messages":[{"role":"user","content":"Hi"}]}`,
		},
		{
			name:  "vertex_publisher_path_model_name",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"publishers/google/models/gemini-1.5-pro",` +
				`"messages":[{"role":"user","content":"Hi"}]}`,
		},
		{
			// A relative --model path is literally what llama.cpp and vLLM
			// echo back; a bare "." segment must NOT be read as traversal.
			name:  "relative_path_model_name",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"./models/Meta-Llama-3.1-8B-Instruct.Q4_K_M.gguf",` +
				`"messages":[{"role":"user","content":"Hi"}]}`,
		},
		{
			name:  "leading_slash_model_name",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"/models/mistral-7b.gguf",` +
				`"messages":[{"role":"user","content":"Hi"}]}`,
		},
		{
			name:  "openai_finetune_model_name",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"ft:gpt-4o-2024-08-06:my-org:custom-suffix:AbC123",` +
				`"messages":[{"role":"user","content":"Hi"}]}`,
		},
		{
			// The longest realistic identifier class: a HuggingFace triple
			// of repo owner / repo name / GGUF filename. This one measures
			// 145 bytes — it is the concrete case a 128-byte ceiling
			// wrongly rejected, which is why the ceiling is 255.
			name:  "long_hf_gguf_triple_model_name",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"mradermacher/DeepSeek-R1-Distill-Qwen-32B-Uncensored-` +
				`abliterated-v2-i1-GGUF/DeepSeek-R1-Distill-Qwen-32B-Uncensored-` +
				`abliterated-v2.i1-Q4_K_M.gguf",` +
				`"messages":[{"role":"user","content":"Hi"}]}`,
		},
		{
			// Exactly at the cap. Its sibling at cap+1 is a rejection case
			// in chatCases(), so the boundary is pinned from both sides.
			name:  "model_name_exactly_at_the_cap",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"` + strings.Repeat("m", 255) + `",` +
				`"messages":[{"role":"user","content":"Hi"}]}`,
		},
		{
			name:  "chat_positive_max_tokens",
			route: "/v1/chat/completions", h: chat,
			body: `{"model":"llama-3.1-70b","messages":[{"role":"user","content":"Hi"}],` +
				`"max_tokens":256}`,
		},
		{
			name:  "plain_completions_request",
			route: "/v1/completions", h: comp,
			body: `{"model":"llama-3.1-70b","prompt":"Hi","max_tokens":16}`,
		},
		{
			// Legacy /v1/completions declares max_tokens minimum:0, and 0 is
			// a real use (score a prompt with logprobs/echo, generate
			// nothing). The chat endpoint's stricter floor must not leak
			// across to here.
			name:  "completions_zero_max_tokens_is_legal",
			route: "/v1/completions", h: comp,
			body: `{"model":"llama-3.1-70b","prompt":"Hi","max_tokens":0}`,
		},
		{
			name:  "plain_messages_request",
			route: "/v1/messages", h: msgs,
			body: `{"model":"claude-sonnet-4-20250514",` +
				`"messages":[{"role":"user","content":"Hi"}],"max_tokens":16}`,
		},
		{
			// Anthropic documents max_tokens:0 as prompt-cache pre-warming.
			// Rejecting it would break a documented Anthropic feature. This
			// is also the negative half of the zero_max_tokens_with_stream
			// rejection: 0 alone is fine, 0 WITH stream is not.
			name:  "messages_zero_max_tokens_is_legal",
			route: "/v1/messages", h: msgs,
			body: `{"model":"claude-sonnet-4-20250514",` +
				`"messages":[{"role":"user","content":"Hi"}],"max_tokens":0}`,
		},
	}
}

// TestRequestValidation_ValidRequestsStillDispatch is the anti-over-rejection
// control. Each request here MUST reach the Completer, which means it must
// come back with the exhausted chain's 503 — never a 400. A validator that
// rejected any of these would be a worse regression than the 500s this
// change removes.
func TestRequestValidation_ValidRequestsStillDispatch(t *testing.T) {
	for _, tc := range validRequests() {
		t.Run(tc.name, func(t *testing.T) {
			status, body := postRaw(t, tc.h(), tc.route, tc.body)
			if status == http.StatusBadRequest {
				t.Fatalf("%s %s: a legitimate request was REJECTED with 400 — "+
					"over-rejection is a worse regression than the status it fixes\nbody: %s",
					tc.route, tc.name, body)
			}
			if !redMode() && status != http.StatusServiceUnavailable {
				t.Errorf("%s %s: status = %d, want 503 (the exhausted chain's "+
					"answer, proving the request reached dispatch)\nbody: %s",
					tc.route, tc.name, status, body)
			}
			assertNoInternalAddress(t, tc.route+" "+tc.name, body)
		})
	}
}

// --- Upstream-error disclosure --------------------------------------------

// TestUpstreamError_DoesNotDiscloseInternalAddress drives every handler
// through a provider that fails with the real dial error and asserts the
// address never reaches the client.
//
// RED asserts the address IS present (reproducing the leak on the pre-fix
// artifact); GREEN asserts it is absent AND that the client is still told
// the upstream failed, so the fix is a redaction and not a silencing.
func TestUpstreamError_DoesNotDiscloseInternalAddress(t *testing.T) {
	cases := []struct {
		name  string
		route string
		h     gin.HandlerFunc
		body  string
	}{
		{
			name: "chat_completions", route: "/v1/chat/completions",
			h:    gateway.HandleChatCompletions(dialFailureCompleter{}, nil, nil),
			body: `{"model":"llama-3.1-70b","messages":[{"role":"user","content":"Hi"}]}`,
		},
		{
			name: "chat_completions_streaming", route: "/v1/chat/completions",
			h: gateway.HandleChatCompletions(dialFailureCompleter{}, nil, nil),
			body: `{"model":"llama-3.1-70b","messages":[{"role":"user","content":"Hi"}],` +
				`"stream":true}`,
		},
		{
			name: "completions", route: "/v1/completions",
			h:    gateway.HandleCompletions(dialFailureCompleter{}),
			body: `{"model":"llama-3.1-70b","prompt":"Hi"}`,
		},
		{
			name:  "anthropic_messages",
			route: "/v1/messages",
			h:     gateway.HandleMessages(dialFailureCompleter{}),
			body: `{"model":"claude-sonnet-4-20250514",` +
				`"messages":[{"role":"user","content":"Hi"}],"max_tokens":16}`,
		},
		{
			name:  "anthropic_messages_streaming",
			route: "/v1/messages",
			h:     gateway.HandleMessages(dialFailureCompleter{}),
			body: `{"model":"claude-sonnet-4-20250514",` +
				`"messages":[{"role":"user","content":"Hi"}],"max_tokens":16,"stream":true}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := postRaw(t, tc.h, tc.route, tc.body)
			if redMode() {
				if !internalAddressPattern.MatchString(body) {
					t.Errorf("%s: the PRE-FIX artifact did not leak an address; "+
						"the reproduction is not reproducing anything\nbody: %s",
						tc.name, body)
				}
				return
			}
			assertNoInternalAddress(t, tc.name, body)
			// Redaction must not become silence: the caller is still owed a
			// truthful answer. dialFailureCompleter returns an ORDINARY
			// provider fault (not an exhausted chain), so the honest status
			// is 500 and the message must be the provider-failed text.
			if status != http.StatusInternalServerError {
				t.Errorf("%s: status = %d, want 500 — a provider that WAS reached "+
					"and faulted is an internal error, not an availability condition",
					tc.name, status)
			}
			var got struct {
				Error struct {
					Message string `json:"message"`
					Type    string `json:"type"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatalf("%s: response is not an error envelope: %v\nbody: %s",
					tc.name, err, body)
			}
			if got.Error.Type != "server_error" {
				t.Errorf("%s: error.type = %q, want %q",
					tc.name, got.Error.Type, "server_error")
			}
			if strings.TrimSpace(got.Error.Message) == "" {
				t.Errorf("%s: error.message is empty; the caller must still be told "+
					"the upstream failed, not merely told nothing", tc.name)
			}
		})
	}
}

// TestUpstreamError_DetailIsPreservedForTheOperator proves the fix REDACTS
// rather than DELETES: the full provider error, address and all, must still
// be obtainable on the server side. Without this, "no leak" could be
// achieved by throwing the diagnosis away, which trades a disclosure defect
// for an operability one.
func TestUpstreamError_DetailIsPreservedForTheOperator(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: RED_MODE reproduces the pre-fix artifact, where the " +
			"server-side redaction helper does not yet exist")
	}
	logged := gateway.UpstreamErrorLogDetail(errDialFailure)
	if !strings.Contains(logged, "50052") {
		t.Errorf("operator-facing detail lost the endpoint: %q", logged)
	}
	if !strings.Contains(logged, "connection refused") {
		t.Errorf("operator-facing detail lost the cause: %q", logged)
	}
}

// TestUpstreamError_WebSocketDoesNotDiscloseInternalAddress guards the sixth
// error site, which the five HTTP cases above cannot reach.
//
// This was historically the WORST of the six: the WebSocket frame carried
// err.Error() verbatim with no redaction AND no translation, so a WS client
// received the backend address in the clearest possible form. Measured
// against the pre-fix server:
//
//	pre-fix -> {"error":"all providers exhausted, last error: llamacpp: send
//	            request: Post \"http://localhost:50052/v1/chat/completions\":
//	            dial tcp 127.0.0.1:50052: connect: connection refused"}
//	fixed   -> {"error":"no model-serving backend is currently available to
//	            serve this request; retry shortly"}
//
// Unlike the HTTP sites it drives a REAL gorilla WebSocket over a real
// httptest server, because the redaction happens on the frame-write path
// rather than through gin's renderer and a handler-level test would not
// exercise it.
func TestUpstreamError_WebSocketDoesNotDiscloseInternalAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ws", gateway.HandleWebSocket(dialFailureCompleter{}))
	srv := httptest.NewServer(r)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	req := `{"model":"llama-3.1-70b","messages":[{"role":"user","content":"Hi"}]}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(req)); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	frame := string(raw)

	if redMode() {
		if !internalAddressPattern.MatchString(frame) {
			t.Errorf("the PRE-FIX artifact did not leak an address on the "+
				"WebSocket path; the reproduction is not reproducing anything\nframe: %s",
				frame)
		}
		return
	}

	assertNoInternalAddress(t, "GET /ws", frame)

	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("frame is not a JSON object: %v\nframe: %s", err, frame)
	}
	// Redaction must not become silence here either: the client is still
	// owed the truthful "the provider failed" answer, and it must be the
	// TRANSLATED text rather than a raw Go error — the WS path had no
	// translation at all before this change.
	want := gateway.UpstreamErrorClientText("en", errDialFailure)
	if got["error"] != want {
		t.Errorf("frame error = %q, want the translated upstream message %q",
			got["error"], want)
	}
}
