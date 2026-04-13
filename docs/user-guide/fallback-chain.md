# Multi-Provider Fallback Chain

HelixLLM routes every completion request through an ordered fallback chain of cloud providers before falling back to the local llama.cpp instance. This guide explains what the chain does, how to configure it, and how to interpret its runtime behaviour.

## Overview

The fallback chain solves two related problems:

**Why it exists:** Local CPU inference with llama.cpp is reliable but slow for complex tasks. A GPU-accelerated host helps, but is not always available. The chain lets HelixLLM tap a pool of free-tier cloud providers that are fast for most workloads, while keeping llama.cpp as a guaranteed last resort that never fails due to quota exhaustion.

**What it does:** On every `/v1/chat/completions` request the gateway calls through a `FallbackChain` — an ordered list of provider entries. The chain tries entry 1, then entry 2, and so on until one succeeds or all are exhausted. Entries are ordered by quality scores fetched from LLMsVerifier and refreshed every five minutes in the background. The local llama.cpp entry is pinned at the end and is never reordered.

```
Gateway → FallbackChain → [openrouter 90.0] → [chutes 85.0] → … → [llamacpp 10.0]
```

The chain is transparent to the gateway: `FallbackChain` implements the same `gateway.Completer` interface as a direct provider, so no gateway code changes are needed to add or remove providers from the chain.

## Quick Start

1. Copy the example environment file:

```bash
cp .env.example .env
```

2. Set at least one provider API key. OpenRouter is the easiest starting point because its free tier covers many models without a credit card:

```bash
HELIX_LLM_OPENROUTER_KEY=sk-or-...
```

3. Start the server:

```bash
make dev
```

On startup you will see a log entry for every active chain entry, in priority order:

```
INFO fallback chain entry rank=1 provider=openrouter model=meta-llama/llama-3.3-70b-instruct:free score=90 local=false
INFO fallback chain entry rank=2 provider=chutes model=chutesai/Llama-4-Scout-17B-16E-Instruct score=85 local=false
INFO fallback chain entry rank=3 provider=llamacpp model=Qwen2.5-Coder-3B-Instruct-Q4_K_M score=10 local=true
```

Any provider key left blank is skipped entirely — only configured providers appear in the chain.

## Provider Setup

Seven cloud providers are supported. All offer free tiers that work without a credit card unless noted.

### Chutes

Chutes hosts open-weight models on shared GPU clusters with a generous free tier. Quota varies by model and changes frequently.

| | |
|---|---|
| Sign up | [chutes.ai](https://chutes.ai) |
| Env var | `HELIX_LLM_CHUTES_KEY` |
| API endpoint | `https://llm.chutes.ai/v1` |
| Models | Auto-discovered via `/v1/models`. Includes Llama 4, Qwen 2.5, Phi-4 variants. |
| Free tier | Shared quota, no hard published limit. Expect occasional 429s under heavy load. |

### OpenRouter

OpenRouter aggregates hundreds of models from dozens of providers. Models with the `:free` suffix are zero-cost; HelixLLM automatically filters to these models when using the free tier.

| | |
|---|---|
| Sign up | [openrouter.ai](https://openrouter.ai) |
| Env var | `HELIX_LLM_OPENROUTER_KEY` |
| API endpoint | `https://openrouter.ai/api/v1` |
| Models | Auto-discovered; only `:free`-suffixed models are used. |
| Free tier | Free models are rate-limited per-model and per-IP. The chain automatically moves to the next provider on 429. |

### HuggingFace Inference API

The HuggingFace serverless inference router exposes community models at no cost. Only free inference models are surfaced; no model filtering is needed on HelixLLM's side because the router already gates paid models.

| | |
|---|---|
| Sign up | [huggingface.co](https://huggingface.co/settings/tokens) |
| Env var | `HELIX_LLM_HUGGINGFACE_KEY` |
| API endpoint | `https://router.huggingface.co/v1` |
| Models | Auto-discovered. Free inference models only. |
| Free tier | Rate-limited per API key. Heavy use may require upgrading to a paid tier. |

### Nvidia NIM

Nvidia's NIM platform offers optimised inference for popular models. New accounts receive 1,000 API calls per day on the free tier.

| | |
|---|---|
| Sign up | [build.nvidia.com](https://build.nvidia.com) |
| Env var | `HELIX_LLM_NVIDIA_KEY` |
| API endpoint | `https://integrate.api.nvidia.com/v1` |
| Models | Auto-discovered. Includes Llama, Mistral, and Phi families with NVIDIA-optimised quantisation. |
| Free tier | 1,000 requests/day. No credit card required. |

### Cerebras

Cerebras uses dedicated wafer-scale silicon for inference. It delivers some of the highest tokens-per-second rates available on any free tier.

| | |
|---|---|
| Sign up | [cerebras.ai](https://cloud.cerebras.ai) |
| Env var | `HELIX_LLM_CEREBRAS_KEY` |
| API endpoint | `https://api.cerebras.ai/v1` |
| Models | Auto-discovered. Llama 3.1/3.3 variants are available on the free tier. |
| Free tier | Free tier available; rate limits apply per minute and per day. |

### SambaNova

SambaNova Cloud accelerates inference on reconfigurable dataflow hardware. Its free tier is one of the most capable for open-weight models.

| | |
|---|---|
| Sign up | [sambanova.ai](https://cloud.sambanova.ai) |
| Env var | `HELIX_LLM_SAMBANOVA_KEY` |
| API endpoint | `https://api.sambanova.ai/v1` |
| Models | Auto-discovered. Llama 3.1 405B and DeepSeek R1 are available on the free tier. |
| Free tier | Free tier available with per-minute and per-day rate limits. |

### Together AI

Together AI offers competitive pricing with a $5 free credit on sign-up. The credit does not expire and covers a large number of completions on small open-weight models.

| | |
|---|---|
| Sign up | [together.ai](https://api.together.ai) |
| Env var | `HELIX_LLM_TOGETHER_KEY` |
| API endpoint | `https://api.together.xyz/v1` |
| Models | Auto-discovered via `/v1/models`. Wide selection of open-weight models. |
| Free tier | $5 credit on sign-up. No ongoing free tier after credit is consumed. |

## Configuration Reference

All fallback chain settings live in `.env`. Copy `.env.example` to `.env` and edit in place.

### Provider Keys

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIX_LLM_CHUTES_KEY` | (empty) | Chutes API key. Provider is disabled when empty. |
| `HELIX_LLM_OPENROUTER_KEY` | (empty) | OpenRouter API key. Provider is disabled when empty. |
| `HELIX_LLM_HUGGINGFACE_KEY` | (empty) | HuggingFace access token. Provider is disabled when empty. |
| `HELIX_LLM_NVIDIA_KEY` | (empty) | Nvidia NIM API key. Provider is disabled when empty. |
| `HELIX_LLM_CEREBRAS_KEY` | (empty) | Cerebras Cloud API key. Provider is disabled when empty. |
| `HELIX_LLM_SAMBANOVA_KEY` | (empty) | SambaNova Cloud API key. Provider is disabled when empty. |
| `HELIX_LLM_TOGETHER_KEY` | (empty) | Together AI API key. Provider is disabled when empty. |

### Chain Behaviour

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIX_LLM_VERIFIER_URL` | `http://localhost:7061` | Base URL of the LLMsVerifier service. Used to fetch live quality scores for ranking providers. When unreachable, static fallback scores are used instead. |
| `HELIX_LLM_SCORE_REFRESH_INTERVAL` | `5m` | How often the background refresh loop re-fetches scores and re-sorts the chain. Accepts Go duration strings: `30s`, `2m`, `1h`. |

### Memory Sync (Optional)

| Variable | Default | Description |
|----------|---------|-------------|
| `HELIX_LLM_MEMORY_SYNC_ENABLED` | `false` | When `true`, memories with importance >= 0.7 are forwarded asynchronously to the HelixMemory service after each successful completion. |
| `HELIX_LLM_MEMORY_URL` | `http://localhost:7061` | Base URL of the HelixMemory service. Only used when `HELIX_LLM_MEMORY_SYNC_ENABLED=true`. |

### Minimal Working Configuration

To use the chain with two providers:

```bash
# .env
HELIX_LLM_OPENROUTER_KEY=sk-or-your-key
HELIX_LLM_CEREBRAS_KEY=csk-your-key
HELIX_LLM_DEFAULT_PROVIDER=auto
```

The chain will be: `[openrouter 90.0] → [cerebras 70.0] → [llamacpp 10.0]`.

### All Seven Providers

```bash
# .env
HELIX_LLM_CHUTES_KEY=your-chutes-key
HELIX_LLM_OPENROUTER_KEY=sk-or-your-key
HELIX_LLM_HUGGINGFACE_KEY=hf_your-token
HELIX_LLM_NVIDIA_KEY=nvapi-your-key
HELIX_LLM_CEREBRAS_KEY=csk-your-key
HELIX_LLM_SAMBANOVA_KEY=your-sambanova-key
HELIX_LLM_TOGETHER_KEY=your-together-key
HELIX_LLM_VERIFIER_URL=http://localhost:7061
HELIX_LLM_SCORE_REFRESH_INTERVAL=5m
```

## How the Chain Works

### Startup

At boot, the chain is built in three steps:

1. **Provider registration** — `Brain.New` inspects `LLMConfig` and registers only the providers whose API key field is non-empty. Providers with no key are not instantiated and do not appear in the chain.

2. **Model discovery** — For each registered provider, the first model ID returned by `Provider.Models()` is recorded. OpenRouter and HuggingFace filter to free models during this step. The model map is passed to the scorer so each chain entry carries a concrete model ID.

3. **Scoring and sorting** — `ScorerBridge.FetchScores` contacts `HELIX_LLM_VERIFIER_URL/api/scores`. On success it uses the live scores. On any failure (network error, non-200, empty payload) it falls back to built-in static scores:

   | Provider | Static Score |
   |----------|-------------|
   | openrouter | 90 |
   | chutes | 85 |
   | huggingface | 80 |
   | nvidia | 75 |
   | cerebras | 70 |
   | sambanova | 65 |
   | together | 60 |
   | llamacpp | 10 (always last) |

   `BuildEntries` sorts cloud providers by score descending and appends llamacpp at the end with `IsLocalFallback=true`. Each entry gets its own `CircuitBreaker(maxFailures=3, openTimeout=2m)`.

4. The resulting `[]ChainEntry` is loaded into the chain with `SetEntries`. The startup log prints one line per entry.

### Request Flow

When the gateway calls `FallbackChain.Complete` (or `CompleteStream`), the chain iterates entries in order:

```
for each entry in order:
    if entry is not available → skip (cooldown or circuit open)
    if rate limiter says skip → skip (proactive, not local)
    call provider.Complete(ctx, req)
    if success → return response
    if 429   → mark entry Exhausted, set cooldown, try next
    if 5xx   → record circuit failure, try next
    if other → record circuit failure, try next
if no entry succeeded → return "all providers exhausted" error
```

The local llama.cpp entry is never skipped by the rate limiter (`IsLocalFallback=true`), so it always gets a chance as the final fallback.

### Rate Limit Handling

Two mechanisms protect against quota exhaustion:

**Reactive (429 response):** When a provider returns HTTP 429, the chain immediately marks that entry `Exhausted` and calculates a cooldown:

- If the response includes a `Retry-After` header (integer seconds or HTTP-date), that value is used.
- Otherwise, exponential backoff applies: 60s → 120s → 240s → 480s → capped at 15 minutes.

The entry is skipped for all requests during the cooldown window and automatically reactivated once the window elapses.

**Proactive (header inspection):** `RateLimitTracker` reads `x-ratelimit-remaining-requests` and `x-ratelimit-remaining-tokens` from every successful response. When remaining values drop below the configured minimums (default: 5 requests, 1,000 tokens), the provider is deprioritised on the next request without waiting for a 429.

The backoff counter resets to zero on each successful response.

### Circuit Breaker

Each chain entry has an independent circuit breaker with three states:

| State | Condition | Behaviour |
|-------|-----------|-----------|
| **Closed** | Normal operation | All requests pass through |
| **Open** | 3 consecutive failures (5xx, network errors, or timeouts) | Entry is skipped entirely. No requests sent. |
| **Half-open** | 2 minutes after opening | One probe request is allowed. Success → Closed. Failure → Open again. |

**Important:** HTTP 429 rate-limit responses do not count toward the circuit breaker threshold. They are handled exclusively by `RateLimitTracker`.

### Score Refresh

`ScorerBridge.StartRefreshLoop` runs a background goroutine that calls `FetchScores` and rebuilds the entry list every `HELIX_LLM_SCORE_REFRESH_INTERVAL`. The chain is updated atomically via `SetEntries` under a write lock. In-flight requests use the ordering that was current at dispatch time and are not affected by mid-flight refreshes.

When the verifier is unreachable during a refresh cycle, the loop logs a warning and retains the existing entry ordering until the next successful fetch.

## Monitoring

### Startup Rank Log

The most direct view of the chain is the startup log. Each entry appears as a structured log line:

```
INFO fallback chain entry  rank=1  provider=openrouter  model=meta-llama/llama-3.3-70b-instruct:free  score=90  local=false
INFO fallback chain entry  rank=2  provider=chutes       model=chutesai/Llama-4-Scout-17B-16E-Instruct  score=85  local=false
INFO fallback chain entry  rank=3  provider=huggingface  model=Qwen2.5-72B-Instruct                     score=80  local=false
INFO fallback chain entry  rank=7  provider=llamacpp     model=Qwen2.5-Coder-3B-Instruct-Q4_K_M        score=10  local=true
```

A provider missing from this list means its API key was not set in `.env`.

### Rate Limit Events

When a provider returns 429 or is deprioritised proactively, the chain logs a warning:

```
WARN fallback: rate-limited, cooling down  provider=openrouter  cooldown=1m0s
WARN fallback: skipping rate-limited entry  provider=chutes
```

### Circuit Breaker Events

```
WARN fallback: upstream 5xx, recording circuit failure  provider=nvidia  status=503
WARN fallback: skipping unavailable entry               provider=nvidia
```

After the 2-minute open window:

```
# Half-open probe succeeds — circuit closes silently (no log, RecordSuccess resets counter)
# Half-open probe fails — circuit re-opens:
WARN fallback: upstream 5xx, recording circuit failure  provider=nvidia  status=500
```

### Score Refresh

```
DEBUG scorer_bridge: chain entries refreshed  count=7
```

This appears every `HELIX_LLM_SCORE_REFRESH_INTERVAL`. If the verifier is unreachable:

```
WARN scorer_bridge: verifier unreachable, using static scores  url=http://localhost:7061/api/scores  error=...
```

### All Providers Exhausted

If every entry is unavailable simultaneously, the gateway receives:

```
all providers exhausted, last error: <last provider error>
```

The gateway returns HTTP 503 to the client.

### Enable Debug Logging

To see every skip decision and provider attempt:

```bash
HELIX_LOG_LEVEL=debug make dev
```

Debug-level messages include per-entry availability checks, which entry was selected, and the model override applied.

## Troubleshooting

### "All providers exhausted" on every request

All cloud providers are simultaneously in cooldown or their circuits are open, and llama.cpp is not responding.

Steps:
1. Check if llama.cpp is running: `curl http://localhost:50052/health`
2. Review logs for `rate-limited, cooling down` entries and their cooldown durations.
3. Add more provider API keys to increase the pool. With seven providers, simultaneous exhaustion is very unlikely.
4. If only one or two providers are configured, the chain is thin — a brief rate-limit event on all configured providers causes this error.

### Slow responses on every request

The chain is consistently reaching llama.cpp because all cloud providers are rate-limited or have open circuits.

Steps:
1. Check the startup log: how many providers are in the chain?
2. Add the missing provider API keys (see [Provider Setup](#provider-setup)).
3. Check `WARN fallback: rate-limited` messages — if they are frequent, you are exhausting free-tier quotas. Spread load across more providers.

### No models discovered for a provider

The provider is registered (API key set) but `discoverProviderModels` could not fetch a model list. The chain entry will have an empty `ModelID`, which means the provider uses whatever its default model is.

Steps:
1. Set `HELIX_LOG_LEVEL=debug` and look for `FetchModels` errors.
2. Verify the API key is valid by making a direct test call:

```bash
curl https://openrouter.ai/api/v1/models \
  -H "Authorization: Bearer $HELIX_LLM_OPENROUTER_KEY"
```

3. Check for network issues (proxy, firewall, DNS).

### Scores not refreshing

The score refresh log line (`scorer_bridge: chain entries refreshed`) does not appear.

Steps:
1. Verify `HELIX_LLM_VERIFIER_URL` is reachable from the HelixLLM process:

```bash
curl http://localhost:7061/api/scores
```

2. If the verifier is not running, that is expected — static scores are used and the chain still functions. The refresh log line only appears when the verifier responds successfully.
3. Check `HELIX_LLM_SCORE_REFRESH_INTERVAL` is a valid Go duration string (e.g. `5m`, `30s`). An unparseable value falls back to the 5-minute default.

### Provider is never tried despite having an API key

The provider's circuit breaker may be open from a previous failure burst.

Steps:
1. Restart HelixLLM to reset all circuit breaker state (circuit state is in-memory only and does not persist across restarts).
2. Check for `upstream 5xx` or `skipping unavailable entry` log messages to confirm the circuit is open.
3. Wait 2 minutes for the half-open probe — a single successful response closes the circuit.

### Memory sync not working

High-importance memories are not appearing in the HelixAgent memory service.

Steps:
1. Confirm `HELIX_LLM_MEMORY_SYNC_ENABLED=true` in `.env`.
2. Verify `HELIX_LLM_MEMORY_URL` points at a running HelixAgent instance.
3. Note that only memories with importance >= 0.7 are forwarded. Lower-importance memories remain local to the session.

---

## Related Guides

- [Configuration Reference](configuration.md) — all environment variables
- [Monitoring](monitoring.md) — Prometheus metrics, Grafana dashboards, health endpoints
- [Models](models.md) — local vs cloud model configuration and hardware profiling
- [Getting Started](getting-started.md) — first-run walkthrough
