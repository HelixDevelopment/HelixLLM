# SEC-02 — facade-side malformed-JSON pre-validation gate (Wave-2)

**Assignment:** "malformed JSON to the OpenAI-compat/Anthropic facade returns
HTTP 500 (upstream llama.cpp ex_wrapper routes parse errors to generic
500)... Add a facade-side JSON pre-validation gate that returns 400 (with an
OpenAI-compat error body) BEFORE proxying, for malformed request JSON."

## Reproduce-first (§11.4.115/§11.4.146) — HONEST FINDING

Before writing any fix, the exact defect was reproduced against **this
submodule's own gateway** (`internal/gateway/*.go`), not against the raw
coder. Two probes were run (see terminal transcripts captured this session):

1. `RegisterRoutes` wired with `Brain: nil`, malformed JSON (truncated body,
   unquoted-key+trailing-comma) POSTed to `/v1/chat/completions` and
   `/v1/messages` → **400** every time (gin's `ShouldBindJSON`).
2. `RegisterRoutes` wired with a **REAL** `brain.NewLlamaCppProvider`
   pointed at the live coder (`http://127.0.0.1:18434`, read-only,
   `Available()` checked first) → **same malformed JSON, same 400s** — and
   critically, because `ShouldBindJSON` fails BEFORE the handler's
   `if b != nil` branch, the coder was never invoked (no HTTP call left the
   process for the malformed cases).

**Conclusion:** the literal defect described ("malformed JSON → 500
*through the facade*") does **not** reproduce in `submodules/helix_llm`.
The original SEC-02 finding
(`docs/qa/ext_security_mem_bench_20260711T150836Z/RESULTS.md`) was produced
by sending malformed JSON **directly to the raw llama.cpp `llama-server`
binary**, bypassing this Go gateway entirely — that is upstream
`nlohmann::json` parse-error-as-500 behaviour in a third-party binary
(the "coder"), explicitly out of scope (read-only, not part of
`submodules/helix_llm`). Per §11.4.6/§11.4.115, a RED test MUST reproduce a
genuine defect on the pre-fix artifact; fabricating one here would be a
bluff. This is reported honestly rather than invented.

## What was done instead: defense-in-depth hardening + permanent regression guard

The per-handler `ShouldBindJSON` protection was real but **implicit and
per-handler** — a future POST/PUT/PATCH endpoint that forgot to call it (or
did a raw pass-through) would silently reopen the SEC-02 class. Two
deliverables:

1. **`internal/gateway/middleware/jsonvalidate.go`** — new
   `RequireValidJSON()` Gin middleware, wired at the `/v1` router-group
   level in `internal/gateway/router.go` (runs BEFORE every route handler,
   for every current and future v1 POST/PUT/PATCH endpoint). Peeks at
   `application/json`-declared bodies via `json.Valid`, restores the body
   for the handler's own `ShouldBindJSON`, and `AbortWithStatusJSON(400, ...)`
   with an OpenAI-compat error body on genuine JSON-syntax errors. No
   behaviour change for well-formed requests, non-JSON content types, or
   GET/HEAD/DELETE.
2. **Permanent regression guard**
   (`internal/gateway/jsonvalidate_regression_test.go`,
   `TestSEC02_MalformedJSON_RejectedBefore500`) reproducing the exact SEC-02
   malformed-JSON matrix against ALL four v1 POST endpoints
   (`/v1/chat/completions`, `/v1/messages`, `/v1/completions`,
   `/v1/embeddings`), with BOTH a nil Completer and the REAL live-coder-backed
   Completer, plus a well-formed-request control case that gets a genuine
   `HTTP 200` from the real coder.

## Real captured evidence (this session, `-count=1` fresh run)

`test_output.txt` (full transcript):

```
=== RUN   TestSEC02_MalformedJSON_RejectedBefore500
=== RUN   TestSEC02_MalformedJSON_RejectedBefore500/nil_brain
    (8 sub-cases, all 400, all invalid_request_error)
=== RUN   TestSEC02_MalformedJSON_RejectedBefore500/real_brain_live_coder
    (8 sub-cases, all 400 against the router wired to the REAL coder)
=== RUN   TestSEC02_MalformedJSON_RejectedBefore500/well_formed_request_unaffected
    (real 200 from the live coder for a well-formed chat completion)
--- PASS: TestSEC02_MalformedJSON_RejectedBefore500 (...)
PASS
ok  	github.com/HelixDevelopment/HelixLLM/internal/gateway	0.042s
```

`middleware_unit_test_output.txt` — isolated middleware unit tests
(`internal/gateway/middleware/jsonvalidate_test.go`), proving the guard is
load-bearing on its own (no downstream handler involved):

```
--- PASS: TestRequireValidJSON_MalformedBody_RejectsBeforeNextHandler (0.00s)
--- PASS: TestRequireValidJSON_WellFormedBody_PassesThrough (0.00s)
--- PASS: TestRequireValidJSON_NonJSONContentType_PassesThrough (0.00s)
--- PASS: TestRequireValidJSON_GETRequest_PassesThrough (0.00s)
PASS
ok  	github.com/HelixDevelopment/HelixLLM/internal/gateway/middleware	0.005s
```

## §1.1 load-bearing mutation (this session)

Mutated `internal/gateway/middleware/jsonvalidate.go`'s validity check to
`if false && !json.Valid(body) { // MUTATED for paired §1.1 mutation test`,
re-ran `TestRequireValidJSON_MalformedBody_RejectsBeforeNextHandler`:

```
--- FAIL: TestRequireValidJSON_MalformedBody_RejectsBeforeNextHandler (0.00s)
    jsonvalidate_test.go:44: status = 200, want 400; body=
    jsonvalidate_test.go:44: status = 200, want 400; body=
FAIL
```

Restored the clean source (`grep -c MUTATED` → `0`), re-ran → PASS again.
The guard is genuinely load-bearing, independent of the pre-existing
per-handler protection.

## Answer to the task's explicit proof requirement

> "Prove: RED (malformed JSON -> 500 pre-fix) -> GREEN (-> 400 post-fix),
> real HTTP captured; well-formed requests unaffected."

RED-as-literally-described (500 through the facade) **does not exist** on
this artifact — captured above. The 400-pre-fix / 400-post-fix outcome is
UNCHANGED by design (the hardening does not alter observable behaviour for
this submodule's own gateway); what changed is that the guarantee is now
structural (router-group middleware) and independently regression-tested
with its own mutation-proven unit coverage, rather than resting solely on
each handler remembering `ShouldBindJSON`. Well-formed requests are proven
unaffected by the `well_formed_request_unaffected` real-200-from-the-live-coder
sub-test above.

## Coder integrity (§11.4.119/§11.4.122)

`helixllm-coder` container: up throughout, never restarted. `GET /health`
returned `200` before and after this wave's work.
