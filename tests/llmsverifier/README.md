# HelixLLM Advanced LLMsVerifier

Comprehensive test suite for validating HelixLLM's capabilities as a software engineering AI system.

## Overview

This test suite validates HelixLLM across 11 categories with **950 total points**:

| Category | Points | Description |
|----------|--------|-------------|
| Core Model Capabilities | 150 | Reasoning, coding, math, logic |
| MCP Integration | 100 | Tool calling, external services |
| RAG System | 100 | Knowledge retrieval, context injection |
| LSP Integration | 100 | Code intelligence, language servers |
| ACP Multi-Agent | 100 | Agent coordination, task delegation |
| Embeddings | 75 | Vector operations, similarity search |
| Streaming | 75 | Real-time response handling |
| Context Window | 75 | Long context handling (8K+) |
| Performance | 75 | Latency, throughput benchmarks |
| API Compatibility | 50 | OpenAI/Anthropic API compatibility |
| Security | 50 | TLS, validation, rate limiting |

## Grading Scale

| Grade | Range | Status |
|-------|-------|--------|
| **A+** | 95-100% | Excellent - Production Ready |
| **A** | 90-94% | Very Good |
| **A-** | 85-89% | Good |
| **B+** | 80-84% | Above Average |
| **B** | 70-79% | Acceptable |
| **C** | 60-69% | Needs Improvement |
| **F** | <60% | Not Production Ready |

## Prerequisites

1. HelixLLM running with all services:
   ```bash
   cd /path/to/HelixAgent
   podman-compose -f docker-compose.helixllm.yml --profile full up -d
   ```

2. llama.cpp loaded with a model (Qwen2.5-1.5B or similar)

3. Environment configured:
   ```bash
   export HELIXLLM_ENDPOINT=https://localhost:8443
   export LLAMACPP_ENDPOINT=http://localhost:8081
   ```

## Usage

### Basic Run
```bash
./tests/llmsverifier/helixllm_verifier.sh
```

### With Custom Endpoint
```bash
HELIXLLM_ENDPOINT=https://localhost:8443 ./tests/llmsverifier/helixllm_verifier.sh
```

### Verbose Mode
```bash
VERBOSE=true ./tests/llmsverifier/helixllm_verifier.sh
```

## Test Categories

### 1. Core Model Capabilities (150 pts)

Tests the base LLM's ability to:
- **Chain of Thought Reasoning**: Multi-step logical deduction
- **Logical Deduction**: Syllogistic reasoning
- **Multi-step Problem Solving**: Complex math problems
- **Code Generation**: Python functions with type hints
- **Code Explanation**: Understanding recursive functions
- **Debug Code**: Identifying and fixing bugs
- **Algorithm Design**: O(n) complexity solutions
- **Mathematics**: Algebra, arithmetic, pattern recognition

### 2. MCP Integration (100 pts)

Validates Model Context Protocol:
- Tool discovery endpoint
- Echo tool execution
- Time tool execution
- Knowledge query tool

### 3. RAG System (100 pts)

Tests Retrieval-Augmented Generation:
- Document ingestion
- Knowledge query with context
- Collection statistics
- System health

### 4. LSP Integration (100 pts)

Validates Language Server Protocol:
- LSP bridge health
- Language server management
- Code diagnostics
- Definition lookup

### 5. ACP Multi-Agent (100 pts)

Tests Agent Communication Protocol:
- Agent registry
- Task creation and management
- Coordinator health
- Memory operations (remember/recall)

### 6. Embeddings (75 pts)

Validates embedding system:
- Text to vector generation
- Vector dimension verification
- Batch processing

### 7. Streaming (75 pts)

Tests real-time capabilities:
- Server-sent events
- Streaming chat completions
- Response chunking

### 8. Context Window (75 pts)

Validates long-context handling:
- 8K token context support
- Multi-turn conversations
- Context retention across turns

### 9. Performance (75 pts)

Benchmarks system performance:
- Response latency (<500ms = excellent)
- Concurrent request handling
- Token generation rate

### 10. API Compatibility (50 pts)

Validates API compliance:
- OpenAI-compatible endpoints
- Anthropic-compatible endpoints
- Health check endpoints

### 11. Security (50 pts)

Tests security features:
- TLS/HTTPS enforcement
- Request validation
- Rate limiting headers
- Content Security Policy

## Output

Reports are generated in `reports/llmsverifier-<timestamp>/`:
- `report.json` - Machine-readable results
- `REPORT.md` - Human-readable report
- `test.log` - Detailed execution log

## Interpreting Results

### For Software Engineering Use

**Minimum Requirements:**
- Grade A- (85%+) for production coding assistance
- All coding tests passing
- LSP integration functional
- MCP tools operational

**Ideal Requirements:**
- Grade A+ (95%+) for mission-critical development
- Sub-500ms response times
- All 950 points achieved

### Example Output

```
====================================
HelixLLM Advanced LLMsVerifier Test Suite
====================================
[INFO] Report Directory: /path/to/reports/llmsverifier-20260405-230000
[INFO] Configuration:
[INFO]   HELIXLLM_ENDPOINT: https://localhost:8443
...
====================================
FINAL SUMMARY
====================================
Total Tests: 42
  Passed: 38
  Failed: 0
  Skipped: 4

Score: 875/950 (92.1%)
[PASS] ALL TESTS PASSED!
```

## Continuous Integration

Add to your CI pipeline:

```yaml
# Example GitLab CI (not allowed in this project per Constitution)
test-helixllm:
  script:
    - podman-compose -f docker-compose.helixllm.yml up -d
    - sleep 60  # Wait for model download
    - ./tests/llmsverifier/helixllm_verifier.sh
    - cat reports/llmsverifier-*/REPORT.md
```

## Troubleshooting

### Model Not Loaded
```bash
# Check llama.cpp status
curl http://localhost:8081/health
```

### Connection Refused
```bash
# Verify HelixLLM is running
podman ps | grep helixllm
curl -k https://localhost:8443/internal/health
```

### Tests Timing Out
```bash
# Increase timeout
TEST_TIMEOUT=600 ./tests/llmsverifier/helixllm_verifier.sh
```

## Contributing

When adding new tests:
1. Maintain 950 total point scale
2. Add tests to appropriate category
3. Update category descriptions
4. Document in README

## License

Same as HelixLLM project
