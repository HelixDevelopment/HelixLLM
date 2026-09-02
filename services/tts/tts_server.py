"""HelixLLM text-to-speech HTTP shim (T080).

A minimal FastAPI server fronting a REAL Kokoro-82M ONNX synthesis pipeline on
the processor. It is REAL infrastructure, NOT a test double: every returned WAV
comes from an actual forward pass over real weights, and passes the
anti-simulation guard before it is encoded. There is NO simulated synthesis, NO
placeholder audio, NO hardcoded response (BLUFF-001 anti-pattern — forbidden).

Model default: whatever `plan_for_host` selects for the DECLARED USAGE PURPOSE
on the MEASURED host (FR-056) — no configuration value here names a model. For
the ordinary case, a commercial caller on a machine with no accelerator, that
resolves to Kokoro-82M, whose Apache-2.0 licence was verified against the
Hugging Face model API on 2026-09-02. That matters more than it may look: the
one text-to-speech entry the repository catalogue currently loads is `xtts-v2`
under the Coqui Public Model License, which forbids commercial use and cannot
be licensed for it because Coqui Inc shut down in January 2024. Defaulting to
it would ship a service the paying caller may not lawfully use.

Contract
--------
    GET  /health
        -> { status, engine_ready, catalogue_key, model, license_id,
             sample_rate, declared_usage_purpose, withheld, reason }
        Reports whether a model could be SELECTED and whether the engine is
        loaded, WITHOUT forcing a load. `withheld` lists the models that were
        not offered and names the restricting term for each, so a caller can
        see that an option exists but its licence excludes them (FR-054) rather
        than believing no option exists.

    GET  /v1/audio/voices
        -> { voices: [...] }  the voices baked into the selected model.

    POST /v1/audio/speech    (OpenAI-audio-style)
        body: { "input": str, "voice": str?, "speed": float?,
                "response_format": "wav" }
        -> audio/wav bytes
        The bytes are a REAL synthesised waveform. If no model can be selected,
        the endpoint returns 503 with WHICH of the three reasons applies —
        the host lacks resources, no option supports this configuration, or
        every option is excluded by its usage terms — never a generic failure
        and never fabricated audio.
"""

from __future__ import annotations

import os

from fastapi import FastAPI, HTTPException, Response
from pydantic import BaseModel, Field
import uvicorn

import helix_model_gate as gate
import tts_engine

# Catalogue PATHS, not model names. Configuration is allowed to say where
# files live; it is not allowed to say which model runs.
CATALOGUE_PATHS = [
    p.strip()
    for p in os.environ.get(
        "HELIXLLM_CATALOGUE_PATHS",
        os.pathsep.join(["/app/models.yaml", "/app/catalogue/speech_audio.yaml"]),
    ).split(os.pathsep)
    if p.strip()
]
DECLARED_PURPOSE = os.environ.get("HELIXLLM_USAGE_PURPOSE", gate.USAGE_COMMERCIAL)
LISTEN_PORT = int(os.environ.get("HELIXLLM_TTS_PORT", "8080"))

app = FastAPI(title="helixllm-tts", version="1.0.0")

_plan: tts_engine.SynthesisPlan | None = None
_plan_error: dict | None = None


def _measure_host() -> gate.HostMeasurement:
    """Measure THIS host. An unreadable figure yields measured=False, which
    causes a refusal to choose rather than a guess (FR-056)."""
    try:
        system_free = os.sysconf("SC_PAGE_SIZE") * os.sysconf("SC_AVPHYS_PAGES")
        st = os.statvfs(os.environ.get("HELIXLLM_TTS_WEIGHTS_DIR", "/"))
        free_disk = st.f_bavail * st.f_frsize
    except (ValueError, OSError, AttributeError):
        return gate.HostMeasurement(measured=False)

    has_accel, accel_free = False, 0
    try:
        import onnxruntime as ort

        providers = ort.get_available_providers()
        if "CUDAExecutionProvider" in providers:
            # Presence of the provider is not free memory. Only claim an
            # accelerator when its USABLE memory can actually be read.
            try:
                import torch

                if torch.cuda.is_available():
                    free, _total = torch.cuda.mem_get_info(0)
                    has_accel, accel_free = True, int(free)
            except Exception:
                has_accel, accel_free = False, 0
    except Exception:
        pass

    return gate.HostMeasurement(
        measured=True,
        has_accelerator=has_accel,
        accelerator_free_bytes=accel_free,
        system_free_bytes=int(system_free),
        free_disk_bytes=int(free_disk),
    )


def get_plan() -> tts_engine.SynthesisPlan | None:
    """Select the model once. A failure is RECORDED so /health can report it."""
    global _plan, _plan_error
    if _plan is not None or _plan_error is not None:
        return _plan
    try:
        entries = gate.load_catalogue(*[p for p in CATALOGUE_PATHS if os.path.exists(p)])
    except gate.CatalogueError as exc:
        _plan_error = {
            "reason": "catalogue_unreadable",
            "detail": str(exc),
            "catalogue_paths": CATALOGUE_PATHS,
        }
        return None
    try:
        _plan = tts_engine.plan_for_host(entries, _measure_host(), DECLARED_PURPOSE)
    except gate.CannotChoose as exc:
        _plan_error = exc.as_dict()
        return None
    return _plan


@app.get("/health")
def health():
    plan = get_plan()
    body = {
        "status": "ok",
        "engine_ready": plan is not None,
        "engine_loaded": tts_engine._engine is not None,
        "declared_usage_purpose": DECLARED_PURPOSE,
        "catalogue_paths": CATALOGUE_PATHS,
        "weights": tts_engine.weight_paths(),
    }
    if plan is None:
        body.update({"catalogue_key": None, "model": None, "reason": _plan_error})
        return body
    body.update(
        {
            "catalogue_key": plan.catalogue_key,
            "model": plan.catalogue_key,
            "engine": plan.engine,
            "sample_rate": plan.sample_rate,
            "license_id": plan.license_id,
            "selection_basis": plan.selection_basis,
            "withheld": list(plan.withheld),
            "reason": tts_engine.engine_error(),
        }
    )
    return body


@app.get("/v1/audio/voices")
def voices():
    plan = get_plan()
    if plan is None:
        raise HTTPException(status_code=503, detail=_plan_error or {"reason": "no model selected"})
    try:
        engine = tts_engine.load_engine()
    except RuntimeError as exc:
        raise HTTPException(status_code=503, detail={"reason": "engine_unavailable", "detail": str(exc)})
    return {"model": plan.catalogue_key, "voices": sorted(engine.get_voices())}


class SpeechRequest(BaseModel):
    input: str = Field(..., min_length=1)
    voice: str | None = None
    speed: float = 1.0
    response_format: str = "wav"
    # `model` is accepted for OpenAI wire compatibility and deliberately NOT
    # honoured as a selection: which model runs is decided from the measured
    # host (FR-056). The response header states what actually ran.
    model: str | None = None


@app.post("/v1/audio/speech")
def speech(req: SpeechRequest):
    plan = get_plan()
    if plan is None:
        raise HTTPException(
            status_code=503,
            detail={
                "message": (
                    "no text-to-speech model could be offered for this host and declared "
                    "usage, so nothing was synthesised. No default model is started here."
                ),
                **(_plan_error or {}),
            },
        )
    if req.response_format.lower() not in ("wav", "pcm"):
        raise HTTPException(
            status_code=400,
            detail=f"response_format {req.response_format!r} is not produced by this service (wav)",
        )

    try:
        engine = tts_engine.load_engine()
    except RuntimeError as exc:
        # Engine could not load — the exact reason. NEVER fabricated audio.
        raise HTTPException(
            status_code=503,
            detail={"reason": "engine_unavailable", "detail": str(exc)},
        )

    try:
        samples, sample_rate = tts_engine.synthesise(
            engine, req.input, voice=req.voice, speed=req.speed
        )
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc))

    try:
        # The guard runs inside encode_wav, before any bytes are produced.
        blob = tts_engine.encode_wav(samples, sample_rate)
    except gate.SimulationSuspected as exc:
        # The engine returned something with the shape of a fabricated
        # response. Refuse loudly rather than hand it to the caller.
        raise HTTPException(
            status_code=500,
            detail={
                "reason": "anti_simulation_guard_tripped",
                "detail": str(exc),
                "model": plan.catalogue_key,
            },
        )

    return Response(
        content=blob,
        media_type="audio/wav",
        headers={
            "X-Helix-Model": plan.catalogue_key,
            "X-Helix-License": plan.license_id,
            "X-Helix-Sample-Rate": str(sample_rate),
        },
    )


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=LISTEN_PORT)
