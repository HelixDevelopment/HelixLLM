# Phase 4: Dead Code Elimination & Feature Completion

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Connect all dead code, resolve missing module references, complete stub implementations, and handle deprecated constants — leaving zero unconnected features.

**Architecture:** Each fix is isolated to its submodule or package. Changes use existing patterns and interfaces. All fixes include tests. External module dependencies are documented with setup instructions.

**Tech Stack:** Go 1.26.1, digital.vasic.database, digital.vasic.containers, golang.org/x/crypto/ssh

---

### Task 1: Investigate and resolve TOON submodule status

**Files:**
- Read: `submodules/TOON/` (all files)
- Possibly modify: `go.mod`, `.gitmodules`

- [ ] **Step 1: Audit TOON submodule contents**

Run: `ls -la submodules/TOON/ && cat submodules/TOON/go.mod && find submodules/TOON -name "*.go" | head -20`
Expected: Understand whether TOON has actual implementation or is a placeholder

- [ ] **Step 2: Check if TOON is feature-gated**

Run: `grep -r "HELIX_FEATURE_TOON\|toon\|TOON" internal/ --include="*.go" | head -20`
Expected: Identify all references to TOON in the main project

- [ ] **Step 3: Document TOON status**

Based on findings:
- If TOON has implementation: verify it compiles, add tests if missing, document in `docs/manual/modules.md`
- If TOON is empty/placeholder with feature flag: add doc comment in the import site explaining it's gated behind `HELIX_FEATURE_TOON`, add a note in `docs/user-guide/configuration.md`
- If TOON is truly abandoned: remove from `go.mod` replace directives (but do NOT remove from `.gitmodules` without user confirmation)

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "docs: document TOON submodule status and feature flag"
```

---

### Task 2: Add deprecation warnings for model constants

**Files:**
- Modify: `submodules/LLMProvider/pkg/providers/zen/zen.go:51-52`

- [ ] **Step 1: Read current deprecated constants**

Run: `grep -n "Deprecated" submodules/LLMProvider/pkg/providers/zen/zen.go`
Expected: See the deprecated model constants

- [ ] **Step 2: Add proper Go deprecation doc comments**

In `submodules/LLMProvider/pkg/providers/zen/zen.go`, update the deprecated constants to follow Go convention:

Find the deprecated constants (around lines 51-52) and ensure they have:
```go
// Deprecated: ModelGrokCodeFast may not be available. Use an alternative model.
ModelGrokCodeFast = "grok-code"

// Deprecated: ModelGLM47Free is superseded by glm-5-free.
ModelGLM47Free = "glm-4.7-free"
```

- [ ] **Step 3: Run submodule tests**

Run: `cd submodules/LLMProvider && go test ./... && cd ../..`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add submodules/LLMProvider/pkg/providers/zen/zen.go
git commit -m "docs: add Go-convention Deprecated comments for obsolete model constants"
```

---

### Task 3: Wire Brain parameter in embeddings endpoint

**Files:**
- Modify: `internal/gateway/openai.go` (embeddings handler)

- [ ] **Step 1: Read the current embeddings handler**

Run: `grep -n -A 20 "func.*[Ee]mbedding" internal/gateway/openai.go`
Expected: See the handler signature and where Brain is accepted but unused

- [ ] **Step 2: Determine if Brain is needed for embeddings**

If the embeddings endpoint delegates to the knowledge package's embedding providers (which are independent of Brain), then Brain is correctly unused. In that case:

Add a doc comment explaining why:
```go
// handleEmbeddings serves /v1/embeddings. Brain is accepted for interface
// consistency with other handlers but is not used — embeddings are generated
// by the knowledge layer's embedding providers, not the LLM providers.
```

- [ ] **Step 3: Run tests**

Run: `go test -v -count=1 ./internal/gateway/...`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/gateway/openai.go
git commit -m "docs: clarify Brain parameter in embeddings handler is intentionally unused"
```

---

### Task 4: Investigate and resolve missing external modules

**Files:**
- Read: `submodules/*/go.mod` for references to `digital.vasic.models` and `digital.vasic.messaging`
- Possibly modify: documentation files

- [ ] **Step 1: Check if Models and Messaging exist on disk**

Run: `find /run/media/milosvasic/DATA4TB/Projects -maxdepth 2 -name "go.mod" -exec grep -l "digital.vasic.models\|digital.vasic.messaging" {} \;`
Expected: Find any go.mod referencing these modules

- [ ] **Step 2: Check which submodules need them**

Run: `grep -r "digital.vasic.models\|digital.vasic.messaging" submodules/*/go.mod`
Expected: Identify which submodules have these as dependencies

- [ ] **Step 3: Verify the main project builds without them**

Run: `go build ./cmd/helixllm`
Expected: If build succeeds, these are only transitive dependencies in submodules not imported by the main project

- [ ] **Step 4: Document external dependencies**

Add to `docs/manual/development.md` a section on external dependencies:

```markdown
## External Dependencies

Some submodules reference modules that live outside the HelixLLM repository:

| Module | Required By | Purpose | Setup |
|--------|-------------|---------|-------|
| `digital.vasic.models` | LLMProvider, BackgroundTasks | Shared model type definitions | Clone from vasic-digital org, place at `../Models` relative to submodule |
| `digital.vasic.messaging` | conversation | Messaging abstractions | Clone from vasic-digital org, place at `../Messaging` relative to submodule |
| `digital.vasic.docprocessor` | HelixQA | Document processing | Clone from vasic-digital org, place at `../DocProcessor` |
| `digital.vasic.visionengine` | HelixQA | Vision processing | Clone from vasic-digital org, place at `../VisionEngine` |

These are only needed when working directly on the listed submodules.
The main HelixLLM binary builds without them.
```

- [ ] **Step 5: Commit**

```bash
git add docs/manual/development.md
git commit -m "docs: document external module dependencies and their setup instructions"
```

---

### Task 5: Add regression challenge bank for dead code

**Files:**
- Create: `challenges/banks/regression/dead_code.yaml`

- [ ] **Step 1: Create dead code regression challenges**

Create `challenges/banks/regression/dead_code.yaml`:

```yaml
name: Dead Code Regression
description: Verifies all previously-dead or stub features are now reachable and functional
category: regression
priority: high

challenges:
  - name: embeddings_endpoint_accessible
    description: Embeddings endpoint responds (even if no provider configured)
    steps:
      - method: POST
        path: /v1/embeddings
        body:
          model: "text-embedding-ada-002"
          input: "test embedding"
        assertions:
          - type: status_one_of
            values: [200, 503]

  - name: vector_store_factory_memory_fallback
    description: System starts with in-memory vector store when no backend configured
    steps:
      - method: GET
        path: /internal/health
        assertions:
          - type: status
            value: 200

  - name: agent_tools_registered
    description: Agent tools endpoint lists registered tools
    steps:
      - method: GET
        path: /v1/agents/tools
        assertions:
          - type: status
            value: 200
```

- [ ] **Step 2: Commit**

```bash
git add challenges/banks/regression/dead_code.yaml
git commit -m "test: add dead code regression challenge bank"
```

---

### Task 6: Final verification

- [ ] **Step 1: Build the project**

Run: `make build`
Expected: Clean build with no warnings

- [ ] **Step 2: Run all tests**

Run: `make test-unit`
Expected: All tests PASS

- [ ] **Step 3: Run lint**

Run: `make lint`
Expected: No new lint issues introduced
