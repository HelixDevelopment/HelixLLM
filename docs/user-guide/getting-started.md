# Getting Started

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
