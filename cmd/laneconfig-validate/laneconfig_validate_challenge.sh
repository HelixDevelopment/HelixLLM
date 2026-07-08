#!/usr/bin/env bash
# laneconfig_validate_challenge.sh — §11.4.6 project-rule Challenge test for
# the pre-boot lane-config validator (Serving-plan Task 1.5, master plan §6.3,
# danger zones D1/D4/D7).
#
# Real process execution (go build + real binary invocation), captured
# stdout/exit-code — no mocks (CONST-050(A) — mocks are unit-test-only; this
# is a Challenge, not a unit test). PURE Go, NO GPU, NO container boot: every
# fixture supplies explicit budget_bytes/headroom_bytes so the run is fully
# deterministic (§11.4.50) regardless of the host's live VRAM state.
#
# Cases (§11.4.146 extend-to-all-cases — one fixture per D1/D4/D7 check):
#   1. over-budget config  -> non-zero exit + STATIC_FOOTPRINT_EXCEEDS_BUDGET
#   2. valid config        -> exit 0
#   3. ngl-below-residency -> non-zero exit + NGL_BELOW_FULL_RESIDENCY
#   4. port collision      -> non-zero exit + PORT_COLLISION
#
# Usage: ./laneconfig_validate_challenge.sh

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$(mktemp /tmp/laneconfig-validate.XXXXXX)"
rm -f "$BIN" # go build wants to create it

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*"; exit 1; }

cleanup() { rm -f "$BIN"; }
trap cleanup EXIT

echo "== building laneconfig-validate =="
if ! (cd "$SCRIPT_DIR" && go build -o "$BIN" .); then
	fail "go build failed"
fi
pass "go build ./cmd/laneconfig-validate succeeded"

echo
echo "== case 1: over-budget config MUST exit non-zero with STATIC_FOOTPRINT_EXCEEDS_BUDGET =="
OUT1="$("$BIN" "$SCRIPT_DIR/testdata/over_budget_lane_config.json" 2>&1)"
RC1=$?
echo "$OUT1"
if [ "$RC1" -eq 0 ]; then
	fail "over-budget config exited 0 (expected non-zero) — rc=$RC1"
fi
if ! echo "$OUT1" | grep -q "FAIL: STATIC_FOOTPRINT_EXCEEDS_BUDGET:"; then
	fail "over-budget config output did not contain the specific error string 'FAIL: STATIC_FOOTPRINT_EXCEEDS_BUDGET:'"
fi
pass "over-budget config -> rc=$RC1 (non-zero), specific error present"

echo
echo "== case 2: valid config MUST exit zero =="
OUT2="$("$BIN" "$SCRIPT_DIR/testdata/valid_lane_config.json" 2>&1)"
RC2=$?
echo "$OUT2"
if [ "$RC2" -ne 0 ]; then
	fail "valid config exited $RC2 (expected 0)"
fi
if ! echo "$OUT2" | grep -q "^OK: lane config admissible"; then
	fail "valid config output did not contain the expected OK line"
fi
pass "valid config -> rc=0"

echo
echo "== case 3: ngl-below-residency config MUST exit non-zero with NGL_BELOW_FULL_RESIDENCY =="
OUT3="$("$BIN" "$SCRIPT_DIR/testdata/ngl_below_residency_lane_config.json" 2>&1)"
RC3=$?
echo "$OUT3"
if [ "$RC3" -eq 0 ]; then
	fail "ngl-below-residency config exited 0 (expected non-zero)"
fi
if ! echo "$OUT3" | grep -q "FAIL: NGL_BELOW_FULL_RESIDENCY:"; then
	fail "ngl-below-residency output did not contain the specific error string"
fi
pass "ngl-below-residency config -> rc=$RC3 (non-zero), specific error present"

echo
echo "== case 4: port-collision config MUST exit non-zero with PORT_COLLISION =="
OUT4="$("$BIN" "$SCRIPT_DIR/testdata/port_collision_lane_config.json" 2>&1)"
RC4=$?
echo "$OUT4"
if [ "$RC4" -eq 0 ]; then
	fail "port-collision config exited 0 (expected non-zero)"
fi
if ! echo "$OUT4" | grep -q "FAIL: PORT_COLLISION:"; then
	fail "port-collision output did not contain the specific error string"
fi
pass "port-collision config -> rc=$RC4 (non-zero), specific error present"

echo
pass "ALL laneconfig-validate Challenge cases passed"
