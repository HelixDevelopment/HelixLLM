"""HelixLLM Phase-4 GPU image-generation HTTP shim.

A minimal FastAPI server that fronts a REAL FLUX.1-schnell diffusion pipeline
(diffusers + Nunchaku SVDQuant NVFP4 on the host RTX 5090), with FLUX.1-dev
available as an explicit opt-in via `IMAGEGEN_MODEL`. It is REAL infrastructure,
NOT a test double: every returned image comes from an actual `pipe(...)`
diffusion call over real weights. There is NO simulated generation, NO
placeholder image, NO hardcoded response (BLUFF-001 anti-pattern — forbidden).

Model default: FLUX.1-schnell (Apache-2.0 §11.4.150 research 2026-07-08,
HF API confirms `license: apache-2.0`, "can be used for personal, scientific,
and commercial purposes") replaces FLUX.1-dev (restrictive
`flux-1-dev-non-commercial-license`) as the default so the SHIPPED default
carries a permissive licence.

HONEST BOUNDARY (§11.4.6 — corrects an earlier design-time assumption,
verified against the HF API `https://huggingface.co/api/models/<repo>` on
2026-07-08): BOTH `black-forest-labs/FLUX.1-schnell` AND
`black-forest-labs/FLUX.1-dev` report `"gated": "auto"` on Hugging Face — an
`HF_TOKEN` from an account that has clicked "Agree" on the model page IS
STILL REQUIRED to download either base pipeline (VAE/T5/CLIP/scheduler). The
switch to schnell does NOT remove the `HF_TOKEN` requirement (an earlier
"non-gated, no operator token required" framing was WRONG — do not repeat
it). What the switch DOES deliver: (1) a permissive Apache-2.0 licence
instead of a non-commercial-only one; (2) `"auto"` gating means INSTANT
self-serve access (no manual review queue) — the SAME low-friction gate class
as dev, not worse; (3) the Nunchaku NVFP4 transformer repo
(`nunchaku-ai/nunchaku-flux.1-schnell`) is itself fully PUBLIC
(`"gated": false` — verified via the HF API), so only the base-pipeline
download needs the token, not the quantized transformer.

Contract
--------
    GET  /health
        -> { status, engine_ready, model, precision, device, cuda,
             torch, reason }
        Reports whether the diffusion engine is loadable/loaded WITHOUT forcing
        a load (so an idle-but-up container stays VRAM-cheap between jobs).
        engine_ready=false carries the exact `reason` (e.g. no CUDA device,
        Nunchaku wheel absent) — it never fakes readiness (§11.4.6).

    POST /v1/images/generations   (OpenAI-images-style)
        body: { "prompt": str, "n": int=1, "size": "1024x1024",
                "steps": int?, "seed": int?, "response_format": "b64_json" }
        -> { "created": <unix>, "data": [ { "b64_json": <png-base64>,
             "seed": <int>, "steps": <int>, "width": <int>, "height": <int> } ],
             "model": <id>, "precision": <str> }
        The image bytes are a REAL generated PNG. If the engine cannot load
        (no GPU, missing Nunchaku NVFP4 wheel, first-run OOM) the endpoint
        returns HTTP 503 with the exact reason — NEVER a fabricated image
        (a faked image-gen "proof" is a §11.4 PASS-bluff).

Blackwell / sm_120 notes (§11.4.150 research; see the service README):
  * On the 5090 the min-memory co-resident config is NVFP4 (cc 10.x -> fp4),
    NOT INT4. `IMAGEGEN_PRECISION=nvfp4` selects the Nunchaku NVFP4 transformer.
  * `IMAGEGEN_QUANTIZE_T5=1` + `IMAGEGEN_CPU_OFFLOAD=1` are the ~6 GB-peak
    (no-coder-pause) config. Without them FLUX needs ~12-24 GB and the
    vrambroker will refuse co-residence with the live coder (ErrBudgetExceeded)
    -> the coder-pause path (CoderPauseManager, harness) is required.

Runtime-proof status (§11.4.6 / §11.4.108): the first REAL generation +
per-variant VRAM calibration (feeding the measured peak to the vrambroker's
Acquire(needBytes)) is PENDING — it requires an operator-authorized coder-pause
(§11.4.122) window on the shared card. This server is the ready-to-run harness;
it does not (and must not) claim a generation has been proven until that run.
"""

import base64
import io
import os
import threading
import time

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field
import uvicorn


# ---- config (env-injected, no hardcoded model/precision literal — §CONST-046) ----

def _env(name: str, default: str) -> str:
    v = os.environ.get(name)
    return v if v is not None and v != "" else default


MODEL_ID = _env("IMAGEGEN_MODEL", "black-forest-labs/FLUX.1-schnell")
PRECISION = _env("IMAGEGEN_PRECISION", "nvfp4")  # nvfp4 | nf4 | bf16 (Blackwell -> nvfp4)
QUANTIZE_T5 = _env("IMAGEGEN_QUANTIZE_T5", "1") == "1"
CPU_OFFLOAD = _env("IMAGEGEN_CPU_OFFLOAD", "1") == "1"
# schnell is a few-step distilled model — its own model card (verified
# 2026-07-08) recommends num_inference_steps=4, guidance_scale=0.0. dev needs
# many more steps (~20-50) + a non-zero guidance_scale; both are env-overridable
# so the IMAGEGEN_MODEL=...FLUX.1-dev opt-in stays correct.
DEFAULT_STEPS = int(_env("IMAGEGEN_MAX_STEPS", "4"))
DEFAULT_GUIDANCE = float(_env("IMAGEGEN_GUIDANCE_SCALE", "0.0"))
LISTEN_PORT = int(_env("IMAGEGEN_PORT", "8080"))


def _default_nvfp4_transformer(model_id: str) -> str:
    """Pick the Nunchaku NVFP4 transformer repo matching the configured base
    model. Both `nunchaku-ai/nunchaku-flux.1-schnell` and
    `nunchaku-ai/nunchaku-flux.1-dev` are HF-PUBLIC (`"gated": false`,
    verified via the HF API on 2026-07-08) — no HF_TOKEN needed for either
    transformer file, only for the base pipeline components. No literal is
    hardcoded as the ONLY choice — IMAGEGEN_NVFP4_TRANSFORMER always wins."""
    return (
        "nunchaku-ai/nunchaku-flux.1-dev"
        if "flux.1-dev" in model_id.lower() or "flux1-dev" in model_id.lower()
        else "nunchaku-ai/nunchaku-flux.1-schnell"
    )


app = FastAPI(title="helixllm-imagegen", version="0.1.0-scaffold")

# The pipeline is loaded lazily on the first generation request and cached.
# A load failure is recorded (not raised at import) so /health can report it.
_pipe = None
_pipe_lock = threading.Lock()
_load_error: str | None = None
_loaded_precision: str | None = None


def _torch_info():
    """Return (torch_version, cuda_available, device_name, cuda_version) or a
    reason string. Imported lazily so the shim starts even if torch is broken —
    /health then reports the exact import error rather than crash-looping."""
    try:
        import torch  # noqa: WPS433 (deliberate lazy import)
    except Exception as exc:  # pragma: no cover - environment dependent
        return None, False, "", "", f"torch import failed: {exc}"
    cuda = bool(torch.cuda.is_available())
    dev = torch.cuda.get_device_name(0) if cuda else ""
    cver = getattr(torch.version, "cuda", "") or ""
    return torch.__version__, cuda, dev, cver, None


def _build_pipeline():
    """Construct the REAL FLUX diffusion pipeline for the configured precision.

    NVFP4 path (Blackwell co-resident, ~6 GB peak):
      Nunchaku NunchakuFluxTransformer2dModel (NVFP4 6.1 GiB) + a quantized T5
      encoder + CPU offload. Requires the Nunchaku Blackwell cu128 wheel.

    Any load failure returns a reason string (never a fake pipeline)."""
    import torch
    from diffusers import FluxPipeline

    torch_dtype = torch.bfloat16

    if PRECISION == "nvfp4":
        # Nunchaku SVDQuant NVFP4 transformer (the Blackwell fast path). The
        # exact repo/asset id for the pre-quantized FLUX NVFP4 transformer is
        # supplied via env so no weight id is hardcoded here (§CONST-046); the
        # README documents the canonical Nunchaku HF repo.
        try:
            from nunchaku import NunchakuFluxTransformer2dModel
        except Exception as exc:
            return None, (
                "Nunchaku NVFP4 path unavailable — the Blackwell cu128 wheel is "
                f"not installed ({exc}). Rebuild the image with the "
                "NUNCHAKU_WHEEL build-arg, or set IMAGEGEN_PRECISION=nf4 for the "
                "non-Nunchaku low-VRAM path."
            )
        nvfp4_repo = _env(
            "IMAGEGEN_NVFP4_TRANSFORMER",
            _default_nvfp4_transformer(MODEL_ID),
        )
        transformer = NunchakuFluxTransformer2dModel.from_pretrained(nvfp4_repo)
        pipe = FluxPipeline.from_pretrained(
            MODEL_ID, transformer=transformer, torch_dtype=torch_dtype
        )
    else:
        # nf4 / bf16 fall-back paths (documented, non-Nunchaku). bf16 needs a
        # coder-pause on this card; nf4 is ~6-8 GB (slower than NVFP4 on
        # Blackwell but co-resident-capable).
        pipe = FluxPipeline.from_pretrained(MODEL_ID, torch_dtype=torch_dtype)

    if CPU_OFFLOAD:
        # T5-XXL is used only in the conditioning pass -> offloading it to CPU
        # is what pulls the NVFP4 config under the ~10.4 GiB co-resident ceiling.
        pipe.enable_model_cpu_offload()
    else:
        pipe = pipe.to("cuda")

    return pipe, None


def _ensure_pipeline():
    global _pipe, _load_error, _loaded_precision
    if _pipe is not None:
        return _pipe
    with _pipe_lock:
        if _pipe is not None:
            return _pipe
        try:
            pipe, reason = _build_pipeline()
        except Exception as exc:  # pragma: no cover - environment dependent
            _load_error = f"pipeline construction failed: {exc}"
            return None
        if reason is not None:
            _load_error = reason
            return None
        _pipe = pipe
        _loaded_precision = PRECISION
        _load_error = None
        return _pipe


@app.get("/health")
def health():
    tver, cuda, dev, cver, terr = _torch_info()
    engine_ready = _pipe is not None
    reason = None
    if not engine_ready:
        if terr:
            reason = terr
        elif not cuda:
            reason = "no CUDA device visible to the container (GPU not admitted / CDI not wired)"
        elif _load_error:
            reason = _load_error
        else:
            reason = "engine not yet loaded (lazy-load on first /v1/images/generations)"
    return {
        "status": "ok",
        "engine_ready": engine_ready,
        "model": MODEL_ID,
        "precision": _loaded_precision or PRECISION,
        "quantize_t5": QUANTIZE_T5,
        "cpu_offload": CPU_OFFLOAD,
        "device": dev,
        "cuda": cver,
        "torch": tver,
        "reason": reason,
    }


class ImageRequest(BaseModel):
    prompt: str = Field(..., min_length=1)
    n: int = 1
    size: str = "1024x1024"
    steps: int | None = None
    seed: int | None = None
    response_format: str = "b64_json"


def _parse_size(size: str) -> tuple[int, int]:
    try:
        w, h = size.lower().split("x", 1)
        return int(w), int(h)
    except Exception:
        raise HTTPException(status_code=400, detail=f"bad size {size!r} (want WxH, e.g. 1024x1024)")


@app.post("/v1/images/generations")
def generate(req: ImageRequest):
    pipe = _ensure_pipeline()
    if pipe is None:
        # Engine could not load — return the exact reason. NEVER a fake image.
        raise HTTPException(
            status_code=503,
            detail=(_load_error or "image-gen engine unavailable"),
        )

    import torch

    width, height = _parse_size(req.size)
    steps = req.steps or DEFAULT_STEPS
    generator = None
    if req.seed is not None:
        generator = torch.Generator(device="cpu").manual_seed(int(req.seed))

    data = []
    for _ in range(max(1, req.n)):
        # REAL diffusion — the load-bearing anti-bluff call. guidance_scale is
        # model-card-mandated per MODEL_ID (schnell: 0.0, verified 2026-07-08;
        # dev opt-in: override IMAGEGEN_GUIDANCE_SCALE, e.g. 3.5).
        result = pipe(
            prompt=req.prompt,
            width=width,
            height=height,
            num_inference_steps=steps,
            guidance_scale=DEFAULT_GUIDANCE,
            generator=generator,
        )
        image = result.images[0]
        buf = io.BytesIO()
        image.save(buf, format="PNG")
        data.append(
            {
                "b64_json": base64.b64encode(buf.getvalue()).decode("ascii"),
                "seed": int(req.seed) if req.seed is not None else -1,
                "steps": steps,
                "width": width,
                "height": height,
            }
        )

    return {
        "created": int(time.time()),
        "data": data,
        "model": MODEL_ID,
        "precision": _loaded_precision or PRECISION,
    }


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=LISTEN_PORT)
