# Challenge-suite failure triage — 2026-09-03

**Scope:** `challenges/banks/` run via `./bin/helixllm --challenges`, after the
harness defect that made it pass unconditionally was fixed. This is the first
run whose red is believed to mean something, and nobody had yet worked out what.

**Deliverable:** a root-cause grouping of every failure, ranked by user impact,
plus one fix. This is a triage, not a burn-down — 78 of 79 remaining failures
are deliberately left open and characterised here.

---

## 1. Measured baseline

`83` was a remembered number. Measured:

```
$ ./bin/helixllm --challenges --banks-dir=challenges/banks/ \
    --base-url=https://localhost:8443 --ca-cert=certs/cert.pem
20 passed, 83 failed, 28 skipped
EXIT=1
```

The 28 skips are honest and already surfaced by `Runner.Verify`: every one is a
`benchmark` or `chaos` step, for which `internal/testing/runner.go:376-380`
ships no executor. They are reported, counted, and turned into a non-zero exit —
not absorbed green. No change proposed.

**Environment at time of run (proven, not assumed):**

| Precondition | State | Evidence |
|---|---|---|
| Gateway under test | UP | `ss -ltnp` → `*:8443 users:(("helixllm",pid=30808))` |
| llama.cpp backend (`:50052`) | **DOWN** | `ss -ltn \| grep 50052` → no listener; server error body `dial tcp 127.0.0.1:50052: connect: connection refused` |
| llama.cpp binary | present | `/usr/bin/llama-server` |
| GGUF model weights | **absent** | `find "$HOME" -maxdepth 4 -name '*.gguf'` → empty |
| API-key auth | **not configured** | `middleware.APIKeyAuth` open-access mode, `internal/gateway/middleware/auth.go:26-32` |

The backend is genuinely unavailable *and cannot be made available here*: the
binary exists but there are no model weights on the host, and obtaining them is a
multi-GB download that is an operator decision, not an agent one.

---

## 2. Root-cause groups

All 83 accounted for; none unclassified.

| # | Group | Count | Category |
|---|---|---|---|
| G1 | No LLM backend reachable → provider dispatch fails | 51 | (c) environmental |
| G2a | No semantic request validation → malformed input reaches dispatch | 11 | **(a) product defect** |
| G2b | Auth challenges expect 401; server is in open-access mode | 3 | (c) config precondition |
| G3 | Provider-unavailable reported as 500, not 503 | 5 | **(a) product defect** |
| G4 | `/v1/models` returns `"data": null` | 4 | **(a) product defect** |
| G5 | `/health` returns 503 because no provider is up | 6 | (c) environmental |
| G6 | p99 latency budget exceeded | 1 | (c) environmental / (b) unstable threshold |
| G7 | `GET /v1/models/:id` 404 (downstream of G4) | 1 | (c) consequence |
| G8 | RAG ingest response shape | 1 | (d) undetermined |

### G1 — no LLM backend (51) · category (c)

Every failure carries `brain error: all providers exhausted` (48) or the
`connection refused` variant naming `:50052` (3). These are the workflow banks
(coding / debugging / documentation / review), llamacpp-only, routing, and the
OWASP cases whose payload is only reachable with a live model. **Not fixable
here** — see the precondition table. These are not converted to skips: the
challenges are correct, the environment is the gap, and silently greening them
would recreate the harness defect that was just removed.

### G2a — missing semantic request validation (11) · **category (a)**

`internal/gateway/openai.go:39-56`: after `ShouldBindJSON` (which catches only
JSON *syntax* errors) the handler goes straight to brain dispatch. There is no
check that `messages` is non-empty, no sign check on `max_tokens`, no model-name
validation. `pkg/api/openai.go:4-18` carries no binding tags, and a repo-wide
search finds no validator.

Verified independently of the harness:

```
malformed-json          HTTP=400  {"error":{"message":"invalid request body: malformed JSON"...
empty-messages          HTTP=500  {"error":{"message":"brain error: all providers exhausted...
negative-max-tokens     HTTP=500  {"error":{"message":"brain error: all providers exhausted...
empty-bearer            HTTP=500  {"error":{"message":"brain error: all providers exhausted...
```

Syntax validation exists and works; semantic validation does not exist. The
previous agent's lead — *"every malformed and unauthenticated request reaches
provider dispatch"* — is **confirmed for malformed**; see G2b for the auth half.

User impact: a bad request returns an opaque 500 instead of a 400 saying what is
wrong, and the error body leaks internal backend topology
(`dial tcp 127.0.0.1:50052`). With a live provider, unvalidated input is
forwarded to the model rather than rejected.

Not fixed here: the correct validation policy (which fields, what length ceiling
for "very long model name", what counts as an invalid model string) is not
specified anywhere in the repo, so a fix would mean inventing a spec. That is a
design decision worth making deliberately, not a contained repair.

### G2b — auth expectations vs open-access config (3) · category (c)

`internal/gateway/middleware/auth.go:26-32` documents that an empty key list is
open-access by design. `known-bug-regression/auth-empty-bearer-token` and
`owasp-top10-security/auth-bypass-malformed-bearer` assert a hard `401`, which is
**unreachable** in this configuration — they silently assume a keyed deployment
without declaring it. `Security Scanning Validation/invalid_auth_rejected`
accepts `[401 200]` and would pass under open access once a provider exists.

The auth middleware itself is implemented and unit-tested; nothing is broken.
The two hard-401 challenges need either a keyed-deployment precondition or a
declared skip reason. Left open — writing that precondition is a small change
but it belongs with the G2a validation decision.

### G3 — provider outage reported as 500 (5) · **category (a)** — FIXED

These five are the diagnostic key to the whole run: they were *authored to pass
with no provider*, accepting `503`. They failed only because the gateway
hardcoded `500` at five sites. The project's own banks declare the contract:

- `challenges/banks/chaos/provider_failure.yaml:8-21` — "Chat completion returns
  meaningful error when no LLM is available" → `status_one_of: [404, 503]`
- `challenges/banks/regression/dead_code.yaml:100-116` → `one_of: [200, 503]`

Fix applied — see §4.

### G4 — `/v1/models` returns `"data": null` (4) · **category (a)**

`internal/gateway/openai.go:543-548` (nil-brain branch) states the rule
explicitly:

> *"Explicitly empty, never nil: `"data": []` is a listing that states 'none',
> while `"data": null` reads as a malformed body."*

The sibling branch at `openai.go:536-541` violates it: it passes `b.Models()`
straight through, and `internal/brain/brain.go:371-376` returns a nil slice when
no option is available (`var models []api.Model`, append-only). Observed live:

```
$ curl .../v1/models   →  HTTP 200  {"object":"list","data":null}
```

User impact: an OpenAI-compatible client iterating `response.data` gets `null`
and raises — the discovery endpoint every such tool calls first breaks a
correct client making a correct request, whenever no model is currently served.

**Not fixed here despite being real and contained**, for one honest reason: the
correct repair is in `internal/brain/brain.go`, which is off-limits this session
(another agent is working there). It can be repaired gateway-side, but the fix
belongs at the source that produces the nil. Recommended as the next item.
Note the fix would **not** turn these four challenges green — they assert
`not_empty` on `body.data`, which needs real models. It is worth doing anyway.

### G5 — `/health` 503 (6) · category (c)

Health correctly reports `unhealthy` because `llm_providers` is unhealthy —
*"all 1 registered providers report unavailable (llamacpp) — every completion
request will fail"*. The endpoint is telling the truth. The challenges assert
`200` and so fail on a truthfully-degraded system. Environmental; no defect.

### G6 — p99 latency (1) · category (c)/(b)

`P99 Latency Assertions/health_p99`: 123.2ms against a 50ms budget, on a host
running concurrent agent workloads. Related finding below.

### G7 — `GET /v1/models/:id` 404 (1) · category (c)

Downstream of G4: the model list is empty, so every id is a miss. The 404 body
is already explicit about why. Resolves when G1 resolves.

### G8 — RAG ingest (1) · category (d) — undetermined

`rag-ingestion/ingest-document` — `field body.chunks is not countable`. Not
investigated; it is the only genuinely unexamined failure in the run and is
recorded as undetermined rather than guessed at.

---

## 3. Impact ranking

Ordered by what a real user experiences, not by how many tests are red.

1. **G1** — nothing works at all. Highest impact; environmental, not repairable here.
2. **G3** — a correct client making a correct request during a backend outage is
   told "this build is broken" (500) instead of "retry shortly" (503). Breaks
   client retry/backoff, load-balancer routing, and readiness probes on the
   product's core path. *Real defect, contained, fixed.*
3. **G4** — a correct client making a correct request gets a `null` where the
   protocol requires an array, and crashes. Real defect; repair belongs in an
   off-limits file this session.
4. **G2a** — an incorrect request gets an opaque 500 that leaks backend topology.
   Real defect; fixing it means first deciding a validation spec.
5. **G2b / G5 / G7 / G6 / G8** — preconditions, downstream effects, and one
   unexamined case.

G3 was chosen for the single fix: it is the highest-impact item that is both a
genuine product defect and repairable without inventing a specification — the
expected behaviour is declared by the project's own challenge banks — and it is
the only group provable end-to-end without changing the environment.

---

## 4. The fix (G3 only)

**Change:** `internal/fallback/chain.go` gains `ErrProvidersExhausted` +
`IsProvidersExhausted`; both exhausted-return sites wrap it (message text
byte-identical, because `chain_test.go:217` and `Chain.Complete`'s doc comment
assert on the `"all providers exhausted"` substring). A new
`internal/gateway/completer_status.go` maps that condition to 503 and everything
else to 500; the five `Completer`-error sites in `openai.go` (3) and
`anthropic.go` (2) use it.

An ordinary provider fault — a provider that *was* reached and returned an error
— deliberately stays 500. The pre-existing tests that assert 500 for that case
(`openai_test.go:498,526,592`, `anthropic_test.go:172,201`) still pass, which is
the intended containment.

**Reproduce-first, with the §11.4.115 polarity switch**
(`internal/gateway/provider_unavailable_test.go`, driven by a real
zero-entry `fallback.Chain`, not a stubbed error):

```
$ RED_MODE=1 go test -run TestProviderExhausted ./internal/gateway/   # pre-fix
ok  	github.com/HelixDevelopment/HelixLLM/internal/gateway	0.014s

$ go test -run TestProviderExhausted ./internal/gateway/              # pre-fix
--- FAIL: TestProviderExhausted_ChatCompletions           status = 500, want 503
--- FAIL: TestProviderExhausted_ChatCompletionsStreaming  status = 500, want 503
--- FAIL: TestProviderExhausted_AnthropicMessages         status = 500, want 503
--- FAIL: TestProviderExhausted_AnthropicMessagesStreaming status = 500, want 503
--- FAIL: TestProviderExhausted_Completions               status = 500, want 503
FAIL
```

**Post-fix, plus no package regressions:**

```
$ go test -count=1 ./internal/gateway/... ./internal/fallback/...
ok  	.../internal/gateway		0.075s
ok  	.../internal/gateway/middleware	0.008s
ok  	.../internal/fallback		0.847s
```

**Paired mutation (§1.1):**

| Mutation | Guard result |
|---|---|
| A — `completerErrorStatus` always returns 500 | 5 tests FAIL |
| B — strip the sentinel wrap in `chain.go` | 6 tests FAIL |
| restored | byte-identical to pre-mutation; zero mutation markers; GREEN |

**Runtime signature on a real server (§11.4.108 layer 3)** — same request, old
binary on `:8443` vs fixed binary on `:8444`:

```
OLD binary (pre-fix)  /v1/chat/completions   HTTP 500
OLD binary (pre-fix)  /v1/messages           HTTP 500
OLD binary (pre-fix)  /v1/completions        HTTP 500
NEW binary (fixed)    /v1/chat/completions   HTTP 503
NEW binary (fixed)    /v1/messages           HTTP 503
NEW binary (fixed)    /v1/completions        HTTP 503

body (new): {"error":{"message":"brain error: all providers exhausted: no entries available",...}}
```

**Full suite against the fixed server:**

```
before: 20 passed, 83 failed, 28 skipped
after:  24 passed, 79 failed, 28 skipped
```

Exact set diff — 5 fixed, and one apparent new failure:

```
FIXED:
  + LLM Provider Failure/chat_without_provider
  + dead-code-regression/chat-completions-wired
  + dead-code-regression/anthropic-messages-wired
  + Concurrent Request Stress/100_concurrent_completions
  + Memory Pressure/large_prompt
NEW FAILURES:
  ! Concurrent Request Stress/500_concurrent_model_list
```

That one is **not** caused by the fix, and this was tested rather than asserted.
It is a latency assertion on `GET /v1/models` — code the diff never touches. Run
three times against both binaries:

```
run1 OLD(:8443)  FAIL  slowest 1742.7ms > 1000ms
run1 NEW(:8444)  FAIL  slowest 1037.3ms > 1000ms
run2 OLD(:8443)  1 passed
run2 NEW(:8444)  FAIL  slowest 1135.9ms > 1000ms
run3 OLD(:8443)  FAIL  slowest 1016.7ms > 1000ms
run3 NEW(:8444)  1 passed
```

The unfixed binary fails it more often and more severely than the fixed one. The
assertion is host-load-sensitive and non-deterministic (§11.4.50) — it was in
the passing bucket of the baseline by luck. **New triage finding, logged below.**

---

## 5. Recommended next steps

1. **G4 `"data": null`** — smallest real defect with a real user-visible
   consequence. Fix at source in `internal/brain/brain.go:371` (return
   `[]api.Model{}` rather than a nil slice) once that file is free, or defensively
   in `internal/gateway/openai.go:536-541`. Will not move the challenge count.
2. **G2a validation spec** — decide the policy first (empty `messages` → 400;
   `max_tokens <= 0` → 400; model-name charset/length ceiling), then implement in
   one place for both `openai.go` and `anthropic.go`. Closes 11 challenges without
   needing a live provider, and stops the backend-topology leak in error bodies.
3. **`500_concurrent_model_list` determinism** — a 1000ms wall-clock ceiling on
   500 concurrent requests is not a stable assertion on a shared host. Either
   pin it to a percentile with headroom or mark it operator-attended. Right now
   it flips green/red run to run on *both* binaries and will keep generating
   false signal.
4. **G2b auth preconditions** — the two hard-401 challenges need a declared
   keyed-deployment precondition, or an explicit skip naming open-access mode.
5. **G8** — the one unexamined failure; investigate `rag-ingestion/ingest-document`.
6. **G1 (51)** — needs GGUF weights on the host. Operator decision (multi-GB
   download). Until then these stay red, and that is the honest state: they are
   correct challenges against an incomplete environment.

## 6. What was deliberately not done

No challenge was weakened, skipped, or deleted to improve a number. The 51
backend-blocked failures stay red rather than becoming skips, because their
precondition is absent *here* and not absent in principle — converting them
would rebuild the always-passing harness that was just dismantled. The count
moved 83 → 79 because five defects were actually repaired, and the report states
plainly that the sixth delta is a pre-existing flake rather than counting it as
a win.

---

## Follow-up: `:8443` now runs the fixed binary (2026-09-03, later the same day)

The verification above deliberately ran the fixed binary on `:8444` and left the
**stale** one on `:8443`. That stale process then kept serving for 16 hours:

```
pid 30808  ./bin/helixllm   started Wed Sep  2 22:13:40   binary mtime Sep 2 22:13
HEAD at the time of this note: 1efda3b (Sep 3 12:48)
```

Its `/v1/models` was still answering

```json
{"object":"list","data":null}
```

— the exact defect `ab34fa8` ("a listing of nothing is an empty list, not null")
had already fixed **in source**. Source-green said nothing about what was
serving; this is the §11.4.108 SOURCE → RUNTIME gap, and no gate catches it
because every gate reads the source.

Rebuilt from HEAD and restarted on `:8443`. It now returns a real list:

```json
{"object":"list","data":[{"id":"helixllm-anton-llama-3-1-70b-instruct-q4_k_m-b57fe6665058",
  "owned_by":"llamacpp","model_identity":"helixllm/anton/Llama-3.1-70B-Instruct-Q4_K_M",
  "host":"anton","availability":"withheld","withheld_reason":"provider_unavailable"}]}
```

The `withheld` is CORRECT, not a regression: `anton` IS this host, no GGUF
weights exist on it, and no `llama-server` is running. Note separately that
`HELIX_LLM_LOCAL_MODEL` defaults to that 70B Q4_K_M (~40 GB) while the host has
30 GB RAM and 12 GB VRAM — it could not run its own default even with weights.

**Operational note for anyone re-running this triage:** compare
`ps -o lstart= -p <pid>` against `git log -1` before trusting any live probe.
A 200 from a stale process is evidence about the stale process and nothing more.
