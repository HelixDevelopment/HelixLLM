---
title: "Development Guide"
weight: 1
bookToC: true
---


How to build HelixLLM from source, run tests, and extend the system.

## Prerequisites

- **Go 1.24+**
- **Git** with submodule support
- **openssl** for TLS certificate generation
- **golangci-lint** for linting
- **goimports** for formatting

## Building from Source

### Clone and Initialize

```bash
git clone --recurse-submodules https://github.com/HelixDevelopment/HelixLLM.git
cd HelixLLM
```

If submodules are missing:

```bash
make deps
```

### Build the Binary

```bash
make build
```

Output: `./bin/helixllm` (stripped, no debug symbols via `-ldflags="-s -w"`).

### Run in Development Mode

```bash
make dev
```

This generates TLS certs (if missing) and runs in `full` mode with `go run`.

## Project Layout

```
cmd/helixllm/main.go       Entry point -- config, mode, wiring, server start
internal/
  gateway/                  API handlers and middleware
  brain/                    LLM provider management and routing
  knowledge/                RAG pipeline (chunking, embedding, storage)
  agents/                   ReAct agent loop, tools, sessions
  control/                  Cluster management (probing, scheduling, deploying)
  mode/                     Mode enum and parsing
  server/                   HTTP/3 + HTTP/2 server
  shared/                   Config, events, health, logging, observability
pkg/
  api/                      Public request/response types
  types/                    Shared internal types
submodules/                 43 Git submodules
tests/                      Integration tests
challenges/                 Challenge banks
```

## Adding a New Module

### 1. Add the Gateway Endpoint

If your module needs HTTP endpoints, register them in `cmd/helixllm/main.go`:

```go
// In main(), after server creation:
mymodule.RegisterRoutes(srv.Router(), mymodule.Options{...})
```

### 2. Create the Package

Create a new package under `internal/`:

```
internal/mymodule/
  mymodule.go       Core logic
  mymodule_test.go  Tests
  api.go            HTTP handlers (if needed)
  api_test.go       Handler tests
```

### 3. Register Routes

Follow the pattern used by existing modules:

```go
package mymodule

import "github.com/gin-gonic/gin"

func RegisterRoutes(r *gin.Engine, opts Options) {
    g := r.Group("/internal/mymodule")
    g.GET("/status", handleStatus(opts))
    g.POST("/action", handleAction(opts))
}
```

### 4. Write Tests

Every file needs a corresponding `_test.go`:

```go
package mymodule

import (
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/gin-gonic/gin"
)

func TestHandleStatus(t *testing.T) {
    gin.SetMode(gin.TestMode)
    r := gin.New()
    RegisterRoutes(r, Options{})

    req := httptest.NewRequest(http.MethodGet, "/internal/mymodule/status", nil)
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("expected 200, got %d", w.Code)
    }
}
```

## Adding a New LLM Provider

1. Implement the provider interface in `internal/brain/`:

```go
type Provider interface {
    Name() string
    Models() []string
    ChatCompletion(ctx context.Context, req *types.InternalChatRequest) (*types.InternalChatResponse, error)
}
```

2. Register it in `brain.New()` based on configuration.

3. Update the router to recognize the provider's model patterns.

## Adding a New Agent Tool

1. Create a new file in `internal/agents/tools/`:

```go
package tools

import "context"

type MyTool struct{}

func (t *MyTool) Name() string                          { return "my_tool" }
func (t *MyTool) Description() string                   { return "Does something useful" }
func (t *MyTool) Parameters() map[string]interface{}     { return map[string]interface{}{} }
func (t *MyTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
    return "result", nil
}
```

2. Register it in `cmd/helixllm/main.go`:

```go
toolReg.Register(&tools.MyTool{})
```

## Make Targets

| Target | Description |
|--------|-------------|
| `make build` | Build the binary to `./bin/helixllm` |
| `make dev` | Generate certs and run in full mode |
| `make test-unit` | Run unit tests with coverage report |
| `make test-integration` | Run integration tests |
| `make test-all` | Run all test types |
| `make coverage` | Check coverage meets 85% threshold |
| `make lint` | Run golangci-lint |
| `make fmt` | Run gofmt and goimports |
| `make gen` | Run go generate |
| `make deps` | Update submodules and tidy go.mod |
| `make clean` | Remove build artifacts, coverage files, certs |
| `make certs` | Generate self-signed TLS certificates |
| `make container` | Build container image (auto-detects Podman/Docker) |
| `make container-push` | Push container image to registry |

## Code Style

- Follow standard Go conventions (`gofmt`, `goimports`)
- Package comments on every package
- Doc comments on all exported types and functions
- Error wrapping with `fmt.Errorf("context: %w", err)`
- Table-driven tests preferred
- No external test frameworks -- use the standard `testing` package

## Submodule Management

Submodules live in `submodules/` and are referenced via `replace` directives in `go.mod`:

```
replace digital.vasic.config => ./submodules/Config
```

To update a submodule:

```bash
cd submodules/Config
git pull origin main
cd ../..
go mod tidy
```

To update all:

```bash
make deps
```

## Container Build

The Containerfile uses multi-stage builds:

1. **Builder stage:** Full Go toolchain, compiles the binary
2. **Runtime stage:** Minimal image with just the binary

```bash
make container         # Build
make container-push    # Push to registry
```

Compatible with both Podman and Docker (no Docker-specific features used).

## Environment Variables for Development

Useful overrides for local development:

```bash
HELIX_MODE=full
HELIX_PORT=8443
HELIX_LOG_LEVEL=debug
HELIX_LOG_FORMAT=text
HELIX_OTEL_EXPORTER=stdout
HELIX_LLM_DEFAULT_PROVIDER=local
```

## External Dependencies

Some submodules reference modules that live outside the HelixLLM repository:

| Module | Required By | Purpose | Setup |
|--------|-------------|---------|-------|
| `digital.vasic.models` | LLMProvider, BackgroundTasks | Shared model type definitions | Clone from vasic-digital org, place at `../Models` relative to submodule |
| `digital.vasic.messaging` | conversation | Messaging abstractions | Clone from vasic-digital org, place at `../Messaging` relative to submodule |
| `digital.vasic.docprocessor` | HelixQA | Document processing | Clone from vasic-digital org, place at `../DocProcessor` |
| `digital.vasic.visionengine` | HelixQA | Vision processing | Clone from vasic-digital org, place at `../VisionEngine` |

These are only needed when working directly on the listed submodules. The main HelixLLM binary builds without them.
