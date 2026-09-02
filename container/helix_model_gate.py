"""One schema for serving a catalogued audio-capability model.

This module is the SINGLE shared vocabulary for the three audio capability
families served out of this repository — speech-to-text (wired in
`container/stt_catalogue_wiring.py`), text-to-speech (`services/tts/`) and
audio-classification (`services/audio/`). It exists because those three were
built together on purpose: the previous attempt split them across separate
efforts and four mutually incompatible model/licence schemas were written,
none of which could gate the others' offers.

It deliberately does three things and no more:

  1. Loads catalogue entries in the wire shape of
     `internal/catalogue/data/*.yaml`, and refuses a record it cannot read
     honestly (an absent resource figure is NOT a small one).
  2. Gates an entry against a DECLARED USAGE PURPOSE and a MEASURED HOST,
     returning one of the three distinct unavailability reasons FR-055
     requires rather than a single generic "unavailable".
  3. Provides the two anti-simulation guards the synthesising and classifying
     services assert their real output against.

It performs NO inference, holds NO model weights, and names NO model as a
default. Which model runs is decided by `select()` from the measured host and
the declared purpose (FR-056) — there is no configuration value here that
names a model, and no fixed fallback when measurement is unavailable.

Field names mirror `internal/catalogue/entry.go` exactly (UsagePurpose,
RestrictionTerm, UsageTerms.Permits, UsageTerms.RestrictionFor,
CapabilityFamily) so the Go catalogue and these Python services cannot drift
into disagreeing about what a licence permits. Where Go and this module
differ, Go is the source of truth and this module is the defect.

ONE RECORDED EXCEPTION, resolved the other way (2026-09-02). The two paths
once disagreed about WHICH admissible entry wins: this module ranked
cheapest-first, while the Go boot lanes ranked most-capable-first, so the same
host chose a q4_k_m build here and an f16 build there. That was resolved by
changing GO TO MATCH THIS MODULE, not the reverse, because the cheapest option
that genuinely runs is the one that leaves headroom for CO-RESIDENT models: a
host serves a coder model beside a vision or video one on the same
accelerator (see internal/vrambroker), and taking the largest thing that fits
is how it ends up unable to load the second. The ordering now lives in Go at
internal/selection (FamilyResult.Offered comes back ordered) rather than in
each boot lane, and uses the same key select() uses below — memory, then
storage, then the entry key.

So: do NOT "fix" the ranking here to match an older most-capable-first Go
lane. The rule above is the agreed one on both sides; a Go path that ranks
largest-first is the defect.
"""

from __future__ import annotations

import dataclasses
import math
import os
from typing import Any, Iterable, Mapping, Sequence


# --------------------------------------------------------------------------
# Capability families — mirrors internal/catalogue/entry.go CapabilityFamily.
#
# audio-classification and audio-generation are SEPARATE families and must
# stay separate: classification runs on any processor, generation has no
# processor-viable option at all. Merging them would either offer a
# no-accelerator host something it cannot run, or hide generation entirely.
# --------------------------------------------------------------------------

FAMILY_SPEECH_TO_TEXT = "speech-to-text"
FAMILY_TEXT_TO_SPEECH = "text-to-speech"
FAMILY_AUDIO_CLASSIFICATION = "audio-classification"
FAMILY_AUDIO_GENERATION = "audio-generation"

KNOWN_FAMILIES = frozenset(
    {
        "text",
        "vision",
        "image-generation",
        "video-generation",
        FAMILY_SPEECH_TO_TEXT,
        FAMILY_TEXT_TO_SPEECH,
        FAMILY_AUDIO_GENERATION,
        FAMILY_AUDIO_CLASSIFICATION,
        "embedding",
        "vector",
    }
)

# Usage purposes — mirrors entry.go UsagePurpose.
USAGE_COMMERCIAL = "commercial"
USAGE_PERSONAL = "personal"
USAGE_RESEARCH = "research"
USAGE_EVALUATION = "evaluation"

KNOWN_PURPOSES = frozenset(
    {USAGE_COMMERCIAL, USAGE_PERSONAL, USAGE_RESEARCH, USAGE_EVALUATION}
)

# The three distinct unavailability reasons FR-055 requires be told apart,
# plus the FR-056 refusal-to-choose. They have different remedies: buy RAM,
# buy an accelerator, obtain a licence, or fix the measurement.
REASON_INSUFFICIENT_RESOURCES = "insufficient_resources"
REASON_UNSUPPORTED_CONFIGURATION = "unsupported_configuration"
REASON_USAGE_TERMS = "usage_terms_excluded"
REASON_HOST_UNMEASURED = "host_unmeasured"


class CatalogueError(ValueError):
    """A catalogue record could not be read honestly, so it is not an entry."""


class SimulationSuspected(RuntimeError):
    """Output failed an anti-simulation guard and MUST NOT be returned.

    Raised when a produced waveform or score vector has the shape of a
    fabricated response rather than a generated one. It is deliberately an
    error and not a warning: returning the output with a caveat would put a
    fake in front of a caller, which is the exact failure this repository's
    anti-bluff posture forbids.
    """


class CannotChoose(RuntimeError):
    """No model can be offered, carrying WHICH of the reasons applies."""

    def __init__(self, kind: str, detail: str, withheld: Sequence["Withheld"] = ()):
        super().__init__(f"{kind}: {detail}")
        self.kind = kind
        self.detail = detail
        self.withheld = tuple(withheld)

    def as_dict(self) -> dict:
        return {
            "reason": self.kind,
            "detail": self.detail,
            "withheld": [dataclasses.asdict(w) for w in self.withheld],
        }


# --------------------------------------------------------------------------
# Usage terms
# --------------------------------------------------------------------------


@dataclasses.dataclass(frozen=True)
class Restriction:
    """One constraint from a licence.

    `excludes` is what makes a restriction actionable. A restriction that
    excludes no purpose (CC-BY attribution, say) constrains how output is
    used but withholds nothing — reporting it as a reason an entry was not
    offered would be simply false.
    """

    term: str
    excludes: tuple[str, ...] = ()
    reference: str = ""

    def excludable(self) -> bool:
        return bool(self.excludes)


@dataclasses.dataclass(frozen=True)
class UsageTerms:
    license_id: str
    permitted: tuple[str, ...] = ()
    restrictions: tuple[Restriction, ...] = ()

    def restriction_for(self, purpose: str) -> Restriction | None:
        """The restriction that excludes `purpose`, so a caller can name it."""
        for r in self.restrictions:
            if purpose in r.excludes:
                return r
        return None

    def permits(self, purpose: str) -> bool:
        """An exclusionary restriction beats the grant.

        A licence that both lists a purpose as permitted AND carries a term
        excluding it is read as EXCLUDING it. Reading it the other way round
        is precisely how a non-commercial model reaches a commercial user.
        """
        if self.restriction_for(purpose) is not None:
            return False
        return purpose in self.permitted


@dataclasses.dataclass(frozen=True)
class CatalogueEntry:
    model_id: str
    family: str
    usage_terms: UsageTerms
    memory_required_bytes: int
    storage_required_bytes: int
    variant: str = ""
    requires_accelerator: bool = False
    runtime: str = "in-memory"
    annotations: Mapping[str, Any] = dataclasses.field(default_factory=dict)
    notes: tuple[str, ...] = ()
    source_path: str = ""

    @property
    def key(self) -> str:
        return f"{self.model_id}:{self.variant}" if self.variant else self.model_id


# --------------------------------------------------------------------------
# Host measurement
# --------------------------------------------------------------------------


@dataclasses.dataclass(frozen=True)
class HostMeasurement:
    """What the host actually has right now, not what it nominally has.

    `measured` false is a first-class state, not a zero: an unmeasured host
    causes a refusal to choose (FR-056), never a fallback to a fixed model.
    """

    measured: bool
    has_accelerator: bool = False
    accelerator_free_bytes: int = 0
    system_free_bytes: int = 0
    free_disk_bytes: int = 0

    def usable_memory_for(self, entry: CatalogueEntry) -> int:
        """The memory axis an entry is actually admitted against.

        When an accelerator is present it is a first-class input and system
        memory alone must not decide (FR-002).
        """
        if entry.requires_accelerator or (self.has_accelerator and self.accelerator_free_bytes > 0):
            return self.accelerator_free_bytes
        return self.system_free_bytes


@dataclasses.dataclass(frozen=True)
class Decision:
    allowed: bool
    kind: str = ""
    detail: str = ""
    term: str = ""
    reference: str = ""


@dataclasses.dataclass(frozen=True)
class Withheld:
    entry_key: str
    kind: str
    detail: str
    term: str = ""
    reference: str = ""


@dataclasses.dataclass(frozen=True)
class Selection:
    entry: CatalogueEntry
    purpose: str
    considered: tuple[str, ...]
    withheld: tuple[Withheld, ...]


# --------------------------------------------------------------------------
# The gate
# --------------------------------------------------------------------------


def evaluate(entry: CatalogueEntry, purpose: str, host: HostMeasurement) -> Decision:
    """Decide whether `entry` may be offered, and if not, precisely why.

    Order matters. Usage terms are checked FIRST: a model a caller may not
    lawfully use should be reported as licence-excluded even on a host that
    could not have run it anyway, because "obtain a licence" and "buy more
    memory" are different remedies and the licence one is the true blocker.
    """
    if purpose not in KNOWN_PURPOSES:
        raise ValueError(f"unknown usage purpose {purpose!r}")

    restriction = entry.usage_terms.restriction_for(purpose)
    if restriction is not None:
        return Decision(
            allowed=False,
            kind=REASON_USAGE_TERMS,
            detail=(
                f"{entry.key} may not be used for {purpose}: its licence "
                f"({entry.usage_terms.license_id}) carries the term "
                f"'{restriction.term}'"
            ),
            term=restriction.term,
            reference=restriction.reference,
        )
    if not entry.usage_terms.permits(purpose):
        return Decision(
            allowed=False,
            kind=REASON_USAGE_TERMS,
            detail=(
                f"{entry.key} grants no {purpose} use: its licence "
                f"({entry.usage_terms.license_id}) permits only "
                f"{', '.join(entry.usage_terms.permitted) or 'nothing recorded'}"
            ),
            term=f"{purpose}-not-granted",
            reference=entry.usage_terms.license_id,
        )

    if entry.requires_accelerator and not host.has_accelerator:
        return Decision(
            allowed=False,
            kind=REASON_UNSUPPORTED_CONFIGURATION,
            detail=(
                f"{entry.key} requires an accelerator and this host has none. "
                "More system memory does not substitute for one."
            ),
        )

    available_memory = host.usable_memory_for(entry)
    if entry.memory_required_bytes > available_memory:
        return Decision(
            allowed=False,
            kind=REASON_INSUFFICIENT_RESOURCES,
            detail=(
                f"{entry.key} needs {entry.memory_required_bytes} bytes of memory; "
                f"{available_memory} bytes are free"
            ),
        )
    # Storage is a SEPARATE axis and is never derived from the memory figure:
    # a model's weight file size is not implied by its runtime footprint.
    if entry.storage_required_bytes > host.free_disk_bytes:
        return Decision(
            allowed=False,
            kind=REASON_INSUFFICIENT_RESOURCES,
            detail=(
                f"{entry.key} needs {entry.storage_required_bytes} bytes on disk; "
                f"{host.free_disk_bytes} bytes are free"
            ),
        )
    return Decision(allowed=True)


def select(
    entries: Iterable[CatalogueEntry],
    family: str,
    purpose: str,
    host: HostMeasurement,
) -> Selection:
    """Choose which model runs, from the measured host and declared purpose.

    Raises CannotChoose carrying the specific reason. There is no fallback to
    a fixed model: an unmeasured host reports that it cannot choose and why
    (FR-056), and a family with no admissible entry reports which of the three
    FR-055 reasons applies rather than a generic unavailability.
    """
    if not host.measured:
        raise CannotChoose(
            REASON_HOST_UNMEASURED,
            "the host could not be measured, so no model can be chosen; "
            "there is deliberately no default model to fall back to",
        )

    in_family = [e for e in entries if e.family == family]
    if not in_family:
        raise CannotChoose(
            REASON_UNSUPPORTED_CONFIGURATION,
            f"no catalogued model serves the {family} capability on any host",
        )

    admissible: list[CatalogueEntry] = []
    withheld: list[Withheld] = []
    for e in in_family:
        d = evaluate(e, purpose, host)
        if d.allowed:
            admissible.append(e)
        else:
            withheld.append(
                Withheld(
                    entry_key=e.key,
                    kind=d.kind,
                    detail=d.detail,
                    term=d.term,
                    reference=d.reference,
                )
            )

    if not admissible:
        kinds = {w.kind for w in withheld}
        if kinds == {REASON_USAGE_TERMS}:
            terms = sorted({w.term for w in withheld if w.term})
            raise CannotChoose(
                REASON_USAGE_TERMS,
                (
                    f"every {family} model this host could otherwise run is excluded "
                    f"from {purpose} use by its licence terms: {', '.join(terms)}"
                ),
                withheld,
            )
        if kinds == {REASON_UNSUPPORTED_CONFIGURATION}:
            raise CannotChoose(
                REASON_UNSUPPORTED_CONFIGURATION,
                f"no {family} model supports this host's configuration",
                withheld,
            )
        if kinds == {REASON_INSUFFICIENT_RESOURCES}:
            raise CannotChoose(
                REASON_INSUFFICIENT_RESOURCES,
                f"this host lacks the resources every {family} model needs",
                withheld,
            )
        raise CannotChoose(
            REASON_INSUFFICIENT_RESOURCES,
            (
                f"no {family} model can be offered; the entries were withheld for "
                f"differing reasons: {', '.join(sorted(kinds))}"
            ),
            withheld,
        )

    # Rank by measured cost, never by name: the cheapest entry that genuinely
    # runs here wins, with a deterministic tiebreak so the same host makes the
    # same choice twice.
    admissible.sort(key=lambda e: (e.memory_required_bytes, e.storage_required_bytes, e.key))
    return Selection(
        entry=admissible[0],
        purpose=purpose,
        considered=tuple(e.key for e in in_family),
        withheld=tuple(withheld),
    )


# --------------------------------------------------------------------------
# Catalogue loading
# --------------------------------------------------------------------------


def _require_positive_int(mapping: Mapping[str, Any], field: str, where: str) -> int:
    if field not in mapping or mapping[field] is None:
        raise CatalogueError(f"{where}: {field} is absent; an absent measurement is not a small one")
    try:
        value = int(mapping[field])
    except (TypeError, ValueError) as exc:
        raise CatalogueError(f"{where}: {field} is not an integer ({exc})") from exc
    if value <= 0:
        raise CatalogueError(
            f"{where}: {field} is {value}; a zero or negative requirement would be "
            "compared against free space as though the model needed none"
        )
    return value


def entry_from_mapping(mapping: Mapping[str, Any], source_path: str = "") -> CatalogueEntry:
    where = f"{source_path or '<catalogue>'}:{mapping.get('model_id', '<no model_id>')}"

    model_id = str(mapping.get("model_id") or "").strip()
    if not model_id:
        raise CatalogueError(f"{where}: model_id is required")

    family = str(mapping.get("family") or "").strip()
    if family not in KNOWN_FAMILIES:
        raise CatalogueError(f"{where}: family {family!r} is not a recorded capability family")

    terms_raw = mapping.get("usage_terms") or {}
    permitted = tuple(str(p) for p in (terms_raw.get("permitted") or ()))
    if not permitted:
        raise CatalogueError(f"{where}: usage_terms permits no usage purpose")
    for p in permitted:
        if p not in KNOWN_PURPOSES:
            raise CatalogueError(f"{where}: unknown permitted purpose {p!r}")

    restrictions = []
    for r in terms_raw.get("restrictions") or ():
        excludes = tuple(str(x) for x in (r.get("excludes") or ()))
        for x in excludes:
            if x not in KNOWN_PURPOSES:
                raise CatalogueError(f"{where}: restriction excludes unknown purpose {x!r}")
        restrictions.append(
            Restriction(
                term=str(r.get("term") or ""),
                excludes=excludes,
                reference=str(r.get("reference") or ""),
            )
        )

    return CatalogueEntry(
        model_id=model_id,
        variant=str(mapping.get("variant") or ""),
        family=family,
        usage_terms=UsageTerms(
            license_id=str(terms_raw.get("license_id") or ""),
            permitted=permitted,
            restrictions=tuple(restrictions),
        ),
        requires_accelerator=bool(mapping.get("requires_accelerator", False)),
        memory_required_bytes=_require_positive_int(mapping, "memory_required_bytes", where),
        storage_required_bytes=_require_positive_int(mapping, "storage_required_bytes", where),
        runtime=str(mapping.get("runtime") or "in-memory"),
        annotations=dict(mapping.get("annotations") or {}),
        notes=tuple(str(n) for n in (mapping.get("notes") or ())),
        source_path=source_path,
    )


def load_catalogue(*paths: str) -> list[CatalogueEntry]:
    """Load entries from one or more catalogue files in the repo wire shape.

    Several paths may be given, and they are unioned with an earlier path
    winning on a duplicate (model_id, variant). This is how a service reads
    BOTH the repository catalogue and its own bundled catalogue of the models
    it can actually serve — they are the same kind of thing in the same
    schema, not a source and a fallback.
    """
    import yaml  # PyYAML; declared in each consuming service's requirements

    seen: dict[tuple[str, str], CatalogueEntry] = {}
    ordered: list[CatalogueEntry] = []
    for path in paths:
        if not path:
            continue
        if not os.path.exists(path):
            raise CatalogueError(f"catalogue file not found: {path}")
        with open(path, "r", encoding="utf-8") as fh:
            doc = yaml.safe_load(fh) or {}
        for raw in doc.get("entries") or ():
            entry = entry_from_mapping(raw, source_path=path)
            k = (entry.model_id, entry.variant)
            if k in seen:
                continue
            seen[k] = entry
            ordered.append(entry)
    return ordered


# --------------------------------------------------------------------------
# Anti-simulation guards
# --------------------------------------------------------------------------

# Calibrated against this repository's own fixtures rather than taken from
# literature: a synthesised utterance shorter than this cannot be a spoken
# response to any real prompt, and the distinct-sample floor is set where the
# sine fixture in the test suite sits comfortably above and every constant or
# near-constant buffer sits below.
MIN_SPEECH_SECONDS = 0.05
MIN_DISTINCT_SAMPLE_RATIO = 0.001


def assert_real_waveform(samples: Sequence[float], sample_rate: int) -> dict:
    """Refuse a waveform that has the shape of a fabricated response.

    A generated utterance varies and has duration. Silence, a DC constant, and
    a three-sample stub do not, and are exactly what a placeholder returns.
    Raising here is the point: a caller must never receive a fake waveform
    with a caveat attached.
    """
    if sample_rate <= 0:
        raise SimulationSuspected(f"sample_rate {sample_rate} is not a real rate")
    n = len(samples)
    if n == 0:
        raise SimulationSuspected("no audio was produced — an empty waveform is not speech")

    duration = n / float(sample_rate)
    if duration < MIN_SPEECH_SECONDS:
        raise SimulationSuspected(
            f"produced {duration:.4f}s of audio, below the {MIN_SPEECH_SECONDS}s floor — "
            "too short to be a synthesised utterance"
        )

    peak = max(abs(float(s)) for s in samples)
    if peak == 0.0:
        raise SimulationSuspected("waveform is entirely silent — no synthesis occurred")

    mean = sum(float(s) for s in samples) / n
    variance = sum((float(s) - mean) ** 2 for s in samples) / n
    if variance == 0.0:
        raise SimulationSuspected(
            "waveform is a constant value — a DC buffer is not synthesised speech"
        )

    distinct_ratio = len({round(float(s), 6) for s in samples}) / n
    if distinct_ratio < MIN_DISTINCT_SAMPLE_RATIO:
        raise SimulationSuspected(
            f"waveform has only {distinct_ratio:.6f} distinct samples per sample — "
            "too degenerate to be synthesised audio"
        )

    return {
        "sample_count": n,
        "sample_rate": sample_rate,
        "duration_seconds": duration,
        "peak": peak,
        "rms": math.sqrt(sum(float(s) ** 2 for s in samples) / n),
        "distinct_ratio": distinct_ratio,
    }


def assert_real_scores(scores: Sequence[float], class_count: int) -> dict:
    """Refuse a score vector that has the shape of a fabricated response.

    The length must match the class map actually loaded from the model's own
    artefact — a hand-written stub will not happen to be 521 long — and the
    distribution must have spread, because a real classifier over real audio
    does not return a uniform or all-zero vector.
    """
    n = len(scores)
    if class_count <= 0:
        raise SimulationSuspected("no class map was loaded, so no score can be attributed")
    if n != class_count:
        raise SimulationSuspected(
            f"produced {n} scores but the loaded class map has {class_count} classes — "
            "the scores did not come from the loaded model"
        )
    values = [float(s) for s in scores]
    if max(values) <= 0.0:
        raise SimulationSuspected("every score is zero — no classification occurred")
    spread = max(values) - min(values)
    if spread == 0.0:
        raise SimulationSuspected(
            "every class scored identically — a uniform vector is not a classification"
        )
    top = max(range(n), key=lambda i: values[i])
    return {
        "class_count": n,
        "spread": spread,
        "top_index": top,
        "top_score": values[top],
    }
