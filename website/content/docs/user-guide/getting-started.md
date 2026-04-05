---
title: "Getting Started"
weight: 1
bookToC: true
---


This guide walks you through installing HelixLLM, running it for the first time, and making your first API call.

## Prerequisites

- **Go 1.24+** (the project uses Go 1.26.1 module syntax but builds with 1.24+)
- **openssl** -- used to generate self-signed TLS certificates for local development
- **Podman** (preferred) or **Docker** -- required for container builds and multi-host deployment
- **Git** -- with submodule support

Optional:
- **golangci-lint** -- for `make lint`
- **goimports** -- for `make fmt`

## Installation

Clone the repository with all submodules:

```bash
git clone --recurse-submodules https://github.com/HelixDevelopment/HelixLLM.git
cd HelixLLM
```

If you already cloned without `--recurse-submodules`, initialize them:

```bash
make deps
```

This runs `git submodule update --init --recursive` and `go mod tidy`.

## Configuration

Copy the example environment file:

```bash
cp .env.example .env
```

For a minimal local setup, the defaults work out of the box. The system starts in `full` mode with all subsystems active, listening on port 8443 with self-signed TLS.

To use cloud providers, add your API keys:

```bash
HELIX_LLM_OPENAI_KEY=sk-your-openai-key
HELIX_LLM_ANTHROPIC_KEY=sk-ant-your-anthropic-key
```

See [configuration.md](configuration.md) for the full variable reference.

## First Run

Generate TLS certificates and start the server:

```bash
make dev
```

This:
1. Creates self-signed TLS certificates in `./certs/` (if not already present)
2. Sets `HELIX_MODE=full`
3. Runs `go run ./cmd/helixllm`

You should see output like:

```
[GIN] mode=release
INFO starting HelixLLM                mode=full
INFO server listening                 addr=0.0.0.0:8443
```

## Your First API Call

The server is now running at `https://localhost:8443`. Since it uses a self-signed certificate, use `-k` with curl to skip verification.

### Health Check

```bash
curl -k https://localhost:8443/internal/health
```

Expected response:

```json
{
  "status": "healthy",
  "checks": []
}
```

### Chat Completion (OpenAI Compatible)

```bash
curl -k https://localhost:8443/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "Llama-3.1-70B-Instruct-Q4_K_M",
    "messages": [
      {"role": "user", "content": "Hello, what is HelixLLM?"}
    ]
  }'
```

### List Models

```bash
curl -k https://localhost:8443/v1/models
```

### Anthropic Messages API

```bash
curl -k https://localhost:8443/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "Hello!"}
    ]
  }'
```

## Single-Host Quick Start

For development or small-scale use, `full` mode is the simplest deployment. Everything runs in a single process:

1. **Copy config:** `cp .env.example .env`
2. **Set mode:** `HELIX_MODE=full` (this is the default)
3. **Start:** `make dev`
4. **Test:** `curl -k https://localhost:8443/internal/health`

All subsystems (gateway, brain, knowledge, agents, control plane) are active and communicate via direct Go function calls -- no network overhead.

## Building the Binary

To build a production binary:

```bash
make build
```

The binary is created at `./bin/helixllm`. Run it directly:

```bash
./bin/helixllm
```

Or with a specific mode:

```bash
./bin/helixllm --mode=gateway
```

## Building a Container Image

```bash
make container
```

This auto-detects Podman or Docker and builds the image as `helixllm:dev`.

## Next Steps

- [Configuration Reference](configuration.md) -- all environment variables and their defaults
- [API Reference](api-reference.md) -- complete endpoint documentation
- [Models](models.md) -- local vs cloud model configuration
- [Multi-Host Setup](multi-host-setup.md) -- deploying across multiple machines

---

## Platform-Specific Setup

### Linux

Most Linux distributions provide the required tools via package managers.

**Fedora / RHEL / ALT Linux:**

```bash
sudo dnf install golang openssl podman git
```

**Ubuntu / Debian:**

```bash
sudo apt update
sudo apt install golang openssl podman git
```

**Arch Linux:**

```bash
sudo pacman -S go openssl podman git
```

Verify Go version (1.24+ required):

```bash
go version
```

Install optional development tools:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install golang.org/x/tools/cmd/goimports@latest
```

On Linux, Podman runs rootless by default. No additional configuration is needed for container builds.

### macOS

Install prerequisites via Homebrew:

```bash
brew install go openssl podman git
```

Initialize the Podman machine (macOS requires a Linux VM for containers):

```bash
podman machine init
podman machine start
```

Verify the setup:

```bash
podman info
go version
```

Note: macOS uses libressl by default. The Homebrew openssl is required for `make certs` to generate certificates with the correct extensions.

### Windows (WSL 2)

HelixLLM runs on Windows via WSL 2. Native Windows builds are not supported.

1. Install WSL 2 with Ubuntu:

```powershell
wsl --install -d Ubuntu
```

2. Inside the WSL terminal, install dependencies:

```bash
sudo apt update
sudo apt install golang openssl podman git
```

3. Clone and build as on Linux:

```bash
git clone --recurse-submodules https://github.com/HelixDevelopment/HelixLLM.git
cd HelixLLM
make deps
make build
```

WSL 2 shares the host network, so the server at `https://localhost:8443` is accessible from Windows browsers and tools.

## GPU Setup

Local LLM inference via llama.cpp benefits significantly from GPU acceleration. HelixLLM delegates GPU management to the llama.cpp server process.

### NVIDIA GPU (CUDA)

1. Install the NVIDIA driver and CUDA toolkit:

```bash
# Fedora / RHEL
sudo dnf install nvidia-driver cuda-toolkit

# Ubuntu
sudo apt install nvidia-driver-550 nvidia-cuda-toolkit
```

2. Verify GPU detection:

```bash
nvidia-smi
```

3. The llama.cpp server automatically detects CUDA GPUs. Set the number of GPU layers to offload in your llama.cpp server configuration. More layers offloaded to GPU means faster inference but higher VRAM usage.

4. For multi-GPU setups, the control plane scheduler uses the `gpu-affinity` strategy:

```bash
HELIX_SCHEDULE_STRATEGY=gpu-affinity
```

### AMD GPU (ROCm)

1. Install ROCm:

```bash
sudo apt install rocm-hip-libraries rocm-dev
```

2. Verify with:

```bash
rocm-smi
```

3. Ensure your llama.cpp build includes ROCm support (`-DLLAMA_HIPBLAS=ON`).

### CPU-Only Mode

If no GPU is available, llama.cpp falls back to CPU inference. Performance depends on the CPU core count and available RAM. Quantized models (Q4_K_M, Q5_K_M) are recommended for CPU-only setups to reduce memory requirements.

For CPU-only deployments, consider using cloud providers instead:

```bash
HELIX_LLM_DEFAULT_PROVIDER=openai
# or
HELIX_LLM_DEFAULT_PROVIDER=anthropic
```

## First Agent Chat Walkthrough

The agent system provides a conversational interface with tool calling, session memory, and RAG-augmented responses.

### 1. Start the Server

```bash
make dev
```

Wait until you see `INFO server listening addr=0.0.0.0:8443`.

### 2. Send Your First Agent Message

```bash
curl -k https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "What time is it?"}
    ]
  }'
```

The agent uses its built-in `time` tool to respond with the current time. The response includes:

```json
{
  "session_id": "a1b2c3d4-...",
  "choices": [
    {
      "message": {
        "role": "assistant",
        "content": "The current time is 2026-04-05T14:30:00Z."
      }
    }
  ]
}
```

### 3. Continue the Conversation

Use the `session_id` from the previous response to maintain context:

```bash
curl -k https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "a1b2c3d4-...",
    "messages": [
      {"role": "user", "content": "And what timezone is that in?"}
    ]
  }'
```

The agent remembers the previous exchange and responds in context.

### 4. List Available Tools

```bash
curl -k https://localhost:8443/v1/agents/tools
```

Returns the registered tools (echo, time, knowledge_query, etc.) with their descriptions and parameter schemas.

### 5. Query Knowledge

If you have ingested documents into the knowledge layer, the agent automatically uses RAG to augment its responses:

```bash
# First, ingest some documents
curl -k https://localhost:8443/internal/knowledge/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Project README",
    "content": "HelixLLM is a distributed LLM serving platform...",
    "collection": "docs"
  }'

# Then ask the agent about it
curl -k https://localhost:8443/v1/agents/chat \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "What is HelixLLM?"}
    ]
  }'
```

The RAG hook automatically retrieves relevant chunks from the knowledge base and includes them in the agent's context.

## Troubleshooting

### Common First-Run Issues

| Problem | Cause | Solution |
|---------|-------|----------|
| `TLS handshake error` | Missing or expired certificates | Run `make certs` to regenerate self-signed certificates |
| `bind: address already in use` | Port 8443 is occupied by another process | Change `HELIX_PORT` in `.env` or stop the conflicting process with `lsof -i :8443` |
| `submodule path 'submodules/X' not initialized` | Submodules were not cloned | Run `make deps` to initialize all submodules |
| `go: module not found` | Missing replace directives or stale module cache | Run `make deps` then `go clean -modcache` if needed |
| `connection refused` on API calls | Server not running or wrong port | Verify with `curl -k https://localhost:8443/internal/health` |
| `401 Unauthorized` | API key auth is enabled but no key provided | Add `-H "Authorization: Bearer <key>"` or clear `HELIX_AUTH_API_KEYS` in `.env` |
| `no provider available` | No LLM provider is configured or reachable | Set at least one provider key (`HELIX_LLM_OPENAI_KEY` or `HELIX_LLM_ANTHROPIC_KEY`) or ensure llama.cpp is running |
| `certificate signed by unknown authority` | Using self-signed cert without `-k` flag | Add `-k` to curl commands, or import the cert into your system trust store |
| `QUIC handshake timeout` | Firewall blocking UDP on the server port | Ensure UDP is allowed on port 8443 or the configured `HELIX_PORT`; the server falls back to HTTP/2 over TCP |
| `permission denied` on SSH commands | SSH key not authorized on remote host | Add your public key to `~/.ssh/authorized_keys` on the target host |
| `race detected during execution` | Concurrent access issue (development only) | Report the full race trace as a bug; this should not occur in released builds |

### Diagnostic Commands

Check server health:

```bash
curl -k https://localhost:8443/internal/health
```

View available models:

```bash
curl -k https://localhost:8443/v1/models
```

Check cluster status (multi-host mode):

```bash
curl -k https://localhost:8443/internal/cluster/status
```

View knowledge store stats:

```bash
curl -k https://localhost:8443/internal/knowledge/stats
```

Enable debug logging for verbose output:

```bash
HELIX_LOG_LEVEL=debug make dev
```
