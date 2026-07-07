"""
Local faster-whisper CPU Speech-To-Text service — OpenAI-compatible endpoint.

Serves POST /v1/audio/transcriptions returning:
  { text, raw_text, language, language_probability, duration,
    segments: [ {start, end, text, avg_logprob, no_speech_prob,
    compression_ratio} ], max_no_speech_prob, silence_guard: {triggered,
    threshold, reason} }
Plus GET /health.

CPU-only (device=cpu, compute_type=int8) — the GPU is reserved for the
main-stream fleet-model proof (helixllm-coder, :18434). Model + compute type
are env-configurable (CONST-045/046: no hardcoded host/model literal here —
values come from the environment injected by the compose file / harness).

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

from fastapi import FastAPI, File, Form, UploadFile
from faster_whisper import WhisperModel

MODEL_NAME = os.environ.get("WHISPER_MODEL", "base")
DEVICE = os.environ.get("WHISPER_DEVICE", "cpu")
COMPUTE_TYPE = os.environ.get("WHISPER_COMPUTE_TYPE", "int8")
# Calibrated on the project's own fixtures (§11.4.6 measured-not-guessed) —
# see repo-root docs/qa/phase3_whisper_stt_20260707/RESULTS.md.
NO_SPEECH_THRESHOLD = float(os.environ.get("WHISPER_NO_SPEECH_THRESHOLD", "0.6"))

app = FastAPI(title="helixllm-stt", version="1.0.0")
_model = None


def get_model() -> WhisperModel:
    global _model
    if _model is None:
        _model = WhisperModel(MODEL_NAME, device=DEVICE, compute_type=COMPUTE_TYPE)
    return _model


@app.get("/health")
def health():
    import faster_whisper

    return {
        "status": "ok",
        "faster_whisper_version": faster_whisper.__version__,
        "model": MODEL_NAME,
        "device": DEVICE,
        "compute_type": COMPUTE_TYPE,
        "no_speech_threshold": NO_SPEECH_THRESHOLD,
    }


@app.post("/v1/audio/transcriptions")
async def transcriptions(
    file: UploadFile = File(...),
    model: str = Form(default=MODEL_NAME),
    language: str = Form(default=None),
    response_format: str = Form(default="json"),
):
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
