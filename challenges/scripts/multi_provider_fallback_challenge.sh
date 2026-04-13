#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PASS=0; FAIL=0; TOTAL=0
pass() { PASS=$((PASS+1)); TOTAL=$((TOTAL+1)); echo "  PASS: $1"; }
fail() { FAIL=$((FAIL+1)); TOTAL=$((TOTAL+1)); echo "  FAIL: $1"; }
check() { if eval "$2" >/dev/null 2>&1; then pass "$1"; else fail "$1"; fi; }

echo "=== Multi-Provider Fallback Chain Challenge ==="

# Check provider files exist (7 providers + shared base)
for p in chutes openrouter huggingface nvidia cerebras sambanova together; do
    check "Provider: ${p}_provider.go" "test -f '$PROJECT_ROOT/internal/brain/${p}_provider.go'"
    check "Test: ${p}_provider_test.go" "test -f '$PROJECT_ROOT/internal/brain/${p}_provider_test.go'"
done
check "Shared base" "test -f '$PROJECT_ROOT/internal/brain/openai_compat_provider.go'"

# Check fallback package files
for f in chain.go chain_entry.go rate_limit.go circuit_breaker.go scorer_bridge.go memory_adapter.go; do
    check "Fallback: $f" "test -f '$PROJECT_ROOT/internal/fallback/$f'"
done

# Check config has new env vars
check "ChutesKey in config" "grep -q 'HELIX_LLM_CHUTES_KEY' '$PROJECT_ROOT/internal/shared/config/config.go'"
check "OpenRouterKey in config" "grep -q 'HELIX_LLM_OPENROUTER_KEY' '$PROJECT_ROOT/internal/shared/config/config.go'"

# Check gateway uses Completer
check "Gateway Completer" "grep -q 'Completer' '$PROJECT_ROOT/internal/gateway/openai.go'"

# Check main.go wiring
check "main imports fallback" "grep -q 'fallback' '$PROJECT_ROOT/cmd/helixllm/main.go'"
check "main creates Chain" "grep -q 'NewChain\|fallback.NewChain' '$PROJECT_ROOT/cmd/helixllm/main.go'"

# Run unit tests
cd "$PROJECT_ROOT"
if GOMAXPROCS=2 nice -n 19 go test ./internal/brain/ ./internal/fallback/ -count=1 -p 1 -short -timeout 120s; then
    pass "Unit tests"
else
    fail "Unit tests"
fi

# Build
if GOMAXPROCS=2 nice -n 19 go build ./...; then
    pass "Build"
else
    fail "Build"
fi

echo ""
echo "=== Results: $PASS/$TOTAL passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
