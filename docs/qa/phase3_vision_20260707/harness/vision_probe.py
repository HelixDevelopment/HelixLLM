#!/usr/bin/env python3
"""
Phase-3 VISION harness — grounded VLM probe + golden-BAD self-validation.

Feeds a REAL image (known content: RED CIRCLE on WHITE, text 'HELIX') to a
llama.cpp VLM over the OpenAI vision API (`/v1/chat/completions`,
image_url = base64 data URI), then:

  §11.4.108 runtime signature — GROUNDED assertion: the model's real
    description MUST CONTAIN the known content (color 'red' AND a
    circle-word). This is grounded in the actual pixels, not "returned
    something" — a different image would produce a different description.

  §11.4.107(10) golden-BAD — the SAME checker run with a WRONG-content
    assertion (color 'blue' AND shape 'square', which are NOT in the image)
    MUST FAIL. If the golden-bad passed, the checker is a rubber stamp.

Exit 0 only when GROUNDED passes AND golden-BAD fails. Writes request,
response, and verdict evidence to the given evidence dir.

Usage: vision_probe.py <vlm_base_url> <image_path> <evidence_dir> <model_id>
"""
import base64
import json
import os
import sys
import time
import urllib.request

vlm_url, image_path, evdir, model_id = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
os.makedirs(evdir, exist_ok=True)

with open(image_path, "rb") as f:
    b64 = base64.b64encode(f.read()).decode("ascii")
data_uri = f"data:image/png;base64,{b64}"

PROMPT = "Describe this image in detail. What shape and color is the main object, and what text (if any) appears?"

req_body = {
    "model": model_id,
    "messages": [
        {
            "role": "user",
            "content": [
                {"type": "text", "text": PROMPT},
                {"type": "image_url", "image_url": {"url": data_uri}},
            ],
        }
    ],
    "temperature": 0.0,
    "max_tokens": 256,
}

# Redacted request record (do NOT dump the multi-MB base64 blob)
req_record = json.loads(json.dumps(req_body))
req_record["messages"][0]["content"][1]["image_url"]["url"] = (
    f"data:image/png;base64,<{len(b64)} base64 chars of {os.path.basename(image_path)}>"
)
with open(os.path.join(evdir, "06_vision_request.json"), "w") as f:
    json.dump({"endpoint": f"{vlm_url}/v1/chat/completions", "prompt": PROMPT, "body": req_record}, f, indent=2)

t0 = time.time()
r = urllib.request.Request(
    f"{vlm_url}/v1/chat/completions",
    data=json.dumps(req_body).encode(),
    headers={"Content-Type": "application/json"},
)
with urllib.request.urlopen(r, timeout=180) as resp:
    raw = resp.read().decode()
elapsed = time.time() - t0
out = json.loads(raw)
with open(os.path.join(evdir, "07_vision_response.json"), "w") as f:
    json.dump(out, f, indent=2)

description = out["choices"][0]["message"]["content"]
desc_l = description.lower()


def all_present(text, tokens):
    return all(any(v in text for v in variants) for variants in tokens)


# GROUNDED positive: color red AND a circle-word (both must be present).
GROUNDED = [["red"], ["circle", "round", "disc", "dot"]]
# GOLDEN-BAD wrong content: color blue AND shape square (NOT in the image).
GOLDEN_BAD = [["blue"], ["square", "rectangle", "triangle"]]

grounded_pass = all_present(desc_l, GROUNDED)
golden_bad_pass = all_present(desc_l, GOLDEN_BAD)  # MUST be False
ocr_helix = "helix" in desc_l  # extra grounding: OCR of the rendered text

verdict = {
    "model": model_id,
    "vlm_url": vlm_url,
    "image": os.path.basename(image_path),
    "known_content": "RED CIRCLE on WHITE background, text 'HELIX'",
    "latency_seconds": round(elapsed, 2),
    "description": description,
    "grounded_assertion": {
        "tokens_required_all_of": GROUNDED,
        "result": "PASS" if grounded_pass else "FAIL",
    },
    "golden_bad_assertion": {
        "tokens_required_all_of": GOLDEN_BAD,
        "must_be": "FAIL",
        "result": "PASS(BAD)" if golden_bad_pass else "FAIL(as-required)",
    },
    "ocr_helix_bonus": ocr_helix,
    "overall": "PASS" if (grounded_pass and not golden_bad_pass) else "FAIL",
}
with open(os.path.join(evdir, "08_grounded_assertion.txt"), "w") as f:
    f.write(json.dumps(verdict, indent=2) + "\n")

print(json.dumps(verdict, indent=2))
sys.exit(0 if verdict["overall"] == "PASS" else 1)
