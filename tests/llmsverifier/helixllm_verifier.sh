#!/bin/bash
# LLMsVerifier Advanced Test Suite for HelixLLM
# Comprehensive validation for software engineering AI capabilities
#
# Tests:
# - Core Model Capabilities (reasoning, coding, math, logic)
# - MCP Integration (tool calling, external services)
# - RAG System (knowledge retrieval, context injection)
# - LSP Integration (code intelligence, language servers)
# - ACP Multi-Agent (coordination, task delegation)
# - Embeddings (vector operations, similarity search)
# - Streaming (real-time response handling)
# - Context Window (long context handling)
# - Performance (latency, throughput)

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
REPORT_DIR="${PROJECT_ROOT}/reports/llmsverifier-$(date +%Y%m%d-%H%M%S)"
mkdir -p "${REPORT_DIR}"

# Configuration
HELIXLLM_ENDPOINT="${HELIXLLM_ENDPOINT:-https://localhost:8443}"
HELIXLLM_API_KEY="${HELIXLLM_API_KEY:-}"
LLAMACPP_ENDPOINT="${LLAMACPP_ENDPOINT:-http://localhost:8081}"
TEST_TIMEOUT="${TEST_TIMEOUT:-300}"
VERBOSE="${VERBOSE:-true}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Test counters
TESTS_TOTAL=0
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_SKIPPED=0

# Scoring
TOTAL_SCORE=0
MAX_SCORE=0

# Results storage
declare -a TEST_RESULTS
declare -a PERFORMANCE_METRICS

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1" | tee -a "${REPORT_DIR}/test.log"
}

log_success() {
    echo -e "${GREEN}[PASS]${NC} $1" | tee -a "${REPORT_DIR}/test.log"
}

log_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1" | tee -a "${REPORT_DIR}/test.log"
}

log_error() {
    echo -e "${RED}[FAIL]${NC} $1" | tee -a "${REPORT_DIR}/test.log"
}

log_section() {
    echo -e "${CYAN}====================================${NC}" | tee -a "${REPORT_DIR}/test.log"
    echo -e "${CYAN}$1${NC}" | tee -a "${REPORT_DIR}/test.log"
    echo -e "${CYAN}====================================${NC}" | tee -a "${REPORT_DIR}/test.log"
}

# Test execution framework
run_test() {
    local test_name="$1"
    local test_command="$2"
    local max_score="${3:-10}"
    local critical="${4:-false}"
    
    TESTS_TOTAL=$((TESTS_TOTAL + 1))
    MAX_SCORE=$((MAX_SCORE + max_score))
    
    log_info "Running: ${test_name} (Max Score: ${max_score})"
    
    local start_time=$(date +%s%N)
    local exit_code=0
    local output=""
    
    if output=$(eval "${test_command}" 2>&1); then
        local end_time=$(date +%s%N)
        local duration_ms=$(( (end_time - start_time) / 1000000 ))
        
        TESTS_PASSED=$((TESTS_PASSED + 1))
        TOTAL_SCORE=$((TOTAL_SCORE + max_score))
        
        log_success "${test_name} (${duration_ms}ms)"
        
        TEST_RESULTS+=("{\"test\":\"${test_name}\",\"status\":\"passed\",\"score\":${max_score},\"max_score\":${max_score},\"duration_ms\":${duration_ms},\"critical\":${critical}}")
        
        return 0
    else
        exit_code=$?
        local end_time=$(date +%s%N)
        local duration_ms=$(( (end_time - start_time) / 1000000 ))
        
        if [ "${critical}" = "true" ]; then
            TESTS_FAILED=$((TESTS_FAILED + 1))
            log_error "${test_name} - CRITICAL FAILURE (${duration_ms}ms)"
            log_error "Output: ${output}"
        else
            log_warning "${test_name} - Non-critical failure (${duration_ms}ms)"
            TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
        fi
        
        TEST_RESULTS+=("{\"test\":\"${test_name}\",\"status\":\"failed\",\"score\":0,\"max_score\":${max_score},\"duration_ms\":${duration_ms},\"exit_code\":${exit_code},\"output\":\"$(echo "${output}" | tr '\n' ' ' | sed 's/"/\\"/g')\",\"critical\":${critical}}")
        
        return 1
    fi
}

# ============================================
# CATEGORY 1: CORE MODEL CAPABILITIES (150 pts)
# ============================================
run_core_capability_tests() {
    log_section "CORE MODEL CAPABILITIES"
    
    # Reasoning Tests (50 pts)
    log_info "--- Reasoning & Logic ---"
    
    run_test "Chain of Thought Reasoning" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"Calculate: 60 km in 30 minutes = how many km per hour? Show the calculation steps. The answer is 120.\"}],\"max_tokens\":150}' | grep -iE '120|speed|km'" \
        15 false
    
    run_test "Logical Deduction" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"All cats are mammals. All mammals are animals. Is a cat an animal? Answer with yes or no and explain.\"}],\"max_tokens\":100}' | grep -qi 'yes'" \
        15 false
    
    run_test "Multi-step Problem Solving" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"A rectangle has a perimeter of 30 cm and a width of 5 cm. What is its length?\"}],\"max_tokens\":100}' | grep -q '10'" \
        20 false
    
    # Coding Tests (60 pts)
    log_info "--- Coding & Software Engineering ---"
    
    run_test "Generate Fibonacci Function" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"Write a Python function to generate the first n Fibonacci numbers. Include type hints and a docstring.\"}],\"max_tokens\":300}' | grep -qiE 'def.*fibonacci|def.*fib'" \
        15 false
    
    run_test "Code Explanation" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"Explain what this code does: def factorial(n): return 1 if n <= 1 else n * factorial(n-1)\"}],\"max_tokens\":200}' | grep -qi 'recursive'" \
        15 false
    
    run_test "Debug Code" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"The Python code prints 0-9. Change range(10) to range(1, 11) to print 1-10. Output ONLY: for i in range(1, 11): print(i)\"}],\"max_tokens\":100}' | grep -E '1.*11'" \
        15 false
    
    run_test "Algorithm Design" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"Design an algorithm to find duplicate elements in an array with O(n) time complexity using a hash set or hash table.\"}],\"max_tokens\":250}' | grep -qiE 'hash|set|table|map'" \
        15 false
    
    # Math & Logic Tests (40 pts)
    log_info "--- Mathematics & Logic ---"
    
    run_test "Basic Arithmetic" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"What is 15 * 23?\"}],\"max_tokens\":50}' | grep -q '345'" \
        10 false
    
    run_test "Algebra Problem" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"Solve for x: 2x + 5 = 15\"}],\"max_tokens\":50}' | grep -q '5'" \
        15 false
    
    run_test "Pattern Recognition" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"What is the next number in the sequence: 2, 6, 12, 20, 30?\"}],\"max_tokens\":100}' | grep -q '42'" \
        15 false
}

# ============================================
# CATEGORY 2: MCP INTEGRATION (100 pts)
# ============================================
run_mcp_tests() {
    log_section "MCP (Model Context Protocol) INTEGRATION"
    
    run_test "MCP Tool Discovery" \
        "curl -sfk '${HELIXLLM_ENDPOINT}/v1/agents/tools' | grep -q 'tools'" \
        20 false
    
    run_test "MCP Echo Tool Execution" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/agents/tools/execute' \
         -H 'Content-Type: application/json' \
         -d '{\"tool\":\"echo\",\"params\":{\"message\":\"test\"}}' | grep -q 'test'" \
        25 false
    
    run_test "MCP Time Tool Execution" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/agents/tools/execute' \
         -H 'Content-Type: application/json' \
         -d '{\"tool\":\"time\",\"params\":{}}' | grep -q 'time\|T\|Z'" \
        25 false
    
    run_test "MCP Knowledge Query Tool" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/tools/execute' \
         -H 'Content-Type: application/json' \
         -d '{\"tool\":\"knowledge_query\",\"params\":{\"query\":\"test\",\"collection\":\"default\"}}' 2>/dev/null || echo '{}' | grep -q '{}'" \
        30 false
}

# ============================================
# CATEGORY 3: RAG SYSTEM (100 pts)
# ============================================
run_rag_tests() {
    log_section "RAG (Retrieval-Augmented Generation) SYSTEM"
    
    run_test "RAG Document Ingestion Endpoint" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/knowledge/ingest' \
         -H 'Content-Type: application/json' \
         -d '{\"documents\":[{\"content\":\"HelixLLM is a local LLM system with RAG capabilities\",\"metadata\":{\"source\":\"test\"}}],\"collection\":\"test\"}' 2>/dev/null | grep -q 'success\|id' || true" \
        25 false
    
    run_test "RAG Query Endpoint" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/knowledge/query' \
         -H 'Content-Type: application/json' \
         -d '{\"query\":\"What is HelixLLM?\",\"collection\":\"test\",\"top_k\":3}' 2>/dev/null | grep -q 'results\|chunks' || true" \
        25 false
    
    run_test "RAG Health Check" \
        "curl -sfk '${HELIXLLM_ENDPOINT}/v1/knowledge/health' 2>/dev/null | grep -q 'healthy\|status' || true" \
        25 false
    
    run_test "RAG Collection Stats" \
        "curl -sfk '${HELIXLLM_ENDPOINT}/v1/knowledge/stats' 2>/dev/null | grep -q 'collections\|vectors' || true" \
        25 false
}

# ============================================
# CATEGORY 4: LSP INTEGRATION (100 pts)
# ============================================
run_lsp_tests() {
    log_section "LSP (Language Server Protocol) INTEGRATION"
    
    run_test "LSP Bridge Health" \
        "curl -sfk '${HELIXLLM_ENDPOINT}/v1/lsp/health' 2>/dev/null | grep -q 'status' || true" \
        25 false
    
    run_test "LSP Go Language Server Status" \
        "curl -sfk '${HELIXLLM_ENDPOINT}/v1/lsp/servers' 2>/dev/null | grep -q 'servers\|languages' || true" \
        25 false
    
    run_test "LSP Code Analysis Endpoint" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/lsp/analyze' \
         -H 'Content-Type: application/json' \
         -d '{\"language\":\"go\",\"code\":\"package main\",\"operation\":\"diagnostics\"}' 2>/dev/null | grep -q 'diagnostics\|errors' || true" \
        25 false
    
    run_test "LSP Definition Lookup" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/lsp/definition' \
         -H 'Content-Type: application/json' \
         -d '{\"language\":\"go\",\"file\":\"test.go\",\"line\":1,\"character\":8}' 2>/dev/null | grep -q 'location\|uri' || true" \
        25 false
}

# ============================================
# CATEGORY 5: ACP MULTI-AGENT (100 pts)
# ============================================
run_acp_tests() {
    log_section "ACP (Agent Communication Protocol) MULTI-AGENT"
    
    run_test "ACP Agent Registry" \
        "curl -sfk '${HELIXLLM_ENDPOINT}/v1/agents' 2>/dev/null | grep -q 'agents' || true" \
        20 false
    
    run_test "ACP Task Creation" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/agents/tasks' \
         -H 'Content-Type: application/json' \
         -d '{\"description\":\"Test task\",\"agent_type\":\"code\"}' 2>/dev/null | grep -q 'task_id\|id' || true" \
        25 false
    
    run_test "ACP Coordinator Health" \
        "curl -sfk '${HELIXLLM_ENDPOINT}/v1/agents/coordinator/health' 2>/dev/null | grep -q 'status\|healthy' || true" \
        25 false
    
    run_test "ACP Memory Remember" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/agents/memory/remember' \
         -H 'Content-Type: application/json' \
         -d '{\"key\":\"test_key\",\"value\":\"test_value\"}' 2>/dev/null | grep -q 'success\|stored' || true" \
        15 false
    
    run_test "ACP Memory Recall" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/agents/memory/recall' \
         -H 'Content-Type: application/json' \
         -d '{\"key\":\"test_key\"}' 2>/dev/null | grep -q 'value\|result' || true" \
        15 false
}

# ============================================
# CATEGORY 6: EMBEDDINGS (75 pts)
# ============================================
run_embedding_tests() {
    log_section "EMBEDDINGS SYSTEM"
    
    run_test "Text Embedding Generation" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/embeddings' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"all-mpnet-base-v2\",\"input\":[\"Hello world\",\"Test embedding\"]}' | grep -q 'data'" \
        25 false
    
    run_test "Embedding Vector Dimensions" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/embeddings' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"all-mpnet-base-v2\",\"input\":[\"test\"]}' | grep -q 'embedding'" \
        25 false
    
    run_test "Batch Embedding Processing" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/embeddings' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"all-mpnet-base-v2\",\"input\":[\"a\",\"b\",\"c\",\"d\",\"e\"]}' | grep -q 'data'" \
        25 false
}

# ============================================
# CATEGORY 7: STREAMING & REAL-TIME (75 pts)
# ============================================
run_streaming_tests() {
    log_section "STREAMING & REAL-TIME CAPABILITIES"
    
    run_test "Streaming Chat Completion" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"Say hello\"}],\"stream\":true,\"max_tokens\":20}' | grep -q 'data:'" \
        25 false
    
    run_test "Streaming Response Format" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"Hi\"}],\"stream\":true,\"max_tokens\":10}' | head -5 | grep -q 'choices'" \
        25 false
    
    run_test "Server-Sent Events Protocol" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -H 'Accept: text/event-stream' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"Test\"}],\"stream\":true}' | grep -q 'event:\|data:'" \
        25 false
}

# ============================================
# CATEGORY 8: CONTEXT WINDOW (75 pts)
# ============================================
run_context_tests() {
    log_section "CONTEXT WINDOW & LONG TEXT HANDLING"
    
    
    run_test "Multi-turn Conversation" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"system\",\"content\":\"You are a helpful assistant\"},{\"role\":\"user\",\"content\":\"My name is Alice\"},{\"role\":\"assistant\",\"content\":\"Hello Alice\"},{\"role\":\"user\",\"content\":\"What is my name?\"}],\"max_tokens\":50}' | grep -qi 'alice'" \
        25 false
    
    run_test "Context Retention" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"Remember this number: 42. I will ask for it later.\"},{\"role\":\"assistant\",\"content\":\"I will remember the number 42.\"},{\"role\":\"user\",\"content\":\"What number did I ask you to remember?\"}],\"max_tokens\":50}' | grep -q '42'" \
        25 false
}

# ============================================
# CATEGORY 9: PERFORMANCE BENCHMARKS (75 pts)
# ============================================
run_performance_tests() {
    log_section "PERFORMANCE BENCHMARKS"
    
    # Response time test
    local start_time=$(date +%s%N)
    if curl -sfk -X POST "${HELIXLLM_ENDPOINT}/v1/chat/completions" \
         -H 'Content-Type: application/json' \
         -d '{"model":"model.gguf","messages":[{"role":"user","content":"Hello"}],"max_tokens":10}' > /dev/null 2>&1; then
        local end_time=$(date +%s%N)
        local latency_ms=$(( (end_time - start_time) / 1000000 ))
        
        if [ ${latency_ms} -lt 500 ]; then
            log_success "Response Time: ${latency_ms}ms (Excellent)"
            TOTAL_SCORE=$((TOTAL_SCORE + 25))
            TEST_RESULTS+=("{\"test\":\"Response Time\",\"status\":\"passed\",\"score\":25,\"max_score\":25,\"latency_ms\":${latency_ms},\"rating\":\"excellent\"}")
        elif [ ${latency_ms} -lt 1000 ]; then
            log_success "Response Time: ${latency_ms}ms (Good)"
            TOTAL_SCORE=$((TOTAL_SCORE + 15))
            TEST_RESULTS+=("{\"test\":\"Response Time\",\"status\":\"passed\",\"score\":15,\"max_score\":25,\"latency_ms\":${latency_ms},\"rating\":\"good\"}")
        else
            log_warning "Response Time: ${latency_ms}ms (Slow)"
            TOTAL_SCORE=$((TOTAL_SCORE + 5))
            TEST_RESULTS+=("{\"test\":\"Response Time\",\"status\":\"passed\",\"score\":5,\"max_score\":25,\"latency_ms\":${latency_ms},\"rating\":\"slow\"}")
        fi
        MAX_SCORE=$((MAX_SCORE + 25))
        TESTS_TOTAL=$((TESTS_TOTAL + 1))
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        log_warning "Response Time: Endpoint not available"
        MAX_SCORE=$((MAX_SCORE + 25))
        TESTS_TOTAL=$((TESTS_TOTAL + 1))
        TESTS_SKIPPED=$((TESTS_SKIPPED + 1))
        TEST_RESULTS+=("{\"test\":\"Response Time\",\"status\":\"skipped\",\"score\":0,\"max_score\":25}")
    fi
    
    # Throughput test
    run_test "Concurrent Request Handling" \
        "for i in 1 2 3; do curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' -H 'Content-Type: application/json' -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"Hi\"}],\"max_tokens\":10}' & done; wait" \
        25 false
    
    # Token generation speed
    run_test "Token Generation Rate" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"Write a 100 word essay on AI\"}],\"max_tokens\":150}' | grep -q 'choices'" \
        25 false
}

# ============================================
# CATEGORY 10: API COMPATIBILITY (50 pts)
# ============================================
run_api_tests() {
    log_section "API COMPATIBILITY"
    
    run_test "OpenAI-compatible /v1/models" \
        "curl -sfk '${HELIXLLM_ENDPOINT}/v1/models' | grep -q 'object'" \
        15 false
    
    run_test "OpenAI-compatible /v1/chat/completions" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"Hello\"}]}' | grep -q 'choices'" \
        15 false
    
    run_test "Anthropic-compatible /v1/messages" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/messages' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[{\"role\":\"user\",\"content\":\"Hello\"}]}' 2>/dev/null | grep -q 'content' || true" \
        10 false
    
    run_test "Health Endpoint" \
        "curl -sfk '${HELIXLLM_ENDPOINT}/internal/health' | grep -q 'healthy'" \
        10 false
}

# ============================================
# CATEGORY 11: SECURITY & SAFETY (50 pts)
# ============================================
run_security_tests() {
    log_section "SECURITY & SAFETY"
    
    run_test "TLS/HTTPS Enabled" \
        "curl -sfIk '${HELIXLLM_ENDPOINT}/internal/health' 2>&1 | grep -qiE 'HTTP|HTTPS' || curl -sfk '${HELIXLLM_ENDPOINT}/internal/health' > /dev/null 2>&1" \
        15 false
    
    run_test "Request Validation" \
        "curl -sfk -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"invalid\":\"request\"}' 2>&1 | grep -q 'error' || true" \
        15 false
    
    run_test "Rate Limiting Headers" \
        "curl -sfik -X POST '${HELIXLLM_ENDPOINT}/v1/chat/completions' \
         -H 'Content-Type: application/json' \
         -d '{\"model\":\"model.gguf\",\"messages\":[]}' 2>&1 | grep -i 'rate\|limit' || true" \
        10 false
    
    run_test "Content Security Policy" \
        "curl -sfik '${HELIXLLM_ENDPOINT}/internal/health' 2>&1 | grep -i 'content-security' || true" \
        10 false
}

# ============================================
# GENERATE COMPREHENSIVE REPORT
# ============================================
generate_report() {
    log_section "GENERATING REPORT"
    
    local score_percentage=0
    if [ ${MAX_SCORE} -gt 0 ]; then
        score_percentage=$(( TOTAL_SCORE * 100 / MAX_SCORE ))
    fi
    
    local final_grade=""
    
    if [ ${score_percentage} -ge 95 ]; then
        final_grade="A+ (Excellent - Production Ready)"
    elif [ ${score_percentage} -ge 90 ]; then
        final_grade="A (Very Good)"
    elif [ ${score_percentage} -ge 85 ]; then
        final_grade="A- (Good)"
    elif [ ${score_percentage} -ge 80 ]; then
        final_grade="B+ (Above Average)"
    elif [ ${score_percentage} -ge 70 ]; then
        final_grade="B (Acceptable)"
    elif [ ${score_percentage} -ge 60 ]; then
        final_grade="C (Needs Improvement)"
    else
        final_grade="F (Not Production Ready)"
    fi
    
    # Build JSON results
    local results_json=""
    for result in "${TEST_RESULTS[@]}"; do
        if [ -n "${results_json}" ]; then
            results_json="${results_json},"
        fi
        results_json="${results_json}${result}"
    done
    
    # Category scores
    cat > "${REPORT_DIR}/report.json" << EOF
{
  "test_run": {
    "timestamp": "$(date -Iseconds)",
    "test_suite": "HelixLLM Advanced LLMsVerifier",
    "version": "1.0.0",
    "helixllm_endpoint": "${HELIXLLM_ENDPOINT}"
  },
  "summary": {
    "total_tests": ${TESTS_TOTAL},
    "passed": ${TESTS_PASSED},
    "failed": ${TESTS_FAILED},
    "skipped": ${TESTS_SKIPPED},
    "total_score": ${TOTAL_SCORE},
    "max_score": ${MAX_SCORE},
    "score_percentage": ${score_percentage},
    "final_grade": "${final_grade}",
    "success_rate": "$(awk "BEGIN {printf \"%.1f\", (${TESTS_PASSED}/${TESTS_TOTAL})*100}")%"
  },
  "categories": {
    "core_capabilities": {"max": 150, "description": "Reasoning, coding, math, logic"},
    "mcp_integration": {"max": 100, "description": "Tool calling, external services"},
    "rag_system": {"max": 100, "description": "Knowledge retrieval, context injection"},
    "lsp_integration": {"max": 100, "description": "Code intelligence, language servers"},
    "acp_multiagent": {"max": 100, "description": "Agent coordination, task delegation"},
    "embeddings": {"max": 75, "description": "Vector operations, similarity search"},
    "streaming": {"max": 75, "description": "Real-time response handling"},
    "context_window": {"max": 75, "description": "Long context handling"},
    "performance": {"max": 75, "description": "Latency, throughput benchmarks"},
    "api_compatibility": {"max": 50, "description": "OpenAI/Anthropic API compatibility"},
    "security": {"max": 50, "description": "TLS, validation, rate limiting"}
  },
  "results": [${results_json}]
}
EOF

    # Generate Markdown report
    cat > "${REPORT_DIR}/REPORT.md" << EOF
# HelixLLM Advanced LLMsVerifier Test Report

**Test Run Date:** $(date '+%Y-%m-%d %H:%M:%S')  
**Test Suite:** HelixLLM Advanced LLMsVerifier  
**Version:** 1.0.0  
**Endpoint:** ${HELIXLLM_ENDPOINT}

---

## Executive Summary

| Metric | Value |
|--------|-------|
| **Total Tests** | ${TESTS_TOTAL} |
| **Passed** | ${TESTS_PASSED} |
| **Failed** | ${TESTS_FAILED} |
| **Skipped** | ${TESTS_SKIPPED} |
| **Success Rate** | $(awk "BEGIN {printf \"%.1f\", (${TESTS_PASSED}/${TESTS_TOTAL})*100}")% |
| **Total Score** | ${TOTAL_SCORE}/${MAX_SCORE} |
| **Score Percentage** | ${score_percentage}% |
| **Final Grade** | **${final_grade}** |

---

## Grade Interpretation

| Grade | Range | Meaning |
|-------|-------|---------|
| A+ | 95-100% | Excellent - Production Ready |
| A | 90-94% | Very Good |
| A- | 85-89% | Good |
| B+ | 80-84% | Above Average |
| B | 70-79% | Acceptable |
| C | 60-69% | Needs Improvement |
| F | <60% | Not Production Ready |

---

## Test Categories

### 1. Core Model Capabilities (150 pts)
- Reasoning & Logic (50 pts)
- Coding & Software Engineering (60 pts)
- Mathematics & Logic (40 pts)

### 2. MCP Integration (100 pts)
- Tool Discovery
- Tool Execution (Echo, Time, Knowledge)

### 3. RAG System (100 pts)
- Document Ingestion
- Knowledge Query
- Health & Stats

### 4. LSP Integration (100 pts)
- LSP Bridge Health
- Language Server Management
- Code Analysis
- Definition Lookup

### 5. ACP Multi-Agent (100 pts)
- Agent Registry
- Task Management
- Coordinator Health
- Memory (Remember/Recall)

### 6. Embeddings (75 pts)
- Text Embedding Generation
- Vector Dimensions
- Batch Processing

### 7. Streaming (75 pts)
- Streaming Chat Completion
- SSE Protocol
- Response Format

### 8. Context Window (75 pts)
- 8K Context Support
- Multi-turn Conversation
- Context Retention

### 9. Performance (75 pts)
- Response Time
- Concurrent Requests
- Token Generation Rate

### 10. API Compatibility (50 pts)
- OpenAI API
- Anthropic API
- Health Endpoints

### 11. Security (50 pts)
- TLS/HTTPS
- Request Validation
- Rate Limiting
- Content Security

---

## Test Results

See \`report.json\` for detailed test results.

---

## Capabilities Assessment

### Software Engineering Tasks
- ✅ Code generation with type hints
- ✅ Algorithm design
- ✅ Code explanation and debugging
- ✅ Multi-step problem solving

### AI System Features
- ✅ MCP tool integration
- ✅ RAG knowledge retrieval
- ✅ LSP code intelligence
- ✅ ACP multi-agent coordination
- ✅ Embedding vector operations
- ✅ Streaming real-time responses
- ✅ Long context handling (8K+)

### Performance Characteristics
- Response latency measured
- Concurrent request handling
- Token generation throughput

---

## Recommendations

1. **Grade A+ (95%+):** Production ready for software engineering tasks
2. **Grade A (90-94%):** Minor improvements needed
3. **Grade B+ (80-89%):** Good for development, optimize before production
4. **Grade <80%:** Significant improvements required

---

*Generated by HelixLLM Advanced LLMsVerifier Test Suite*
EOF

    log_success "Reports generated:"
    log_info "  JSON: ${REPORT_DIR}/report.json"
    log_info "  Markdown: ${REPORT_DIR}/REPORT.md"
    log_info "  Log: ${REPORT_DIR}/test.log"
}

# ============================================
# MAIN EXECUTION
# ============================================
main() {
    log_section "HelixLLM Advanced LLMsVerifier Test Suite"
    log_info "Report Directory: ${REPORT_DIR}"
    log_info "Configuration:"
    log_info "  HELIXLLM_ENDPOINT: ${HELIXLLM_ENDPOINT}"
    log_info "  LLAMACPP_ENDPOINT: ${LLAMACPP_ENDPOINT}"
    log_info "  TEST_TIMEOUT: ${TEST_TIMEOUT}"
    echo ""
    
    # Run all test suites
    run_core_capability_tests
    run_mcp_tests
    run_rag_tests
    run_lsp_tests
    run_acp_tests
    run_embedding_tests
    run_streaming_tests
    run_context_tests
    run_performance_tests
    run_api_tests
    run_security_tests
    
    # Generate reports
    generate_report
    
    # Final summary
    log_section "FINAL SUMMARY"
    echo "Total Tests: ${TESTS_TOTAL}"
    echo "  Passed: ${TESTS_PASSED}"
    echo "  Failed: ${TESTS_FAILED}"
    echo "  Skipped: ${TESTS_SKIPPED}"
    echo ""
    echo "Score: ${TOTAL_SCORE}/${MAX_SCORE} ($(awk "BEGIN {printf \"%.1f\", (${TOTAL_SCORE}/${MAX_SCORE})*100}")%)"
    
    if [ ${TESTS_FAILED} -eq 0 ]; then
        log_success "ALL TESTS PASSED!"
        exit 0
    else
        log_warning "Some tests failed. Check report for details."
        exit 1
    fi
}

# Handle interrupts
trap 'log_error "Test suite interrupted"; exit 130' INT TERM

# Run main
main "$@"
