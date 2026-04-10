#!/usr/bin/env bash
# HelixLLM Multi-Model Fleet Challenge
set -euo pipefail

PASS=0
FAIL=0
TOTAL=0

pass() { PASS=$((PASS+1)); TOTAL=$((TOTAL+1)); echo "  PASS: $1"; }
fail() { FAIL=$((FAIL+1)); TOTAL=$((TOTAL+1)); echo "  FAIL: $1"; }
check() { if eval "$2" >/dev/null 2>&1; then pass "$1"; else fail "$1"; fi; }

echo "=== HelixLLM Multi-Model Fleet Challenge ==="
echo ""

echo "--- Hardware Profiler ---"
check "profiler.go exists" "[ -f internal/shared/hardware/profiler.go ]"
check "profiler_test.go exists" "[ -f internal/shared/hardware/profiler_test.go ]"
check "profiler tests pass" "go test -short ./internal/shared/hardware/ -count=1"

echo "--- Model Catalog ---"
check "catalog.go exists" "[ -f internal/brain/models/catalog.go ]"
check "catalog has 4 models" "go test -run TestDefaultCatalog_HasFourModels ./internal/brain/models/ -count=1"
check "catalog has fast tier" "go test -run TestDefaultCatalog_HasFastTier ./internal/brain/models/ -count=1"
check "catalog filters by VRAM" "go test -run TestCatalog_FilterByVRAM ./internal/brain/models/ -count=1"

echo "--- Model Registry ---"
check "registry.go exists" "[ -f internal/brain/models/registry.go ]"
check "registry fallback works" "go test -run TestRegistry_BestAvailable_FallbackToLower ./internal/brain/models/ -count=1"
check "registry tracks status" "go test -run TestRegistry_UpdateStatus ./internal/brain/models/ -count=1"

echo "--- Complexity Analyzer ---"
check "complexity.go exists" "[ -f internal/brain/complexity.go ]"
check "simple → fast" "go test -run TestComplexityAnalyzer_SimpleToolCall ./internal/brain/ -count=1"
check "complex → powerful" "go test -run TestComplexityAnalyzer_ComplexMultiTool ./internal/brain/ -count=1"

echo "--- Preset Generator ---"
check "preset.go exists" "[ -f internal/brain/models/preset.go ]"
check "generates valid INI" "go test -run TestGeneratePresets_Consumer6GB ./internal/brain/models/ -count=1"

echo "--- Model Downloader ---"
check "downloader.go exists" "[ -f internal/brain/downloader.go ]"
check "download works" "go test -run TestDownloader ./internal/brain/ -count=1"

echo "--- Server Manager ---"
check "server.go exists" "[ -f internal/brain/server.go ]"
check "health check works" "go test -run TestLlamaServer_HealthCheck ./internal/brain/ -count=1"
check "builds args" "go test -run TestLlamaServerConfig_BuildArgs ./internal/brain/ -count=1"

echo "--- Local Embedder ---"
check "llama_embedder.go exists" "[ -f internal/knowledge/llama_embedder.go ]"
check "embedding works" "go test -run TestLlamaEmbedder_Embed_Success ./internal/knowledge/ -count=1"

echo "--- Container ---"
check "router Containerfile exists" "[ -f container/Containerfile.llamacpp-router ]"
check "has CUDA flag" "grep -q 'GGML_CUDA=ON' container/Containerfile.llamacpp-router"
check "has router mode" "grep -q 'models-dir' container/Containerfile.llamacpp-router"

echo "--- Configuration ---"
check "HELIX_MODELS_DIR configured" "grep -q 'HELIX_MODELS_DIR' internal/shared/config/config.go"
check "HELIX_COMPLEXITY_ENABLED configured" "grep -q 'HELIX_COMPLEXITY_ENABLED' internal/shared/config/config.go"

echo "--- Compilation ---"
check "project compiles" "go build ./cmd/helixllm/"
check "all unit tests pass" "go test -short ./internal/... -count=1"

echo "--- Integration ---"
check "integration tests pass" "go test ./tests/integration/ -run 'TestComplexity_To_Registry|TestPresetGeneration|TestCatalog_VRAMFiltering' -count=1"

echo ""
echo "=== Results: $PASS/$TOTAL passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ] && echo "ALL CHECKS PASSED" || echo "SOME CHECKS FAILED"
exit "$FAIL"
