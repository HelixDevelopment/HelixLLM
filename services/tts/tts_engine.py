"""Text-to-speech engine: catalogue selection, real synthesis, WAV encoding.

Separated from the HTTP shim on purpose. Everything here except the actual
forward pass is importable with nothing but the standard library plus PyYAML,
so the licence gate and the anti-simulation guard — the two load-bearing
invariants of this service — are testable on any host, not only inside a built
container. Invariants that can only be tested where nobody runs tests are not
invariants.

The engine is Kokoro-82M through onnxruntime on the processor. That choice is
not hardcoded here: it is what `plan_for_host` selects from the catalogue for a
commercial caller on a machine with no accelerator, and it is selected because
its Apache-2.0 licence is the only permissive one among the text-to-speech
models this repository can currently serve. `resolve()` maps whichever entry
selection produced onto a runnable engine, and REFUSES by name when it cannot —
substituting a different model for the one that was selected would silently
serve the wrong licence.

NO SIMULATED SPEECH. Every returned waveform comes from a real forward pass and
is checked by `gate.assert_real_waveform` before it can be encoded. Silence, a
DC constant, and a too-short stub are rejected, because those are the shapes a
fabricated response takes.
"""

from __future__ import annotations

import dataclasses
import io
import os
import struct
import sys
import threading
import wave
from typing import Iterable, Sequence

# The shared schema lives with the speech-to-text wiring; the three audio
# families deliberately share one vocabulary rather than each inventing a
# licence model of their own.
sys.path.insert(
    0,
    os.environ.get(
        "HELIXLLM_GATE_DIR",
        os.path.join(
            os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))),
            "container",
        ),
    ),
)

import helix_model_gate as gate  # noqa: E402


class EngineUnsupported(RuntimeError):
    """A catalogued entry is real, but this service cannot run it."""


@dataclasses.dataclass(frozen=True)
class SynthesisPlan:
    catalogue_key: str
    engine: str
    sample_rate: int
    license_id: str
    purpose: str = ""
    selection_basis: str = ""
    withheld: tuple[dict, ...] = ()

    def as_dict(self) -> dict:
        d = dataclasses.asdict(self)
        d["withheld"] = list(self.withheld)
        return d


# Kokoro's ONNX graph emits 24 kHz mono. This is a property of the published
# artefact, not a tunable.
_KOKORO_SAMPLE_RATE = 24000


def resolve(entry: gate.CatalogueEntry) -> SynthesisPlan:
    """Map one already-selected catalogue entry onto a runnable engine."""
    if entry.family != gate.FAMILY_TEXT_TO_SPEECH:
        raise EngineUnsupported(
            f"{entry.key} is a {entry.family} model; this service serves "
            f"{gate.FAMILY_TEXT_TO_SPEECH} only"
        )
    if entry.model_id.lower().startswith("kokoro"):
        return SynthesisPlan(
            catalogue_key=entry.key,
            engine="kokoro-onnx",
            sample_rate=_KOKORO_SAMPLE_RATE,
            license_id=entry.usage_terms.license_id,
            selection_basis="resolved from a supplied catalogue entry",
        )
    raise EngineUnsupported(
        f"{entry.key} cannot be served by this service: it carries a Kokoro ONNX "
        f"runtime, and '{entry.model_id}' needs an engine this container does not "
        "have. Refusing rather than substituting a different model — a substitute "
        "would also be a substitute licence."
    )


def plan_for_host(
    entries: Iterable[gate.CatalogueEntry],
    host: gate.HostMeasurement,
    purpose: str = gate.USAGE_COMMERCIAL,
) -> SynthesisPlan:
    """Measured host + declared purpose -> a runnable synthesis plan.

    Entries the gate admits but this service cannot run are dropped and the
    selection retried, so an un-runnable entry does not mask a runnable one.
    When nothing is left, the specific reason is raised — never a default.
    """
    candidates = list(entries)
    tried: list[str] = []
    while True:
        selection = gate.select(candidates, gate.FAMILY_TEXT_TO_SPEECH, purpose, host)
        try:
            plan = resolve(selection.entry)
        except EngineUnsupported as exc:
            tried.append(f"{selection.entry.key}: {exc}")
            candidates = [e for e in candidates if e.key != selection.entry.key]
            if not any(e.family == gate.FAMILY_TEXT_TO_SPEECH for e in candidates):
                raise gate.CannotChoose(
                    gate.REASON_UNSUPPORTED_CONFIGURATION,
                    (
                        "every text-to-speech model this host could run and this caller "
                        "may use needs an engine this service does not carry: "
                        + " | ".join(tried)
                    ),
                    selection.withheld,
                ) from exc
            continue
        return dataclasses.replace(
            plan,
            purpose=purpose,
            selection_basis=(
                "measured host + declared usage purpose over the model catalogue "
                "(no configured model name)"
            ),
            withheld=tuple(dataclasses.asdict(w) for w in selection.withheld),
        )


# --------------------------------------------------------------------------
# Weights
# --------------------------------------------------------------------------


def weight_paths() -> dict:
    """Where the weight files live. Configuration says WHERE, never WHICH."""
    root = os.environ.get("HELIXLLM_TTS_WEIGHTS_DIR", "/models/tts")
    return {
        "model": os.path.join(root, os.environ.get("HELIXLLM_TTS_MODEL_FILE", "kokoro-v1.0.onnx")),
        "voices": os.path.join(root, os.environ.get("HELIXLLM_TTS_VOICES_FILE", "voices-v1.0.bin")),
    }


_engine = None
_engine_error: str | None = None
_engine_lock = threading.Lock()


def load_engine():
    """Load the REAL ONNX engine. A failure is recorded, never faked."""
    global _engine, _engine_error
    if _engine is not None:
        return _engine
    with _engine_lock:
        if _engine is not None:
            return _engine
        paths = weight_paths()
        missing = [p for p in paths.values() if not os.path.exists(p)]
        if missing:
            _engine_error = (
                f"model weights are not present: {missing}. Obtain them via the "
                "service's .gitignore-meta regeneration manifest."
            )
            raise RuntimeError(_engine_error)
        try:
            from kokoro_onnx import Kokoro
        except Exception as exc:
            _engine_error = f"the kokoro-onnx runtime is not installed: {exc}"
            raise RuntimeError(_engine_error) from exc
        _engine = Kokoro(paths["model"], paths["voices"])
        _engine_error = None
        return _engine


def engine_error() -> str | None:
    return _engine_error


DEFAULT_VOICE = os.environ.get("HELIXLLM_TTS_VOICE", "af_heart")
DEFAULT_LANGUAGE = os.environ.get("HELIXLLM_TTS_LANGUAGE", "en-us")


def synthesise(engine, text: str, voice: str | None = None, speed: float = 1.0):
    """Run a REAL forward pass. Returns (samples, sample_rate).

    A voice is a rendering preference, not a model choice — selecting among the
    54 voices baked into the one selected model does not name a model.
    """
    if not text or not text.strip():
        raise ValueError("no text to synthesise")
    samples, sample_rate = engine.create(
        text,
        voice=voice or DEFAULT_VOICE,
        speed=float(speed),
        lang=DEFAULT_LANGUAGE,
    )
    return samples, int(sample_rate)


# --------------------------------------------------------------------------
# Encoding
# --------------------------------------------------------------------------


def encode_wav(samples: Sequence[float], sample_rate: int) -> bytes:
    """Encode float samples to 16-bit mono PCM WAV.

    The anti-simulation guard runs BEFORE encoding, so a fabricated buffer
    cannot become a WAV file at all — there is no path by which a caller
    receives fake audio with a warning attached.
    """
    gate.assert_real_waveform(samples, sample_rate)

    frames = bytearray()
    for s in samples:
        v = float(s)
        if v > 1.0:
            v = 1.0
        elif v < -1.0:
            v = -1.0
        frames += struct.pack("<h", int(round(v * 32767.0)))

    buf = io.BytesIO()
    with wave.open(buf, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(int(sample_rate))
        w.writeframes(bytes(frames))
    return buf.getvalue()
