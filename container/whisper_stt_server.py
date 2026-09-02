"""
Local faster-whisper CPU Speech-To-Text service — OpenAI-compatible endpoint.

Serves POST /v1/audio/transcriptions returning:
  { text, raw_text, language, language_probability, duration,
    segments: [ {start, end, text, avg_logprob, no_speech_prob,
    compression_ratio} ], max_no_speech_prob, silence_guard: {triggered,
    threshold, reason} }
Plus GET /health.

WHICH MODEL RUNS is decided by `stt_catalogue_wiring` from the MEASURED host
and the declared usage purpose over the model catalogue — never from a
configuration value naming a model (FR-056). Configuration still says where
weights live and may pin a device or precision; it no longer says which model.
When the host cannot be measured, or no catalogued entry can be served here,
the server reports that it cannot choose AND WHY, and refuses to transcribe —
it does not fall back to an arbitrary model that may not fit or may not be the
one the caller is licensed for.

Silence-hallucination + wrong-language guards (§11.4 anti-bluff): Whisper's
autoregressive decoder can invent text on silence/noise input. Two guards
compose so the analyzer downstream cannot be bluffed by a hallucinated
transcript:
  - VAD filter (Silero, via faster-whisper) drops non-speech regions before
    the decoder gets a chance to invent text.
  - A no_speech_prob threshold nulls any segment that survives VAD but the
    decoder itself flags as low-confidence-speech (the exact "silence -> a
    hallucinated word" failure mode described in the STT/OCR design research,
    docs/research/07.2026/07_stt_ocr_whisper_tesseract/…, §5 risk 1).
"""
import os
import tempfile
import threading

from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from faster_whisper import WhisperModel

import helix_model_gate as gate
import stt_catalogue_wiring as wiring

# Where the catalogue lives — a PATH, not a model name. Configuration is
# allowed to say where files are; it is not allowed to say which model runs.
CATALOGUE_PATHS = [
    p.strip()
    for p in os.environ.get(
        "HELIXLLM_CATALOGUE_PATHS",
        "/app/catalogue/speech_audio.yaml",
    ).split(os.pathsep)
    if p.strip()
]
# The purpose the OPERATOR declares this deployment's output will be put to.
# It gates which licences may be offered (FR-054); it names no model.
DECLARED_PURPOSE = os.environ.get("HELIXLLM_USAGE_PURPOSE", gate.USAGE_COMMERCIAL)
# Calibrated on the project's own fixtures (§11.4.6 measured-not-guessed) —
# see repo-root docs/qa/phase3_whisper_stt_20260707/RESULTS.md.
NO_SPEECH_THRESHOLD = float(os.environ.get("WHISPER_NO_SPEECH_THRESHOLD", "0.6"))

app = FastAPI(title="helixllm-stt", version="2.0.0")

_model = None
_plan: wiring.EnginePlan | None = None
_plan_error: dict | None = None
_lock = threading.Lock()


def get_plan() -> wiring.EnginePlan | None:
    """Resolve which model to serve, once, from the measured host.

    A failure is RECORDED rather than raised at import so /health can report
    the exact reason. It is never replaced by a default model.
    """
    global _plan, _plan_error
    if _plan is not None or _plan_error is not None:
        return _plan
    with _lock:
        if _plan is not None or _plan_error is not None:
            return _plan
        try:
            entries = gate.load_catalogue(*CATALOGUE_PATHS)
        except gate.CatalogueError as exc:
            _plan_error = {
                "reason": "catalogue_unreadable",
                "detail": str(exc),
                "catalogue_paths": CATALOGUE_PATHS,
            }
            return None
        try:
            _plan = wiring.plan_for_host(
                entries, wiring.measure_host(), purpose=DECLARED_PURPOSE
            )
        except gate.CannotChoose as exc:
            _plan_error = exc.as_dict()
            return None
        return _plan


def get_model() -> WhisperModel:
    global _model
    if _model is None:
        plan = get_plan()
        if plan is None:
            raise RuntimeError("no model was selected")
        with _lock:
            if _model is None:
                _model = WhisperModel(
                    plan.model_name,
                    device=plan.device,
                    compute_type=plan.compute_type,
                )
    return _model


@app.get("/health")
def health():
    import faster_whisper

    plan = get_plan()
    body = {
        "status": "ok",
        "faster_whisper_version": faster_whisper.__version__,
        "engine_ready": plan is not None,
        "engine_loaded": _model is not None,
        "no_speech_threshold": NO_SPEECH_THRESHOLD,
        "declared_usage_purpose": DECLARED_PURPOSE,
        "catalogue_paths": CATALOGUE_PATHS,
    }
    if plan is None:
        # Never fake readiness: carry the exact reason no model was chosen.
        body["catalogue_key"] = None
        body["model"] = None
        body["reason"] = _plan_error
        return body
    body.update(
        {
            "catalogue_key": plan.catalogue_key,
            "model": plan.model_name,
            "device": plan.device,
            "compute_type": plan.compute_type,
            "license_id": plan.license_id,
            "selection_basis": plan.selection_basis,
            "withheld": list(plan.withheld),
            "reason": None,
        }
    )
    return body


@app.post("/v1/audio/transcriptions")
async def transcriptions(
    file: UploadFile = File(...),
    model: str = Form(default=""),
    language: str = Form(default=None),
    response_format: str = Form(default="json"),
):
    # The OpenAI-compatible `model` form field is ACCEPTED for wire
    # compatibility and deliberately NOT honoured as a selection: which model
    # runs is decided from the measured host (FR-056). The response states
    # what actually ran so a caller is never misled about it.
    plan = get_plan()
    if plan is None:
        raise HTTPException(
            status_code=503,
            detail={
                "message": (
                    "no speech-to-text model could be chosen for this host, so nothing "
                    "was transcribed. No default model is started in this situation."
                ),
                **(_plan_error or {}),
            },
        )

    suffix = os.path.splitext(file.filename or "audio.wav")[1] or ".wav"
    with tempfile.NamedTemporaryFile(suffix=suffix, delete=False) as tmp:
        tmp.write(await file.read())
        tmp_path = tmp.name

    try:
        m = get_model()
        # VAD filter on: drop non-speech regions before the decoder can hallucinate.
        segments_iter, info = m.transcribe(
            tmp_path,
            language=language,
            beam_size=5,
            vad_filter=True,
            word_timestamps=False,
        )

        seg_list = []
        kept_texts = []
        max_no_speech = 0.0
        for s in segments_iter:
            nsp = float(getattr(s, "no_speech_prob", 0.0) or 0.0)
            max_no_speech = max(max_no_speech, nsp)
            seg = {
                "id": s.id,
                "start": round(s.start, 3),
                "end": round(s.end, 3),
                "text": s.text,
                "avg_logprob": round(float(s.avg_logprob), 4),
                "no_speech_prob": round(nsp, 4),
                "compression_ratio": round(float(s.compression_ratio), 4),
            }
            seg_list.append(seg)
            # Silence-hallucination guard: a segment above the no-speech floor
            # is treated as NOT real speech and excluded from the transcript.
            if nsp < NO_SPEECH_THRESHOLD:
                kept_texts.append(s.text)

        guard_triggered = len(seg_list) > 0 and len(kept_texts) == 0
        text = "".join(kept_texts).strip()

        return {
            "text": text,
            "model": plan.model_name,
            "catalogue_key": plan.catalogue_key,
            "license_id": plan.license_id,
            "raw_text": "".join(x["text"] for x in seg_list).strip(),
            "language": info.language,
            "language_probability": round(float(info.language_probability), 4),
            "duration": round(float(info.duration), 3),
            "segments": seg_list,
            "max_no_speech_prob": round(max_no_speech, 4),
            "silence_guard": {
                "triggered": guard_triggered,
                "threshold": NO_SPEECH_THRESHOLD,
                "reason": (
                    "all segments >= no_speech_prob threshold -> transcript nulled"
                    if guard_triggered
                    else "speech detected"
                ),
            },
        }
    finally:
        os.unlink(tmp_path)
