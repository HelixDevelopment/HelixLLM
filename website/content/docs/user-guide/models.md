---
title: "Models"
weight: 1
bookToC: true
---


HelixLLM supports local LLM inference via llama.cpp and cloud providers (OpenAI, Anthropic). The Brain layer routes requests to the appropriate provider based on model name, capability, cost, or latency.

## Provider Overview

| Provider | Models | Requires | Set By |
|----------|--------|----------|--------|
| Local (llama.cpp) | Any GGUF model | llama.cpp server running | `HELIX_LLM_DEFAULT_PROVIDER=local` |
| OpenAI | gpt-4o, gpt-4, o1, etc. | `HELIX_LLM_OPENAI_KEY` | `HELIX_LLM_DEFAULT_PROVIDER=openai` |
| Anthropic | claude-sonnet-4-20250514, claude-3.5-haiku, etc. | `HELIX_LLM_ANTHROPIC_KEY` | `HELIX_LLM_DEFAULT_PROVIDER=anthropic` |

## Local Models (llama.cpp)

The default local model is set by:

```bash
HELIX_LLM_LOCAL_MODEL=Llama-3.1-70B-Instruct-Q4_K_M
HELIX_LLM_LOCAL_RPC_PORT=50052
```

llama.cpp runs as a separate process or container. In a multi-host setup, the control plane deploys it on the host with the best GPU (gpu-affinity strategy).

### Supported Hardware

- **NVIDIA GPUs** via CUDA (Linux)
- **Apple Silicon** via Metal (macOS)
- **AMD GPUs** via ROCm (Linux)
- **CPU-only** fallback (any platform, slower)

### GGUF Model Format

llama.cpp uses GGUF quantized models. Common quantization levels:

| Quantization | Size (70B) | Quality | Speed |
|-------------|-----------|---------|-------|
| Q4_K_M | ~40 GB | Good | Fast |
| Q5_K_M | ~48 GB | Better | Moderate |
| Q6_K | ~55 GB | High | Slower |
| Q8_0 | ~70 GB | Highest quantized | Slowest |

### Multi-GPU Inference

llama.cpp supports distributed inference across multiple GPUs via its RPC protocol. In a multi-host cluster, the control plane sets up RPC workers on GPU hosts and connects them:

1. RPC worker runs on each GPU host (port 50052)
2. The master llama.cpp instance connects with the `--rpc` flag
3. Model layers are split across available GPUs

## Cloud Models

### OpenAI

Set your API key to enable OpenAI models:

```bash
HELIX_LLM_OPENAI_KEY=sk-your-key-here
```

Available models include all models supported by the OpenAI API (gpt-4o, gpt-4, o1, etc.). They appear in the `/v1/models` listing when the key is configured.

### Anthropic

Set your API key to enable Anthropic models:

```bash
HELIX_LLM_ANTHROPIC_KEY=sk-ant-your-key-here
```

Available models include Claude Sonnet, Haiku, and Opus variants. Use the Anthropic Messages API endpoint (`/v1/messages`) or the OpenAI-compatible endpoint.

## Default Provider

The `HELIX_LLM_DEFAULT_PROVIDER` variable controls which provider handles requests when no routing rule matches:

```bash
HELIX_LLM_DEFAULT_PROVIDER=local       # Always use local llama.cpp
HELIX_LLM_DEFAULT_PROVIDER=openai      # Always use OpenAI
HELIX_LLM_DEFAULT_PROVIDER=anthropic   # Always use Anthropic
HELIX_LLM_DEFAULT_PROVIDER=auto        # Route by model name
```

## Model Routing

When `HELIX_LLM_DEFAULT_PROVIDER=auto`, the Brain layer routes by model name:

| Model Pattern | Provider |
|--------------|----------|
| `gpt-*`, `o1-*` | OpenAI |
| `claude-*` | Anthropic |
| Everything else | Local (llama.cpp) |

Additional routing strategies:

- **By capability:** If the request needs a feature (e.g., vision), route to a provider that supports it
- **By cost:** Prefer local when quality threshold is met
- **By latency:** Route to the fastest responding backend
- **Fallback chain:** If the primary provider fails, try the next in the chain

## Switching Models at Request Time

Every API request accepts a `model` field. The Brain routes to the correct provider:

```bash
# Uses local llama.cpp
curl -k https://localhost:8443/v1/chat/completions \
  -d '{"model": "Llama-3.1-70B-Instruct-Q4_K_M", "messages": [...]}'

# Uses OpenAI
curl -k https://localhost:8443/v1/chat/completions \
  -d '{"model": "gpt-4o", "messages": [...]}'

# Uses Anthropic
curl -k https://localhost:8443/v1/messages \
  -d '{"model": "claude-sonnet-4-20250514", "messages": [...]}'
```

## Listing Available Models

```bash
curl -k https://localhost:8443/v1/models
```

Returns all models from all configured providers. Each model shows its provider in the `owned_by` field.

## Circuit Breakers

Each provider has an independent circuit breaker. If a provider fails repeatedly:

1. The circuit opens (requests fail fast without calling the provider)
2. After a cooldown, a test request is sent
3. If the test succeeds, the circuit closes and normal traffic resumes

This prevents cascading failures when a provider is down.
