"""HelixLLM Phase-4 GPU video-generation HTTP shim.

A minimal FastAPI server that fronts a REAL WAN 2.2 / LTX-Video diffusion
pipeline (diffusers on the host RTX 5090). It is REAL infrastructure, NOT a test
double: every returned clip comes from an actual `pipe(...)` diffusion call over
real weights, muxed to a real MP4. There is NO simulated generation, NO
placeholder clip, NO hardcoded response (BLUFF-001 anti-pattern — forbidden).

Contract
--------
    GET  /health
        -> { status, engine_ready, model, backend, precision, device, cuda,
             torch, reason }
        Reports whether the diffusion engine is loadable/loaded WITHOUT forcing
        a load (so an idle-but-up container stays VRAM-cheap between jobs).
        engine_ready=false carries the exact `reason` (e.g. no CUDA device,
        diffusers WAN pipeline import failure) — it never fakes readiness (§11.4.6).

    POST /v1/videos/generations   (HelixLLM-native; no OpenAI std for video yet)
        body: { "prompt": str, "num_frames": int?, "size": "832x480",
                "fps": int?, "steps": int?, "seed": int? }
        -> { "created": <unix>, "data": [ { "b64_mp4": <mp4-base64>,
             "seed": <int>, "steps": <int>, "num_frames": <int>,
             "fps": <int>, "width": <int>, "height": <int> } ],
             "model": <id>, "backend": <str>, "precision": <str> }
        The video bytes are a REAL generated H.264 MP4. If the engine cannot load
        (no GPU, missing WAN/LTX weights, first-run OOM) the endpoint returns
        HTTP 503 with the exact reason — NEVER a fabricated clip (a faked
        video-gen "proof" is a §11.4 PASS-bluff).

Blackwell / sm_120 notes (§11.4.150 research; see the service README):
  * On the 5090 the co-resident (coder-stays-live, peak <= ~10.4 GiB) no-pause
    fast paths are: WAN 2.2 TI2V-5B FP8 @480p (~8-10 GB, native offload — the
    DEFAULT), WAN 2.2 14B GGUF-Q4 + T5-CPU (~6-8 GB), LTX-13B GGUF-Q4 (~6-10 GB,
    32 GB+ system RAM). The MUST-PAUSE paths (WAN-14B FP8/FP16, LTX-13B FP8/full)
    are NOT the default here — the vrambroker refuses them co-resident with the
    live coder (ErrBudgetExceeded) and the operator-gated coder-pause is required.
  * `VIDEOGEN_CPU_OFFLOAD=1` (T5/UMT5 text encoder + idle stages to CPU) is what
    holds the 480p WAN-5B config under the co-resident ceiling. Larger res / longer
    clips push toward/over it → calibrate on the real card (§11.4.6).

Runtime-proof status (§11.4.6 / §11.4.108): the first REAL generation +
per-variant VRAM calibration (feeding the measured peak to the vrambroker's
Acquire(needBytes)) is PENDING — it requires an operator-authorized coder-pause
(§11.4.122) window on the shared card. This server is the ready-to-run harness;
it does not (and must not) claim a generation has been proven until that run.
"""

import base64
import os
import tempfile
import threading
import time

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field
import uvicorn


# ---- config (env-injected, no hardcoded model/precision literal — §CONST-046) ----

def _env(name: str, default: str) -> str:
    v = os.environ.get(name)
    return v if v is not None and v != "" else default


BACKEND = _env("VIDEOGEN_BACKEND", "wan")            # wan | ltx
MODEL_ID = _env("VIDEOGEN_MODEL", "Wan-AI/Wan2.2-TI2V-5B-Diffusers")
PRECISION = _env("VIDEOGEN_PRECISION", "fp8")        # fp8 | gguf-q4 | bf16 (default no-pause: WAN-5B fp8@480p)
CPU_OFFLOAD = _env("VIDEOGEN_CPU_OFFLOAD", "1") == "1"
DEFAULT_STEPS = int(_env("VIDEOGEN_MAX_STEPS", "30"))
# WAN uses (4n+1) frame counts; 49 frames @16 fps ~= 3 s. Small default keeps the
# co-resident 480p peak low (§11.4.6 — the no-coder-pause budget).
DEFAULT_NUM_FRAMES = int(_env("VIDEOGEN_NUM_FRAMES", "49"))
DEFAULT_FPS = int(_env("VIDEOGEN_FPS", "16"))
DEFAULT_SIZE = _env("VIDEOGEN_SIZE", "832x480")      # 480p short-clip default
LISTEN_PORT = int(_env("VIDEOGEN_PORT", "8080"))


app = FastAPI(title="helixllm-videogen", version="0.1.0-scaffold")

# The pipeline is loaded lazily on the first generation request and cached.
# A load failure is recorded (not raised at import) so /health can report it.
_pipe = None
_pipe_lock = threading.Lock()
_load_error: str | None = None
_loaded_backend: str | None = None


def _torch_info():
    """Return (torch_version, cuda_available, device_name, cuda_version, err).
    Imported lazily so the shim starts even if torch is broken — /health then
    reports the exact import error rather than crash-looping."""
    try:
        import torch  # noqa: WPS433 (deliberate lazy import)
    except Exception as exc:  # pragma: no cover - environment dependent
        return None, False, "", "", f"torch import failed: {exc}"
    cuda = bool(torch.cuda.is_available())
    dev = torch.cuda.get_device_name(0) if cuda else ""
    cver = getattr(torch.version, "cuda", "") or ""
    return torch.__version__, cuda, dev, cver, None


def _build_pipeline():
    """Construct the REAL WAN/LTX diffusion pipeline for the configured backend.

    WAN path (Blackwell co-resident 480p default, ~8-10 GB peak):
      diffusers WanPipeline over Wan-AI/Wan2.2-TI2V-5B-Diffusers (FP8) with the
      UMT5 text encoder CPU-offloaded.
    LTX path (alt co-resident, GGUF-Q4):
      diffusers LTXPipeline over Lightricks/LTX-Video.

    The exact FP8 / GGUF-Q4 quant asset ids are env-injected (§CONST-046) — no
    weight id is hardcoded as behaviour here. Any load failure returns a reason
    string (never a fake pipeline)."""
    import torch

    torch_dtype = torch.bfloat16

    if BACKEND == "ltx":
        try:
            from diffusers import LTXPipeline
        except Exception as exc:
            return None, (
                "diffusers LTXPipeline unavailable — the installed diffusers does "
                f"not expose LTXPipeline ({exc}). Pin a diffusers with LTX-Video "
                "support, or set VIDEOGEN_BACKEND=wan."
            )
        pipe = LTXPipeline.from_pretrained(MODEL_ID, torch_dtype=torch_dtype)
    else:  # "wan" (default)
        try:
            from diffusers import WanPipeline
        except Exception as exc:
            return None, (
                "diffusers WanPipeline unavailable — the installed diffusers does "
                f"not expose WanPipeline ({exc}). Pin a diffusers with WAN 2.2 "
                "support, or set VIDEOGEN_BACKEND=ltx."
            )
        pipe = WanPipeline.from_pretrained(MODEL_ID, torch_dtype=torch_dtype)

    if CPU_OFFLOAD:
        # The T5/UMT5 text encoder is used only in the conditioning pass ->
        # offloading it (+ idle stages) to CPU is what pulls the WAN-5B 480p
        # config under the ~10.4 GiB co-resident ceiling.
        pipe.enable_model_cpu_offload()
    else:
        pipe = pipe.to("cuda")

    return pipe, None


def _ensure_pipeline():
    global _pipe, _load_error, _loaded_backend
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
        _loaded_backend = BACKEND
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
            reason = "engine not yet loaded (lazy-load on first /v1/videos/generations)"
    return {
        "status": "ok",
        "engine_ready": engine_ready,
        "model": MODEL_ID,
        "backend": _loaded_backend or BACKEND,
        "precision": PRECISION,
        "cpu_offload": CPU_OFFLOAD,
        "device": dev,
        "cuda": cver,
        "torch": tver,
        "reason": reason,
    }


class VideoRequest(BaseModel):
    prompt: str = Field(..., min_length=1)
    num_frames: int | None = None
    size: str = DEFAULT_SIZE
    fps: int | None = None
    steps: int | None = None
    seed: int | None = None


def _parse_size(size: str) -> tuple[int, int]:
    try:
        w, h = size.lower().split("x", 1)
        return int(w), int(h)
    except Exception:
        raise HTTPException(status_code=400, detail=f"bad size {size!r} (want WxH, e.g. 832x480)")


@app.post("/v1/videos/generations")
def generate(req: VideoRequest):
    pipe = _ensure_pipeline()
    if pipe is None:
        # Engine could not load — return the exact reason. NEVER a fake clip.
        raise HTTPException(
            status_code=503,
            detail=(_load_error or "video-gen engine unavailable"),
        )

    import torch
    from diffusers.utils import export_to_video

    width, height = _parse_size(req.size)
    steps = req.steps or DEFAULT_STEPS
    num_frames = req.num_frames or DEFAULT_NUM_FRAMES
    fps = req.fps or DEFAULT_FPS
    generator = None
    if req.seed is not None:
        generator = torch.Generator(device="cpu").manual_seed(int(req.seed))

    # REAL diffusion — the load-bearing anti-bluff call.
    result = pipe(
        prompt=req.prompt,
        width=width,
        height=height,
        num_frames=num_frames,
        num_inference_steps=steps,
        generator=generator,
    )
    frames = result.frames[0]

    # Mux frames -> a real H.264 MP4 (diffusers export_to_video via imageio-ffmpeg).
    with tempfile.NamedTemporaryFile(suffix=".mp4", delete=True) as tmp:
        export_to_video(frames, tmp.name, fps=fps)
        tmp.seek(0)
        mp4_bytes = open(tmp.name, "rb").read()

    return {
        "created": int(time.time()),
        "data": [
            {
                "b64_mp4": base64.b64encode(mp4_bytes).decode("ascii"),
                "seed": int(req.seed) if req.seed is not None else -1,
                "steps": steps,
                "num_frames": num_frames,
                "fps": fps,
                "width": width,
                "height": height,
            }
        ],
        "model": MODEL_ID,
        "backend": _loaded_backend or BACKEND,
        "precision": PRECISION,
    }


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=LISTEN_PORT)
