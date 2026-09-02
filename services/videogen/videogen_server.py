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

WHICH MODEL RUNS IS DECIDED UPSTREAM, AND THIS SERVER HAS NO DEFAULT (FR-056)
---------------------------------------------------------------------------
VIDEOGEN_MODEL / _BACKEND / _PRECISION / _SIZE / _NUM_FRAMES / _FPS are the
OUTPUTS of the measured selection performed by `cmd/videogen-boot` before the
container is started: it measures the host, joins that measurement against the
recorded catalogue under the declared usage, and writes the chosen model's
values for compose to interpolate.

This server therefore carries NO fallback for any of them. A hardcoded default
model would be a model nobody chose, served on a host nobody measured, at a
memory cost nobody admitted — precisely what FR-056 forbids. When the decision
is absent the server starts (so it can be asked WHY) but reports itself
UNCONFIGURED: `/health` answers HTTP 503 with the missing variables named, and
`/v1/videos/generations` answers 503 for the same reason. It never picks a
model to stand in.

VIDEOGEN_CPU_OFFLOAD, VIDEOGEN_MAX_STEPS and VIDEOGEN_PORT keep defaults: they
say HOW the service runs, and none of them names a model.

WHAT THIS SERVICE CAN ACTUALLY LOAD, AND WHAT IT REFUSES (§11.4.6)
-----------------------------------------------------------------
The pipeline is built with exactly one call shape:
`WanPipeline/LTXPipeline.from_pretrained(MODEL_ID, torch_dtype=...)`. That
resolves a DIFFUSERS-FORMAT repository through its `model_index.json`. Two
configurations are therefore NAMED by the catalogue and the weight manifest but
CANNOT be served by this loader, and both are refused at CONFIGURATION time —
before any weight is fetched — rather than allowed to load something else:

  * precision `gguf-q4`. There is no GGUF load path here: no
    `from_single_file`, no `GGUFQuantizationConfig`. A GGUF quant repository is
    a bag of single-file `.gguf` checkpoints with no `model_index.json`, so
    `from_pretrained` cannot read one (verified 2026-09-02 against every
    published LTX 13B Q4 repository). Pointing this loader at a diffusers-format
    repository "instead" does not serve the quant — it serves the UNQUANTISED
    model, at a memory cost the recorded figure was never measured for.

  * backend `ltx`. No source is established that serves the recorded 13B
    GGUF-Q4 build through this loader. The obvious repository is worse than
    useless here: it carries `model_index.json`, so `from_pretrained` SUCCEEDS
    — and resolves a 28-layer / 2048-hidden transformer (~2B), not the 13B the
    catalogue records (48 layers / 4096 hidden). Succeeding with a smaller model
    than the caller was told they are getting is the failure this refusal
    exists to prevent; a user would attribute ~2B output to a 13B model.

Both markers state what would establish support, and both are mirrored in
`.gitignore-meta/wan_ltx_gguf.yaml` (`unimplemented_precisions` /
`unsupported_backends`). `test_manifest_catalogue_agreement.py` asserts the
markers, the manifest and the catalogue cannot drift apart: lifting a marker
without recording a serving source — or recording one without lifting the
marker — fails that guard.

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
    """Read an operator knob that legitimately has a default (HOW, never WHICH)."""
    v = os.environ.get(name)
    return v if v is not None and v != "" else default


def _decided(name: str) -> str | None:
    """Read one OUTPUT of the upstream measured selection.

    There is deliberately no default: a value invented here would be a model,
    or a clip shape, that no measurement chose (FR-056). Absence is returned as
    None and reported, never filled in.
    """
    v = os.environ.get(name)
    return v if v is not None and v != "" else None


def _decided_int(name: str) -> int | None:
    """As _decided, for a whole-number output. A malformed value is an absent
    decision, not a licence to guess."""
    raw = _decided(name)
    if raw is None:
        return None
    try:
        return int(raw)
    except ValueError:
        return None


# --- OUTPUTS of the upstream measured selection: no defaults, ever ---
BACKEND = _decided("VIDEOGEN_BACKEND")               # wan | ltx
MODEL_ID = _decided("VIDEOGEN_MODEL")
PRECISION = _decided("VIDEOGEN_PRECISION")           # fp8 | gguf-q4 | bf16
DECIDED_SIZE = _decided("VIDEOGEN_SIZE")             # WxH, the shape admitted for
DECIDED_NUM_FRAMES = _decided_int("VIDEOGEN_NUM_FRAMES")
DECIDED_FPS = _decided_int("VIDEOGEN_FPS")

# --- operator knobs: HOW the service runs, never WHICH model ---
CPU_OFFLOAD = _env("VIDEOGEN_CPU_OFFLOAD", "1") == "1"
DEFAULT_STEPS = int(_env("VIDEOGEN_MAX_STEPS", "30"))
LISTEN_PORT = int(_env("VIDEOGEN_PORT", "8080"))

# The decision variables, in the order a reader would want them reported.
_DECISION_VARS = (
    ("VIDEOGEN_MODEL", MODEL_ID),
    ("VIDEOGEN_BACKEND", BACKEND),
    ("VIDEOGEN_PRECISION", PRECISION),
    ("VIDEOGEN_SIZE", DECIDED_SIZE),
    ("VIDEOGEN_NUM_FRAMES", DECIDED_NUM_FRAMES),
    ("VIDEOGEN_FPS", DECIDED_FPS),
)


def _undecided() -> list[str]:
    """Names of the selection outputs this container was not given."""
    return [name for name, value in _DECISION_VARS if value is None]


def _unconfigured_reason() -> str | None:
    """Why this container cannot serve, when no decision reached it."""
    missing = _undecided()
    if not missing:
        return None
    return (
        "no measured model decision reached this container: "
        + ", ".join(missing)
        + " unset or malformed. This server has no default model — a model nobody "
        "chose would be served on a host nobody measured, at a memory cost nobody "
        "admitted (FR-056). Boot through `videogen-boot boot`, which measures this "
        "host and writes these values."
    )


# --- what this loader can serve: refusals that fire before any weight is read ---
#
# These are NOT model names and cannot select anything — each one can only ever
# REFUSE a configuration the upstream selection produced. Both are mirrored in
# the weight manifest's `unimplemented_precisions` / `unsupported_backends`.

_UNIMPLEMENTED_PRECISIONS = {
    "gguf-q4": (
        "this service has no GGUF load path: it builds pipelines only through "
        "`from_pretrained`, which resolves a diffusers-format repository via "
        "model_index.json and cannot read single-file .gguf weights. Serving a "
        "diffusers repository in its place would load the UNQUANTISED model at a "
        "memory cost this build's recorded figure was never measured for. "
        "To establish support: add a real GGUF path (transformer via "
        "`from_single_file(..., quantization_config=GGUFQuantizationConfig(...))`, "
        "text encoder / VAE / tokenizer / scheduler sourced separately, pipeline "
        "assembled from those components), then record the quant asset's source "
        "and MEASURED size on the affected catalogue entries and re-measure their "
        "memory figures against the assembled pipeline."
    ),
}

_UNSUPPORTED_BACKENDS = {
    "ltx": (
        "no source is established that serves the recorded `ltx-video-13b` "
        "GGUF-Q4 build through this loader. The obvious repository is not a safe "
        "stand-in: it carries model_index.json, so `from_pretrained` SUCCEEDS and "
        "returns a 28-layer/2048-hidden transformer (~2B) instead of the recorded "
        "13B (48 layers/4096 hidden) — output a caller would attribute to a 13B "
        "model. Refusing loudly here is the lesser harm. "
        "To establish support: EITHER add the GGUF load path above plus a recorded "
        "source for a 13B Q4_K asset, OR re-record the entry against a "
        "diffusers-format 13B repository `from_pretrained` genuinely resolves to a "
        "48-layer transformer — re-measuring its memory figure, since that "
        "transformer alone is ~24 GiB and is a coder-pause size, not the no-pause "
        "path the entry currently claims."
    ),
}


def _unservable_reason() -> str | None:
    """Why a fully-decided configuration still cannot be served here.

    Distinct from `_unconfigured_reason`, which reports a decision that never
    arrived. This one reports a decision that DID arrive and names something
    this loader cannot honour. Both must refuse, and for the same reason: the
    alternative is loading whatever `from_pretrained` happens to accept and
    reporting it under the identity the caller asked for.

    Checked at configuration time so the refusal costs no download.
    """
    if PRECISION is not None:
        why = _UNIMPLEMENTED_PRECISIONS.get(PRECISION.strip().lower())
        if why is not None:
            return f"precision {PRECISION!r} is not servable by this build: {why}"
    if BACKEND is not None:
        why = _UNSUPPORTED_BACKENDS.get(BACKEND.strip().lower())
        if why is not None:
            return f"backend {BACKEND!r} is not servable by this build: {why}"
    return None


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

    The backend and the weight repository are both OUTPUTS of the upstream
    measured selection (VIDEOGEN_BACKEND / VIDEOGEN_MODEL) — no repository is
    named here, because naming one would make it servable without a
    measurement. WAN builds load through diffusers WanPipeline, LTX builds
    through LTXPipeline; which of the two, and over which repository, is the
    decision's answer and not this function's.

    With VIDEOGEN_CPU_OFFLOAD=1 the text encoder and idle stages are offloaded,
    which is what holds the co-resident 480p configurations under the ceiling
    their recorded memory figures were taken at.

    Any load failure returns a reason string (never a fake pipeline)."""
    reason = _unconfigured_reason()
    if reason is not None:
        # Unreachable through /v1/videos/generations (which refuses first), but
        # stated here too so no path can ever construct a pipeline for a model
        # nobody chose.
        return None, reason

    unservable = _unservable_reason()
    if unservable is not None:
        # Same defence in depth, for the decided-but-unservable case: no path may
        # reach a `from_pretrained` call that would load a DIFFERENT build than
        # the one recorded and admitted.
        return None, unservable

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

    # A container that was never told which model to serve is NOT healthy. This
    # is distinct from "configured but not yet lazily loaded", which stays a
    # 200 below: that one will serve on the first request, this one never can.
    unconfigured = _unconfigured_reason()
    if unconfigured is not None:
        raise HTTPException(status_code=503, detail=unconfigured)

    # A container told to serve something this build cannot load is not healthy
    # either, and — unlike a lazy-load pending — it never will be. Report it now,
    # not at first request after a long weight download.
    unservable = _unservable_reason()
    if unservable is not None:
        raise HTTPException(status_code=503, detail=unservable)

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
    """A generation request.

    The shape fields default to None, not to a literal: the served shape is the
    one the upstream selection decided and the broker admitted VRAM for. A
    request may ask for LESS than that; it may not ask for more (see
    _resolve_shape).
    """

    prompt: str = Field(..., min_length=1)
    num_frames: int | None = None
    size: str | None = None
    fps: int | None = None
    steps: int | None = None
    seed: int | None = None


def _parse_size(size: str) -> tuple[int, int]:
    try:
        w, h = size.lower().split("x", 1)
        return int(w), int(h)
    except Exception:
        raise HTTPException(status_code=400, detail=f"bad size {size!r} (want WxH, e.g. 832x480)")


def _resolve_shape(req: "VideoRequest") -> tuple[int, int, int, int]:
    """Resolve the clip shape to generate: (width, height, num_frames, fps).

    The decided shape is the default AND the ceiling. The VRAM this job holds
    was admitted for the decided shape, so a larger request would exceed a
    budget the broker granted on different terms — on a shared card that is not
    a slow render, it is an out-of-memory abort that can take the co-resident
    coder with it. A smaller request is cheaper than what was admitted and is
    allowed, so nothing a caller could previously ask for within budget is lost.
    """
    dw, dh = _parse_size(DECIDED_SIZE)
    width, height = (dw, dh) if req.size is None else _parse_size(req.size)
    num_frames = DECIDED_NUM_FRAMES if req.num_frames is None else int(req.num_frames)
    fps = DECIDED_FPS if req.fps is None else int(req.fps)

    if num_frames < 1 or fps < 1 or width < 1 or height < 1:
        raise HTTPException(status_code=400, detail="size, num_frames and fps must all be positive")

    decided_budget = dw * dh * DECIDED_NUM_FRAMES
    requested_budget = width * height * num_frames
    if requested_budget > decided_budget:
        raise HTTPException(
            status_code=400,
            detail=(
                f"requested {width}x{height} x {num_frames} frames exceeds the shape this host was "
                f"measured and admitted for ({dw}x{dh} x {DECIDED_NUM_FRAMES} frames). The VRAM "
                "currently held was granted for the smaller shape; serving the larger one would "
                "exceed a budget nobody admitted. Ask for that shape or less."
            ),
        )
    return width, height, num_frames, fps


@app.post("/v1/videos/generations")
def generate(req: VideoRequest):
    unconfigured = _unconfigured_reason()
    if unconfigured is not None:
        # No model was chosen for this container. Refuse — never substitute one.
        raise HTTPException(status_code=503, detail=unconfigured)

    unservable = _unservable_reason()
    if unservable is not None:
        # A model WAS chosen, and this build cannot load it. Refuse — never serve
        # a different build under the identity the caller asked for.
        raise HTTPException(status_code=503, detail=unservable)

    pipe = _ensure_pipeline()
    if pipe is None:
        # Engine could not load — return the exact reason. NEVER a fake clip.
        raise HTTPException(
            status_code=503,
            detail=(_load_error or "video-gen engine unavailable"),
        )

    import torch
    from diffusers.utils import export_to_video

    width, height, num_frames, fps = _resolve_shape(req)
    steps = req.steps or DEFAULT_STEPS
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
