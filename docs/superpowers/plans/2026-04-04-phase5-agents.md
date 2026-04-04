# Phase 5: Agents Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Agents layer that provides agentic capabilities for HelixLLM. This covers a ReAct agent loop (reason, act, observe), a tool registry with built-in tools, MCP client integration, conversation context management, and HTTP API endpoints. The agent loop delegates reasoning to the Brain layer, executes tools when the LLM requests them, and returns final answers. All implementations use in-memory components so tests run without external services. Complex multi-agent coordination (HiPlan, MCTS, phase-based orchestration) is deferred to later refinement.

**Architecture:** The Agents layer defines a Tool interface and ToolRegistry for registering and executing tools. Three built-in tools are provided (echo, time, knowledge_query). The Agent struct ties the Brain, ToolRegistry, and optional RAG hook together in a ReAct loop: it sends messages to the Brain with tool descriptions in the system prompt, parses tool-call responses, executes tools, appends results, and loops until the LLM produces a final answer or a max-turns limit is hit. ConversationContext provides simple in-memory session storage for multi-turn conversations. Gin HTTP handlers expose `/v1/agents/chat` (agent-powered chat) and `/v1/agents/tools` (list available tools). The Agent is wired into main.go using the existing Brain, Knowledge pipeline, and RAG hook.

**Tech Stack:** Go 1.26+, Gin Gonic, vasic-digital modules (Agentic, Planning, ToolSchema, conversation, SkillRegistry, MCP_Module, LLMOrchestrator, Memory -- added as submodules, not yet wired into implementations), `encoding/json` (tool call parsing), `sync` (ConversationContext thread safety), `time` (time tool), `net/http/httptest` (API testing)

**Spec Reference:** `docs/superpowers/specs/2026-04-04-helixllm-master-design.md` -- Section 8 (Agents Layer Design), Section 4.4 (Agents Layer Submodules)

**Important notes:**
- All implementations are in-memory for this phase. The vasic-digital modules (Agentic, Planning, ToolSchema, etc.) are added as Git submodules and `go.mod` entries but NOT wired into code yet. They will be integrated as a later refinement when their full capabilities are needed.
- The agent loop uses a text-based tool-call protocol embedded in the LLM response. The system prompt instructs the LLM to output tool calls in a JSON format (`{"tool_call": {"name": "...", "arguments": {...}}}`). This is simpler than native function calling and works with all providers. Native function calling (OpenAI tools API, Anthropic tool_use) will be added in refinement.
- The mock Brain used in tests returns canned responses that simulate the tool-call/final-answer flow. This allows testing the full ReAct loop without a real LLM.
- ConversationContext uses a simple `sync.RWMutex`-protected map. Production will use `digital.vasic.conversation` with Kafka event sourcing.
- Tests are written first (TDD) and must pass without any external services running.
- The max-turns limit (default 10) prevents infinite loops in the agent. Each tool call + observation counts as one turn.

---

## File Structure

```
helixllm/
  cmd/helixllm/
    main.go                                Updated to create Agent and wire into server
  internal/
    agents/
      tool.go                              Tool interface + ToolRegistry
      tool_test.go
      tools/
        echo.go                            Echo tool (returns input)
        echo_test.go
        time.go                            Time tool (returns current time)
        time_test.go
        knowledge.go                       Knowledge query tool (queries RAG pipeline)
        knowledge_test.go
      agent.go                             Agent struct + ReAct loop
      agent_test.go
      context.go                           ConversationContext (in-memory session storage)
      context_test.go
      api.go                               Gin HTTP handlers for /v1/agents/*
      api_test.go
  submodules/
    Agentic/                               digital.vasic.agentic (added, not yet wired)
    Planning/                              digital.vasic.planning (added, not yet wired)
    ToolSchema/                            digital.vasic.toolschema (added, not yet wired)
    Conversation/                          digital.vasic.conversation (added, not yet wired)
    SkillRegistry/                         digital.vasic.skillregistry (added, not yet wired)
    MCP/                                   digital.vasic.mcp (added, not yet wired)
    LLMOrchestrator/                       digital.vasic.llmorchestrator (added, not yet wired)
    Memory/                                digital.vasic.memory (added, not yet wired)
  go.mod                                   Updated with new submodules + replace directives
  go.sum
```

---

### Task 1: Add Agents Submodules

**Files:**
- Modify: `.gitmodules`
- Modify: `go.mod`
- Create: `submodules/` entries for Agentic, Planning, ToolSchema, Conversation, SkillRegistry, MCP, LLMOrchestrator, Memory

- [ ] **Step 1: Add Agents layer submodules from vasic-digital (GitHub)**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
git submodule add git@github.com:vasic-digital/Agentic.git submodules/Agentic
git submodule add git@github.com:vasic-digital/Planning.git submodules/Planning
git submodule add git@github.com:vasic-digital/ToolSchema.git submodules/ToolSchema
git submodule add git@github.com:vasic-digital/Conversation.git submodules/Conversation
git submodule add git@github.com:vasic-digital/SkillRegistry.git submodules/SkillRegistry
git submodule add git@github.com:vasic-digital/MCP.git submodules/MCP
git submodule add git@github.com:vasic-digital/LLMOrchestrator.git submodules/LLMOrchestrator
git submodule add git@github.com:vasic-digital/Memory.git submodules/Memory
```

Expected: each submodule cloned into `submodules/`, `.gitmodules` updated with 8 new entries.

- [ ] **Step 2: Add replace directives to go.mod**

Add these `replace` directives to the existing `replace` block in `go.mod`:

```
replace (
    // ... existing Phase 1 + Phase 2 + Phase 3 + Phase 4 replacements ...
    digital.vasic.agentic => ./submodules/Agentic
    digital.vasic.planning => ./submodules/Planning
    digital.vasic.toolschema => ./submodules/ToolSchema
    digital.vasic.conversation => ./submodules/Conversation
    digital.vasic.skillregistry => ./submodules/SkillRegistry
    digital.vasic.mcp => ./submodules/MCP
    digital.vasic.llmorchestrator => ./submodules/LLMOrchestrator
    digital.vasic.memory => ./submodules/Memory
)
```

Also add to the `require` block:

```
require (
    // ... existing Phase 1 + Phase 2 + Phase 3 + Phase 4 requirements ...
    digital.vasic.agentic v0.0.0
    digital.vasic.planning v0.0.0
    digital.vasic.toolschema v0.0.0
    digital.vasic.conversation v0.0.0
    digital.vasic.skillregistry v0.0.0
    digital.vasic.mcp v0.0.0
    digital.vasic.llmorchestrator v0.0.0
    digital.vasic.memory v0.0.0
)
```

**Note:** These modules are added for availability but are NOT imported by any Go code in this phase. They will be integrated in a follow-up refinement task.

- [ ] **Step 3: Tidy modules**

```bash
go mod tidy
```

Expected: `go.sum` updated, all new dependencies resolved.

- [ ] **Step 4: Verify build**

```bash
go build ./...
```

Expected: builds successfully with all new submodules resolved.

- [ ] **Step 5: Commit**

```bash
git add .gitmodules submodules/ go.mod go.sum
git commit -m "feat: add Phase 5 Agents submodules (Agentic, Planning, ToolSchema, Conversation, SkillRegistry, MCP, LLMOrchestrator, Memory)"
```

---

### Task 2: Tool Interface and Registry

**Files:**
- Create: `internal/agents/tool.go`
- Create: `internal/agents/tool_test.go`

- [ ] **Step 1: Write failing tests for Tool interface and ToolRegistry**

Create `internal/agents/tool_test.go`:

```go
package agents_test

import (
	"context"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
)

// stubTool is a minimal Tool implementation for testing the registry.
type stubTool struct {
	name        string
	description string
	params      map[string]interface{}
	result      string
	err         error
}

func (s *stubTool) Name() string                    { return s.name }
func (s *stubTool) Description() string             { return s.description }
func (s *stubTool) Parameters() map[string]interface{} { return s.params }
func (s *stubTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	return s.result, s.err
}

func TestToolRegistryRegisterAndGet(t *testing.T) {
	reg := agents.NewToolRegistry()

	tool := &stubTool{
		name:        "test_tool",
		description: "A test tool",
		params:      map[string]interface{}{"input": "string"},
		result:      "ok",
	}

	err := reg.Register(tool)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	got, ok := reg.Get("test_tool")
	if !ok {
		t.Fatal("Get returned false for registered tool")
	}
	if got.Name() != "test_tool" {
		t.Errorf("expected name test_tool, got %s", got.Name())
	}
}

func TestToolRegistryGetNotFound(t *testing.T) {
	reg := agents.NewToolRegistry()

	_, ok := reg.Get("nonexistent")
	if ok {
		t.Error("Get returned true for unregistered tool")
	}
}

func TestToolRegistryDuplicateRegistration(t *testing.T) {
	reg := agents.NewToolRegistry()

	tool := &stubTool{name: "dup", description: "first"}
	err := reg.Register(tool)
	if err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	tool2 := &stubTool{name: "dup", description: "second"}
	err = reg.Register(tool2)
	if err == nil {
		t.Error("expected error on duplicate registration, got nil")
	}
}

func TestToolRegistryList(t *testing.T) {
	reg := agents.NewToolRegistry()

	_ = reg.Register(&stubTool{name: "alpha", description: "tool alpha", params: map[string]interface{}{"x": "int"}})
	_ = reg.Register(&stubTool{name: "beta", description: "tool beta", params: map[string]interface{}{"y": "string"}})

	infos := reg.List()
	if len(infos) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(infos))
	}

	// List should contain both tools (order is not guaranteed).
	names := map[string]bool{}
	for _, info := range infos {
		names[info.Name] = true
		if info.Description == "" {
			t.Errorf("tool %s has empty description", info.Name)
		}
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("expected alpha and beta in list, got %v", names)
	}
}

func TestToolRegistryExecute(t *testing.T) {
	reg := agents.NewToolRegistry()

	tool := &stubTool{
		name:        "exec_tool",
		description: "executes",
		result:      "executed!",
	}
	_ = reg.Register(tool)

	result, err := reg.Execute(context.Background(), "exec_tool", map[string]interface{}{"key": "value"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result != "executed!" {
		t.Errorf("expected 'executed!', got %s", result)
	}
}

func TestToolRegistryExecuteNotFound(t *testing.T) {
	reg := agents.NewToolRegistry()

	_, err := reg.Execute(context.Background(), "missing", nil)
	if err == nil {
		t.Error("expected error for missing tool, got nil")
	}
}

func TestToolRegistryExecuteWithError(t *testing.T) {
	reg := agents.NewToolRegistry()

	tool := &stubTool{
		name:        "fail_tool",
		description: "always fails",
		err:         context.DeadlineExceeded,
	}
	_ = reg.Register(tool)

	_, err := reg.Execute(context.Background(), "fail_tool", nil)
	if err == nil {
		t.Error("expected error from failing tool, got nil")
	}
}
```

Expected: tests fail because `internal/agents` package does not exist yet.

- [ ] **Step 2: Implement Tool interface and ToolRegistry**

Create `internal/agents/tool.go`:

```go
package agents

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Tool defines the interface that all tools must implement. A tool has a name,
// description, parameter schema, and an Execute method that performs the action.
type Tool interface {
	// Name returns the unique identifier for this tool.
	Name() string

	// Description returns a human-readable description of what this tool does.
	// This is included in the system prompt so the LLM knows when to use it.
	Description() string

	// Parameters returns a JSON Schema-like map describing the tool's arguments.
	Parameters() map[string]interface{}

	// Execute runs the tool with the given arguments and returns a string result.
	Execute(ctx context.Context, args map[string]interface{}) (string, error)
}

// ToolInfo is a read-only summary of a registered tool, returned by List().
type ToolInfo struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ToolRegistry holds registered tools and provides lookup and execution.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates an empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry. Returns an error if a tool with the
// same name is already registered.
func (r *ToolRegistry) Register(tool Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := tool.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q is already registered", name)
	}
	r.tools[name] = tool
	return nil
}

// Get returns a tool by name. The second return value is false if the tool is
// not found.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.tools[name]
	return t, ok
}

// List returns a sorted list of ToolInfo summaries for all registered tools.
func (r *ToolRegistry) List() []ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]ToolInfo, 0, len(r.tools))
	for _, t := range r.tools {
		infos = append(infos, ToolInfo{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos
}

// Execute looks up a tool by name and runs it with the given arguments.
// Returns an error if the tool is not found or if execution fails.
func (r *ToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("tool %q not found", name)
	}
	return tool.Execute(ctx, args)
}
```

- [ ] **Step 3: Run tests**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go test ./internal/agents/ -v -count=1
```

Expected: all 7 tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/agents/tool.go internal/agents/tool_test.go
git commit -m "feat: add Tool interface and ToolRegistry for agents layer"
```

---

### Task 3: Built-in Tools

**Files:**
- Create: `internal/agents/tools/echo.go`
- Create: `internal/agents/tools/echo_test.go`
- Create: `internal/agents/tools/time.go`
- Create: `internal/agents/tools/time_test.go`
- Create: `internal/agents/tools/knowledge.go`
- Create: `internal/agents/tools/knowledge_test.go`

- [ ] **Step 1: Write failing tests for built-in tools**

Create `internal/agents/tools/echo_test.go`:

```go
package tools_test

import (
	"context"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	"github.com/HelixDevelopment/HelixLLM/internal/agents/tools"
)

func TestEchoToolInterface(t *testing.T) {
	var _ agents.Tool = (*tools.EchoTool)(nil)
}

func TestEchoToolName(t *testing.T) {
	tool := tools.NewEchoTool()
	if tool.Name() != "echo" {
		t.Errorf("expected name 'echo', got %s", tool.Name())
	}
}

func TestEchoToolDescription(t *testing.T) {
	tool := tools.NewEchoTool()
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestEchoToolParameters(t *testing.T) {
	tool := tools.NewEchoTool()
	params := tool.Parameters()
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}
	if _, ok := params["message"]; !ok {
		t.Error("expected 'message' parameter in schema")
	}
}

func TestEchoToolExecute(t *testing.T) {
	tool := tools.NewEchoTool()
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"message": "hello world",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %s", result)
	}
}

func TestEchoToolExecuteMissingMessage(t *testing.T) {
	tool := tools.NewEchoTool()
	_, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing message, got nil")
	}
}

func TestEchoToolExecuteNilArgs(t *testing.T) {
	tool := tools.NewEchoTool()
	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil args, got nil")
	}
}
```

Create `internal/agents/tools/time_test.go`:

```go
package tools_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	"github.com/HelixDevelopment/HelixLLM/internal/agents/tools"
)

func TestTimeToolInterface(t *testing.T) {
	var _ agents.Tool = (*tools.TimeTool)(nil)
}

func TestTimeToolName(t *testing.T) {
	tool := tools.NewTimeTool()
	if tool.Name() != "time" {
		t.Errorf("expected name 'time', got %s", tool.Name())
	}
}

func TestTimeToolDescription(t *testing.T) {
	tool := tools.NewTimeTool()
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestTimeToolParameters(t *testing.T) {
	tool := tools.NewTimeTool()
	params := tool.Parameters()
	// Time tool takes no required parameters (optional timezone).
	if params == nil {
		t.Fatal("expected non-nil parameters map")
	}
}

func TestTimeToolExecuteDefault(t *testing.T) {
	tool := tools.NewTimeTool()
	result, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty time string")
	}
	// Verify it's a valid time by checking it contains the current year.
	year := time.Now().Format("2006")
	if !strings.Contains(result, year) {
		t.Errorf("expected result to contain year %s, got %s", year, result)
	}
}

func TestTimeToolExecuteWithTimezone(t *testing.T) {
	tool := tools.NewTimeTool()
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"timezone": "UTC",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !strings.Contains(result, "UTC") {
		t.Errorf("expected UTC in result, got %s", result)
	}
}

func TestTimeToolExecuteInvalidTimezone(t *testing.T) {
	tool := tools.NewTimeTool()
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"timezone": "Not/A/Timezone",
	})
	if err == nil {
		t.Error("expected error for invalid timezone, got nil")
	}
}

func TestTimeToolExecuteNilArgs(t *testing.T) {
	tool := tools.NewTimeTool()
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute with nil args failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty time string with nil args")
	}
}
```

Create `internal/agents/tools/knowledge_test.go`:

```go
package tools_test

import (
	"context"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	"github.com/HelixDevelopment/HelixLLM/internal/agents/tools"
	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

func TestKnowledgeQueryToolInterface(t *testing.T) {
	var _ agents.Tool = (*tools.KnowledgeQueryTool)(nil)
}

func TestKnowledgeQueryToolName(t *testing.T) {
	tool := tools.NewKnowledgeQueryTool(nil, "default")
	if tool.Name() != "knowledge_query" {
		t.Errorf("expected name 'knowledge_query', got %s", tool.Name())
	}
}

func TestKnowledgeQueryToolDescription(t *testing.T) {
	tool := tools.NewKnowledgeQueryTool(nil, "default")
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestKnowledgeQueryToolParameters(t *testing.T) {
	tool := tools.NewKnowledgeQueryTool(nil, "default")
	params := tool.Parameters()
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}
	if _, ok := params["query"]; !ok {
		t.Error("expected 'query' parameter in schema")
	}
}

func TestKnowledgeQueryToolExecuteNilPipeline(t *testing.T) {
	tool := tools.NewKnowledgeQueryTool(nil, "default")
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "test query",
	})
	if err == nil {
		t.Error("expected error when pipeline is nil, got nil")
	}
}

func TestKnowledgeQueryToolExecuteMissingQuery(t *testing.T) {
	tool := tools.NewKnowledgeQueryTool(nil, "default")
	_, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing query, got nil")
	}
}

func TestKnowledgeQueryToolExecuteWithPipeline(t *testing.T) {
	// Set up an in-memory pipeline with some data.
	embedder := knowledge.NewHashEmbedder(64)
	store := knowledge.NewMemoryStore()
	chunker := knowledge.NewFixedSizeChunker(500, 50)
	pipeline := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          embedder,
		Store:             store,
		Chunker:           chunker,
		DefaultCollection: "test",
		DefaultTopK:       3,
	})

	// Ingest a document.
	_, err := pipeline.Ingest(context.Background(), knowledge.IngestRequest{
		Title:      "Go Guide",
		Content:    "Go is a statically typed, compiled language designed at Google.",
		Source:     "test",
		Collection: "test",
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	tool := tools.NewKnowledgeQueryTool(pipeline, "test")
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "Go language",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result from knowledge query")
	}
}

func TestKnowledgeQueryToolExecuteWithCollectionOverride(t *testing.T) {
	embedder := knowledge.NewHashEmbedder(64)
	store := knowledge.NewMemoryStore()
	chunker := knowledge.NewFixedSizeChunker(500, 50)
	pipeline := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          embedder,
		Store:             store,
		Chunker:           chunker,
		DefaultCollection: "default",
		DefaultTopK:       3,
	})

	_, err := pipeline.Ingest(context.Background(), knowledge.IngestRequest{
		Title:      "Custom",
		Content:    "Custom collection content for testing.",
		Source:     "test",
		Collection: "custom",
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}

	tool := tools.NewKnowledgeQueryTool(pipeline, "default")
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"query":      "custom content",
		"collection": "custom",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result from custom collection query")
	}
}
```

Expected: all tests fail because the `tools` package does not exist.

- [ ] **Step 2: Implement EchoTool**

Create `internal/agents/tools/echo.go`:

```go
package tools

import (
	"context"
	"fmt"
)

// EchoTool returns its input unchanged. Useful for testing the tool pipeline.
type EchoTool struct{}

// NewEchoTool creates a new EchoTool.
func NewEchoTool() *EchoTool {
	return &EchoTool{}
}

func (e *EchoTool) Name() string        { return "echo" }
func (e *EchoTool) Description() string { return "Returns the input message unchanged. Useful for testing." }

func (e *EchoTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"message": map[string]interface{}{
			"type":        "string",
			"description": "The message to echo back",
			"required":    true,
		},
	}
}

func (e *EchoTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("echo: arguments must not be nil")
	}
	msg, ok := args["message"]
	if !ok {
		return "", fmt.Errorf("echo: missing required parameter 'message'")
	}
	return fmt.Sprintf("%v", msg), nil
}
```

- [ ] **Step 3: Implement TimeTool**

Create `internal/agents/tools/time.go`:

```go
package tools

import (
	"context"
	"fmt"
	"time"
)

// TimeTool returns the current date and time, optionally in a specified timezone.
type TimeTool struct{}

// NewTimeTool creates a new TimeTool.
func NewTimeTool() *TimeTool {
	return &TimeTool{}
}

func (t *TimeTool) Name() string        { return "time" }
func (t *TimeTool) Description() string { return "Returns the current date and time. Optionally accepts a 'timezone' parameter (e.g. 'UTC', 'America/New_York')." }

func (t *TimeTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"timezone": map[string]interface{}{
			"type":        "string",
			"description": "IANA timezone name (e.g. 'UTC', 'America/New_York'). Defaults to local time.",
			"required":    false,
		},
	}
}

func (t *TimeTool) Execute(_ context.Context, args map[string]interface{}) (string, error) {
	loc := time.Now().Location()

	if args != nil {
		if tz, ok := args["timezone"]; ok {
			tzStr, isStr := tz.(string)
			if isStr && tzStr != "" {
				parsed, err := time.LoadLocation(tzStr)
				if err != nil {
					return "", fmt.Errorf("time: invalid timezone %q: %w", tzStr, err)
				}
				loc = parsed
			}
		}
	}

	now := time.Now().In(loc)
	return now.Format(time.RFC3339) + " " + loc.String(), nil
}
```

- [ ] **Step 4: Implement KnowledgeQueryTool**

Create `internal/agents/tools/knowledge.go`:

```go
package tools

import (
	"context"
	"fmt"

	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
)

// KnowledgeQueryTool queries the RAG pipeline and returns matching context.
type KnowledgeQueryTool struct {
	pipeline          *knowledge.Pipeline
	defaultCollection string
}

// NewKnowledgeQueryTool creates a KnowledgeQueryTool that queries the given
// pipeline. If collection is empty in the arguments, defaultCollection is used.
func NewKnowledgeQueryTool(pipeline *knowledge.Pipeline, defaultCollection string) *KnowledgeQueryTool {
	return &KnowledgeQueryTool{
		pipeline:          pipeline,
		defaultCollection: defaultCollection,
	}
}

func (k *KnowledgeQueryTool) Name() string { return "knowledge_query" }
func (k *KnowledgeQueryTool) Description() string {
	return "Searches the knowledge base for relevant information. Use this when you need to look up facts, documentation, or previously ingested content."
}

func (k *KnowledgeQueryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"query": map[string]interface{}{
			"type":        "string",
			"description": "The search query to find relevant knowledge",
			"required":    true,
		},
		"collection": map[string]interface{}{
			"type":        "string",
			"description": "The knowledge collection to search. Defaults to the configured collection.",
			"required":    false,
		},
	}
}

func (k *KnowledgeQueryTool) Execute(ctx context.Context, args map[string]interface{}) (string, error) {
	if args == nil {
		return "", fmt.Errorf("knowledge_query: arguments must not be nil")
	}

	queryVal, ok := args["query"]
	if !ok {
		return "", fmt.Errorf("knowledge_query: missing required parameter 'query'")
	}
	query, ok := queryVal.(string)
	if !ok || query == "" {
		return "", fmt.Errorf("knowledge_query: 'query' must be a non-empty string")
	}

	if k.pipeline == nil {
		return "", fmt.Errorf("knowledge_query: knowledge pipeline is not available")
	}

	collection := k.defaultCollection
	if colVal, ok := args["collection"]; ok {
		if colStr, ok := colVal.(string); ok && colStr != "" {
			collection = colStr
		}
	}

	result, err := k.pipeline.Query(ctx, knowledge.QueryRequest{
		Query:      query,
		Collection: collection,
		TopK:       5,
	})
	if err != nil {
		return "", fmt.Errorf("knowledge_query: %w", err)
	}

	if result.Context == "" {
		return "No relevant information found.", nil
	}
	return result.Context, nil
}
```

- [ ] **Step 5: Run tests**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go test ./internal/agents/tools/ -v -count=1
```

Expected: all tests pass (echo: 6, time: 6, knowledge: 7 = 19 tests).

- [ ] **Step 6: Commit**

```bash
git add internal/agents/tools/
git commit -m "feat: add built-in tools (echo, time, knowledge_query) for agents layer"
```

---

### Task 4: Agent Loop (ReAct Pattern)

**Files:**
- Create: `internal/agents/agent.go`
- Create: `internal/agents/agent_test.go`

- [ ] **Step 1: Write failing tests for Agent ReAct loop**

Create `internal/agents/agent_test.go`:

```go
package agents_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// mockProvider implements brain.Provider for testing the agent loop.
type mockProvider struct {
	responses []*types.InternalChatResponse
	callIndex int
}

func (m *mockProvider) Complete(_ context.Context, _ *types.InternalChatRequest) (*types.InternalChatResponse, error) {
	if m.callIndex >= len(m.responses) {
		return nil, fmt.Errorf("mock: no more responses (called %d times, have %d)", m.callIndex+1, len(m.responses))
	}
	resp := m.responses[m.callIndex]
	m.callIndex++
	return resp, nil
}

func (m *mockProvider) CompleteStream(_ context.Context, _ *types.InternalChatRequest) (<-chan types.StreamChunk, error) {
	return nil, fmt.Errorf("mock: streaming not implemented")
}

func (m *mockProvider) Models() []string    { return []string{"mock-model"} }
func (m *mockProvider) Name() string        { return "mock" }
func (m *mockProvider) Available() bool     { return true }

func newMockBrain(responses []*types.InternalChatResponse) *brain.Brain {
	b := brain.New(brain.Config{})
	mock := &mockProvider{responses: responses}
	b.RegisterProvider("mock", mock)
	return b
}

func TestAgentRunDirectAnswer(t *testing.T) {
	// LLM returns a direct answer (no tool call).
	b := newMockBrain([]*types.InternalChatResponse{
		{
			ID:           "resp-1",
			Model:        "mock-model",
			Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "The answer is 42."},
			FinishReason: "stop",
			Provider:     "mock",
		},
	})

	reg := agents.NewToolRegistry()
	agent := agents.NewAgent(agents.AgentConfig{
		Brain:    b,
		Tools:    reg,
		MaxTurns: 5,
	})

	messages := []types.InternalMessage{
		{Role: types.RoleUser, Content: "What is the meaning of life?"},
	}

	resp, err := agent.Run(context.Background(), messages)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if resp.Message.Content != "The answer is 42." {
		t.Errorf("expected 'The answer is 42.', got %s", resp.Message.Content)
	}
}

func TestAgentRunWithToolCall(t *testing.T) {
	// First response is a tool call, second is the final answer.
	b := newMockBrain([]*types.InternalChatResponse{
		{
			ID:    "resp-1",
			Model: "mock-model",
			Message: types.InternalMessage{
				Role:    types.RoleAssistant,
				Content: `I need to check the time. {"tool_call": {"name": "echo", "arguments": {"message": "hello from tool"}}}`,
			},
			FinishReason: "stop",
			Provider:     "mock",
		},
		{
			ID:    "resp-2",
			Model: "mock-model",
			Message: types.InternalMessage{
				Role:    types.RoleAssistant,
				Content: "The tool returned: hello from tool",
			},
			FinishReason: "stop",
			Provider:     "mock",
		},
	})

	reg := agents.NewToolRegistry()
	_ = reg.Register(&stubTool{
		name:        "echo",
		description: "echoes input",
		result:      "hello from tool",
	})

	agent := agents.NewAgent(agents.AgentConfig{
		Brain:    b,
		Tools:    reg,
		MaxTurns: 5,
	})

	messages := []types.InternalMessage{
		{Role: types.RoleUser, Content: "Echo hello from tool"},
	}

	resp, err := agent.Run(context.Background(), messages)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if resp.Message.Content != "The tool returned: hello from tool" {
		t.Errorf("expected final answer, got %s", resp.Message.Content)
	}
}

func TestAgentRunMaxTurnsExceeded(t *testing.T) {
	// LLM always returns tool calls, hitting the max turns limit.
	toolCallResp := &types.InternalChatResponse{
		ID:    "resp-loop",
		Model: "mock-model",
		Message: types.InternalMessage{
			Role:    types.RoleAssistant,
			Content: `{"tool_call": {"name": "echo", "arguments": {"message": "loop"}}}`,
		},
		FinishReason: "stop",
		Provider:     "mock",
	}

	// Create enough responses for max turns + 1.
	responses := make([]*types.InternalChatResponse, 15)
	for i := range responses {
		responses[i] = toolCallResp
	}

	b := newMockBrain(responses)

	reg := agents.NewToolRegistry()
	_ = reg.Register(&stubTool{
		name:        "echo",
		description: "echoes input",
		result:      "loop",
	})

	agent := agents.NewAgent(agents.AgentConfig{
		Brain:    b,
		Tools:    reg,
		MaxTurns: 3,
	})

	messages := []types.InternalMessage{
		{Role: types.RoleUser, Content: "Keep calling tools forever"},
	}

	_, err := agent.Run(context.Background(), messages)
	if err == nil {
		t.Error("expected error for max turns exceeded, got nil")
	}
}

func TestAgentRunToolNotFound(t *testing.T) {
	// LLM requests a tool that does not exist, then gives a final answer.
	b := newMockBrain([]*types.InternalChatResponse{
		{
			ID:    "resp-1",
			Model: "mock-model",
			Message: types.InternalMessage{
				Role:    types.RoleAssistant,
				Content: `{"tool_call": {"name": "nonexistent_tool", "arguments": {}}}`,
			},
			FinishReason: "stop",
			Provider:     "mock",
		},
		{
			ID:    "resp-2",
			Model: "mock-model",
			Message: types.InternalMessage{
				Role:    types.RoleAssistant,
				Content: "I could not find that tool, so here is my answer directly.",
			},
			FinishReason: "stop",
			Provider:     "mock",
		},
	})

	reg := agents.NewToolRegistry()

	agent := agents.NewAgent(agents.AgentConfig{
		Brain:    b,
		Tools:    reg,
		MaxTurns: 5,
	})

	messages := []types.InternalMessage{
		{Role: types.RoleUser, Content: "Use nonexistent_tool"},
	}

	resp, err := agent.Run(context.Background(), messages)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	// Agent should recover: tool error gets appended as a tool message, LLM
	// responds with a final answer.
	if resp.Message.Content != "I could not find that tool, so here is my answer directly." {
		t.Errorf("unexpected response: %s", resp.Message.Content)
	}
}

func TestAgentRunWithRAGHook(t *testing.T) {
	// Verify that the RAG hook is applied (by checking the response is still correct).
	b := newMockBrain([]*types.InternalChatResponse{
		{
			ID:           "resp-1",
			Model:        "mock-model",
			Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "Context-enriched answer."},
			FinishReason: "stop",
			Provider:     "mock",
		},
	})

	hookCalled := false
	ragHook := func(req *types.InternalChatRequest) *types.InternalChatRequest {
		hookCalled = true
		return req
	}

	reg := agents.NewToolRegistry()
	agent := agents.NewAgent(agents.AgentConfig{
		Brain:    b,
		Tools:    reg,
		RAGHook:  ragHook,
		MaxTurns: 5,
	})

	messages := []types.InternalMessage{
		{Role: types.RoleUser, Content: "Tell me about the architecture"},
	}

	_, err := agent.Run(context.Background(), messages)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !hookCalled {
		t.Error("expected RAG hook to be called")
	}
}

func TestAgentRunContextCancelled(t *testing.T) {
	b := newMockBrain([]*types.InternalChatResponse{
		{
			ID:           "resp-1",
			Model:        "mock-model",
			Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "answer"},
			FinishReason: "stop",
			Provider:     "mock",
		},
	})

	reg := agents.NewToolRegistry()
	agent := agents.NewAgent(agents.AgentConfig{
		Brain:    b,
		Tools:    reg,
		MaxTurns: 5,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	messages := []types.InternalMessage{
		{Role: types.RoleUser, Content: "test"},
	}

	_, err := agent.Run(ctx, messages)
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}

func TestNewAgentDefaultMaxTurns(t *testing.T) {
	b := newMockBrain(nil)
	reg := agents.NewToolRegistry()

	agent := agents.NewAgent(agents.AgentConfig{
		Brain: b,
		Tools: reg,
		// MaxTurns not set -- should default to 10.
	})

	if agent == nil {
		t.Fatal("expected non-nil agent")
	}
}
```

Expected: tests fail because `agent.go` does not exist.

- [ ] **Step 2: Implement Agent with ReAct loop**

Create `internal/agents/agent.go`:

```go
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// DefaultMaxTurns is the default maximum number of reasoning-action turns
// before the agent gives up.
const DefaultMaxTurns = 10

// AgentConfig holds the dependencies for creating an Agent.
type AgentConfig struct {
	Brain    *brain.Brain
	Tools    *ToolRegistry
	RAGHook  func(*types.InternalChatRequest) *types.InternalChatRequest
	MaxTurns int
}

// Agent implements the ReAct (Reason-Act-Observe) loop. It sends messages to
// the Brain, detects tool-call requests in the response, executes the tools,
// appends results, and loops until the LLM produces a final answer or the max
// turns limit is reached.
type Agent struct {
	brain    *brain.Brain
	tools    *ToolRegistry
	ragHook  func(*types.InternalChatRequest) *types.InternalChatRequest
	maxTurns int
}

// toolCallPayload is the JSON structure the LLM uses to request a tool call.
type toolCallPayload struct {
	ToolCall struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	} `json:"tool_call"`
}

// NewAgent creates an Agent from the given config. If MaxTurns is 0, it
// defaults to DefaultMaxTurns.
func NewAgent(cfg AgentConfig) *Agent {
	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}
	return &Agent{
		brain:    cfg.Brain,
		tools:    cfg.Tools,
		ragHook:  cfg.RAGHook,
		maxTurns: maxTurns,
	}
}

// Run executes the ReAct loop starting from the given messages.
//
// Loop:
//  1. Build an InternalChatRequest from the accumulated messages.
//  2. Apply the RAG hook (if set) to inject knowledge context.
//  3. Inject tool descriptions into a system prompt.
//  4. Send to Brain.Complete().
//  5. If the response contains a tool_call JSON → execute the tool, append
//     assistant + tool messages, and loop.
//  6. If the response is a plain answer → return it.
//  7. If max turns is reached → return an error.
func (a *Agent) Run(ctx context.Context, messages []types.InternalMessage) (*types.InternalChatResponse, error) {
	// Copy messages to avoid mutating the caller's slice.
	msgs := make([]types.InternalMessage, len(messages))
	copy(msgs, messages)

	for turn := 0; turn < a.maxTurns; turn++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("agent: context cancelled: %w", err)
		}

		req := &types.InternalChatRequest{
			Messages: a.buildMessages(msgs),
		}

		// Apply RAG hook.
		if a.ragHook != nil {
			req = a.ragHook(req)
		}

		resp, err := a.brain.Complete(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("agent: brain complete: %w", err)
		}

		// Check if the response contains a tool call.
		toolCall, found := parseToolCall(resp.Message.Content)
		if !found {
			// No tool call -- this is the final answer.
			return resp, nil
		}

		// Append the assistant's reasoning message.
		msgs = append(msgs, types.InternalMessage{
			Role:    types.RoleAssistant,
			Content: resp.Message.Content,
		})

		// Execute the tool.
		result, execErr := a.tools.Execute(ctx, toolCall.ToolCall.Name, toolCall.ToolCall.Arguments)
		if execErr != nil {
			// Tool execution failed -- report the error back to the LLM.
			msgs = append(msgs, types.InternalMessage{
				Role:    types.RoleTool,
				Name:    toolCall.ToolCall.Name,
				Content: fmt.Sprintf("Error: %v", execErr),
			})
			continue
		}

		// Append the tool result.
		msgs = append(msgs, types.InternalMessage{
			Role:    types.RoleTool,
			Name:    toolCall.ToolCall.Name,
			Content: result,
		})
	}

	return nil, fmt.Errorf("agent: max turns (%d) exceeded without a final answer", a.maxTurns)
}

// buildMessages prepends a system prompt with tool descriptions to the
// conversation messages.
func (a *Agent) buildMessages(msgs []types.InternalMessage) []types.InternalMessage {
	toolDesc := a.buildToolSystemPrompt()
	if toolDesc == "" {
		return msgs
	}

	systemMsg := types.InternalMessage{
		Role:    types.RoleSystem,
		Content: toolDesc,
	}

	result := make([]types.InternalMessage, 0, len(msgs)+1)
	result = append(result, systemMsg)
	result = append(result, msgs...)
	return result
}

// buildToolSystemPrompt creates a system prompt that describes all available
// tools so the LLM knows how to invoke them.
func (a *Agent) buildToolSystemPrompt() string {
	if a.tools == nil {
		return ""
	}
	tools := a.tools.List()
	if len(tools) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("You have access to the following tools. To use a tool, include a JSON object in your response with the following format:\n")
	sb.WriteString(`{"tool_call": {"name": "<tool_name>", "arguments": {<args>}}}`)
	sb.WriteString("\n\nAvailable tools:\n")

	for _, t := range tools {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", t.Name, t.Description))
		if len(t.Parameters) > 0 {
			paramsJSON, err := json.Marshal(t.Parameters)
			if err == nil {
				sb.WriteString(fmt.Sprintf("  Parameters: %s\n", string(paramsJSON)))
			}
		}
	}

	sb.WriteString("\nIf you do not need a tool, respond with your answer directly (without the tool_call JSON).")
	return sb.String()
}

// parseToolCall tries to extract a tool_call JSON from the LLM response
// content. It looks for the pattern {"tool_call": ...} anywhere in the text.
func parseToolCall(content string) (*toolCallPayload, bool) {
	// Find the first '{' that could start a tool_call JSON.
	idx := strings.Index(content, `{"tool_call"`)
	if idx < 0 {
		return nil, false
	}

	// Try to parse from that position. Find the matching closing brace.
	candidate := content[idx:]
	depth := 0
	end := -1
	for i, ch := range candidate {
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
		}
		if end > 0 {
			break
		}
	}

	if end <= 0 {
		return nil, false
	}

	jsonStr := candidate[:end]
	var payload toolCallPayload
	if err := json.Unmarshal([]byte(jsonStr), &payload); err != nil {
		return nil, false
	}

	if payload.ToolCall.Name == "" {
		return nil, false
	}

	return &payload, true
}
```

- [ ] **Step 3: Run tests**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go test ./internal/agents/ -v -count=1
```

Expected: all agent tests pass (7 agent tests + 7 registry tests = 14 total).

- [ ] **Step 4: Commit**

```bash
git add internal/agents/agent.go internal/agents/agent_test.go
git commit -m "feat: add Agent ReAct loop with tool-call parsing and max-turns safety"
```

---

### Task 5: Conversation Context

**Files:**
- Create: `internal/agents/context.go`
- Create: `internal/agents/context_test.go`

- [ ] **Step 1: Write failing tests for ConversationContext**

Create `internal/agents/context_test.go`:

```go
package agents_test

import (
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func TestConversationContextAddAndGet(t *testing.T) {
	ctx := agents.NewConversationContext(100)

	ctx.Add("session-1", types.InternalMessage{
		Role:    types.RoleUser,
		Content: "Hello",
	})
	ctx.Add("session-1", types.InternalMessage{
		Role:    types.RoleAssistant,
		Content: "Hi there!",
	})

	msgs := ctx.Get("session-1")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != types.RoleUser {
		t.Errorf("expected first message role user, got %s", msgs[0].Role)
	}
	if msgs[1].Content != "Hi there!" {
		t.Errorf("expected second message content 'Hi there!', got %s", msgs[1].Content)
	}
}

func TestConversationContextGetEmpty(t *testing.T) {
	ctx := agents.NewConversationContext(100)
	msgs := ctx.Get("nonexistent")
	if msgs == nil {
		t.Error("expected non-nil empty slice for nonexistent session")
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestConversationContextMaxHistory(t *testing.T) {
	ctx := agents.NewConversationContext(3)

	ctx.Add("s1", types.InternalMessage{Role: types.RoleUser, Content: "msg1"})
	ctx.Add("s1", types.InternalMessage{Role: types.RoleAssistant, Content: "msg2"})
	ctx.Add("s1", types.InternalMessage{Role: types.RoleUser, Content: "msg3"})
	ctx.Add("s1", types.InternalMessage{Role: types.RoleAssistant, Content: "msg4"})

	msgs := ctx.Get("s1")
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (maxHistory=3), got %d", len(msgs))
	}
	// Should have the 3 most recent messages.
	if msgs[0].Content != "msg2" {
		t.Errorf("expected first message 'msg2', got %s", msgs[0].Content)
	}
	if msgs[2].Content != "msg4" {
		t.Errorf("expected last message 'msg4', got %s", msgs[2].Content)
	}
}

func TestConversationContextMultipleSessions(t *testing.T) {
	ctx := agents.NewConversationContext(100)

	ctx.Add("s1", types.InternalMessage{Role: types.RoleUser, Content: "s1-msg"})
	ctx.Add("s2", types.InternalMessage{Role: types.RoleUser, Content: "s2-msg"})

	s1 := ctx.Get("s1")
	s2 := ctx.Get("s2")

	if len(s1) != 1 || s1[0].Content != "s1-msg" {
		t.Errorf("session 1 unexpected: %v", s1)
	}
	if len(s2) != 1 || s2[0].Content != "s2-msg" {
		t.Errorf("session 2 unexpected: %v", s2)
	}
}

func TestConversationContextAddMultiple(t *testing.T) {
	ctx := agents.NewConversationContext(100)

	batch := []types.InternalMessage{
		{Role: types.RoleUser, Content: "first"},
		{Role: types.RoleAssistant, Content: "second"},
		{Role: types.RoleUser, Content: "third"},
	}
	ctx.AddMultiple("s1", batch)

	msgs := ctx.Get("s1")
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
}

func TestConversationContextClear(t *testing.T) {
	ctx := agents.NewConversationContext(100)

	ctx.Add("s1", types.InternalMessage{Role: types.RoleUser, Content: "hello"})
	ctx.Clear("s1")

	msgs := ctx.Get("s1")
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after clear, got %d", len(msgs))
	}
}

func TestConversationContextClearNonexistent(t *testing.T) {
	ctx := agents.NewConversationContext(100)
	// Should not panic.
	ctx.Clear("nonexistent")
}

func TestConversationContextSessions(t *testing.T) {
	ctx := agents.NewConversationContext(100)

	ctx.Add("alpha", types.InternalMessage{Role: types.RoleUser, Content: "a"})
	ctx.Add("beta", types.InternalMessage{Role: types.RoleUser, Content: "b"})
	ctx.Add("gamma", types.InternalMessage{Role: types.RoleUser, Content: "c"})

	sessions := ctx.Sessions()
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}

	names := map[string]bool{}
	for _, s := range sessions {
		names[s] = true
	}
	if !names["alpha"] || !names["beta"] || !names["gamma"] {
		t.Errorf("expected alpha, beta, gamma in sessions, got %v", names)
	}
}

func TestConversationContextDefaultMaxHistory(t *testing.T) {
	// When maxHistory is 0, it should default to a reasonable number.
	ctx := agents.NewConversationContext(0)
	if ctx == nil {
		t.Fatal("expected non-nil context with default max history")
	}

	// Add more than default and verify it still works.
	for i := 0; i < 200; i++ {
		ctx.Add("s1", types.InternalMessage{Role: types.RoleUser, Content: "msg"})
	}
	msgs := ctx.Get("s1")
	// Default is 100, so we should have at most 100 messages.
	if len(msgs) > 100 {
		t.Errorf("expected at most 100 messages with default max, got %d", len(msgs))
	}
}

func TestConversationContextImmutableGet(t *testing.T) {
	ctx := agents.NewConversationContext(100)

	ctx.Add("s1", types.InternalMessage{Role: types.RoleUser, Content: "original"})

	// Get should return a copy; modifying it should not affect stored messages.
	msgs := ctx.Get("s1")
	msgs[0].Content = "modified"

	original := ctx.Get("s1")
	if original[0].Content != "original" {
		t.Error("Get did not return a copy; internal state was modified")
	}
}
```

Expected: tests fail because `context.go` does not exist.

- [ ] **Step 2: Implement ConversationContext**

Create `internal/agents/context.go`:

```go
package agents

import (
	"sort"
	"sync"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// DefaultMaxHistory is the default maximum number of messages stored per session.
const DefaultMaxHistory = 100

// ConversationContext provides simple in-memory session storage for
// multi-turn conversations. It is safe for concurrent use.
type ConversationContext struct {
	mu         sync.RWMutex
	sessions   map[string][]types.InternalMessage
	maxHistory int
}

// NewConversationContext creates a ConversationContext with the given max
// history per session. If maxHistory is 0 or negative, DefaultMaxHistory is used.
func NewConversationContext(maxHistory int) *ConversationContext {
	if maxHistory <= 0 {
		maxHistory = DefaultMaxHistory
	}
	return &ConversationContext{
		sessions:   make(map[string][]types.InternalMessage),
		maxHistory: maxHistory,
	}
}

// Add appends a single message to the session. If the session exceeds
// maxHistory, the oldest messages are trimmed.
func (c *ConversationContext) Add(sessionID string, msg types.InternalMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.sessions[sessionID] = append(c.sessions[sessionID], msg)
	c.trim(sessionID)
}

// AddMultiple appends multiple messages to the session.
func (c *ConversationContext) AddMultiple(sessionID string, msgs []types.InternalMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.sessions[sessionID] = append(c.sessions[sessionID], msgs...)
	c.trim(sessionID)
}

// Get returns a copy of all messages for the given session. If the session
// does not exist, an empty (non-nil) slice is returned.
func (c *ConversationContext) Get(sessionID string) []types.InternalMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()

	msgs, ok := c.sessions[sessionID]
	if !ok || len(msgs) == 0 {
		return []types.InternalMessage{}
	}

	// Return a copy to prevent caller mutation.
	result := make([]types.InternalMessage, len(msgs))
	copy(result, msgs)
	return result
}

// Clear removes all messages for the given session.
func (c *ConversationContext) Clear(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.sessions, sessionID)
}

// Sessions returns a sorted list of all active session IDs.
func (c *ConversationContext) Sessions() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := make([]string, 0, len(c.sessions))
	for id := range c.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// trim ensures the session does not exceed maxHistory messages. Called with
// the lock held.
func (c *ConversationContext) trim(sessionID string) {
	msgs := c.sessions[sessionID]
	if len(msgs) > c.maxHistory {
		c.sessions[sessionID] = msgs[len(msgs)-c.maxHistory:]
	}
}
```

- [ ] **Step 3: Run tests**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go test ./internal/agents/ -v -count=1
```

Expected: all conversation context tests pass (10 tests + prior 14 = 24 total in the package).

- [ ] **Step 4: Commit**

```bash
git add internal/agents/context.go internal/agents/context_test.go
git commit -m "feat: add ConversationContext for in-memory session management"
```

---

### Task 6: Agents API

**Files:**
- Create: `internal/agents/api.go`
- Create: `internal/agents/api_test.go`

- [ ] **Step 1: Write failing tests for agent HTTP handlers**

Create `internal/agents/api_test.go`:

```go
package agents_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupAgentRouter() (*gin.Engine, *agents.Agent, *agents.ToolRegistry, *agents.ConversationContext) {
	reg := agents.NewToolRegistry()
	_ = reg.Register(&stubTool{
		name:        "echo",
		description: "echoes input",
		params:      map[string]interface{}{"message": "string"},
		result:      "echoed",
	})

	b := newMockBrain([]*types.InternalChatResponse{
		{
			ID:           "resp-1",
			Model:        "mock-model",
			Message:      types.InternalMessage{Role: types.RoleAssistant, Content: "Agent response."},
			FinishReason: "stop",
			Provider:     "mock",
		},
	})

	convCtx := agents.NewConversationContext(100)
	agent := agents.NewAgent(agents.AgentConfig{
		Brain:    b,
		Tools:    reg,
		MaxTurns: 5,
	})

	r := gin.New()
	agents.RegisterAgentRoutes(r, agent, reg, convCtx)
	return r, agent, reg, convCtx
}

func TestHandleAgentChatSuccess(t *testing.T) {
	r, _, _, _ := setupAgentRouter()

	body := map[string]interface{}{
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello agent"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/agents/chat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if _, ok := resp["message"]; !ok {
		t.Error("expected 'message' field in response")
	}
}

func TestHandleAgentChatEmptyMessages(t *testing.T) {
	r, _, _, _ := setupAgentRouter()

	body := map[string]interface{}{
		"messages": []map[string]interface{}{},
	}
	bodyBytes, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/agents/chat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleAgentChatInvalidJSON(t *testing.T) {
	r, _, _, _ := setupAgentRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/agents/chat", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleListTools(t *testing.T) {
	r, _, _, _ := setupAgentRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/agents/tools", nil)

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	toolsField, ok := resp["tools"]
	if !ok {
		t.Fatal("expected 'tools' field in response")
	}

	toolsList, ok := toolsField.([]interface{})
	if !ok {
		t.Fatal("expected 'tools' to be a list")
	}
	if len(toolsList) != 1 {
		t.Errorf("expected 1 tool, got %d", len(toolsList))
	}
}

func TestHandleAgentChatWithSession(t *testing.T) {
	r, _, _, _ := setupAgentRouter()

	body := map[string]interface{}{
		"session_id": "test-session-123",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Remember this"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/v1/agents/chat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
```

Expected: tests fail because `api.go` does not exist.

- [ ] **Step 2: Implement agent HTTP handlers**

Create `internal/agents/api.go`:

```go
package agents

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/pkg/types"
)

// AgentChatRequest is the request body for POST /v1/agents/chat.
type AgentChatRequest struct {
	SessionID string                   `json:"session_id,omitempty"`
	Messages  []types.InternalMessage  `json:"messages"`
	Model     string                   `json:"model,omitempty"`
	MaxTurns  int                      `json:"max_turns,omitempty"`
}

// AgentChatResponse is the response body for POST /v1/agents/chat.
type AgentChatResponse struct {
	ID           string                `json:"id"`
	Model        string                `json:"model"`
	Message      types.InternalMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
	Provider     types.Provider        `json:"provider"`
	SessionID    string                `json:"session_id,omitempty"`
}

// AgentToolsResponse is the response body for GET /v1/agents/tools.
type AgentToolsResponse struct {
	Tools []ToolInfo `json:"tools"`
}

// AgentErrorResponse is the error response for agent endpoints.
type AgentErrorResponse struct {
	Error AgentErrorDetail `json:"error"`
}

// AgentErrorDetail holds error information.
type AgentErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// RegisterAgentRoutes attaches the agent API handlers to the Gin engine.
//
//	POST /v1/agents/chat   -- Run the agent loop on messages
//	GET  /v1/agents/tools  -- List available tools
func RegisterAgentRoutes(r *gin.Engine, agent *Agent, registry *ToolRegistry, convCtx *ConversationContext) {
	g := r.Group("/v1/agents")
	g.POST("/chat", handleAgentChat(agent, convCtx))
	g.GET("/tools", handleListTools(registry))
}

func handleAgentChat(agent *Agent, convCtx *ConversationContext) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req AgentChatRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, AgentErrorResponse{
				Error: AgentErrorDetail{
					Message: "invalid request body: " + err.Error(),
					Type:    "invalid_request_error",
				},
			})
			return
		}

		if len(req.Messages) == 0 {
			c.JSON(http.StatusBadRequest, AgentErrorResponse{
				Error: AgentErrorDetail{
					Message: "messages must not be empty",
					Type:    "invalid_request_error",
				},
			})
			return
		}

		// If a session ID is provided, load prior context and append new messages.
		var allMessages []types.InternalMessage
		if req.SessionID != "" && convCtx != nil {
			prior := convCtx.Get(req.SessionID)
			allMessages = append(prior, req.Messages...)
		} else {
			allMessages = req.Messages
		}

		resp, err := agent.Run(c.Request.Context(), allMessages)
		if err != nil {
			c.JSON(http.StatusInternalServerError, AgentErrorResponse{
				Error: AgentErrorDetail{
					Message: err.Error(),
					Type:    "agent_error",
				},
			})
			return
		}

		// Store messages in conversation context if a session ID is provided.
		if req.SessionID != "" && convCtx != nil {
			convCtx.AddMultiple(req.SessionID, req.Messages)
			convCtx.Add(req.SessionID, resp.Message)
		}

		c.JSON(http.StatusOK, AgentChatResponse{
			ID:           resp.ID,
			Model:        resp.Model,
			Message:      resp.Message,
			FinishReason: resp.FinishReason,
			Provider:     resp.Provider,
			SessionID:    req.SessionID,
		})
	}
}

func handleListTools(registry *ToolRegistry) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, AgentToolsResponse{
			Tools: registry.List(),
		})
	}
}
```

- [ ] **Step 3: Run tests**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go test ./internal/agents/ -v -count=1
```

Expected: all API tests pass (5 API tests + prior 24 = 29 total in the package).

- [ ] **Step 4: Commit**

```bash
git add internal/agents/api.go internal/agents/api_test.go
git commit -m "feat: add /v1/agents/chat and /v1/agents/tools HTTP endpoints"
```

---

### Task 7: Wire into Server

**Files:**
- Modify: `cmd/helixllm/main.go`
- Modify: `internal/shared/config/config.go` (add AgentsConfig)

- [ ] **Step 1: Add AgentsConfig to config**

In `internal/shared/config/config.go`, add:

1. An `AgentsConfig` struct:

```go
// AgentsConfig holds agents layer settings.
type AgentsConfig struct {
	MaxTurns   int `env:"HELIX_AGENT_MAX_TURNS" default:"10"`
	MaxHistory int `env:"HELIX_AGENT_MAX_HISTORY" default:"100"`
}
```

2. Add the field to `HelixConfig`:

```go
type HelixConfig struct {
    // ... existing fields ...
    Agents  AgentsConfig
}
```

- [ ] **Step 2: Wire Agent into main.go**

Update `cmd/helixllm/main.go` to:

1. Import the agents packages:

```go
import (
    // ... existing imports ...
    "github.com/HelixDevelopment/HelixLLM/internal/agents"
    agenttools "github.com/HelixDevelopment/HelixLLM/internal/agents/tools"
)
```

2. After the knowledge pipeline creation, create and wire the agent:

```go
	// Create agent tool registry and register built-in tools.
	toolRegistry := agents.NewToolRegistry()
	_ = toolRegistry.Register(agenttools.NewEchoTool())
	_ = toolRegistry.Register(agenttools.NewTimeTool())
	_ = toolRegistry.Register(agenttools.NewKnowledgeQueryTool(pipeline, "default"))

	// Create conversation context for session management.
	convCtx := agents.NewConversationContext(cfg.Agents.MaxHistory)

	// Create agent with Brain, tools, and RAG hook.
	ragHook := knowledge.RAGHook(pipeline, "default")
	agentSvc := agents.NewAgent(agents.AgentConfig{
		Brain:    brainSvc,
		Tools:    toolRegistry,
		RAGHook:  ragHook,
		MaxTurns: cfg.Agents.MaxTurns,
	})

	// Register agent routes.
	agents.RegisterAgentRoutes(srv.Router(), agentSvc, toolRegistry, convCtx)
```

3. Remove the unused `_ = knowledge.RAGHook(pipeline, "default")` line since the RAG hook is now actually wired into the agent.

After the changes, the full main.go should look like:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/HelixDevelopment/HelixLLM/internal/agents"
	agenttools "github.com/HelixDevelopment/HelixLLM/internal/agents/tools"
	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/gateway"
	"github.com/HelixDevelopment/HelixLLM/internal/knowledge"
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

	// Create Brain — registers whichever providers are configured.
	brainSvc := brain.New(brain.Config{
		LlamaCppURL:     fmt.Sprintf("http://localhost:%d", cfg.LLM.LocalRPCPort),
		LlamaCppModels:  []string{cfg.LLM.LocalModel},
		OpenAIKey:       cfg.LLM.OpenAIKey,
		AnthropicKey:    cfg.LLM.AnthropicKey,
		DefaultProvider: cfg.LLM.DefaultProvider,
	})

	// Register gateway routes (OpenAI + Anthropic compatible endpoints)
	gateway.RegisterRoutes(srv.Router(), gateway.RouterOptions{
		APIKeys:   cfg.Auth.APIKeys,
		RateLimit: 0, // TODO: add to config
		Brain:     brainSvc,
	})

	// Create knowledge pipeline with in-memory components.
	embedder := knowledge.NewHashEmbedder(768)
	store := knowledge.NewMemoryStore()
	chunker := knowledge.NewFixedSizeChunker(cfg.Knowledge.RAGChunkSize, cfg.Knowledge.RAGChunkOverlap)
	pipeline := knowledge.NewPipeline(knowledge.PipelineConfig{
		Embedder:          embedder,
		Store:             store,
		Chunker:           chunker,
		DefaultCollection: "default",
		DefaultTopK:       cfg.Knowledge.RAGTopK,
	})
	knowledge.RegisterKnowledgeRoutes(srv.Router(), pipeline)

	// Create agent tool registry and register built-in tools.
	toolRegistry := agents.NewToolRegistry()
	_ = toolRegistry.Register(agenttools.NewEchoTool())
	_ = toolRegistry.Register(agenttools.NewTimeTool())
	_ = toolRegistry.Register(agenttools.NewKnowledgeQueryTool(pipeline, "default"))

	// Create conversation context for session management.
	convCtx := agents.NewConversationContext(cfg.Agents.MaxHistory)

	// Create agent with Brain, tools, and RAG hook.
	ragHook := knowledge.RAGHook(pipeline, "default")
	agentSvc := agents.NewAgent(agents.AgentConfig{
		Brain:    brainSvc,
		Tools:    toolRegistry,
		RAGHook:  ragHook,
		MaxTurns: cfg.Agents.MaxTurns,
	})

	// Register agent routes.
	agents.RegisterAgentRoutes(srv.Router(), agentSvc, toolRegistry, convCtx)

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

- [ ] **Step 3: Verify build**

```bash
cd /run/media/milosvasic/DATA4TB/Projects/HelixLLM
go build ./...
```

Expected: builds successfully with all agents layer code compiled.

- [ ] **Step 4: Run all tests**

```bash
go test ./internal/agents/... -v -count=1
go test ./... -count=1
```

Expected: all tests pass across the entire project.

- [ ] **Step 5: Commit**

```bash
git add cmd/helixllm/main.go internal/shared/config/config.go
git commit -m "feat: wire agents layer into server with tool registry, RAG hook, and conversation context"
```
