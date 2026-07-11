# ANTHROPIC-WIRE-DROPS-TOOLS — Wave-2 fix, RED→GREEN, production-path proof

**Finding (prior docs-V&V stream):** the Anthropic `/v1/messages` facade
accepts `tools`/`tool_choice` in its schema (`api.MessageRequest.Tools` /
`ToolChoice`) but `anthropicToInternal` silently drops them — the OpenAI
wire (`openAIToInternal`/`internalToOpenAI`) does tools end-to-end, the
Anthropic wire did not.

**Verification (§11.4.6):** CONFIRMED TRUE by direct source read.
- `internal/gateway/anthropic.go` `anthropicToInternal` only ever read
  `req.Model`, `req.Messages` (string-content only), `req.MaxTokens`,
  `req.Stream`, `req.Temperature` — `req.Tools` and `req.ToolChoice` were
  never referenced anywhere in the function.
- `internalToAnthropic` only ever emitted a single `{"type":"text",...}`
  content block from `resp.Message.Content` — `resp.Message.ToolCalls` was
  never referenced, so even a provider that DID return a tool call had it
  silently discarded on the way back to the Anthropic-format client.
- By contrast `internal/gateway/openai.go`'s `openAIToInternal` (lines
  ~787-806) explicitly passes `req.Tools`/`req.ToolChoice` through, and
  `internalToOpenAI` (lines ~931-950) explicitly emits `tool_calls` —
  confirming the OpenAI/Anthropic asymmetry described in the finding.
- `pkg/types/types.go` already fully supports tools end-to-end
  (`InternalTool`, `InternalToolCall`, `InternalChatRequest.Tools`/
  `.ToolChoice`, `InternalMessage.ToolCalls`) — the schema and the internal
  plumbing were both tool-capable; only the Anthropic-facing conversion
  functions were the gap. Tools are therefore squarely IN-SCOPE for this
  facade (the schema itself declares the fields) — the fix wires them
  through rather than making the drop honest-reject.

## Fix

`internal/gateway/anthropic.go`:
1. `anthropicToInternal` now converts `req.Tools` (Anthropic
   `{name, description, input_schema}`) to OpenAI-function-shaped
   `types.InternalTool` (the exact shape every downstream OpenAI-compatible
   provider — llama.cpp, openai_compat — already consumes), and converts
   `req.ToolChoice` via `anthropicToolChoiceToInternal`.
2. `anthropicToolChoiceToInternal` maps Anthropic's tool_choice vocabulary to
   OpenAI's: `{"type":"auto"}`→`"auto"`, `{"type":"any"}`→`"required"`,
   `{"type":"none"}`→`"none"`, `{"type":"tool","name":X}`→
   `{"type":"function","function":{"name":X}}`.
3. Message content that is a content-block array (not a bare string) is now
   parsed: `tool_use` blocks in assistant history become `ToolCalls` on the
   assistant `InternalMessage`; `tool_result` blocks become their own
   `RoleTool` `InternalMessage` keyed by `ToolCallID` — the multi-turn
   agentic-loop half of the round trip, not just the first hop.
4. `internalToAnthropic` now emits a real `"tool_use"` content block per
   `resp.Message.ToolCalls` entry (with `Input` decoded from the internal
   JSON-string `Arguments`), and sets `stop_reason` to `"tool_use"` via the
   new `anthropicStopReason` helper (also correctly maps `"length"`→
   `"max_tokens"`, `"stop"`/`""`→`"end_turn"`, and passes through
   already-Anthropic-native values unchanged).
5. Zero regression for the plain-text (no-tools) path — verified by
   `TestInternalToAnthropic_PlainText_Unchanged`,
   `TestInternalToAnthropic_EmptyResponse_Unchanged`,
   `TestAnthropicToInternal_PlainStringContent_Unchanged`,
   `TestAnthropicToInternal_NoTools_NilPassthrough`, and the full pre-existing
   `anthropic_test.go` suite (still 100% green).

## RED (§11.4.115) — reproduce-first on the pre-fix artifact

Reverting `internal/gateway/anthropic.go` (`git stash`) and running the new
tests produced a build failure — the fix's own functions did not exist on
the pre-fix artifact, proving the wiring mechanism (and the multi-turn
content-block parsing, and the tool_use emission, and the stop_reason
mapping) was absent entirely:

```
internal/gateway/anthropic_internal_test.go:105:11: undefined: anthropicToolChoiceToInternal
internal/gateway/anthropic_internal_test.go:275:10: undefined: anthropicStopReason
FAIL	github.com/HelixDevelopment/HelixLLM/internal/gateway [build failed]
```

## GREEN — same test source, fix restored

```
=== RUN   TestAnthropicToInternal_ToolsAndToolChoice_PassThrough
--- PASS: TestAnthropicToInternal_ToolsAndToolChoice_PassThrough (0.00s)
=== RUN   TestAnthropicToolChoiceToInternal_AllVariants
    --- PASS: TestAnthropicToolChoiceToInternal_AllVariants/auto (0.00s)
    --- PASS: TestAnthropicToolChoiceToInternal_AllVariants/any->required (0.00s)
    --- PASS: TestAnthropicToolChoiceToInternal_AllVariants/none (0.00s)
    --- PASS: TestAnthropicToolChoiceToInternal_AllVariants/tool->function (0.00s)
    --- PASS: TestAnthropicToolChoiceToInternal_AllVariants/unknown-type-defaults-auto (0.00s)
    --- PASS: TestAnthropicToolChoiceToInternal_AllVariants/already-openai-shaped-string (0.00s)
=== RUN   TestAnthropicToInternal_ToolResultContentBlock_MapsToToolRole
--- PASS: TestAnthropicToInternal_ToolResultContentBlock_MapsToToolRole (0.00s)
=== RUN   TestInternalToAnthropic_ToolUse_EmitsToolUseBlockAndStopReason
--- PASS: TestInternalToAnthropic_ToolUse_EmitsToolUseBlockAndStopReason (0.00s)
=== RUN   TestAnthropicStopReason_MapsAllKnownValues
--- PASS: TestAnthropicStopReason_MapsAllKnownValues (0.00s)
=== RUN   TestMessages_WithBrain_ToolCallRoundTrip
--- PASS: TestMessages_WithBrain_ToolCallRoundTrip (0.00s)
PASS
ok  	github.com/HelixDevelopment/HelixLLM/internal/gateway	0.034s
```

Regression guard (§11.4.135): all of these are now permanent tests in
`internal/gateway/anthropic_internal_test.go` (unexported-function level) and
`internal/gateway/anthropic_test.go` (full-facade HTTP level), running on
every `go test ./...`.

## §1.1 load-bearing mutation

`TestAnthropicStopReason_MapsAllKnownValues` and
`TestAnthropicToolChoiceToInternal_AllVariants` are closed-set truth tables —
mutating any branch of `anthropicStopReason` or `anthropicToolChoiceToInternal`
(e.g. dropping the `hasToolCalls || finishReason == "tool_calls"` check, or
mis-mapping `"any"`→`"auto"` instead of `"required"`) flips at least one
subtest from PASS to FAIL. The runtime-RED experiment for finding #1
(pipeline.go) already demonstrated the same technique is load-bearing against
this defect class; the compile-failure RED above is the equivalent proof
for this fix (the mutation IS "does not exist yet").

## LIVE production-path proof — real Qwen3-Coder tool call, real facade

`internal/gateway/anthropic_tools_live_test.go` ::
`TestAnthropicMessages_ToolCallRoundTrip_LiveCoder`, run with
`HELIX_LIVE_CODER_TOOLS_TEST=true` against the live coder llama.cpp server
(`helixllm-coder`, Qwen3-Coder-30B, `:18434` — READ-ONLY throughout, never
restarted or reconfigured per §11.4.119/§11.4.122; health-checked before and
after). Drives a REAL `gin` HTTP server wired with the REAL, unmodified
`gateway.HandleMessages`, backed by a REAL `brain.Brain` pointed at the live
coder — the exact production facade under test, not a reimplementation.

Terminal output (this run):

```
=== RUN   TestAnthropicMessages_ToolCallRoundTrip_LiveCoder
    run_id=anthropic_tools_wave2_20260711T155528Z coder_base=http://localhost:18434 (read-only, never restarted §11.4.119/§11.4.122)
    discovered live coder model: /models/Qwen3-Coder-30B-A3B-Instruct-Q4_K_M.gguf
    real /v1/messages response:
    {
      "id": "chatcmpl-SY6FirLknxrzfdN7O2BTXRGrppCWgQ9a",
      "type": "message",
      "role": "assistant",
      "content": [
        {
          "type": "tool_use",
          "id": "2iYNaJVBQGk4zwl4xmlG1Nl6UPXgWUhl",
          "name": "get_weather",
          "input": { "city": "Paris" }
        }
      ],
      "model": "/models/Qwen3-Coder-30B-A3B-Instruct-Q4_K_M.gguf",
      "stop_reason": "tool_use",
      "stop_sequence": null,
      "usage": { "input_tokens": 373, "output_tokens": 22 }
    }
    RESULT: PASS — a real tool call from the live coder round-tripped through POST /v1/messages as a genuine Anthropic tool_use content block (id=2iYNaJVBQGk4zwl4xmlG1Nl6UPXgWUhl name=get_weather) with stop_reason=tool_use.
--- PASS: TestAnthropicMessages_ToolCallRoundTrip_LiveCoder (0.16s)
PASS
ok  	github.com/HelixDevelopment/HelixLLM/internal/gateway	0.171s
```

**The concrete case (task deliverable):** a request to `POST /v1/messages`
declaring the `get_weather` tool with `tool_choice: {"type":"any"}` and the
prompt "What is the current weather in Paris, France? You MUST call the
get_weather function tool to answer" was sent through the REAL, unmodified
production facade to the REAL live coder model. The model genuinely decided
to call `get_weather({"city":"Paris"})`; the fixed `anthropicToInternal`
carried the tool definition + tool_choice to the model, and the fixed
`internalToAnthropic` correctly converted the model's real tool call into an
Anthropic `tool_use` content block with `stop_reason: "tool_use"` — the
exact end-to-end round trip the finding said was impossible.

### Post-run environment integrity (§11.4.119 / §11.4.122)

```
containers: helixllm-coder Up 3 hours   (untouched)
coder :18434 /v1/models: responsive after the run
```
