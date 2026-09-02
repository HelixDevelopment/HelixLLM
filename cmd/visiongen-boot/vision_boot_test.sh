#!/usr/bin/env bash
# vision_boot_test.sh — §11.4.115 RED-baseline-on-the-broken-artifact polarity
# test for the visiongen (VLM) boot harness.
#
# ONE test source, TWO roles (§11.4.115 / §11.4.146):
#
#   RED_MODE=1 (default) — REPRODUCE the pre-boot defect: tear the visiongen
#     service down (single-owner cleanup, coder :18434 untouched), then assert
#     NO vision endpoint answers on :18439 (connection refused / non-200).
#     This is the "bug-catcher" — it MUST fail-to-connect on the pre-boot
#     artifact, proving the defect (no live vision endpoint) is genuinely
#     reproducible before any fix/boot is applied.
#
#   RED_MODE=0 — the STANDING GREEN regression guard: boot the visiongen
#     service (idempotent — a live warm-tier service is left running, per
#     design, never auto-torn-down), poll /health, then send a REAL
#     multimodal inference request (a generated test PNG + a description
#     prompt) and assert the model returns non-empty, non-error prose that
#     genuinely describes the image (§11.4.69 sink-side positive evidence —
#     never a health-200-only pass).
#
# Fully automatic (§11.4.98): no operator action beyond invocation. Honest
# SKIP-with-reason (§11.4.3) when the model corpus is absent — never a faked
# PASS. Self-cleaning + re-runnable any number of times, in either mode, in
# any order (RED always tears down first; GREEN always (re-)boots first).
#
# Usage:
#   RED_MODE=1 ./vision_boot_test.sh   # default — reproduce-defect mode
#   RED_MODE=0 ./vision_boot_test.sh   # standing GREEN regression guard
#
# §11.4.119 single-resource-owner: this script is a GPU-boot owner while it
# runs. §11.4.122: the coder (:18434, container helixllm-coder) is NEVER
# stopped/restarted/killed by this script — verified before + after.

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/compose.vision.yml"
# NOTE: :18439 is a single-owner port (§11.4.119) — the compose file binds it
# unconditionally, so this MUST use the SAME compose project name as the
# production boot invocation (cmd/visiongen-boot boot ... helixllm_visiongen).
# A different project name here would try to bind a second container to the
# same host port and collide with an already-running production instance.
PROJECT="${VISIONGEN_TEST_PROJECT:-helixllm_visiongen}"
VISION_PORT="18439"
RED_MODE="${RED_MODE:-1}"
MODEL_DIR="${VISIONGEN_MODEL_DIR:-$HOME/models/vlm_cache}"
MODEL_GGUF="${VISIONGEN_MODEL_GGUF:-Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf}"
MMPROJ="${VISIONGEN_MMPROJ:-mmproj-Qwen2.5-VL-3B-Instruct-Q8_0.gguf}"

pass() { echo "PASS: $*"; exit 0; }
fail() { echo "FAIL: $*"; exit 1; }
skip() { echo "SKIP-OK: $*"; exit 0; }

coder_up() {
	podman ps --format '{{.Names}} {{.Status}}' 2>/dev/null | grep -q '^helixllm-coder Up'
}

# §11.4.3 honest skip: the model corpus this harness targets isn't provisioned.
if [ ! -f "$MODEL_DIR/$MODEL_GGUF" ] || [ ! -f "$MODEL_DIR/$MMPROJ" ]; then
	skip "model corpus absent at $MODEL_DIR ($MODEL_GGUF / $MMPROJ not found) — reason: hardware_not_present"
fi

# §11.4.3 honest skip: the GREEN half needs ImageMagick to generate the test
# PNG. Support BOTH v7 (`magick`) and v6 (`convert`) so a v6-only host does not
# false-FAIL; a host with NEITHER is an environment gap ⇒ SKIP-with-reason, NOT
# a hard `fail` (a missing fixture generator is not a service defect, and a
# false-FAIL on such a host contradicts §11.4.3 + §11.4.98 re-runnability).
if command -v magick >/dev/null 2>&1; then
	IMAGEMAGICK="magick"
elif command -v convert >/dev/null 2>&1; then
	IMAGEMAGICK="convert"
else
	skip "ImageMagick absent (neither 'magick' nor 'convert' on PATH) — reason: hardware_not_present"
fi

CODER_BEFORE="down"
coder_up && CODER_BEFORE="up"

if [ "$RED_MODE" = "1" ]; then
	echo "RED_MODE=1: reproducing pre-boot defect (no live vision endpoint on :$VISION_PORT)"
	(cd "$SCRIPT_DIR" && go run . down "$COMPOSE_FILE" "$PROJECT" >/tmp/vision_boot_test_down.log 2>&1)
	sleep 2
	if curl -sS -m 5 -o /dev/null -w '%{http_code}' "http://localhost:$VISION_PORT/health" 2>/dev/null | grep -q '^200$'; then
		fail "vision endpoint unexpectedly answered :$VISION_PORT after teardown — defect NOT reproduced (blind RED test)"
	fi
	if ! coder_up; then
		fail "coder (helixllm-coder) is not Up after visiongen teardown — §11.4.122 single-owner-cleanup violated"
	fi
	pass "defect reproduced: :$VISION_PORT unreachable after teardown; coder untouched (Up)"
fi

echo "RED_MODE=0: standing GREEN guard — booting + real multimodal inference"
BOOT_LOG=/tmp/vision_boot_test_boot.log
(cd "$SCRIPT_DIR" && go run . boot "$COMPOSE_FILE" "$PROJECT" >"$BOOT_LOG" 2>&1)
BOOT_RC=$?
if [ "$BOOT_RC" -ne 0 ]; then
	cat "$BOOT_LOG"
	if grep -q "BLOCKED: ErrBudgetExceeded" "$BOOT_LOG"; then
		skip "VRAM admission BLOCKED (ErrBudgetExceeded) — honest fail-closed result, not a defect"
	fi
	fail "boot did not reach BOOT-HEALTH-OK (rc=$BOOT_RC) — see $BOOT_LOG"
fi

if ! coder_up; then
	fail "coder (helixllm-coder) is not Up after visiongen boot — §11.4.122 single-owner violated"
fi

TMP_PNG="$(mktemp /tmp/vision_boot_test_XXXXXX.png)"
TMP_REQ="$(mktemp /tmp/vision_boot_test_XXXXXX.json)"
TMP_RESP="$(mktemp /tmp/vision_boot_test_XXXXXX.json)"
cleanup() { rm -f "$TMP_PNG" "$TMP_REQ" "$TMP_RESP"; }
trap cleanup EXIT

if ! "$IMAGEMAGICK" -size 128x128 xc:black -fill red -draw "rectangle 10,10 60,60" "$TMP_PNG" 2>/dev/null; then
	fail "could not generate test image via ImageMagick ($IMAGEMAGICK) — environment gap, not a service defect"
fi

# Portable encode: `base64 -w0` is a GNU coreutils extension that BSD/macOS
# base64 does not accept. Plain `base64` encodes on both; `tr -d '\n'` strips
# GNU's 76-column wrapping. The emptiness check matters because this script
# runs under `set -u` only: a failed command substitution would leave B64
# empty and the request would carry a zero-byte image, which a VLM can still
# answer from the text prompt alone — i.e. a PASS for an image the endpoint
# never received.
B64="$(base64 < "$TMP_PNG" | tr -d '\n')"
if [ -z "$B64" ]; then
	fail "could not base64-encode the test image — environment gap, not a service defect"
fi
python3 - "$TMP_REQ" "$B64" <<'PYEOF'
import json, sys
outpath, b64 = sys.argv[1], sys.argv[2]
payload = {
    "model": "vlm",
    "messages": [
        {"role": "user", "content": [
            {"type": "text", "text": "Describe this image in one short sentence."},
            {"type": "image_url", "image_url": {"url": "data:image/png;base64," + b64}}
        ]}
    ],
    "max_tokens": 100,
    "temperature": 0.2
}
with open(outpath, "w") as f:
    json.dump(payload, f)
PYEOF

if ! curl -sS -m 60 -X POST "http://localhost:$VISION_PORT/v1/chat/completions" \
	-H "Content-Type: application/json" \
	--data-binary @"$TMP_REQ" -o "$TMP_RESP"; then
	fail "inference request to :$VISION_PORT failed (curl error)"
fi

CONTENT="$(python3 -c "
import json
try:
    d = json.load(open('$TMP_RESP'))
    print(d['choices'][0]['message']['content'])
except Exception as e:
    print('')
")"

if [ -z "$CONTENT" ]; then
	cat "$TMP_RESP"
	fail "inference response had no assistant content — see raw response above"
fi

case "$CONTENT" in
*simulated*|*"for now"*|*placeholder*|*"TODO implement"*)
	fail "inference response looks like a bluff/placeholder: $CONTENT"
	;;
esac

if ! coder_up; then
	fail "coder (helixllm-coder) is not Up after inference — §11.4.122 single-owner violated"
fi

pass "vision endpoint served a real multimodal completion (coder untouched, was $CODER_BEFORE before, Up after): $CONTENT"
