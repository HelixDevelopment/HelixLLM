# Gateway request validation and upstream-error disclosure — 2026-09-03

**Scope:** group G2a of `docs/challenge_triage_20260903.md` — "no semantic
request validation", 11 challenge failures, the largest genuine product
defect the triage identified.

This document is the *decision record*. The enforcing code, with the same
citations inline, is `internal/gateway/requestvalidate.go` and
`internal/gateway/upstream_error.go`; the guard is
`internal/gateway/requestvalidate_test.go`.

---

## 1. What was actually wrong

`ShouldBindJSON` catches JSON **syntax** errors. Nothing between it and
provider dispatch checked whether the parsed body **meant** anything. Two
consequences, both measured against the shipped binary before the fix:

```
$ curl .../v1/chat/completions -d '{"model":"llama-3.1-70b","messages":[]}'
HTTP=503 {"error":{"message":"brain error: all providers exhausted, last error:
llamacpp: send request: Post \"http://localhost:50052/v1/chat/completions\":
dial tcp 127.0.0.1:50052: connect: connection refused", ...}}
```

1. **Wrong answer.** A caller who sent a bad request was told the *server*
   had failed. After the G3 fix made provider-unavailable a 503, this got
   *worse*, not better: 503 means "retry with backoff", and a retry of
   `messages: []` can never succeed, so a correct client backs off and
   retries forever.

2. **Information disclosure.** The body was the raw provider error, naming
   the internal backend's host, port, and path — on an unauthenticated
   endpoint. This was **not** confined to malformed requests: a perfectly
   valid request during a backend outage produced the same body.

### Scope found

The triage named `openai.go` and said the policy should be implemented
"once for both handlers". There are **three** body-taking completion
handlers, not two, and **six** error sites, not five:

| Handler | File | Validated | Error site(s) |
|---|---|---|---|
| `POST /v1/chat/completions` | `openai.go` | yes | 2 (complete, stream) |
| `POST /v1/completions` | `openai.go` | yes | 1 |
| `POST /v1/messages` | `anthropic.go` | yes | 2 (complete, stream) |
| `GET /ws` | `websocket.go` | no (see §3) | 1 |

The WebSocket site was the worst of the six: it wrote `err.Error()` with no
redaction *and* no translation at all.

---

## 2. The validation policy

Nothing below is invented. Each rule cites either the vendor API this
gateway is compatible with, or this project's own challenge banks, which are
the executable specification wherever they speak.

| Rule | Endpoints | Authority |
|---|---|---|
| `messages` must contain ≥ 1 message | chat, messages | OpenAI OpenAPI `CreateChatCompletionRequest.messages: minItems: 1`; bank `regression/known_bugs.yaml` **CH-REG-001** asserts 400. Anthropic Messages takes "a single `user`-role message, or … multiple". |
| every `messages[].role` non-empty | chat, messages | bank **CH-REG-006** (`"role": null` → 400). OpenAI's request-message union *discriminates on* `role`, so a message with no role matches no variant. |
| `max_tokens ≥ 1` | **chat only** | banks **CH-REG-007** (`-1` → 400) and **CH-REG-004** (`0` → 400). OpenAI's chat schema declares *no* minimum for `max_tokens` (the field is `deprecated` there), so the project's own banks govern this endpoint. |
| `max_tokens ≥ 0` | completions | OpenAI OpenAPI `CreateCompletionRequest.max_tokens: minimum: 0`. Zero is **legal** here — it scores a prompt with `logprobs`/`echo` without generating. |
| `max_tokens ≥ 0` | messages | Anthropic documents `max_tokens` `minimum: 0`, where **0 pre-warms the prompt cache**. |
| `model` ≤ 128 chars | all three | bank **CH-REG-010** (330-char name → 400). |
| `model` charset `[A-Za-z0-9._\-/:@+]` | all three | bank **CH-REG-002** (non-ASCII → 400). |
| `model` has no `.` / `..` path segment | all three | bank `security/owasp.yaml` **CH-SEC-007** (`../../etc/passwd` → 400). |

Rejections are OpenAI-shaped: `400`, `type: "invalid_request_error"`, a
`param` naming the offending field (`messages`, `messages[1].role`,
`max_tokens`, `model`) and a `code` drawn from OpenAI's own vocabulary
(`empty_array`, `invalid_value`, `string_above_max_length`,
`missing_required_parameter`). A client cannot fix a field the error does
not name.

### The `max_tokens` asymmetry is deliberate

Three endpoints, three floors. The asymmetry is the *vendors'*, not ours:
flatten it to a single floor of 1 and `/v1/completions` starts rejecting a
documented OpenAI use, and `/v1/messages` starts rejecting a documented
Anthropic feature. Flatten it to 0 and the chat endpoint stops honouring its
own two regression banks. A paired mutation (M4 below) pins this.

### Model well-formedness is not model existence

This rejects names that **are not model names**. It never rejects a name
that is merely *unknown* — a well-formed unknown model still reaches
dispatch. "Requested resource does not exist" is **404** in OpenAI's
error-codes guide, and that is already what `/v1/models/:id` returns.

The charset admits every identifier shape this project's own configuration
uses and every provider convention checked: `meta-llama/Llama-3.3-70B-Instruct-Turbo-Free`,
`llama3:8b`, `ghcr.io/ggml-org/llama.cpp:server-cuda`, `helixllm/gpu-01/llama3:8b`,
`anthropic.claude-3-5-sonnet-20240620-v1:0`, `llama3@sha256:…`. The 128-char
ceiling sits well clear of the longest real identifier (44 chars here) and
well below the 330-char case that must fail.

---

## 3. What is deliberately NOT validated, and why

Rejecting something a real client legitimately sends would be a worse
regression than the wrong status this fixes. So:

- **Message `content` is never required to be non-empty.** An assistant turn
  carrying `tool_calls` has empty content *by design*; requiring content
  would break every correct agent conversation.
- **The role ENUM is not enforced** — only non-emptiness. OpenAI has
  `developer` and `function` beyond this project's four roles and keeps
  adding more. Rejecting an unknown-but-present role would reject requests a
  correct client sends to a newer API.
- **`n`, `temperature`, `top_p`, `presence_penalty`, `frequency_penalty`.**
  OpenAI declares ranges (`n` is 1..128), but no bank speaks and no defect
  was reported. Tightening them is an unforced behaviour change.
- **An empty `model`.** Both handlers already substitute a default; changing
  that would break every client relying on it.
- **Model existence** — 404 territory, see above.
- **The *presence* of `max_tokens` on `/v1/messages`.** Anthropic documents
  it as required, but `api.MessageRequest.MaxTokens` is a plain `int`, so an
  omitted field is indistinguishable from an explicit `0` — and `0` is
  legal. Requiring presence would mean changing the type, a wider blast
  radius than this fix warrants, so only an explicitly *negative* value is
  rejected. Every bank that posts to `/v1/messages` sends `max_tokens`
  anyway (checked: `routing.yaml`, `dead_code.yaml`, `llamacpp_only.yaml`,
  `latency.yaml`).
- **`/v1/embeddings`.** No bank speaks; out of scope for G2a.
- **The WebSocket request body.** It carries an internal type
  (`types.InternalChatRequest`), not the OpenAI wire type, and no bank
  speaks. Its *disclosure* defect is fixed; its validation is left open and
  recorded here rather than silently assumed done.
- **Authentication.** An empty or malformed bearer token is a **401**
  answered by `middleware.APIKeyAuth` — a different question with a
  different status. The validator never reads the `Authorization` header, so
  it *cannot* collapse an auth failure into a 400. Those cases
  (`CH-REG-005`, `CH-SEC-005`) are triage group **G2b**, unreachable while
  the deployment runs in open-access mode, and remain out of scope.

---

## 4. The disclosure fix

All six sites funnel through `writeUpstreamError` / `upstreamErrorTextForLang`:

- **Client** gets a translated, topology-free message, split the same way
  `completerErrorStatus` splits the status — "no model-serving backend is
  currently available … retry shortly" (503) vs "the model provider failed
  to complete this request" (500).
- **Operator** gets the *full* error, address and all, in the server log:
  `[HelixLLM] upstream complete failed for POST /v1/chat/completions: …
  dial tcp 127.0.0.1:50052: connect: connection refused`.

This is a **redaction, not a deletion**. `UpstreamErrorLogDetail` is
exported precisely so a guard can prove the detail survives; mutation M5
below pins it.

---

## 5. Evidence

Reproduced before fixing, live, against the running server; polarity switch
per §11.4.115 (`RED_MODE=1` reproduces the defect, `RED_MODE=0` guards it).

```
GREEN polarity on the PRE-FIX tree : 23 failing assertions
RED   polarity on the PRE-FIX tree : ok  (defect reproduces)
GREEN polarity on the FIXED tree   : ok
RED   polarity on the FIXED tree   : 23 failing assertions
```

Runtime signature — same request, pre-fix binary vs fixed binary:

| Request | pre-fix | fixed |
|---|---|---|
| `messages: []` | 503 | **400** `param=messages` `code=empty_array` |
| `max_tokens: -1` | 503 | **400** `param=max_tokens` |
| `max_tokens: 0` (streaming) | 503 | **400** `param=max_tokens` |
| `"role": null` | 503 | **400** `param=messages[0].role` |
| non-ASCII model | 503 | **400** `param=model` |
| 330-char model | 503 | **400** `param=model` `code=string_above_max_length` |
| `../../etc/passwd` model | 503 | **400** `param=model` |
| `/v1/messages` `messages: []` | 503 | **400** |
| `/v1/completions` `max_tokens: -5` | 503 | **400** |

### The disclosure, matched pair, both servers live

The WebSocket path gives the cleanest side-by-side — same frame, same
moment, pre-fix server on `:8445` and fixed server on `:8446`:

```
SENT {"model":"llama-3.1-70b","messages":[{"role":"user","content":"Hi"}]}

  pre-fix (:8445) -> {"error":"all providers exhausted, last error: llamacpp:
                      send request: Post \"http://localhost:50052/v1/chat/completions\":
                      dial tcp 127.0.0.1:50052: connect: connection refused"}

  fixed   (:8446) -> {"error":"no model-serving backend is currently available
                      to serve this request; retry shortly"}
```

And the fixed server's own log for that same request:

```
[HelixLLM] upstream complete failed for POST /v1/chat/completions:
all providers exhausted, last error: llamacpp: send request: Post
"http://localhost:50052/v1/chat/completions": dial tcp 127.0.0.1:50052:
connect: connection refused
```

The address left the client and stayed with the operator, which is the whole
of the fix.

The HTTP re-capture on the pre-fix binary is timing-dependent — once the
chain has circuit-broken the unreachable lane it answers "no entries
available" without dialling — so the deterministic proof of the HTTP half is
`TestUpstreamError_DoesNotDiscloseInternalAddress`, which drives all five
HTTP sites through a Completer failing with the *verbatim* string captured
above.

### Challenge suite, measured before and after

Same banks, same host, sequential runs against two servers built from the
pre-fix and fixed trees:

```
before (pre-fix binary, :8445):  25 passed, 78 failed, 28 skipped
after  (fixed binary,   :8446):  34 passed, 69 failed, 28 skipped
```

Exact set diff — 9 fixed, **zero** new failures:

```
FIXED:
  known-bug-regression/empty-message-array
  known-bug-regression/negative-max-tokens
  known-bug-regression/null-role-in-messages
  known-bug-regression/streaming-zero-max-tokens
  known-bug-regression/unicode-in-model-name
  known-bug-regression/very-long-model-name
  owasp-top10-security/path-traversal-model-name
  owasp-top10-security/sql-injection-model-field
  owasp-top10-security/unicode-smuggling-model-name
NEW FAILURES:
  (none)
```

The last two were not individually targeted but fall out of the stated
model-name rule and are correct under it: `llama-3.1-70b'; DROP TABLE
models; --` and `llama\u00003.1-70b` (an embedded NUL) are not model names.

No challenge was weakened, skipped, or deleted. The 28 skips are the
harness's own unimplemented `benchmark` / `chaos` step types, unchanged.

**Two G2a-adjacent challenges deliberately left failing.**
`known-bug-regression/missing-content-type` and `.../duplicate-stream-field`
fail identically before and after with `status = 503, want one of [200 400
415]` / `[200 400]`. Neither request is semantically defective — a missing
`Content-Type` and a duplicated JSON key are not validation faults — so they
reach dispatch and receive the truthful 503 from the down backend. Their
banks' allowed-status sets predate the provider-unavailable 503 mapping.
Making them pass would require rejecting valid requests, which is exactly
the over-rejection this policy refuses.

### Paired mutations (§1.1) — each `diff`-proved to have applied

| # | Mutation | Result |
|---|---|---|
| M1 | remove the chat validation call | 10 assertions FAIL |
| M2 | re-leak the raw provider error into the client body | 6 FAIL |
| M3 | make model-name validation accept everything | 8 FAIL |
| M4 | leak chat's `max_tokens` floor onto `/v1/completions` (over-rejection) | positive control `completions_zero_max_tokens_is_legal` FAILs |
| M5 | delete the operator detail instead of redacting it | operator-detail guard FAILs |

M4 is the one that matters most: it proves the anti-over-rejection control
is load-bearing, so the guard cannot be satisfied by a validator that simply
rejects more.

---

## 6. Left open

- **G2b** (3 failures) — the hard-401 auth challenges
  (`known-bug-regression/auth-empty-bearer-token`,
  `owasp-top10-security/auth-bypass-malformed-bearer`), unreachable in
  open-access mode. Unchanged and deliberately untouched: an auth failure
  and a malformed request are different answers with different status codes,
  and this validator never reads the `Authorization` header.
- The two bank status-sets noted above (`missing-content-type`,
  `duplicate-stream-field`) predate the 503 mapping. Widening them to
  include 503 is a bank change, not a code change, and belongs to whoever
  owns G3's follow-through.
- **`/v1/embeddings`** and the **WebSocket request body** carry no semantic
  validation. No bank speaks for either; recorded, not silently assumed.
- The 51 backend-blocked failures (G1) still need model weights on the host.

## Sources verified 2026-09-03

- OpenAI OpenAPI specification (authoritative machine-readable constraints):
  <https://raw.githubusercontent.com/openai/openai-openapi/master/openapi.yaml>
- OpenAI error-codes guide (400 vs 401 vs 404 semantics):
  <https://developers.openai.com/api/docs/guides/error-codes>
- Anthropic Messages API reference (`max_tokens` minimum, roles, 400):
  <https://platform.claude.com/docs/en/api/messages>
- This project's banks: `challenges/banks/regression/known_bugs.yaml`,
  `challenges/banks/security/owasp.yaml`, `challenges/banks/api/*.yaml`
