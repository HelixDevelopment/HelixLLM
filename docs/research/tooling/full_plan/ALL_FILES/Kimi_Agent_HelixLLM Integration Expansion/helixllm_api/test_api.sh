#!/bin/bash
# HelixLLM API Test Script
# ========================
# Comprehensive test suite for HelixLLM OpenAI-compatible API

set -e

# Configuration
API_BASE_URL="${HELIXLLM_BASE_URL:-http://localhost:8000}"
API_KEY="${HELIXLLM_API_KEY:-}"
MODEL="${HELIXLLM_MODEL:-helix-llm}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Counter for tests
TESTS_PASSED=0
TESTS_FAILED=0

# Helper functions
print_header() {
    echo -e "\n${BLUE}========================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}========================================${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
    ((TESTS_PASSED++))
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
    ((TESTS_FAILED++))
}

print_info() {
    echo -e "${YELLOW}ℹ $1${NC}"
}

make_request() {
    local method=$1
    local endpoint=$2
    local data=$3
    local headers=""
    
    if [ -n "$API_KEY" ]; then
        headers="-H \"Authorization: Bearer $API_KEY\""
    fi
    
    if [ -n "$data" ]; then
        curl -s -X "$method" "$API_BASE_URL$endpoint" \
            -H "Content-Type: application/json" \
            $headers \
            -d "$data"
    else
        curl -s -X "$method" "$API_BASE_URL$endpoint" \
            $headers
    fi
}

# =============================================================================
# TEST SUITE
# =============================================================================

echo -e "${BLUE}"
echo "╔════════════════════════════════════════════════════════════╗"
echo "║         HelixLLM API Test Suite                            ║"
echo "╚════════════════════════════════════════════════════════════╝"
echo -e "${NC}"
print_info "API Base URL: $API_BASE_URL"
print_info "Model: $MODEL"
print_info "Auth: $([ -n "$API_KEY" ] && echo "Enabled" || echo "Disabled")"

# -----------------------------------------------------------------------------
# Test 1: Health Check
# -----------------------------------------------------------------------------
print_header "Test 1: Health Check"
response=$(make_request "GET" "/health")
if echo "$response" | grep -q "healthy"; then
    print_success "Health check passed"
    echo "  Response: $response"
else
    print_error "Health check failed"
    echo "  Response: $response"
fi

# -----------------------------------------------------------------------------
# Test 2: Root Endpoint
# -----------------------------------------------------------------------------
print_header "Test 2: Root Endpoint"
response=$(make_request "GET" "/")
if echo "$response" | grep -q "HelixLLM"; then
    print_success "Root endpoint accessible"
    echo "  Response: $response"
else
    print_error "Root endpoint failed"
    echo "  Response: $response"
fi

# -----------------------------------------------------------------------------
# Test 3: List Models
# -----------------------------------------------------------------------------
print_header "Test 3: List Models"
response=$(make_request "GET" "/v1/models")
if echo "$response" | grep -q "\"object\":\"list\""; then
    print_success "Model listing works"
    echo "  Models: $(echo "$response" | grep -o '"id":"[^"]*"' | head -1)"
else
    print_error "Model listing failed"
    echo "  Response: $response"
fi

# -----------------------------------------------------------------------------
# Test 4: Get Specific Model
# -----------------------------------------------------------------------------
print_header "Test 4: Get Specific Model"
response=$(make_request "GET" "/v1/models/$MODEL")
if echo "$response" | grep -q "\"id\":\"$MODEL\""; then
    print_success "Model info retrieval works"
    echo "  Model: $(echo "$response" | grep -o '"id":"[^"]*"')"
else
    print_error "Model info retrieval failed"
    echo "  Response: $response"
fi

# -----------------------------------------------------------------------------
# Test 5: Simple Chat Completion
# -----------------------------------------------------------------------------
print_header "Test 5: Simple Chat Completion"
response=$(make_request "POST" "/v1/chat/completions" '{
    "model": "'$MODEL'",
    "messages": [{"role": "user", "content": "Hello!"}]
}')
if echo "$response" | grep -q '"object":"chat.completion"'; then
    print_success "Chat completion works"
    content=$(echo "$response" | grep -o '"content":"[^"]*"' | head -1)
    echo "  Response: $content"
else
    print_error "Chat completion failed"
    echo "  Response: $response"
fi

# -----------------------------------------------------------------------------
# Test 6: Chat Completion with System Message
# -----------------------------------------------------------------------------
print_header "Test 6: Chat Completion with System Message"
response=$(make_request "POST" "/v1/chat/completions" '{
    "model": "'$MODEL'",
    "messages": [
        {"role": "system", "content": "You are a helpful assistant."},
        {"role": "user", "content": "What is your name?"}
    ]
}')
if echo "$response" | grep -q '"object":"chat.completion"'; then
    print_success "Chat with system message works"
else
    print_error "Chat with system message failed"
    echo "  Response: $response"
fi

# -----------------------------------------------------------------------------
# Test 7: Chat Completion with Parameters
# -----------------------------------------------------------------------------
print_header "Test 7: Chat Completion with Parameters"
response=$(make_request "POST" "/v1/chat/completions" '{
    "model": "'$MODEL'",
    "messages": [{"role": "user", "content": "Say hello"}],
    "temperature": 0.5,
    "max_tokens": 100,
    "top_p": 0.9
}')
if echo "$response" | grep -q '"object":"chat.completion"'; then
    print_success "Chat with parameters works"
else
    print_error "Chat with parameters failed"
    echo "  Response: $response"
fi

# -----------------------------------------------------------------------------
# Test 8: Streaming Chat Completion
# -----------------------------------------------------------------------------
print_header "Test 8: Streaming Chat Completion"
response=$(curl -s -X POST "$API_BASE_URL/v1/chat/completions" \
    -H "Content-Type: application/json" \
    -d '{
        "model": "'$MODEL'",
        "messages": [{"role": "user", "content": "Hi"}],
        "stream": true
    }' | head -5)
if echo "$response" | grep -q "data:"; then
    print_success "Streaming works"
    echo "  Stream chunks received"
else
    print_error "Streaming failed"
    echo "  Response: $response"
fi

# -----------------------------------------------------------------------------
# Test 9: Chat Completion with Tools
# -----------------------------------------------------------------------------
print_header "Test 9: Chat Completion with Tools"
response=$(make_request "POST" "/v1/chat/completions" '{
    "model": "'$MODEL'",
    "messages": [{"role": "user", "content": "What is the weather in New York?"}],
    "tools": [{
        "type": "function",
        "function": {
            "name": "get_weather",
            "description": "Get weather information for a location",
            "parameters": {
                "type": "object",
                "properties": {
                    "location": {"type": "string", "description": "City name"}
                },
                "required": ["location"]
            }
        }
    }]
}')
if echo "$response" | grep -q '"object":"chat.completion"'; then
    print_success "Tool calling works"
    if echo "$response" | grep -q "tool_calls"; then
        echo "  Tool call detected!"
    else
        echo "  No tool call (may be expected based on query)"
    fi
else
    print_error "Tool calling failed"
    echo "  Response: $response"
fi

# -----------------------------------------------------------------------------
# Test 10: Legacy Completions
# -----------------------------------------------------------------------------
print_header "Test 10: Legacy Completions"
response=$(make_request "POST" "/v1/completions" '{
    "model": "'$MODEL'",
    "prompt": "Once upon a time",
    "max_tokens": 50
}')
if echo "$response" | grep -q '"object":"text_completion"'; then
    print_success "Legacy completions work"
else
    print_error "Legacy completions failed"
    echo "  Response: $response"
fi

# -----------------------------------------------------------------------------
# Test 11: Embeddings
# -----------------------------------------------------------------------------
print_header "Test 11: Embeddings"
response=$(make_request "POST" "/v1/embeddings" '{
    "model": "'$MODEL'",
    "input": "Hello world"
}')
if echo "$response" | grep -q '"object":"list"'; then
    print_success "Embeddings work"
    embedding_count=$(echo "$response" | grep -o '"embedding":\[' | wc -l)
    echo "  Embeddings generated: $embedding_count"
else
    print_error "Embeddings failed"
    echo "  Response: $response"
fi

# -----------------------------------------------------------------------------
# Test 12: Batch Embeddings
# -----------------------------------------------------------------------------
print_header "Test 12: Batch Embeddings"
response=$(make_request "POST" "/v1/embeddings" '{
    "model": "'$MODEL'",
    "input": ["Hello world", "Goodbye world"]
}')
if echo "$response" | grep -q '"object":"list"'; then
    print_success "Batch embeddings work"
    data_count=$(echo "$response" | grep -o '"index":[0-9]*' | wc -l)
    echo "  Embeddings in batch: $data_count"
else
    print_error "Batch embeddings failed"
    echo "  Response: $response"
fi

# -----------------------------------------------------------------------------
# Test 13: Error Handling - Invalid Model
# -----------------------------------------------------------------------------
print_header "Test 13: Error Handling - Invalid Model"
response=$(make_request "POST" "/v1/chat/completions" '{
    "model": "nonexistent-model",
    "messages": [{"role": "user", "content": "Hello"}]
}')
# Should still work (we allow any model for compatibility)
if echo "$response" | grep -q '"object":"chat.completion"'; then
    print_success "Invalid model handled gracefully"
else
    print_error "Invalid model handling failed"
    echo "  Response: $response"
fi

# -----------------------------------------------------------------------------
# Test 14: Error Handling - Missing Required Field
# -----------------------------------------------------------------------------
print_header "Test 14: Error Handling - Missing Required Field"
response=$(curl -s -X POST "$API_BASE_URL/v1/chat/completions" \
    -H "Content-Type: application/json" \
    -d '{"model": "'$MODEL'"}')
if echo "$response" | grep -q "error"; then
    print_success "Missing field error handled correctly"
else
    print_error "Missing field error not handled"
    echo "  Response: $response"
fi

# -----------------------------------------------------------------------------
# Test 15: Multi-turn Conversation
# -----------------------------------------------------------------------------
print_header "Test 15: Multi-turn Conversation"
response=$(make_request "POST" "/v1/chat/completions" '{
    "model": "'$MODEL'",
    "messages": [
        {"role": "user", "content": "My name is Alice"},
        {"role": "assistant", "content": "Hello Alice! Nice to meet you."},
        {"role": "user", "content": "What is my name?"}
    ]
}')
if echo "$response" | grep -q '"object":"chat.completion"'; then
    print_success "Multi-turn conversation works"
else
    print_error "Multi-turn conversation failed"
    echo "  Response: $response"
fi

# =============================================================================
# SUMMARY
# =============================================================================

echo -e "\n${BLUE}========================================${NC}"
echo -e "${BLUE}Test Summary${NC}"
echo -e "${BLUE}========================================${NC}"
echo -e "${GREEN}Tests Passed: $TESTS_PASSED${NC}"
echo -e "${RED}Tests Failed: $TESTS_FAILED${NC}"

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "\n${GREEN}All tests passed! ✓${NC}"
    exit 0
else
    echo -e "\n${RED}Some tests failed. Please check the output above.${NC}"
    exit 1
fi
