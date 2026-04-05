# Lesson 1: Development Setup

**Duration:** 25 minutes
**Prerequisites:** Course 1 (Getting Started)
**Learning Objectives:**
- Set up a complete development workspace with submodules and IDE support
- Configure VS Code and GoLand for productive Go development
- Use the delve debugger to step through HelixLLM code
- Profile performance with pprof to identify bottlenecks

---

## Scene 1: Workspace Setup (5 min)

**Narration:** "Setting up a HelixLLM development environment means cloning the repository with all 37 submodules, installing Go and development tools, and configuring your IDE. Let me walk through each step."

**Demo steps:**

```bash
# Clone with all submodules
git clone --recurse-submodules https://github.com/HelixDevelopment/HelixLLM.git
cd HelixLLM

# If already cloned, initialize submodules
make deps
```

**Narration:** "The make deps command runs git submodule update --init --recursive followed by go mod tidy. This ensures all 37 submodules are checked out and Go dependencies are resolved."

```bash
# Verify the project structure
ls internal/
# Expected: agents  brain  control  gateway  knowledge  mode  server  shared

ls submodules/ | head -10
# Expected: Auth  BackgroundTasks  Caching  Challenges  Config  ...

# Verify the build works
make build
ls -lh bin/helixllm
```

**Key points:**
- 37 Git submodules under `submodules/` mapped via `replace` directives in `go.mod`
- Each submodule has its own `CLAUDE.md` with documentation
- `make deps` handles both submodule initialization and Go module tidying
- Always run `make deps` after pulling changes that update submodule references

---

## Scene 2: Development Tools (5 min)

**Narration:** "Beyond Go itself, there are several tools that make HelixLLM development more productive."

**Demo steps:**

```bash
# Required: Go 1.24+
go version

# Linting
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
golangci-lint --version

# Import organization
go install golang.org/x/tools/cmd/goimports@latest

# Debugger
go install github.com/go-delve/delve/cmd/dlv@latest
dlv version

# Run all linters
make lint

# Format all code
make fmt
```

**Narration:** "make lint runs golangci-lint with the project's configuration. make fmt runs both gofmt and goimports to ensure consistent formatting. Run both before committing."

**Key points:**
- `golangci-lint` -- comprehensive linting with multiple analyzers
- `goimports` -- organizes imports and formats code
- `delve` -- Go debugger for stepping through code
- `make lint` and `make fmt` should pass before every commit
- `make gen` runs go generate for any code generation

---

## Scene 3: IDE Configuration (5 min)

**Narration:** "Let me show you how to configure the two most popular Go IDEs for HelixLLM development."

**Screen:** VS Code configuration.

```json
// .vscode/settings.json (recommended)
{
    "go.toolsManagement.autoUpdate": true,
    "go.useLanguageServer": true,
    "go.lintTool": "golangci-lint",
    "go.lintFlags": ["--fast"],
    "go.testFlags": ["-v", "-count=1"],
    "go.buildFlags": ["-ldflags=-s -w"],
    "editor.formatOnSave": true,
    "editor.defaultFormatter": "golang.go",
    "go.testTimeout": "120s"
}
```

**Narration:** "VS Code with the official Go extension provides full language server support via gopls. Enable format on save and configure golangci-lint as the lint tool."

**Screen:** GoLand configuration.

**Narration:** "In GoLand, the project should be recognized automatically. Verify that the Go SDK is set correctly and that the project uses Go Modules."

1. **File > Settings > Go > GOROOT** -- set to your Go installation
2. **File > Settings > Go > Go Modules** -- ensure "Enable Go modules integration" is checked
3. **File > Settings > Tools > File Watchers** -- add goimports as a file watcher
4. **Run > Edit Configurations** -- create a Go Build configuration pointing to `cmd/helixllm/main.go`

**Key points:**
- VS Code: install the Go extension, configure golangci-lint
- GoLand: ensure Go Modules integration is enabled
- Both IDEs should recognize the `replace` directives in `go.mod`
- Set test timeout to 120s for integration tests
- Configure the run target as `cmd/helixllm/main.go`

---

## Scene 4: Debugging with Delve (5 min)

**Narration:** "The delve debugger lets you set breakpoints, step through code, and inspect variables in a running HelixLLM instance."

**Demo steps:**

```bash
# Start HelixLLM under the debugger
dlv debug ./cmd/helixllm -- --mode=full

# Inside delve:
# Set a breakpoint on the chat completion handler
(dlv) break internal/gateway/openai.go:42
(dlv) continue

# Now send a request from another terminal:
# curl -sk https://localhost:8443/v1/chat/completions ...

# Delve hits the breakpoint
(dlv) print req
(dlv) next
(dlv) step
(dlv) continue
```

**Narration:** "You can also attach to a running process or use your IDE's debugger. Both VS Code and GoLand have built-in delve integration."

```bash
# Debug a specific test
dlv test ./internal/brain/ -- -test.run TestRouter

# Inside delve:
(dlv) break TestRouter
(dlv) continue
(dlv) print provider
```

**Key points:**
- `dlv debug ./cmd/helixllm` starts the server under the debugger
- Set breakpoints on specific files and line numbers
- `print`, `next`, `step`, `continue` are the core commands
- IDE debuggers (VS Code, GoLand) use delve under the hood
- Debug tests with `dlv test ./package/ -- -test.run TestName`

---

## Scene 5: Profiling with pprof (5 min)

**Narration:** "When you need to optimize performance, Go's built-in pprof profiler shows you exactly where CPU time and memory are spent."

**Demo steps:**

```bash
# Start HelixLLM (pprof endpoints are available in development)
make dev

# CPU profile: capture 30 seconds of CPU activity
go tool pprof https://localhost:8443/debug/pprof/profile?seconds=30

# During the 30-second capture, send traffic:
# for i in $(seq 1 100); do curl -sk https://localhost:8443/v1/models; done

# In the pprof interactive shell:
(pprof) top 20
(pprof) web           # Opens a browser with the call graph
(pprof) list HandleChatCompletions   # Source-level annotation
```

```bash
# Memory profile
go tool pprof https://localhost:8443/debug/pprof/heap

(pprof) top 20
(pprof) web
```

```bash
# Goroutine profile (check for leaks)
go tool pprof https://localhost:8443/debug/pprof/goroutine

(pprof) top 20
```

**Key points:**
- CPU profile: identifies hot code paths
- Heap profile: shows memory allocation patterns
- Goroutine profile: detects goroutine leaks
- `top` shows the most expensive functions
- `web` generates a visual call graph in the browser
- Profile under realistic load for meaningful results

---

## Exercises

1. Clone HelixLLM, run `make deps`, and verify the build succeeds with `make build`, then set up your preferred IDE with Go language server support
2. Use delve to set a breakpoint in `internal/gateway/openai.go` on the chat completion handler, send a request, and inspect the request object when the breakpoint hits
3. Run a CPU profile while sending 50 concurrent requests to `/v1/models` and identify the top three functions by CPU time
