# HelixLLM Phase-3 VISION (VLM) — end-to-end proof on RTX 5090

**Run ID:** `phase3_vision_20260707` · **Date:** 2026-07-07 · **Host:** RTX 5090 (32607 MiB) ·
**Branch:** `feature/helixllm-full-extension` · **Verdict:** ✅ **COMPLETE — grounded PASS + golden-BAD FAIL + VRAM-fit (no OOM)**

Anti-bluff anchors: §11.4.108 (runtime signature), §11.4.5 / §11.4.69 (captured evidence),
§11.4.107(10) (self-validated analyzer / golden-bad), §11.4.119 / §11.4.133 (single-owner, no
resource harm), §11.4.3 (honest SKIP path present but not needed).

---

## 1. What was proven

A **real** vision-language model (Qwen2.5-VL-3B-Instruct, GGUF + mmproj) was booted **on the GPU**,
**alongside** the live resident coder at `:18434` (never touched), fed a **real image with known
unambiguous content**, and its **real description was asserted to CONTAIN that content** — grounded
in the actual pixels, not "it returned something". A wrong-content (golden-BAD) assertion was proven
to **FAIL**, so the checker itself cannot bluff. The VLM was then **torn down** (single-owner cleanup)
and the coder confirmed **still Up and answering**.

| Item | Value |
|---|---|
| **VLM model** | `Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf` (3.09 B params, Q4_K-Medium, 1.79 GiB weights) |
| **Multimodal projector** | `mmproj-Qwen2.5-VL-3B-Instruct-Q8_0.gguf` (806 MiB) |
| **Source** | `ggml-org/Qwen2.5-VL-3B-Instruct-GGUF` — pulled over **HTTPS** via the router image's own curl+openssl stack |
| **Router image** | `localhost/helixllm/llamacpp-router:cuda12.8-sm120` (the already-built artifact) |
| **Boot** | `--device nvidia.com/gpu=all --security-opt=label=disable --network=host -ngl 99 --mmproj … --port 18500` |
| **Server capabilities** | `["completion","multimodal"]` (confirmed at `/v1/models`) |

---

## 2. §11.4.108 runtime signature — grounded description of KNOWN content

**Known content** (the ground truth the VLM had to describe), authored by
`harness/make_test_image.py`, confirmed by eye (`evidence/test_image.png`, md5 `b982a829…`):

> a large **solid RED CIRCLE** on a **WHITE** background, with the black text **"HELIX"** below it.

**Real VLM description** (temperature 0, 0.29 s, via OpenAI vision `/v1/chat/completions`,
`image_url` = base64 data URI — `evidence/07_vision_response.json`):

> *"The image features a simple and clean design. The main object is a large, solid **red circle**
> positioned centrally against a **white** background. Below the circle, the word **"HELIX"** is
> written in uppercase black letters. …"*

| Assertion | Tokens (all-of) | Result |
|---|---|---|
| **GROUNDED** (must PASS) | `red` AND (`circle`\|`round`\|`disc`\|`dot`) | **PASS** ✅ |
| **GOLDEN-BAD** (must FAIL) | `blue` AND (`square`\|`rectangle`\|`triangle`) | **FAIL(as-required)** ✅ |
| **OCR bonus** | rendered text `HELIX` read back | `true` ✅ |

The golden-BAD assertion running the **same checker** with wrong-content tokens **did not pass** →
the analyzer is proven honest (§11.4.107(10)); a rubber-stamp checker would have passed it.
The description is grounded: it names the correct **color**, **shape**, and even **OCRs the text** —
a different image would produce a different answer. Full verdict: `evidence/08_grounded_assertion.txt`.

---

## 3. VRAM fit — coder + VLM BOTH resident, no OOM (§11.4.119 / §11.4.133)

`evidence/05_nvidia_smi_both_fit.txt` — captured while BOTH servers were live:

```
32607, 23575, 8546            # total, used, FREE (MiB)
2394426, 19422 MiB, llama-server   # coder (Qwen3-Coder-30B) — resident, untouched
1902321,  4138 MiB, llama-server   # VLM   (Qwen2.5-VL-3B) — booted for this proof
```

- Free VRAM **before** boot: **12689 MiB** — safety gate required ≥ 8192 MiB (6 G est + 2 G headroom) → **passed**.
- With both models resident: **8546 MiB still free** — headroom **far exceeds** the ≥ 2 GiB mandate; **no OOM**.
- The VLM's real footprint (**4138 MiB**) came in well under the 6 GiB estimate.

---

## 4. Teardown + coder untouched (single-owner cleanup)

`evidence/10_teardown.txt` / `evidence/11_coder_untouched.txt` / `evidence/11_coder_chat_response.json`:

- VLM container `helixllm-vlm-phase3` **removed**; `podman ps` shows **only** `helixllm-coder`.
- VRAM returned to the **exact baseline 12689 MiB free** (VLM fully released).
- Coder `helixllm-coder` **Up 17 hours** (same uptime before/after — never restarted), health `{"status":"ok"}`.
- Real coder inference after teardown: `{"role":"assistant","content":"CODER_OK"}` — model
  `Qwen3-Coder-30B-A3B-Instruct-Q4_K_M`, 540 tok/s prompt, 211 tok/s gen. **The coder was never touched.**

---

## 5. Evidence index (`evidence/`)

| File | Content |
|---|---|
| `test_image.png` | The known-content image (red circle / white / "HELIX"), md5 `b982a829…` |
| `01_vram_before.txt` | Baseline VRAM + coder health + safety-gate arithmetic |
| `02_https_download.txt` | HTTPS pull of GGUF + mmproj (router-image curl progress) |
| `03_vlm_boot.log` | `podman run` boot of the VLM |
| `04_health.txt` | VLM `/health` (ok in 4 s) + `/v1/models` (`multimodal` capability) |
| `05_nvidia_smi_both_fit.txt` | nvidia-smi with coder + VLM both resident (no OOM) |
| `06_vision_request.json` | The vision request (base64 blob redacted) |
| `07_vision_response.json` | Raw VLM response |
| `08_grounded_assertion.txt` | Grounded + golden-BAD verdict (overall PASS) |
| `10_teardown.txt` | VLM removed, VRAM released |
| `11_coder_untouched.txt` / `11_coder_chat_response.json` | Coder still Up + real answer |

## 6. Reproduce

```bash
cd submodules/helix_llm/docs/qa/phase3_vision_20260707
python3 harness/make_test_image.py evidence/test_image.png   # regenerate known-content image
bash    harness/run_vision_proof.sh                            # download→boot→probe→golden-bad→teardown
```

The harness checks free VRAM **before** boot and, if a fitting model + ≥ 2 GiB headroom is
unavailable (or the pull fails), writes `evidence/SKIP.txt` and exits **3** (honest §11.4.3 SKIP) —
it never OOMs the card and never fakes a caption. GGUF weights are cached at `~/models/vlm_cache/`
(outside the repo, gitignored) so re-runs are offline after the first pull.

---

## Honest boundary (§11.4.6)

This proves a **real** VLM **describes a real image's known content** (grounded color + shape + OCR),
with a self-validating golden-BAD negative and captured VRAM-fit evidence — genuine Phase-3 vision
capability on the RTX 5090 sharing the card with the resident coder. It does **not** claim
production-grade VLM accuracy, benchmark scores, or multi-image/video support — only that the
end-to-end vision path (image in → grounded description out, on-GPU, no OOM, coder untouched) works
and is reproducible.
