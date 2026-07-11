# Dual-Wire Facade: OpenAI-Compatible + Anthropic-Compatible Endpoints

Companion to the existing `docs/courses/02-api-deep-dive/lesson-01-openai-compat.md`
and `lesson-02-anthropic-compat.md`, and [`api-reference.md`](api-reference.md).
Verified against the actual Go source (`internal/gateway/openai.go`,
`internal/gateway/anthropic.go`, `internal/gateway/router.go`, `pkg/api/*.go`)
against the current upstream OpenAI and Anthropic wire formats.

## What this is

`internal/gateway/router.go: RegisterRoutes` mounts **both** wire formats
under the same `/v1` prefix, on the same Gin engine, sharing the **same**
underlying completion backend (`opts.Brain`, a `Completer` — either
`*brain.Brain` or a `*fallback.Chain`):

```go
// OpenAI-compatible endpoints
v1.POST("/chat/completions", HandleChatCompletions(opts.Brain, opts.ToolManager, opts.RAGHook))
v1.POST("/completions", HandleCompletions(opts.Brain))
v1.GET("/models", HandleListModels(opts.ModelBrain))
v1.GET("/models/:id", HandleGetModel(opts.ModelBrain))
v1.POST("/embeddings", HandleEmbeddings(opts.ModelBrain, opts.Embedder))

// Anthropic-compatible endpoints
v1.POST("/messages", HandleMessages(opts.Brain))
```

Both wires convert their respective request into ONE shared internal
representation, `types.InternalChatRequest`, run it through the SAME `Brain`
/ fallback-chain routing+provider logic, and convert the result back to
each wire's own response shape. This means: whichever LLM provider actually
serves a request (local llama.cpp, OpenAI, Anthropic, or any other
configured provider via `internal/brain`) is decided by HelixLLM's routing
policy — **not** by which of the two facade endpoints the client called.
A client can call `POST /v1/messages` (Anthropic shape) and have the
request actually served by a local llama.cpp model, or call
`POST /v1/chat/completions` (OpenAI shape) and have it served by Claude —
both wires are pure protocol adapters over the same backend.

Auth, rate limiting, and security headers are shared middleware applied to
the whole `/v1` group (`gwmw.APIKeyAuth`, `gwmw.RateLimit`,
`gwmw.SecurityHeaders`), so both wires are protected identically.

## OpenAI-compatible wire: `POST /v1/chat/completions`

Request (`pkg/api.ChatCompletionRequest`, verified against
`internal/gateway/openai.go: HandleChatCompletions`):

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $HELIX_API_KEY" \
  -d '{
    "model": "llama-3.1-70b",
    "messages": [{"role": "user", "content": "What is 2+2?"}],
    "stream": false
  }'
```

- Standard OpenAI request fields (`model`, `messages`, `tools`,
  `tool_choice`, `stream`, `temperature`, …) are accepted and bound via
  `c.ShouldBindJSON(&req)`.
- **Tool calls are real, both directions.** Incoming OpenAI-shape
  `messages[].tool_calls` / `tool_call_id` are converted to
  `types.InternalToolCall` (`internal/gateway/openai.go` message conversion,
  ~line 764-783). Outgoing responses run through
  `NormalizeToolCalls(resp)` (`internal/gateway/openai.go:320`) — a bridge
  that converts models which emit tool calls as XML/JSON-in-text (instead of
  native OpenAI `tool_calls`, e.g. some Qwen3 variants on Chutes) into
  proper `tool_calls` entries, plus a `convertRespondToolCall` step handling
  an internally-injected "respond" tool pattern. `finish_reason` is set to
  `"tool_calls"` when the normalized response carries tool calls.
- Streaming (`stream: true`) emits standard OpenAI SSE
  (`data: {...}\n\n` chunks, `object: "chat.completion.chunk"`, terminated
  by a literal `data: [DONE]\n\n` line) — read `internal/gateway/openai.go:
  streamChatCompletions` / `HandleChatCompletions`'s streaming branch
  directly for the exact per-chunk field shape when implementing a client
  against this.
- `GET /v1/models` / `GET /v1/models/:id` return the real model list
  discovered via `Brain.Models()` when `ModelBrain` is configured
  (`HandleListModels`), or a small hardcoded fallback list
  (`llama-3.1-70b`, `gpt-4o`, `claude-sonnet-4-20250514` —
  `internal/gateway/openai.go: hardcodedModels`) when no `ModelBrain` is
  wired (development mode). **Do not treat the hardcoded list as a
  capability claim** — it exists only so the endpoint returns something
  in an unconfigured dev server; real deployments always have `ModelBrain`
  set (verify wiring in `cmd/helixllm/main.go` if the returned model list
  looks unexpectedly short).
- `POST /v1/embeddings` delegates to the configured `knowledge.Embedder`
  (`HandleEmbeddings`) — see [`rag-qdrant-reranker.md`](rag-qdrant-reranker.md)
  for which embedder is actually active by default (hash-based, not
  semantic, unless `HELIX_EMBEDDING_PROVIDER` is overridden — a startup
  WARNING now fires when the hash embedder is in use).

## Anthropic-compatible wire: `POST /v1/messages`

Request (`pkg/api.MessageRequest`, verified against
`internal/gateway/anthropic.go: HandleMessages`):

```bash
curl -s http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $HELIX_API_KEY" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "What is 2+2?"}]
  }'
```

Response shape mirrors Anthropic's real `MessageResponse`
(`id`, `type: "message"`, `role: "assistant"`, `content: []ContentBlock`,
`model`, `stop_reason`, `usage.{input_tokens,output_tokens}`), and streaming
(`stream: true`) emits the real Anthropic SSE event sequence — verified
against upstream (see Sources footer) as accurate in this implementation:
`message_start` → `content_block_start` → `content_block_delta` (repeated,
`delta.type: "text_delta"`) → `content_block_stop` → `message_delta`
(carries `stop_reason` + `usage.output_tokens`) → `message_stop`, each as
a named `event: <type>` / `data: <json>` block
(`internal/gateway/anthropic.go: streamBrainMessages`). This matches the
current upstream Anthropic Messages API event sequence exactly.

### Tool use / structured content — wired end-to-end

`pkg/api.MessageRequest` declares `Tools []AnthropicTool` and
`ToolChoice interface{}` fields matching the real Anthropic wire schema, and
(as of `submodules/helix_llm` HEAD `e2ce163`, wave-2 fix `8e18b0c`)
`anthropicToInternal` (`internal/gateway/anthropic.go`) now converts both
into the shared `types.InternalChatRequest` before it reaches the `Brain`:

```go
if len(req.Tools) > 0 {
    internal.Tools = anthropicToolsToInternal(req.Tools)
}
if req.ToolChoice != nil {
    internal.ToolChoice = anthropicToolChoiceToInternal(req.ToolChoice)
}
```

- `anthropicToolsToInternal` converts Anthropic's tool schema into the same
  OpenAI-function-shaped `InternalTool` every downstream OpenAI-compatible
  provider already consumes.
- `anthropicToolChoiceToInternal` maps Anthropic's `tool_choice` shape
  (`auto` / `any` / `none` / `{"type": "tool", "name": "..."}`) to the
  internal equivalent (`auto` / `required` / `none` /
  `{"type": "function", ...}`).
- `AnthropicMessage.Content` (typed `interface{}` because Anthropic allows
  either a plain string or an array of content blocks) is now fully parsed
  for the block-array case: `tool_use` blocks in assistant history become
  `ToolCalls`; `tool_result` blocks become their own `RoleTool` message
  keyed by `ToolCallID` — the multi-turn agentic-loop half of the round
  trip. Plain-string `content` continues to work unchanged.
- On the response side, `internalToAnthropic` emits a real `"tool_use"`
  content block per `resp.Message.ToolCalls` entry and sets `stop_reason`
  via the `anthropicStopReason` helper
  (`tool_calls`/has-`ToolCalls` -> `tool_use`, `length` -> `max_tokens`,
  `stop`/`""` -> `end_turn`, passthrough otherwise).

This brings the Anthropic wire to parity with the OpenAI wire for tool
calling — a client sending `tools`/`tool_choice` on `POST /v1/messages`
now has those definitions genuinely reach the backend LLM, and a
`tool_use` response round-trips correctly through `tool_result` on the
next turn.

## Sources verified 2026-07-11 (code re-cross-checked same date against `submodules/helix_llm` HEAD `e2ce163`, post wave-2 fix `8e18b0c` "wire tools/tool_choice through the Anthropic /v1/messages facade"):
- https://platform.claude.com/docs/en/api/messages (Anthropic Messages API — request/response shape, SSE event sequence, required headers; redirected from https://docs.anthropic.com/en/api/messages)
- https://raw.githubusercontent.com/openai/openai-openapi/master/openapi.yaml (OpenAI Chat Completions request/response schema + SSE `[DONE]` sentinel — used because `https://platform.openai.com/docs/api-reference/chat/create` returned HTTP 403 to automated fetch; the official OpenAPI spec is the authoritative machine-readable source and was used instead, per §11.4.99's "seek secondary authoritative sources" clause)
