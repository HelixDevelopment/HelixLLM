# Lesson 3: Challenge Banks

**Duration:** 25 minutes
**Prerequisites:** Lesson 2 (Testing Strategy)
**Learning Objectives:**
- Understand the challenge bank YAML format and category system
- Run challenge banks against a live HelixLLM instance
- Write custom challenges with assertions for API behavior
- Filter and prioritize challenges by category and severity

---

## Scene 1: What Are Challenge Banks? (4 min)

**Narration:** "Challenge banks are YAML-based test definitions that validate HelixLLM against real-world scenarios. Unlike unit tests that run in isolation with mocks, challenge banks test the live running system. They send real HTTP requests, check real responses, and verify actual behavior."

**Screen:** Show the challenge bank directory structure.

```
challenges/banks/
  llm/          Code generation, multi-turn, tool calling, streaming
  rag/          Retrieval quality, ingestion, embedding accuracy
  api/          OpenAI/Anthropic compat, error handling, auth
  cluster/      Deployment, failover, rebalancing, host probing
  chaos/        Container kills, network partitions, resource exhaustion
  security/     Injection, auth bypass, PII, rate limiting
  benchmarks/   Latency, throughput, concurrent users
  workflows/    Real developer scenarios (coding, review, debugging)
  regression/   Known-fixed bugs, edge cases
```

**Narration:** "Challenge banks are organized by domain. Each directory contains YAML files that define test scenarios. The Challenges framework and HelixQA orchestrator execute them against a running server."

**Key points:**
- Challenge banks test the live system, not mocked components
- Organized by category: api, security, llm, rag, cluster, chaos, benchmarks, workflows, regression
- Executed by the `digital.vasic.challenges` framework
- Orchestrated by `digital.vasic.helixqa`
- Require a running server or `make build` first

---

## Scene 2: Challenge YAML Format (6 min)

**Narration:** "Each challenge is a YAML file with a name, category, severity, test steps, and assertions. Let me walk through the format."

**Screen:** Show a complete challenge example.

```yaml
name: "Chat completion returns valid response"
category: api
severity: critical
steps:
  - action: http_post
    url: /v1/chat/completions
    body:
      model: "Llama-3.1-70B-Instruct-Q4_K_M"
      messages:
        - role: user
          content: "Say hello"
    assertions:
      - status_code: 200
      - json_path: "$.choices[0].message.role"
        equals: "assistant"
      - json_path: "$.choices[0].message.content"
        not_empty: true
      - json_path: "$.usage.total_tokens"
        greater_than: 0
```

**Narration:** "This challenge sends a POST request to the chat completions endpoint and asserts four things: HTTP 200 status, the assistant role is set, the content is not empty, and tokens were consumed."

**Screen:** Show a multi-step challenge.

```yaml
name: "Knowledge ingestion and retrieval"
category: rag
severity: high
steps:
  - action: http_post
    url: /internal/knowledge/ingest
    body:
      content: "HelixLLM uses a mode system with six deployment modes."
      collection: "challenge-test"
      metadata:
        source: "challenge"
    assertions:
      - status_code: 200
      - json_path: "$.status"
        equals: "completed"
      - json_path: "$.chunks"
        greater_than: 0

  - action: http_post
    url: /internal/knowledge/query
    body:
      query: "What deployment modes are available?"
      collection: "challenge-test"
      top_k: 3
    assertions:
      - status_code: 200
      - json_path: "$.results[0].score"
        greater_than: 0.5
      - json_path: "$.results[0].content"
        contains: "mode"
```

**Key points:**
- `name` -- human-readable description of what is being tested
- `category` -- api, security, rag, llm, cluster, chaos, benchmarks, workflows, regression
- `severity` -- critical, high, medium, low
- `steps` -- sequential HTTP actions with assertions
- Assertions: `status_code`, `json_path` with `equals`, `not_empty`, `contains`, `greater_than`

---

## Scene 3: Running Challenge Banks (5 min)

**Narration:** "Challenge banks are run using the HelixLLM binary with the --challenges flag. You need a running server or the built binary."

**Demo steps:**

```bash
# First, ensure the server is running
make dev &

# Run all challenge banks
./bin/helixllm --challenges --base-url=https://localhost:8443
```

**Narration:** "You can also filter by directory, category, or priority."

```bash
# Run only API challenge banks
make test-challenges-api

# Run a specific directory
./bin/helixllm --challenges \
  --banks-dir=challenges/banks/security/ \
  --base-url=https://localhost:8443

# Filter by category
./bin/helixllm --challenges \
  --category=rag \
  --base-url=https://localhost:8443

# Filter by priority
./bin/helixllm --challenges \
  --category=rag \
  --priority=high \
  --base-url=https://localhost:8443
```

**Narration:** "The output shows each challenge name, its result, and details for any failures."

**Expected output:**

```
Running challenges from challenges/banks/api/
  [PASS] Chat completion returns valid response
  [PASS] Models endpoint returns list
  [PASS] Embeddings endpoint returns vectors
  [PASS] Invalid request returns 400
  [FAIL] Streaming response format
    Assertion failed: json_path "$.choices[0].delta.content" not_empty
    Expected: not empty, Got: ""

Results: 4 passed, 1 failed, 0 skipped
```

**Key points:**
- `--challenges` flag activates challenge runner mode
- `--base-url` points to the running server
- `--banks-dir` filters by directory
- `--category` filters by category tag
- `--priority` filters by severity level
- Use `make test-challenges` as a shortcut for all banks

---

## Scene 4: Writing Custom Challenges (6 min)

**Narration:** "Let me show you how to write your own challenges for specific scenarios you need to validate."

**Demo steps:**

```yaml
# challenges/banks/api/custom-validation.yaml
name: "Error responses include message and type"
category: api
severity: high
steps:
  - action: http_post
    url: /v1/chat/completions
    body:
      model: "test"
    assertions:
      - status_code: 400
      - json_path: "$.error.message"
        not_empty: true
      - json_path: "$.error.type"
        equals: "invalid_request_error"
```

```yaml
# challenges/banks/api/health-check.yaml
name: "Health endpoint returns healthy status"
category: api
severity: critical
steps:
  - action: http_get
    url: /internal/health
    assertions:
      - status_code: 200
      - json_path: "$.status"
        equals: "healthy"
```

```yaml
# challenges/banks/security/auth-required.yaml
name: "API endpoints require authentication when keys configured"
category: security
severity: critical
steps:
  - action: http_get
    url: /v1/models
    headers:
      Authorization: "Bearer invalid-key"
    assertions:
      - status_code: 401

  - action: http_get
    url: /v1/models
    assertions:
      - status_code: 401
```

**Narration:** "Each challenge should test one specific behavior. Use multiple steps when the test requires setup, such as ingesting a document before querying."

**Key points:**
- One challenge per YAML file for clarity
- Name the file descriptively (e.g., `error-format-validation.yaml`)
- Place in the appropriate category directory
- Test both positive (expected success) and negative (expected failure) cases
- Use severity to prioritize which challenges must pass before release

---

## Scene 5: Challenge Categories Deep Dive (4 min)

**Narration:** "Let me explain what each category covers so you know where to add new challenges."

**Screen:** Show the category descriptions.

| Category | What It Tests | Examples |
|----------|--------------|---------|
| `api` | OpenAI/Anthropic endpoint compliance | Response format, error handling, auth |
| `security` | Security controls and vulnerability resistance | SQL injection, auth bypass, PII leakage |
| `llm` | LLM response quality and tool calling | Code generation, multi-turn coherence |
| `rag` | Retrieval accuracy and ingestion pipeline | Embedding quality, search relevance |
| `cluster` | Multi-host deployment operations | Probe, deploy, rebalance, failover |
| `chaos` | Resilience under failure conditions | Container kills, resource exhaustion |
| `benchmarks` | Performance under load | Latency percentiles, throughput |
| `workflows` | Real developer usage scenarios | Code review, debugging sessions |
| `regression` | Previously fixed bugs | Edge cases, known-fixed issues |

**Narration:** "For a release, all critical and high-severity challenges must pass. Medium and low challenges are informational and may have acceptable failures in specific environments."

**Key points:**
- Critical severity: must pass for release
- High severity: should pass, investigate failures
- Medium severity: informational, may have acceptable failures
- Low severity: nice-to-have, aspirational quality targets
- Add regression challenges whenever you fix a bug to prevent recurrence

---

## Exercises

1. Run `make test-challenges-api` against a running server and review the results, noting any failures
2. Write a challenge bank YAML file that tests the complete ingest-query cycle: ingest a document, query for it, and assert the relevance score is above 0.5
3. Create a security challenge that sends a request with SQL injection in the messages content and asserts that the server responds without error (proving injection resistance)
