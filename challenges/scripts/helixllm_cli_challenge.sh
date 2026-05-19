#!/usr/bin/env bash
# helixllm_cli_challenge.sh — round-215 deliverable.
#
# Anti-bluff sweep of HelixLLM's CLI surface (`cmd/helixllm/`) with
# paired-mutation self-test per CONST-055 / constitution submodule §1.1.
#
# Six invariants verified, each backed by reproducible runtime evidence:
#
#   1. CLI source compiles (real `go build`, captured exit code).
#   2. Existing unit tests pass (real `go test`, captured exit code).
#   3. CONST-046 round-95 i18n keys exist (KeyHelixllmCLIFailedToLoadBanks
#      + KeyHelixllmCLIErrorLoadingConfig) in internal/shared/i18n/i18n.go.
#   4. CLI call sites in cmd/helixllm/ DO invoke i18n.Translator.T(...)
#      for the two migrated strings (anti-regression).
#   5. CLI source does NOT regress the bare-English literals that round
#      95 migrated away — anti-bluff scan refuses any reintroduction.
#   6. Paired-mutation self-test (§1.1): when the script plants a
#      bare-literal violation in a sandbox copy, invariant 5 MUST flip
#      to FAIL — proving the gate actually walks the source rather
#      than performing a no-op grep on phantom strings.
#
# Verbatim 2026-05-19 operator mandate (CONST-049 §11.4.17 anchor):
#   "all existing tests and Challenges do work in anti-bluff manner -
#    they MUST confirm that all tested codebase really works as
#    expected!  ... execution of tests and Challenges MUST guarantee
#    the quality, the completition and full usability by end users
#    of the product!"
#
# Exit codes:
#   0 — every invariant PASS, including the paired-mutation self-test.
#   1 — any invariant FAIL or paired-mutation false-PASS.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

PASS=0
FAIL=0
TOTAL=0

pass() {
    PASS=$((PASS + 1))
    TOTAL=$((TOTAL + 1))
    printf "  PASS: %s\n" "$1"
}

fail() {
    FAIL=$((FAIL + 1))
    TOTAL=$((TOTAL + 1))
    printf "  FAIL: %s\n" "$1"
}

check() {
    # check <label> <bash-expression>
    if eval "$2" >/dev/null 2>&1; then
        pass "$1"
    else
        fail "$1"
    fi
}

echo "=== HelixLLM CLI Challenge (round 215) ==="
echo "Project root: ${PROJECT_ROOT}"
echo ""

# ─── Invariant 1: CLI source compiles ────────────────────────────────
echo "[1/6] CLI source compiles"
cd "$PROJECT_ROOT"
BUILD_LOG="$(mktemp)"
trap 'rm -f "$BUILD_LOG"' EXIT
if GOMAXPROCS=2 nice -n 19 go build -o /dev/null ./cmd/helixllm >"$BUILD_LOG" 2>&1; then
    pass "go build ./cmd/helixllm"
else
    fail "go build ./cmd/helixllm (see ${BUILD_LOG})"
    sed -n '1,20p' "$BUILD_LOG"
fi

# ─── Invariant 2: existing unit tests pass ───────────────────────────
echo ""
echo "[2/6] Existing CLI unit tests pass (cmd/helixllm)"
TEST_LOG="$(mktemp)"
trap 'rm -f "$BUILD_LOG" "$TEST_LOG"' EXIT
if GOMAXPROCS=2 nice -n 19 go test -count=1 -timeout 120s ./cmd/helixllm >"$TEST_LOG" 2>&1; then
    pass "go test ./cmd/helixllm"
else
    fail "go test ./cmd/helixllm (see ${TEST_LOG})"
    sed -n '1,40p' "$TEST_LOG"
fi

# ─── Invariant 3: i18n keys exist ────────────────────────────────────
echo ""
echo "[3/6] CONST-046 round-95 i18n keys present"
I18N_FILE="$PROJECT_ROOT/internal/shared/i18n/i18n.go"
check "i18n.go exists" "test -f '$I18N_FILE'"
check "Key constant KeyHelixllmCLIFailedToLoadBanks defined" \
    "grep -q 'KeyHelixllmCLIFailedToLoadBanks' '$I18N_FILE'"
check "Key constant KeyHelixllmCLIErrorLoadingConfig defined" \
    "grep -q 'KeyHelixllmCLIErrorLoadingConfig' '$I18N_FILE'"
check "Default English template for KeyHelixllmCLIFailedToLoadBanks loaded" \
    "grep -q 'failed to load banks: {{detail}}' '$I18N_FILE'"
check "Default English template for KeyHelixllmCLIErrorLoadingConfig loaded" \
    "grep -q 'error loading config: {{detail}}' '$I18N_FILE'"

# ─── Invariant 4: CLI call sites invoke i18n.Translator ──────────────
echo ""
echo "[4/6] CLI call sites invoke i18n.Translator.T(...)"
CHALLENGES_GO="$PROJECT_ROOT/cmd/helixllm/challenges.go"
MAIN_GO="$PROJECT_ROOT/cmd/helixllm/main.go"
check "challenges.go imports i18n package" \
    "grep -q '\"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n\"' '$CHALLENGES_GO'"
check "challenges.go calls tr.T(...) with KeyHelixllmCLIFailedToLoadBanks" \
    "grep -q 'tr.T(lang, i18n.KeyHelixllmCLIFailedToLoadBanks' '$CHALLENGES_GO'"
check "main.go imports i18n package" \
    "grep -q '\"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n\"' '$MAIN_GO'"
check "main.go calls tr.T(...) with KeyHelixllmCLIErrorLoadingConfig" \
    "grep -q 'tr.T(lang, i18n.KeyHelixllmCLIErrorLoadingConfig' '$MAIN_GO'"
check "main.go invokes resolveCLILang() before constructing Translator" \
    "grep -q 'lang := resolveCLILang()' '$MAIN_GO'"

# ─── Invariant 5: no bare-English regression ─────────────────────────
# We must NOT see the raw English literals at the migrated call sites.
# A regression would mean an agent reintroduced a hardcoded string
# instead of routing through the translator — equivalent severity to
# a §11.4 PASS-bluff (CONST-046 violation).
echo ""
echo "[5/6] Anti-regression scan — no bare-English literals at CLI call sites"

scan_for_bare_literal() {
    local needle="$1"
    local file="$2"
    # The literal is allowed ONLY inside the i18n default-message map
    # (i18n.go) — that IS the source of truth. We grep cmd/ only.
    if grep -q "$needle" "$file"; then
        return 0   # found = bad
    fi
    return 1
}

if scan_for_bare_literal '"failed to load banks: %v\\n"' "$CHALLENGES_GO" \
   || scan_for_bare_literal '"failed to load banks:' "$CHALLENGES_GO"; then
    fail "bare-literal 'failed to load banks' regressed into challenges.go"
else
    pass "no bare-literal 'failed to load banks' regression in challenges.go"
fi

if scan_for_bare_literal '"error loading config: %v\\n"' "$MAIN_GO" \
   || scan_for_bare_literal '"error loading config:' "$MAIN_GO"; then
    fail "bare-literal 'error loading config' regressed into main.go"
else
    pass "no bare-literal 'error loading config' regression in main.go"
fi

# ─── Invariant 6: paired-mutation self-test (§1.1) ───────────────────
# Plant a known violation into a sandbox copy of challenges.go and
# re-run the invariant 5 scan logic against the sandbox. The gate
# MUST report FAIL for the planted violation — if it reports PASS,
# the gate is performing a no-op grep and is itself a CONST-035
# bluff at the gate-verification layer.
echo ""
echo "[6/6] Paired-mutation self-test (§1.1)"

SANDBOX_DIR="$(mktemp -d)"
trap 'rm -f "$BUILD_LOG" "$TEST_LOG"; rm -rf "$SANDBOX_DIR"' EXIT
cp "$CHALLENGES_GO" "$SANDBOX_DIR/challenges.go.original"

# Plant the violation: append a bare-English fprintf reintroducing
# the exact literal CONST-046 migrated away. The mutated file is
# byte-distinct from the original so any false-positive PASS is
# attributable to the gate logic, not to filesystem caching.
cat >"$SANDBOX_DIR/challenges_mutated.go" <<'MUTATED_EOF'
package main

import (
	"fmt"
	"os"
)

// MUTATION (paired-mutation per §1.1): this file deliberately contains
// a bare English literal that CONST-046 migrated away. The
// helixllm_cli_challenge.sh sweep MUST detect it. If the sweep reports
// PASS for this file, the sweep itself is broken.
func mutatedBlock(err error) {
	fmt.Fprintf(os.Stderr, "failed to load banks: %v\n", err)
}
MUTATED_EOF

if scan_for_bare_literal '"failed to load banks:' "$SANDBOX_DIR/challenges_mutated.go"; then
    pass "paired-mutation: gate correctly detects planted bare-literal violation"
else
    fail "paired-mutation: gate FAILED to detect planted bare-literal — gate is a no-op (CONST-055 violation)"
fi

# Symmetric self-check: original file is unmodified and still clean.
if scan_for_bare_literal '"failed to load banks:' "$SANDBOX_DIR/challenges.go.original"; then
    fail "paired-mutation: original file unexpectedly contains bare-literal (filesystem or repo state corrupted)"
else
    pass "paired-mutation: original file remains clean under sandbox copy"
fi

echo ""
echo "=== Results: ${PASS}/${TOTAL} passed, ${FAIL} failed ==="

if [ "$FAIL" -eq 0 ]; then
    echo "All invariants PASS, paired-mutation self-test confirms gate is live."
    exit 0
fi

echo "One or more invariants FAILED — review the per-line PASS/FAIL"
echo "annotations above. Anti-bluff posture (Article XI §11.9) treats"
echo "any FAIL row as release-blocking until evidence captured + fixed."
exit 1
