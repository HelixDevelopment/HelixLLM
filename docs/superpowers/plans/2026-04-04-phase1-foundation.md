# Phase 1: Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the project scaffold, shared foundation layer (Config, EventBus, Observability), mode system CLI, and a basic Gin HTTP/3 server that proves the architecture works end-to-end.

**Architecture:** Single Go binary with mode-based startup. Shared foundation modules (Config, EventBus, Observability) are vasic-digital Git submodules linked via `replace` directives. The CLI parses `--mode` flag and starts the appropriate Gin server. All code is TDD — tests first.

**Tech Stack:** Go 1.24+, Gin Gonic, quic-go (HTTP/3), andybalholm/brotli, vasic-digital modules (Config, EventBus, Observability, Concurrency, Lazy, Watcher, Containers)

**Spec Reference:** `docs/superpowers/specs/2026-04-04-helixllm-master-design.md` — Sections 1-3, 10, 12, 14 (Phase 1)

---

## File Structure

```
helixllm/
  cmd/helixllm/
    main.go                    CLI entry, flag parsing, mode routing
  internal/
    mode/
      mode.go                  Mode enum and validation
      mode_test.go
    shared/
      config/
        config.go              HelixConfig struct, loading, validation
        config_test.go
      events/
        events.go              EventBus setup, topic constants
        events_test.go
      logging/
        logging.go             Logger setup wrapping Observability
        logging_test.go
      health/
        health.go              Health aggregator setup
        health_test.go
      observability/
        observability.go       Tracer + Metrics setup
        observability_test.go
    server/
      server.go                HTTP/3 + HTTP/2 Gin server with Alt-Svc
      server_test.go
      middleware/
        compression.go         Brotli + gzip content encoding negotiation
        compression_test.go
        requestid.go           Request ID generation + correlation
        requestid_test.go
  container/
    Containerfile              Multi-stage Podman/Docker build
  deploy/
    compose.yaml               Minimal compose for dev (just HelixLLM)
  .env.example                 All Phase 1 config variables
  .gitignore                   Updated with Go-specific ignores
  .gitmodules                  Submodule definitions
  Makefile                     Build, test, dev, lint, fmt, container targets
  go.mod
  go.sum
```

---

### Task 1: Project Scaffold

**Files:**
- Create: `go.mod`
- Create: `cmd/helixllm/main.go` (placeholder)
- Create: `.env.example`
- Modify: `.gitignore`

- [ ] **Step 1: Initialize Go module**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go mod init github.com/HelixDevelopment/HelixLLM
```

Expected: `go.mod` created with module path `github.com/HelixDevelopment/HelixLLM`

- [ ] **Step 2: Create directory structure**

```bash
mkdir -p cmd/helixllm internal/mode internal/shared/config internal/shared/events internal/shared/logging internal/shared/health internal/shared/observability internal/server/middleware container deploy tests/unit tests/integration challenges/banks pkg/api pkg/types docs/user-guide docs/manual
```

- [ ] **Step 3: Create placeholder main.go**

Create `cmd/helixllm/main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("helixllm")
}
```

- [ ] **Step 4: Verify build**

```bash
go build -o bin/helixllm ./cmd/helixllm
./bin/helixllm
```

Expected output: `helixllm`

- [ ] **Step 5: Create .env.example**

Create `.env.example`:

```bash
# ── HelixLLM Configuration ──────────────────────────────
# Copy this file to .env and fill in your values.
# .env is gitignored and will not be committed.

# ── Mode ─────────────────────────────────────────────────
HELIX_MODE=full
# full | gateway | brain | knowledge | agents | control

# ── Cluster ──────────────────────────────────────────────
HELIX_HOSTS=nezha.local,thinker.local,amber.local
HELIX_SSH_USER=milosvasic
HELIX_SSH_KEY=~/.ssh/id_ed25519

# ── Container Runtime ────────────────────────────────────
HELIX_CONTAINER_RUNTIME=auto
# auto | podman | docker

# ── Scheduling ───────────────────────────────────────────
HELIX_SCHEDULE_STRATEGY=auto
# auto | binpack | spread | gpu-affinity | memory-first | latency

# ── Server ───────────────────────────────────────────────
HELIX_HOST=0.0.0.0
HELIX_PORT=8443
HELIX_TLS_CERT=./certs/cert.pem
HELIX_TLS_KEY=./certs/key.pem

# ── LLM ──────────────────────────────────────────────────
HELIX_LLM_LOCAL_MODEL=Llama-3.1-70B-Instruct-Q4_K_M
HELIX_LLM_LOCAL_RPC_PORT=50052
HELIX_LLM_OPENAI_KEY=
HELIX_LLM_ANTHROPIC_KEY=
HELIX_LLM_DEFAULT_PROVIDER=local
# local | openai | anthropic | auto

# ── Knowledge ────────────────────────────────────────────
HELIX_VECTOR_DB=qdrant
# qdrant | pgvector | milvus | pinecone
HELIX_EMBEDDING_PROVIDER=local
# local | openai | cohere | voyage | jina
HELIX_EMBEDDING_MODEL=all-mpnet-base-v2
HELIX_RAG_CHUNK_SIZE=1000
HELIX_RAG_CHUNK_OVERLAP=200
HELIX_RAG_TOP_K=5

# ── Database ─────────────────────────────────────────────
HELIX_DB_HOST=localhost
HELIX_DB_PORT=5432
HELIX_DB_NAME=helixllm
HELIX_DB_USER=helix
HELIX_DB_PASSWORD=

# ── Cache ────────────────────────────────────────────────
HELIX_REDIS_HOST=localhost
HELIX_REDIS_PORT=6379
HELIX_REDIS_PASSWORD=

# ── Messaging ────────────────────────────────────────────
HELIX_KAFKA_BROKERS=localhost:9092

# ── Observability ────────────────────────────────────────
HELIX_OTEL_ENDPOINT=http://localhost:4317
HELIX_OTEL_EXPORTER=none
# none | stdout | otlp | jaeger | zipkin
HELIX_PROMETHEUS_PORT=9090
HELIX_GRAFANA_PORT=3001
HELIX_LOG_LEVEL=info
# debug | info | warn | error
HELIX_LOG_FORMAT=text
# text | json

# ── Auth ─────────────────────────────────────────────────
HELIX_AUTH_JWT_SECRET=
HELIX_AUTH_API_KEYS=
# Comma-separated API keys

# ── Feature Flags ────────────────────────────────────────
HELIX_FEATURE_GRPC=true
HELIX_FEATURE_TOON=true
HELIX_FEATURE_SELFIMPROVE=false
```

- [ ] **Step 6: Update .gitignore**

Update `.gitignore` to:

```gitignore
# Environment
.env

# Superpowers
.superpowers/

# Go
bin/
*.exe
*.exe~
*.dll
*.so
*.dylib
*.test
*.out
vendor/

# TLS certs (dev)
certs/

# IDE
.idea/
.vscode/
*.swp
*.swo

# OS
.DS_Store
Thumbs.db
```

- [ ] **Step 7: Commit scaffold**

```bash
git add go.mod cmd/helixllm/main.go .env.example .gitignore
git commit -m "feat: initialize Go module and project scaffold"
```

---

### Task 2: Add Git Submodules

**Files:**
- Create: `.gitmodules`
- Create: `submodules/` directory with all Phase 1 submodules

- [ ] **Step 1: Add shared foundation submodules from vasic-digital (GitHub)**

```bash
git submodule add git@github.com:vasic-digital/Config.git submodules/Config
git submodule add git@github.com:vasic-digital/EventBus.git submodules/EventBus
git submodule add git@github.com:vasic-digital/Observability.git submodules/Observability
git submodule add git@github.com:vasic-digital/Concurrency.git submodules/Concurrency
git submodule add git@github.com:vasic-digital/Lazy.git submodules/Lazy
git submodule add git@github.com:vasic-digital/Watcher.git submodules/Watcher
git submodule add git@github.com:vasic-digital/Containers.git submodules/Containers
git submodule add git@github.com:vasic-digital/Middleware.git submodules/Middleware
```

- [ ] **Step 2: Add testing submodule**

```bash
git submodule add git@github.com:vasic-digital/Challenges.git submodules/Challenges
```

- [ ] **Step 3: Add replace directives to go.mod**

Add these `replace` directives to `go.mod` to point imports to local submodules:

```
replace (
	digital.vasic.config => ./submodules/Config
	digital.vasic.eventbus => ./submodules/EventBus
	digital.vasic.observability => ./submodules/Observability
	digital.vasic.concurrency => ./submodules/Concurrency
	digital.vasic.lazy => ./submodules/Lazy
	digital.vasic.watcher => ./submodules/Watcher
	digital.vasic.containers => ./submodules/Containers
	digital.vasic.middleware => ./submodules/Middleware
	digital.vasic.challenges => ./submodules/Challenges
)
```

Also add the `require` block for these modules. The exact versions will be resolved from each submodule's `go.mod`. Add:

```
require (
	digital.vasic.config v0.0.0
	digital.vasic.eventbus v0.0.0
	digital.vasic.observability v0.0.0
	digital.vasic.concurrency v0.0.0
	digital.vasic.lazy v0.0.0
	digital.vasic.watcher v0.0.0
	digital.vasic.containers v0.0.0
	digital.vasic.middleware v0.0.0
	digital.vasic.challenges v0.0.0
	github.com/gin-gonic/gin v1.10.0
	github.com/quic-go/quic-go v0.48.0
	github.com/andybalholm/brotli v1.1.0
)
```

- [ ] **Step 4: Tidy modules**

```bash
go mod tidy
```

Expected: `go.sum` populated, all dependencies resolved. If any submodule module paths differ from the assumed `digital.vasic.*`, adjust the `replace` directives to match the actual `module` line in each submodule's `go.mod`.

- [ ] **Step 5: Verify submodules resolve**

```bash
go build ./...
```

Expected: builds successfully (main.go is still a placeholder, but module resolution works).

- [ ] **Step 6: Commit submodules**

```bash
git add .gitmodules submodules/ go.mod go.sum
git commit -m "feat: add shared foundation Git submodules with replace directives"
```

---

### Task 3: Mode System

**Files:**
- Create: `internal/mode/mode.go`
- Create: `internal/mode/mode_test.go`

- [ ] **Step 1: Write failing tests for Mode type**

Create `internal/mode/mode_test.go`:

```go
package mode_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/mode"
)

func TestModeString(t *testing.T) {
	tests := []struct {
		m    mode.Mode
		want string
	}{
		{mode.Full, "full"},
		{mode.Gateway, "gateway"},
		{mode.Brain, "brain"},
		{mode.Knowledge, "knowledge"},
		{mode.Agents, "agents"},
		{mode.Control, "control"},
	}
	for _, tt := range tests {
		if got := tt.m.String(); got != tt.want {
			t.Errorf("Mode.String() = %q, want %q", got, tt.want)
		}
	}
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		input   string
		want    mode.Mode
		wantErr bool
	}{
		{"full", mode.Full, false},
		{"gateway", mode.Gateway, false},
		{"brain", mode.Brain, false},
		{"knowledge", mode.Knowledge, false},
		{"agents", mode.Agents, false},
		{"control", mode.Control, false},
		{"FULL", mode.Full, false},
		{"Gateway", mode.Gateway, false},
		{"invalid", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		got, err := mode.Parse(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("Parse(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("Parse(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestModeAll(t *testing.T) {
	all := mode.All()
	if len(all) != 6 {
		t.Errorf("All() returned %d modes, want 6", len(all))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/mode/ -v
```

Expected: FAIL — package does not exist yet.

- [ ] **Step 3: Implement Mode type**

Create `internal/mode/mode.go`:

```go
package mode

import (
	"fmt"
	"strings"
)

// Mode represents an operating mode for the HelixLLM binary.
type Mode int

const (
	Full      Mode = iota + 1 // All-in-one, single process
	Gateway                   // API surface
	Brain                     // LLM coordination
	Knowledge                 // RAG pipeline
	Agents                    // Multi-agent system
	Control                   // Cluster management
)

var (
	modeNames = map[Mode]string{
		Full:      "full",
		Gateway:   "gateway",
		Brain:     "brain",
		Knowledge: "knowledge",
		Agents:    "agents",
		Control:   "control",
	}
	nameModes = map[string]Mode{}
)

func init() {
	for m, name := range modeNames {
		nameModes[name] = m
	}
}

func (m Mode) String() string {
	if name, ok := modeNames[m]; ok {
		return name
	}
	return fmt.Sprintf("unknown(%d)", int(m))
}

// Parse converts a string to a Mode. Case-insensitive.
func Parse(s string) (Mode, error) {
	if m, ok := nameModes[strings.ToLower(strings.TrimSpace(s))]; ok {
		return m, nil
	}
	return 0, fmt.Errorf("unknown mode: %q (valid: full, gateway, brain, knowledge, agents, control)", s)
}

// All returns all valid modes.
func All() []Mode {
	return []Mode{Full, Gateway, Brain, Knowledge, Agents, Control}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/mode/ -v -count=1
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mode/
git commit -m "feat: add Mode type with parsing and validation"
```

---

### Task 4: Configuration System

**Files:**
- Create: `internal/shared/config/config.go`
- Create: `internal/shared/config/config_test.go`

- [ ] **Step 1: Write failing tests for HelixConfig**

Create `internal/shared/config/config_test.go`:

```go
package config_test

import (
	"os"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/config"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Mode != "full" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "full")
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "0.0.0.0")
	}
	if cfg.Server.Port != 8443 {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 8443)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "info")
	}
}

func TestLoadFromEnv(t *testing.T) {
	os.Setenv("HELIX_MODE", "gateway")
	os.Setenv("HELIX_PORT", "9999")
	os.Setenv("HELIX_LOG_LEVEL", "debug")
	defer func() {
		os.Unsetenv("HELIX_MODE")
		os.Unsetenv("HELIX_PORT")
		os.Unsetenv("HELIX_LOG_LEVEL")
	}()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Mode != "gateway" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "gateway")
	}
	if cfg.Server.Port != 9999 {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 9999)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "debug")
	}
}

func TestConfigValidation(t *testing.T) {
	cfg := &config.HelixConfig{Mode: "invalid"}
	err := cfg.Validate()
	if err == nil {
		t.Error("Validate() should fail for invalid mode")
	}

	cfg = &config.HelixConfig{Mode: "full"}
	cfg.Server.Port = 8443
	cfg.Log.Level = "info"
	cfg.Log.Format = "text"
	err = cfg.Validate()
	if err != nil {
		t.Errorf("Validate() error = %v for valid config", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/shared/config/ -v
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement HelixConfig**

Create `internal/shared/config/config.go`:

```go
package config

import (
	"fmt"
	"strings"

	"digital.vasic.config/pkg/env"
)

// HelixConfig is the root configuration for HelixLLM.
type HelixConfig struct {
	Mode string `env:"HELIX_MODE" default:"full"`

	Hosts  string `env:"HELIX_HOSTS" default:"nezha.local"`
	SSHUser string `env:"HELIX_SSH_USER" default:"milosvasic"`
	SSHKey  string `env:"HELIX_SSH_KEY" default:"~/.ssh/id_ed25519"`

	ContainerRuntime  string `env:"HELIX_CONTAINER_RUNTIME" default:"auto"`
	ScheduleStrategy  string `env:"HELIX_SCHEDULE_STRATEGY" default:"auto"`

	Server ServerConfig
	LLM    LLMConfig
	Knowledge KnowledgeConfig
	DB     DatabaseConfig
	Cache  CacheConfig
	Messaging MessagingConfig
	Log    LogConfig
	Auth   AuthConfig
	Features FeatureConfig
}

type ServerConfig struct {
	Host    string `env:"HELIX_HOST" default:"0.0.0.0"`
	Port    int    `env:"HELIX_PORT" default:"8443"`
	TLSCert string `env:"HELIX_TLS_CERT" default:"./certs/cert.pem"`
	TLSKey  string `env:"HELIX_TLS_KEY" default:"./certs/key.pem"`
}

type LLMConfig struct {
	LocalModel      string `env:"HELIX_LLM_LOCAL_MODEL" default:"Llama-3.1-70B-Instruct-Q4_K_M"`
	LocalRPCPort    int    `env:"HELIX_LLM_LOCAL_RPC_PORT" default:"50052"`
	OpenAIKey       string `env:"HELIX_LLM_OPENAI_KEY"`
	AnthropicKey    string `env:"HELIX_LLM_ANTHROPIC_KEY"`
	DefaultProvider string `env:"HELIX_LLM_DEFAULT_PROVIDER" default:"local"`
}

type KnowledgeConfig struct {
	VectorDB          string `env:"HELIX_VECTOR_DB" default:"qdrant"`
	EmbeddingProvider string `env:"HELIX_EMBEDDING_PROVIDER" default:"local"`
	EmbeddingModel    string `env:"HELIX_EMBEDDING_MODEL" default:"all-mpnet-base-v2"`
	RAGChunkSize      int    `env:"HELIX_RAG_CHUNK_SIZE" default:"1000"`
	RAGChunkOverlap   int    `env:"HELIX_RAG_CHUNK_OVERLAP" default:"200"`
	RAGTopK           int    `env:"HELIX_RAG_TOP_K" default:"5"`
}

type DatabaseConfig struct {
	Host     string `env:"HELIX_DB_HOST" default:"localhost"`
	Port     int    `env:"HELIX_DB_PORT" default:"5432"`
	Name     string `env:"HELIX_DB_NAME" default:"helixllm"`
	User     string `env:"HELIX_DB_USER" default:"helix"`
	Password string `env:"HELIX_DB_PASSWORD"`
}

type CacheConfig struct {
	RedisHost     string `env:"HELIX_REDIS_HOST" default:"localhost"`
	RedisPort     int    `env:"HELIX_REDIS_PORT" default:"6379"`
	RedisPassword string `env:"HELIX_REDIS_PASSWORD"`
}

type MessagingConfig struct {
	KafkaBrokers string `env:"HELIX_KAFKA_BROKERS" default:"localhost:9092"`
}

type LogConfig struct {
	Level        string `env:"HELIX_LOG_LEVEL" default:"info"`
	Format       string `env:"HELIX_LOG_FORMAT" default:"text"`
	OTELExporter string `env:"HELIX_OTEL_EXPORTER" default:"none"`
	OTELEndpoint string `env:"HELIX_OTEL_ENDPOINT" default:"http://localhost:4317"`
}

type AuthConfig struct {
	JWTSecret string `env:"HELIX_AUTH_JWT_SECRET"`
	APIKeys   string `env:"HELIX_AUTH_API_KEYS"`
}

type FeatureConfig struct {
	GRPC        bool `env:"HELIX_FEATURE_GRPC" default:"true"`
	TOON        bool `env:"HELIX_FEATURE_TOON" default:"true"`
	SelfImprove bool `env:"HELIX_FEATURE_SELFIMPROVE" default:"false"`
}

// Load reads configuration from environment variables with defaults.
func Load() (*HelixConfig, error) {
	cfg := &HelixConfig{}
	if err := env.LoadWithPrefix("", cfg); err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	return cfg, nil
}

// Validate checks that configuration values are valid.
func (c *HelixConfig) Validate() error {
	validModes := map[string]bool{
		"full": true, "gateway": true, "brain": true,
		"knowledge": true, "agents": true, "control": true,
	}
	if !validModes[strings.ToLower(c.Mode)] {
		return fmt.Errorf("invalid mode: %q", c.Mode)
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Server.Port)
	}
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[strings.ToLower(c.Log.Level)] {
		return fmt.Errorf("invalid log level: %q", c.Log.Level)
	}
	return nil
}

// HostList returns HELIX_HOSTS as a string slice.
func (c *HelixConfig) HostList() []string {
	if c.Hosts == "" {
		return nil
	}
	hosts := strings.Split(c.Hosts, ",")
	for i := range hosts {
		hosts[i] = strings.TrimSpace(hosts[i])
	}
	return hosts
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/shared/config/ -v -count=1
```

Expected: all 3 tests PASS. If `env.LoadWithPrefix` doesn't work with nested structs via the env prefix, fall back to `env.Load` with explicit `env_prefix` tags on nested structs.

- [ ] **Step 5: Commit**

```bash
git add internal/shared/config/
git commit -m "feat: add HelixConfig with env loading and validation"
```

---

### Task 5: Logging System

**Files:**
- Create: `internal/shared/logging/logging.go`
- Create: `internal/shared/logging/logging_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/shared/logging/logging_test.go`:

```go
package logging_test

import (
	"bytes"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/logging"
)

func TestNewLogger(t *testing.T) {
	log := logging.New("info", "text")
	if log == nil {
		t.Fatal("New() returned nil")
	}
}

func TestLoggerLevels(t *testing.T) {
	var buf bytes.Buffer
	log := logging.NewWithOutput("debug", "text", &buf)

	log.Info("test info")
	if !bytes.Contains(buf.Bytes(), []byte("test info")) {
		t.Error("Info message not found in output")
	}

	buf.Reset()
	log.Debug("test debug")
	if !bytes.Contains(buf.Bytes(), []byte("test debug")) {
		t.Error("Debug message not found in output")
	}
}

func TestLoggerWithField(t *testing.T) {
	var buf bytes.Buffer
	log := logging.NewWithOutput("info", "text", &buf)

	log.WithField("request_id", "abc123").Info("request received")
	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("abc123")) {
		t.Error("Field value not found in output")
	}
}

func TestLoggerWithCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	log := logging.NewWithOutput("info", "text", &buf)

	log.WithCorrelationID("trace-456").Info("correlated event")
	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("trace-456")) {
		t.Error("Correlation ID not found in output")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/shared/logging/ -v
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement Logger**

Create `internal/shared/logging/logging.go`:

```go
package logging

import (
	"io"
	"os"

	obslog "digital.vasic.observability/pkg/logging"
)

// Logger wraps the observability Logger interface for HelixLLM.
type Logger = obslog.Logger

// New creates a Logger with the given level and format, writing to stderr.
func New(level, format string) Logger {
	return NewWithOutput(level, format, os.Stderr)
}

// NewWithOutput creates a Logger writing to the given writer.
func NewWithOutput(level, format string, output io.Writer) Logger {
	lvl := obslog.InfoLevel
	switch level {
	case "debug":
		lvl = obslog.DebugLevel
	case "warn":
		lvl = obslog.WarnLevel
	case "error":
		lvl = obslog.ErrorLevel
	}

	return obslog.NewLogrusAdapter(&obslog.Config{
		Level:       lvl,
		Format:      format,
		Output:      output,
		ServiceName: "helixllm",
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/shared/logging/ -v -count=1
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shared/logging/
git commit -m "feat: add logging system wrapping observability module"
```

---

### Task 6: EventBus Setup

**Files:**
- Create: `internal/shared/events/events.go`
- Create: `internal/shared/events/events_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/shared/events/events_test.go`:

```go
package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/events"
)

func TestNewBus(t *testing.T) {
	bus := events.NewBus()
	if bus == nil {
		t.Fatal("NewBus() returned nil")
	}
	defer bus.Close()
}

func TestPublishSubscribe(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close()

	sub := bus.Subscribe(events.TopicHealthChanged)

	bus.Publish(events.TopicHealthChanged, "test-source", "healthy")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	select {
	case evt := <-sub.Channel:
		if evt.Source != "test-source" {
			t.Errorf("Source = %q, want %q", evt.Source, "test-source")
		}
		if evt.Payload != "healthy" {
			t.Errorf("Payload = %v, want %q", evt.Payload, "healthy")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for event")
	}
}

func TestTopicConstants(t *testing.T) {
	topics := []events.Topic{
		events.TopicServerStarted,
		events.TopicServerStopped,
		events.TopicHealthChanged,
		events.TopicConfigReloaded,
		events.TopicModeChanged,
	}
	for _, topic := range topics {
		if topic == "" {
			t.Error("topic constant is empty")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/shared/events/ -v
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement EventBus wrapper**

Create `internal/shared/events/events.go`:

```go
package events

import (
	"digital.vasic.eventbus/pkg/bus"
	"digital.vasic.eventbus/pkg/event"
)

// Topic is a dot-notation event topic.
type Topic = event.Type

// Event topics for HelixLLM.
const (
	TopicServerStarted Topic = "server.started"
	TopicServerStopped Topic = "server.stopped"
	TopicHealthChanged Topic = "health.changed"
	TopicConfigReloaded Topic = "config.reloaded"
	TopicModeChanged   Topic = "mode.changed"
)

// Bus wraps the eventbus for HelixLLM.
type Bus struct {
	inner *bus.EventBus
}

// NewBus creates a new event bus with default configuration.
func NewBus() *Bus {
	return &Bus{
		inner: bus.New(bus.DefaultConfig()),
	}
}

// Publish sends an event synchronously.
func (b *Bus) Publish(topic Topic, source string, payload interface{}) {
	b.inner.Publish(event.New(topic, source, payload))
}

// PublishAsync sends an event asynchronously.
func (b *Bus) PublishAsync(topic Topic, source string, payload interface{}) {
	b.inner.PublishAsync(event.New(topic, source, payload))
}

// Subscribe returns a subscription for the given topic.
func (b *Bus) Subscribe(topic Topic) *event.Subscription {
	return b.inner.Subscribe(topic)
}

// SubscribeAll returns a subscription for all events.
func (b *Bus) SubscribeAll() *event.Subscription {
	return b.inner.SubscribeAll()
}

// Close shuts down the event bus.
func (b *Bus) Close() error {
	return b.inner.Close()
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/shared/events/ -v -count=1
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shared/events/
git commit -m "feat: add EventBus wrapper with HelixLLM topic constants"
```

---

### Task 7: Health Check System

**Files:**
- Create: `internal/shared/health/health.go`
- Create: `internal/shared/health/health_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/shared/health/health_test.go`:

```go
package health_test

import (
	"context"
	"errors"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
)

func TestNewChecker(t *testing.T) {
	checker := health.NewChecker()
	if checker == nil {
		t.Fatal("NewChecker() returned nil")
	}
}

func TestHealthyReport(t *testing.T) {
	checker := health.NewChecker()
	checker.Register("test-service", func(ctx context.Context) error {
		return nil
	})

	report := checker.Check(context.Background())
	if report.Status != health.StatusHealthy {
		t.Errorf("Status = %q, want %q", report.Status, health.StatusHealthy)
	}
	if len(report.Components) != 1 {
		t.Errorf("Components count = %d, want 1", len(report.Components))
	}
}

func TestUnhealthyReport(t *testing.T) {
	checker := health.NewChecker()
	checker.Register("failing-service", func(ctx context.Context) error {
		return errors.New("connection refused")
	})

	report := checker.Check(context.Background())
	if report.Status != health.StatusUnhealthy {
		t.Errorf("Status = %q, want %q", report.Status, health.StatusUnhealthy)
	}
}

func TestDegradedReport(t *testing.T) {
	checker := health.NewChecker()
	checker.Register("required-service", func(ctx context.Context) error {
		return nil
	})
	checker.RegisterOptional("optional-service", func(ctx context.Context) error {
		return errors.New("unavailable")
	})

	report := checker.Check(context.Background())
	if report.Status != health.StatusDegraded {
		t.Errorf("Status = %q, want %q", report.Status, health.StatusDegraded)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/shared/health/ -v
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement health checker**

Create `internal/shared/health/health.go`:

```go
package health

import (
	"context"
	"time"

	obshealth "digital.vasic.observability/pkg/health"
)

// Status constants.
const (
	StatusHealthy   = obshealth.StatusHealthy
	StatusDegraded  = obshealth.StatusDegraded
	StatusUnhealthy = obshealth.StatusUnhealthy
)

// Report represents the aggregated health status.
type Report = obshealth.Report

// ComponentResult represents one component's health.
type ComponentResult = obshealth.ComponentResult

// CheckFunc is a health check function.
type CheckFunc = obshealth.CheckFunc

// Checker aggregates health checks from all components.
type Checker struct {
	agg *obshealth.Aggregator
}

// NewChecker creates a health checker with a 5-second timeout.
func NewChecker() *Checker {
	return &Checker{
		agg: obshealth.NewAggregator(&obshealth.AggregatorConfig{
			Timeout: 5 * time.Second,
		}),
	}
}

// Register adds a required health check.
func (c *Checker) Register(name string, check CheckFunc) {
	c.agg.Register(name, check)
}

// RegisterOptional adds an optional health check (degrades but doesn't fail).
func (c *Checker) RegisterOptional(name string, check CheckFunc) {
	c.agg.RegisterOptional(name, check)
}

// Check runs all health checks in parallel and returns the report.
func (c *Checker) Check(ctx context.Context) *Report {
	return c.agg.Check(ctx)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/shared/health/ -v -count=1
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shared/health/
git commit -m "feat: add health check system wrapping observability aggregator"
```

---

### Task 8: Observability Setup (Tracing + Metrics)

**Files:**
- Create: `internal/shared/observability/observability.go`
- Create: `internal/shared/observability/observability_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/shared/observability/observability_test.go`:

```go
package observability_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/observability"
)

func TestNewObservability(t *testing.T) {
	obs, err := observability.New(observability.Options{
		ServiceName: "helixllm-test",
		Environment: "test",
		Exporter:    "none",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if obs == nil {
		t.Fatal("New() returned nil")
	}
	defer obs.Shutdown()
}

func TestMetricsCollector(t *testing.T) {
	obs, err := observability.New(observability.Options{
		ServiceName: "helixllm-test",
		Environment: "test",
		Exporter:    "none",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer obs.Shutdown()

	m := obs.Metrics()
	if m == nil {
		t.Fatal("Metrics() returned nil")
	}
	// Should not panic
	m.IncrementCounter("test_counter", map[string]string{"method": "GET"})
}

func TestTracer(t *testing.T) {
	obs, err := observability.New(observability.Options{
		ServiceName: "helixllm-test",
		Environment: "test",
		Exporter:    "none",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer obs.Shutdown()

	tr := obs.Tracer()
	if tr == nil {
		t.Fatal("Tracer() returned nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/shared/observability/ -v
```

Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement Observability**

Create `internal/shared/observability/observability.go`:

```go
package observability

import (
	"context"

	"digital.vasic.observability/pkg/metrics"
	"digital.vasic.observability/pkg/trace"
)

// Options configures the observability stack.
type Options struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	Exporter       string // none | stdout | otlp | jaeger | zipkin
	Endpoint       string
	Namespace      string
}

// Observability holds the tracing and metrics infrastructure.
type Observability struct {
	tracer    *trace.Tracer
	collector *metrics.PrometheusCollector
}

// New initializes tracing and metrics.
func New(opts Options) (*Observability, error) {
	exporterType := trace.ExporterNone
	switch opts.Exporter {
	case "stdout":
		exporterType = trace.ExporterStdout
	case "otlp":
		exporterType = trace.ExporterOTLP
	case "jaeger":
		exporterType = trace.ExporterJaeger
	case "zipkin":
		exporterType = trace.ExporterZipkin
	}

	tracer, err := trace.InitTracer(&trace.TracerConfig{
		ServiceName:    opts.ServiceName,
		ServiceVersion: opts.ServiceVersion,
		Environment:    opts.Environment,
		ExporterType:   exporterType,
		Endpoint:       opts.Endpoint,
		SampleRate:     1.0,
	})
	if err != nil {
		return nil, err
	}

	ns := opts.Namespace
	if ns == "" {
		ns = "helixllm"
	}

	collector := metrics.NewPrometheusCollector(&metrics.PrometheusConfig{
		Namespace: ns,
	})

	return &Observability{
		tracer:    tracer,
		collector: collector,
	}, nil
}

// Tracer returns the OpenTelemetry tracer.
func (o *Observability) Tracer() *trace.Tracer {
	return o.tracer
}

// Metrics returns the Prometheus metrics collector.
func (o *Observability) Metrics() *metrics.PrometheusCollector {
	return o.collector
}

// Shutdown gracefully shuts down tracing.
func (o *Observability) Shutdown() {
	if o.tracer != nil {
		o.tracer.Shutdown(context.Background())
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/shared/observability/ -v -count=1
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shared/observability/
git commit -m "feat: add observability setup with tracing and metrics"
```

---

### Task 9: HTTP/3 Server with Compression Middleware

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/server_test.go`
- Create: `internal/server/middleware/compression.go`
- Create: `internal/server/middleware/compression_test.go`
- Create: `internal/server/middleware/requestid.go`
- Create: `internal/server/middleware/requestid_test.go`

- [ ] **Step 1: Write failing tests for request ID middleware**

Create `internal/server/middleware/requestid_test.go`:

```go
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func TestRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/test", func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		c.String(200, id)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	rid := w.Header().Get("X-Request-ID")
	if rid == "" {
		t.Error("X-Request-ID header not set")
	}
	if len(rid) < 20 {
		t.Errorf("X-Request-ID too short: %q", rid)
	}
}

func TestRequestIDPreserved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/test", func(c *gin.Context) {
		c.String(200, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "custom-id-123")
	r.ServeHTTP(w, req)

	if w.Header().Get("X-Request-ID") != "custom-id-123" {
		t.Error("existing X-Request-ID was overwritten")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/server/middleware/ -v
```

Expected: FAIL.

- [ ] **Step 3: Implement RequestID middleware**

Create `internal/server/middleware/requestid.go`:

```go
package middleware

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/gin-gonic/gin"
)

// RequestID adds an X-Request-ID header to each request if not present.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = generateID()
			c.Request.Header.Set("X-Request-ID", id)
		}
		c.Header("X-Request-ID", id)
		c.Set("request_id", id)
		c.Next()
	}
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/server/middleware/ -run TestRequestID -v -count=1
```

Expected: PASS.

- [ ] **Step 5: Write failing tests for compression middleware**

Create `internal/server/middleware/compression_test.go`:

```go
package middleware_test

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/server/middleware"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
)

func setupCompressionRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Compression())
	r.GET("/test", func(c *gin.Context) {
		// Write enough data for compression to kick in
		data := strings.Repeat("Hello, World! ", 100)
		c.String(200, data)
	})
	return r
}

func TestBrotliCompression(t *testing.T) {
	r := setupCompressionRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "br, gzip")
	r.ServeHTTP(w, req)

	if w.Header().Get("Content-Encoding") != "br" {
		t.Errorf("Content-Encoding = %q, want %q", w.Header().Get("Content-Encoding"), "br")
	}

	reader := brotli.NewReader(w.Body)
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("brotli decode error: %v", err)
	}
	if !strings.Contains(string(body), "Hello, World!") {
		t.Error("decompressed body missing expected content")
	}
}

func TestGzipFallback(t *testing.T) {
	r := setupCompressionRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r.ServeHTTP(w, req)

	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("Content-Encoding = %q, want %q", w.Header().Get("Content-Encoding"), "gzip")
	}

	reader, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("gzip reader error: %v", err)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("gzip decode error: %v", err)
	}
	if !strings.Contains(string(body), "Hello, World!") {
		t.Error("decompressed body missing expected content")
	}
}

func TestNoCompression(t *testing.T) {
	r := setupCompressionRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Header().Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding should be empty, got %q", w.Header().Get("Content-Encoding"))
	}
	if !strings.Contains(w.Body.String(), "Hello, World!") {
		t.Error("body missing expected content")
	}
}
```

- [ ] **Step 6: Implement compression middleware**

Create `internal/server/middleware/compression.go`:

```go
package middleware

import (
	"bufio"
	"compress/gzip"
	"net"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
)

// Compression returns middleware that compresses responses using
// Brotli (preferred) or gzip based on Accept-Encoding.
func Compression() gin.HandlerFunc {
	return func(c *gin.Context) {
		ae := c.GetHeader("Accept-Encoding")
		if ae == "" {
			c.Next()
			return
		}

		if strings.Contains(ae, "br") {
			c.Header("Content-Encoding", "br")
			c.Header("Vary", "Accept-Encoding")
			brw := brotli.NewWriterLevel(c.Writer, brotli.DefaultCompression)
			c.Writer = &compressWriter{ResponseWriter: c.Writer, writer: brw, closer: brw}
			c.Next()
			brw.Close()
			return
		}

		if strings.Contains(ae, "gzip") {
			c.Header("Content-Encoding", "gzip")
			c.Header("Vary", "Accept-Encoding")
			gzw := gzip.NewWriter(c.Writer)
			c.Writer = &compressWriter{ResponseWriter: c.Writer, writer: gzw, closer: gzw}
			c.Next()
			gzw.Close()
			return
		}

		c.Next()
	}
}

type writerCloser interface {
	Write([]byte) (int, error)
	Close() error
}

type compressWriter struct {
	gin.ResponseWriter
	writer writerCloser
	closer writerCloser
}

func (w *compressWriter) Write(data []byte) (int, error) {
	return w.writer.Write(data)
}

func (w *compressWriter) WriteString(s string) (int, error) {
	return w.writer.Write([]byte(s))
}

func (w *compressWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.ResponseWriter.(http.Hijacker).Hijack()
}
```

- [ ] **Step 7: Run all middleware tests**

```bash
go test ./internal/server/middleware/ -v -count=1
```

Expected: all 5 tests PASS.

- [ ] **Step 8: Write failing tests for server**

Create `internal/server/server_test.go`:

```go
package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/server"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
)

func TestNewServer(t *testing.T) {
	checker := health.NewChecker()
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Checker: checker,
	})
	if srv == nil {
		t.Fatal("New() returned nil")
	}
}

func TestHealthEndpoint(t *testing.T) {
	checker := health.NewChecker()
	checker.Register("test", func(ctx __import__) error { return nil })
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Checker: checker,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/health", nil)
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var report map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("json decode error: %v", err)
	}
	if report["status"] != "healthy" {
		t.Errorf("status = %v, want healthy", report["status"])
	}
}

func TestAltSvcHeader(t *testing.T) {
	checker := health.NewChecker()
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    8443,
		Checker: checker,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/health", nil)
	srv.Handler().ServeHTTP(w, req)

	altSvc := w.Header().Get("Alt-Svc")
	if altSvc == "" {
		t.Error("Alt-Svc header not set")
	}
}
```

Wait — I made a mistake in the test. Let me fix the `context` import. The test should use `context.Context`:

Actually, the health check function signature is `func(ctx context.Context) error`. The test above has a typo. Here is the corrected version:

Replace `internal/server/server_test.go` entirely with:

```go
package server_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/server"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
)

func TestNewServer(t *testing.T) {
	checker := health.NewChecker()
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Checker: checker,
	})
	if srv == nil {
		t.Fatal("New() returned nil")
	}
}

func TestHealthEndpoint(t *testing.T) {
	checker := health.NewChecker()
	checker.Register("test", func(ctx context.Context) error { return nil })
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    0,
		Checker: checker,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/health", nil)
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var report map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("json decode error: %v", err)
	}
	if report["status"] != "healthy" {
		t.Errorf("status = %v, want healthy", report["status"])
	}
}

func TestAltSvcHeader(t *testing.T) {
	checker := health.NewChecker()
	srv := server.New(server.Options{
		Host:    "127.0.0.1",
		Port:    8443,
		Checker: checker,
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/internal/health", nil)
	srv.Handler().ServeHTTP(w, req)

	altSvc := w.Header().Get("Alt-Svc")
	if altSvc == "" {
		t.Error("Alt-Svc header not set")
	}
}
```

- [ ] **Step 9: Implement server**

Create `internal/server/server.go`:

```go
package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/quic-go/quic-go/http3"

	"github.com/HelixDevelopment/HelixLLM/internal/server/middleware"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
)

// Options configures the HTTP server.
type Options struct {
	Host    string
	Port    int
	TLSCert string
	TLSKey  string
	Checker *health.Checker
}

// Server manages the HTTP/3 + HTTP/2 Gin server.
type Server struct {
	opts   Options
	router *gin.Engine
}

// New creates a server with standard middleware and health endpoint.
func New(opts Options) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// Middleware
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(middleware.Compression())
	r.Use(altSvcMiddleware(opts.Port))

	// Health endpoint
	r.GET("/internal/health", func(c *gin.Context) {
		report := opts.Checker.Check(c.Request.Context())
		status := 200
		if report.Status == health.StatusUnhealthy {
			status = 503
		}
		c.JSON(status, report)
	})

	return &Server{opts: opts, router: r}
}

// Handler returns the http.Handler for testing.
func (s *Server) Handler() http.Handler {
	return s.router
}

// Router returns the Gin engine for adding routes.
func (s *Server) Router() *gin.Engine {
	return s.router
}

// ListenAndServe starts both HTTP/3 (UDP) and HTTP/2 (TCP) servers.
func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.opts.Host, s.opts.Port)

	tlsCert, err := tls.LoadX509KeyPair(s.opts.TLSCert, s.opts.TLSKey)
	if err != nil {
		return fmt.Errorf("loading TLS certificate: %w", err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"h3", "h2", "http/1.1"},
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	// HTTP/2 on TCP
	wg.Add(1)
	go func() {
		defer wg.Done()
		ln, lnErr := tls.Listen("tcp", addr, tlsConfig)
		if lnErr != nil {
			errCh <- fmt.Errorf("TCP listen: %w", lnErr)
			return
		}
		httpSrv := &http.Server{Handler: s.router}
		go func() {
			<-ctx.Done()
			httpSrv.Shutdown(context.Background())
		}()
		if srvErr := httpSrv.Serve(ln); srvErr != nil && srvErr != http.ErrServerClosed {
			errCh <- srvErr
		}
	}()

	// HTTP/3 on UDP
	wg.Add(1)
	go func() {
		defer wg.Done()
		h3srv := &http3.Server{
			Addr:      addr,
			Handler:   s.router,
			TLSConfig: tlsConfig,
		}
		go func() {
			<-ctx.Done()
			h3srv.Close()
		}()
		if srvErr := h3srv.ListenAndServe(); srvErr != nil && srvErr != http.ErrServerClosed {
			errCh <- srvErr
		}
	}()

	// Wait for either an error or context cancellation
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		wg.Wait()
		return nil
	}
}

func altSvcMiddleware(port int) gin.HandlerFunc {
	altSvc := fmt.Sprintf(`h3=":%d"; ma=86400`, port)
	return func(c *gin.Context) {
		c.Header("Alt-Svc", altSvc)
		c.Next()
	}
}

```

- [ ] **Step 10: Run server tests**

```bash
go test ./internal/server/... -v -count=1
```

Expected: all 3 server tests + 5 middleware tests PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/server/
git commit -m "feat: add HTTP/3 + HTTP/2 Gin server with compression and request ID middleware"
```

---

### Task 10: CLI Entry Point with Mode System

**Files:**
- Modify: `cmd/helixllm/main.go`

- [ ] **Step 1: Implement main.go with mode routing**

Replace `cmd/helixllm/main.go` with:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/HelixDevelopment/HelixLLM/internal/mode"
	"github.com/HelixDevelopment/HelixLLM/internal/server"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/config"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/events"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/health"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/logging"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/observability"
)

func main() {
	modeFlag := flag.String("mode", "", "Operating mode (overrides HELIX_MODE env)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	// CLI flag overrides env
	if *modeFlag != "" {
		cfg.Mode = *modeFlag
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "invalid config: %v\n", err)
		os.Exit(1)
	}

	m, err := mode.Parse(cfg.Mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	log := logging.New(cfg.Log.Level, cfg.Log.Format)
	bus := events.NewBus()
	defer bus.Close()

	obs, err := observability.New(observability.Options{
		ServiceName: "helixllm",
		Environment: "production",
		Exporter:    cfg.Log.OTELExporter,
	})
	if err != nil {
		log.Error(fmt.Sprintf("observability init failed: %v", err))
		os.Exit(1)
	}
	defer obs.Shutdown()

	checker := health.NewChecker()

	log.WithField("mode", m.String()).Info("starting HelixLLM")

	srv := server.New(server.Options{
		Host:    cfg.Server.Host,
		Port:    cfg.Server.Port,
		TLSCert: cfg.Server.TLSCert,
		TLSKey:  cfg.Server.TLSKey,
		Checker: checker,
	})

	// Register mode-specific routes (Phase 2+ will add real routes)
	srv.Router().GET("/v1/models", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"object": "list",
			"data":   []interface{}{},
		})
	})

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info("shutting down...")
		bus.Publish(events.TopicServerStopped, "main", nil)
		cancel()
	}()

	bus.Publish(events.TopicServerStarted, "main", m.String())
	log.WithField("addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)).
		Info("server listening")

	if err := srv.ListenAndServe(ctx); err != nil {
		log.WithError(err).Error("server error")
		os.Exit(1)
	}
}
```

Note: this needs `"github.com/gin-gonic/gin"` imported for the placeholder route. Add it to the imports.

- [ ] **Step 2: Verify build**

```bash
go build -o bin/helixllm ./cmd/helixllm
```

Expected: builds successfully.

- [ ] **Step 3: Commit**

```bash
git add cmd/helixllm/main.go
git commit -m "feat: add CLI entry point with mode system and graceful shutdown"
```

---

### Task 11: Containerfile

**Files:**
- Create: `container/Containerfile`

- [ ] **Step 1: Create multi-stage Containerfile**

Create `container/Containerfile`:

```dockerfile
# ── Builder ──────────────────────────────────────────────
FROM docker.io/library/golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Cache dependencies
COPY go.mod go.sum ./
COPY submodules/ submodules/
RUN go mod download

# Build binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o helixllm ./cmd/helixllm

# ── Runtime ──────────────────────────────────────────────
FROM docker.io/library/alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /build/helixllm /usr/local/bin/helixllm

EXPOSE 8443/tcp 8443/udp

ENTRYPOINT ["helixllm"]
CMD ["--mode=full"]
```

- [ ] **Step 2: Verify Containerfile builds**

```bash
podman build -f container/Containerfile -t helixllm:dev . || docker build -f container/Containerfile -t helixllm:dev .
```

Expected: builds successfully. May fail if submodules aren't initialized — that's fine at this stage. The Containerfile structure is correct.

- [ ] **Step 3: Commit**

```bash
git add container/Containerfile
git commit -m "feat: add multi-stage Containerfile for Podman/Docker"
```

---

### Task 12: Makefile

**Files:**
- Create: `Makefile`

- [ ] **Step 1: Create Makefile**

Create `Makefile`:

```makefile
.PHONY: build dev container container-push test-unit test-integration test-e2e test-stress test-chaos test-security test-benchmark test-automation test-usecases test-all coverage probe deploy status logs monitor rebalance ingest collections stats lint fmt docs gen deps clean certs

# ── Variables ────────────────────────────────────────────
BINARY := helixllm
GOFLAGS := -ldflags="-s -w"
CONTAINER_RUNTIME := $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)
IMAGE := helixllm
TAG := dev

# ── Build ────────────────────────────────────────────────
build:
	go build $(GOFLAGS) -o bin/$(BINARY) ./cmd/helixllm

dev: certs
	HELIX_MODE=full go run ./cmd/helixllm

container:
	$(CONTAINER_RUNTIME) build -f container/Containerfile -t $(IMAGE):$(TAG) .

container-push:
	$(CONTAINER_RUNTIME) push $(IMAGE):$(TAG)

# ── Test ─────────────────────────────────────────────────
test-unit:
	go test -v -count=1 -coverprofile=coverage-unit.out ./internal/...

test-integration:
	@echo "TODO: Phase 7 — integration tests with real services"

test-e2e:
	@echo "TODO: Phase 7 — e2e tests with full cluster"

test-stress:
	@echo "TODO: Phase 7 — stress tests"

test-chaos:
	@echo "TODO: Phase 7 — chaos tests"

test-security:
	@echo "TODO: Phase 7 — security tests"

test-benchmark:
	@echo "TODO: Phase 7 — benchmark tests"

test-automation:
	@echo "TODO: Phase 7 — full automation pipeline"

test-usecases:
	@echo "TODO: Phase 7 — real-world use case validation"

test-all: test-unit test-integration test-e2e test-stress test-chaos test-security test-benchmark test-automation test-usecases

coverage: test-unit
	go tool cover -func=coverage-unit.out
	@echo "---"
	@echo "Full coverage report: go tool cover -html=coverage-unit.out"

# ── Cluster ──────────────────────────────────────────────
probe:
	@echo "TODO: Phase 6 — probe all hosts"

deploy:
	@echo "TODO: Phase 6 — deploy to cluster"

status:
	@echo "TODO: Phase 6 — cluster status"

logs:
	@echo "TODO: Phase 6 — aggregated logs"

monitor:
	@echo "TODO: Phase 6 — TUI monitor"

rebalance:
	@echo "TODO: Phase 6 — rebalance cluster"

# ── Knowledge ────────────────────────────────────────────
ingest:
	@echo "TODO: Phase 4 — ingest documents"

collections:
	@echo "TODO: Phase 4 — list collections"

stats:
	@echo "TODO: Phase 4 — knowledge base stats"

# ── Development ──────────────────────────────────────────
lint:
	golangci-lint run ./...

fmt:
	gofmt -w .
	goimports -w .

docs:
	@echo "TODO: Phase 8 — generate documentation"

gen:
	go generate ./...

deps:
	git submodule update --init --recursive
	go mod tidy

clean:
	rm -rf bin/ coverage-*.out certs/

certs:
	@mkdir -p certs
	@test -f certs/cert.pem || openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
		-keyout certs/key.pem -out certs/cert.pem -days 365 -nodes \
		-subj "/CN=localhost" \
		-addext "subjectAltName=DNS:localhost,DNS:nezha.local,IP:127.0.0.1" 2>/dev/null
	@echo "TLS certs ready at certs/"
```

- [ ] **Step 2: Verify key targets**

```bash
make build
make test-unit
make coverage
```

Expected: binary builds, tests pass, coverage report generated.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "feat: add Makefile with build, test, dev, and cluster targets"
```

---

### Task 13: Compose File for Development

**Files:**
- Create: `deploy/compose.yaml`

- [ ] **Step 1: Create minimal compose file**

Create `deploy/compose.yaml`:

```yaml
# HelixLLM Development Stack
# Usage: podman-compose -f deploy/compose.yaml up -d
#    or: docker compose -f deploy/compose.yaml up -d

services:
  helixllm:
    build:
      context: ..
      dockerfile: container/Containerfile
    ports:
      - "8443:8443/tcp"
      - "8443:8443/udp"
    environment:
      - HELIX_MODE=full
      - HELIX_HOST=0.0.0.0
      - HELIX_PORT=8443
      - HELIX_LOG_LEVEL=debug
      - HELIX_OTEL_EXPORTER=none
    volumes:
      - ../certs:/certs:ro
    command: ["--mode=full"]
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "--no-check-certificate", "-qO-", "https://localhost:8443/internal/health"]
      interval: 10s
      timeout: 5s
      retries: 3
```

- [ ] **Step 2: Commit**

```bash
git add deploy/compose.yaml
git commit -m "feat: add development compose file"
```

---

### Task 14: Final Integration Test

**Files:** (no new files — validates everything works together)

- [ ] **Step 1: Run full test suite**

```bash
make test-unit
```

Expected: all tests PASS.

- [ ] **Step 2: Generate and verify coverage**

```bash
make coverage
```

Expected: 100% coverage on all `internal/` packages created in Phase 1.

- [ ] **Step 3: Build binary and verify startup**

```bash
make certs
make build
./bin/helixllm --mode=full &
sleep 2
curl -k https://localhost:8443/internal/health
kill %1
```

Expected: health endpoint returns JSON with `{"status":"healthy",...}`.

- [ ] **Step 4: Build container**

```bash
make container
```

Expected: container image built successfully.

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "feat: Phase 1 Foundation complete — scaffold, config, events, health, HTTP/3 server, mode system"
```

---

## Summary

| Task | Description | Tests |
|------|-------------|-------|
| 1 | Project scaffold (go.mod, dirs, .env.example) | build verification |
| 2 | Git submodules + replace directives | module resolution |
| 3 | Mode type with parsing | 3 tests |
| 4 | HelixConfig with env loading | 3 tests |
| 5 | Logging system | 4 tests |
| 6 | EventBus wrapper | 3 tests |
| 7 | Health check aggregator | 4 tests |
| 8 | Observability (tracing + metrics) | 3 tests |
| 9 | HTTP/3 server + compression + requestID | 8 tests |
| 10 | CLI entry point with mode routing | build verification |
| 11 | Containerfile | build verification |
| 12 | Makefile | target verification |
| 13 | Compose file | - |
| 14 | Integration verification | end-to-end verification |

**Total: 14 tasks, ~28 unit tests, 100% coverage on Phase 1 code**
