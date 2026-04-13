#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PASS=0; FAIL=0; TOTAL=0
pass() { PASS=$((PASS+1)); TOTAL=$((TOTAL+1)); echo "  PASS: $1"; }
fail() { FAIL=$((FAIL+1)); TOTAL=$((TOTAL+1)); echo "  FAIL: $1"; }
check() { if eval "$2" >/dev/null 2>&1; then pass "$1"; else fail "$1"; fi; }

echo "=== HelixLLM Memory Sync Challenge ==="
check "memory_adapter.go" "test -f '$PROJECT_ROOT/internal/fallback/memory_adapter.go'"
check "memory_adapter_test.go" "test -f '$PROJECT_ROOT/internal/fallback/memory_adapter_test.go'"
check "MEMORY_SYNC_ENABLED config" "grep -q 'HELIX_LLM_MEMORY_SYNC_ENABLED' '$PROJECT_ROOT/internal/shared/config/config.go'"
check "MEMORY_URL config" "grep -q 'HELIX_LLM_MEMORY_URL' '$PROJECT_ROOT/internal/shared/config/config.go'"
check "Buffer cap 1000" "grep -q 'maxPendingMemories.*=.*1000' '$PROJECT_ROOT/internal/fallback/memory_adapter.go'"

cd "$PROJECT_ROOT"
if GOMAXPROCS=2 nice -n 19 go test -v -run TestMemoryAdapter ./internal/fallback/ -count=1 -timeout 60s; then
    pass "Memory adapter tests"
else
    fail "Memory adapter tests"
fi

echo ""
echo "=== Results: $PASS/$TOTAL passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
