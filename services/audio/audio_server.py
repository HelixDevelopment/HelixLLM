"""HelixLLM audio-CLASSIFICATION HTTP shim (T081).

A minimal FastAPI server fronting a REAL YAMNet LiteRT classifier on the
processor. It is REAL infrastructure, NOT a test double: every returned score
vector comes from an actual forward pass over real weights and is checked
against the class map loaded from the model's own published artefact before it
can be returned. There is NO simulated classification, NO placeholder scores,
NO hardcoded response (BLUFF-001 anti-pattern — forbidden).

This service classifies sound: it answers "what sound is this" — a doorbell,
breaking glass, an engine, speech versus music. It does not transcribe words
(that is speech-to-text) and it does not generate audio.

That last point is structural, not incidental. Audio classification and audio
generation are separate capability families here, and this service serves only
the first. Classification is mature, cheap and runs on every host class down to
a machine with no accelerator and 8 GB of RAM. Generation has no
processor-viable option at any RAM size — the cheapest candidate surveyed still
needs roughly 4 GB of accelerator memory, and more host RAM does not substitute
for an accelerator. A host with no accelerator is owed that reason, and gets it
from `/health`, rather than an offer it cannot run.

Contract
--------
    GET  /health
        -> { status, engine_ready, catalogue_key, model, license_id,
             sample_rate, class_count, families_served,
             audio_generation_note, reason }
        Reports whether a model could be SELECTED and whether the interpreter
        is loaded, WITHOUT forcing a load. A not-ready state always carries the
        exact reason; it never fakes readiness.

    POST /v1/audio/classifications
        body: { "samples": [float...], "sample_rate": int, "top_k": int? }
        -> { model, sample_rate, class_count, top: [ {index, label, score} ],
             guard: { class_count, spread, top_index, top_score } }
        The scores are REAL. If no model can be selected, the endpoint returns
        503 with WHICH of the three reasons applies rather than a generic
        failure, and never fabricated scores.
"""

from __future__ import annotations

import os

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field
import uvicorn

import audio_engine
import helix_model_gate as gate

CATALOGUE_PATHS = [
    p.strip()
    for p in os.environ.get(
        "HELIXLLM_CATALOGUE_PATHS",
        os.pathsep.join(["/app/models.yaml", "/app/catalogue/speech_audio.yaml"]),
    ).split(os.pathsep)
    if p.strip()
]
DECLARED_PURPOSE = os.environ.get("HELIXLLM_USAGE_PURPOSE", gate.USAGE_COMMERCIAL)
LISTEN_PORT = int(os.environ.get("HELIXLLM_AUDIO_PORT", "8080"))

# What a host with no accelerator is owed INSTEAD of an audio-generation offer.
# It is the stated limit of the offer, not a withheld option.
AUDIO_GENERATION_NOTE = (
    "This service does not generate audio. No processor-viable open-weight option "
    "for music or sound-effect generation was found at any RAM size — the cheapest "
    "candidate surveyed still requires roughly 4 GB of accelerator memory, and adding "
    "system memory does not substitute for an accelerator. That is the reason, not a "
    "withheld option: audio-generation is a separate capability family, and it is "
    "unavailable on a host with no accelerator by configuration, not by resource level."
)

app = FastAPI(title="helixllm-audio-classification", version="1.0.0")

_plan: audio_engine.ClassificationPlan | None = None
_plan_error: dict | None = None
_class_names: list[str] | None = None


def _measure_host() -> gate.HostMeasurement:
    """Measure THIS host. This family is not gated behind an accelerator, so
    the absence of one is a fully supported configuration, never a failure."""
    try:
        system_free = os.sysconf("SC_PAGE_SIZE") * os.sysconf("SC_AVPHYS_PAGES")
        st = os.statvfs(os.environ.get("HELIXLLM_AUDIO_WEIGHTS_DIR", "/"))
        free_disk = st.f_bavail * st.f_frsize
    except (ValueError, OSError, AttributeError):
        # Unreadable figures mean the host was not measured, which causes a
        # refusal to choose (FR-056) rather than a guess.
        return gate.HostMeasurement(measured=False)
    return gate.HostMeasurement(
        measured=True,
        has_accelerator=False,
        accelerator_free_bytes=0,
        system_free_bytes=int(system_free),
        free_disk_bytes=int(free_disk),
    )


def get_plan() -> audio_engine.ClassificationPlan | None:
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
        _plan = audio_engine.plan_for_host(entries, _measure_host(), DECLARED_PURPOSE)
    except gate.CannotChoose as exc:
        _plan_error = exc.as_dict()
        return None
    return _plan


def get_class_names() -> list[str]:
    global _class_names
    if _class_names is None:
        _class_names = audio_engine.load_class_map(audio_engine.class_map_path())
    return _class_names


@app.get("/health")
def health():
    plan = get_plan()
    body = {
        "status": "ok",
        "engine_ready": plan is not None,
        "engine_loaded": audio_engine._engine is not None,
        "declared_usage_purpose": DECLARED_PURPOSE,
        "catalogue_paths": CATALOGUE_PATHS,
        "families_served": [gate.FAMILY_AUDIO_CLASSIFICATION],
        "audio_generation_note": AUDIO_GENERATION_NOTE,
        "weights": {
            "model": audio_engine.model_path(),
            "class_map": audio_engine.class_map_path(),
        },
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
            "class_count": plan.expected_class_count,
            "license_id": plan.license_id,
            "selection_basis": plan.selection_basis,
            "withheld": list(plan.withheld),
            "reason": audio_engine.engine_error(),
        }
    )
    return body


class ClassificationRequest(BaseModel):
    samples: list[float] = Field(..., min_length=1)
    sample_rate: int = audio_engine.YAMNET_SAMPLE_RATE
    top_k: int = 5
    # Accepted for wire symmetry with the other services and deliberately NOT
    # honoured as a selection: which model runs is decided from the measured
    # host (FR-056).
    model: str | None = None


@app.post("/v1/audio/classifications")
def classify(req: ClassificationRequest):
    plan = get_plan()
    if plan is None:
        raise HTTPException(
            status_code=503,
            detail={
                "message": (
                    "no audio-classification model could be offered for this host and "
                    "declared usage, so nothing was classified. No default model is "
                    "started here."
                ),
                **(_plan_error or {}),
            },
        )
    if req.sample_rate != plan.sample_rate:
        # Resampling silently would change what the model hears and therefore
        # what it reports. Refusing with the required rate named is honest;
        # a wrong-rate classification that looks right is not.
        raise HTTPException(
            status_code=400,
            detail=(
                f"audio is {req.sample_rate} Hz; this model expects {plan.sample_rate} Hz "
                "mono. Resample before sending — resampling here would silently change "
                "what the model hears."
            ),
        )

    try:
        interpreter = audio_engine.load_engine()
        names = get_class_names()
    except (RuntimeError, audio_engine.ClassMapError) as exc:
        raise HTTPException(
            status_code=503,
            detail={"reason": "engine_unavailable", "detail": str(exc)},
        )

    try:
        scores = audio_engine.classify(interpreter, req.samples)
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc))

    try:
        guard = gate.assert_real_scores(scores, len(names))
    except gate.SimulationSuspected as exc:
        # The interpreter returned something with the shape of a fabricated
        # response. Refuse loudly rather than hand it to the caller.
        raise HTTPException(
            status_code=500,
            detail={
                "reason": "anti_simulation_guard_tripped",
                "detail": str(exc),
                "model": plan.catalogue_key,
            },
        )

    return {
        "model": plan.catalogue_key,
        "license_id": plan.license_id,
        "sample_rate": plan.sample_rate,
        "class_count": len(names),
        "top": audio_engine.top_classes(scores, names, req.top_k),
        "guard": guard,
    }


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=LISTEN_PORT)
