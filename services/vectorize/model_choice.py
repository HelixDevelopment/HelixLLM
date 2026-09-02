"""Measured model selection for the VECTOR-GRAPHICS and EMBEDDING lanes (T083 / FR-055 / FR-056).

The rule this module exists to enforce: WHICH model runs is decided from the
MEASURED host and the DECLARED usage purpose, never from a preconfigured value.
Configuration may still say WHERE the catalogue lives
(``HELIXLLM_CATALOGUE_PATHS``), what the operator has DECLARED about their usage
(``HELIXLLM_USAGE_PURPOSE``) and which options they FORBID
(``HELIXLLM_VECTOR_FORBID_MODELS`` / ``HELIXLLM_EMBEDDING_FORBID_MODELS``). None
of those names the model.

WHY THIS SERVES TWO FAMILIES FROM ONE FILE
------------------------------------------
``cmd/videogen-boot/modelchoice.go`` carries a DUPLICATION NOTICE recording that
it is the THIRD near-copy of the same Go decision, after ``visiongen-boot`` and
``imagegen-boot``. Writing a Go copy here would have made a fourth — and it could
not have been a *shared* one anyway: ``services/vectorize`` is its own Go module
(``dev.helix.llm.services.vectorize``) and cannot import
``internal/catalogue`` without a ``replace`` directive.

So this module is Python and uses ``container/helix_model_gate.py`` — the shared
catalogue/usage-terms vocabulary the audio family already built for exactly this
reason. Its own header records why it exists: a previous attempt split the audio
families across separate efforts and "four mutually incompatible model/licence
schemas were written, none of which could gate the others' offers". This module
adds no fifth schema. It parameterises the ONE gate by family, so vector
graphics and embeddings are decided by the same code, against the same wire
shape, with the same three reasons.

Placement is honest rather than ideal: ``services/vectorize/`` is where the
vector lane lives, and the embedding lane has no service directory of its own
yet. If one is created, this module moves wholesale — it holds nothing
vectorize-specific.

WHAT THE CATALOGUE DID NOT HAVE UNTIL NOW
-----------------------------------------
``FamilyEmbedding`` and ``FamilyVector`` were declared everywhere they had to be
— ``internal/catalogue/entry.go``'s const block, ``CapabilityFamily.Known()``,
``internal/selection/family.go``'s ``familyOrder``, this gate's
``KNOWN_FAMILIES``, ``internal/catalogue/family_test.go`` — and had ZERO entries
in ``internal/catalogue/data/``. A declared family with no candidates is not
"unconfigured", it is refused identically on every host, whatever that host
could run. ``internal/catalogue/data/embedding.yaml`` and ``vector.yaml`` are
what make the two families answerable; this module is what asks.

TWO PROPERTIES, BOTH LOAD-BEARING
---------------------------------
* There is NO fixed default. When the host cannot be measured this module says
  it cannot choose, says why, and the CLI exits non-zero. It does not fall back
  to an arbitrary model that may not fit (FR-056).
* The three withheld reasons — insufficient resources, unsupported
  configuration, excluded by usage terms — stay distinct all the way to the
  operator's shell, each with its OWN exit code, because each implies a
  different remedy (buy memory / buy an accelerator / obtain a licence) and
  collapsing them destroys the only actionable part of the answer (FR-055).

WHAT IS NOT DECIDED HERE
------------------------
The vector-graphics DEFAULT path in this repository — vtracer, in ``main.go`` —
is deliberately NOT under this decision. vtracer is an algorithm, not a model:
no weights, no licence gating a purpose, no per-host memory figure to admit
against. It is served unconditionally by the Go shim. Saying so explicitly is
the point; a reader must not conclude from this module's existence that the
default path is gated by it.
"""

from __future__ import annotations

import argparse
import math
import os
import shutil
import subprocess
import sys
from typing import Sequence

# The shared gate lives at the repository root under container/. HELIXLLM_GATE_DIR
# overrides the location for a containerised layout — a PATH, never a model.
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


FAMILY_VECTOR = "vector"
FAMILY_EMBEDDING = "embedding"
SERVED_FAMILIES = (FAMILY_VECTOR, FAMILY_EMBEDDING)

# Exit codes. The three FR-055 reasons get three DIFFERENT codes so the
# distinction survives all the way into a shell script, which is the last place
# it can still be acted on. A caller that only checks "non-zero" loses nothing;
# a caller that wants the remedy can have it without parsing prose.
EXIT_DECIDED = 0
EXIT_HOST_NOT_MEASURED = 20
EXIT_INSUFFICIENT_RESOURCES = 21
EXIT_UNSUPPORTED_CONFIGURATION = 22
EXIT_USAGE_TERMS = 23
EXIT_CATALOGUE_MISSING = 24

_EXIT_FOR_REASON = {
    gate.REASON_HOST_UNMEASURED: EXIT_HOST_NOT_MEASURED,
    gate.REASON_INSUFFICIENT_RESOURCES: EXIT_INSUFFICIENT_RESOURCES,
    gate.REASON_UNSUPPORTED_CONFIGURATION: EXIT_UNSUPPORTED_CONFIGURATION,
    gate.REASON_USAGE_TERMS: EXIT_USAGE_TERMS,
}

# Where the recorded catalogue lives in a checkout. A LOCATION, not a model
# name: it says where the candidates are described, never which of them runs.
_REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
_DEFAULT_CATALOGUE_DIR = os.path.join(_REPO_ROOT, "internal", "catalogue", "data")
_DEFAULT_CATALOGUE_FILES = ("vector.yaml", "embedding.yaml")


def catalogue_paths() -> list[str]:
    """Where the candidate catalogue is read from.

    ``HELIXLLM_CATALOGUE_PATHS`` is an ``os.pathsep``-joined list of FILE PATHS.
    It can point the decision at a different catalogue; it cannot put a model
    into one.
    """
    raw = os.environ.get("HELIXLLM_CATALOGUE_PATHS", "")
    if raw.strip():
        return [p.strip() for p in raw.split(os.pathsep) if p.strip()]
    return [os.path.join(_DEFAULT_CATALOGUE_DIR, name) for name in _DEFAULT_CATALOGUE_FILES]


def declared_purpose() -> tuple[str, bool]:
    """How the operator has said the output will be used, and whether it defaulted.

    Selection requires it: terms cannot be applied against an undeclared usage,
    and assuming a permissive one would offer models the operator may not be
    permitted to use.

    The default is the NARROWEST purpose, commercial. Defaulting narrow can only
    ever withhold an option the operator was in fact entitled to — never offer
    one they are not — and the default is always REPORTED, so the assumption is
    visible and can be widened deliberately.
    """
    raw = os.environ.get("HELIXLLM_USAGE_PURPOSE", "").strip().lower()
    if not raw:
        return gate.USAGE_COMMERCIAL, True
    if raw not in gate.KNOWN_PURPOSES:
        raise ValueError(
            f"HELIXLLM_USAGE_PURPOSE={raw!r} is not a recorded usage purpose "
            f"({', '.join(sorted(gate.KNOWN_PURPOSES))})"
        )
    return raw, False


def forbid_key(family: str) -> str:
    """The env var an operator forbids options through, for one family."""
    return f"HELIXLLM_{family.upper()}_FORBID_MODELS"


def forbidden(family: str) -> set[str]:
    """The operator's forbid-list.

    Forbidding options is a legitimate configuration act: it can only ever
    REMOVE a candidate the measurement offered, never introduce one it did not.
    That asymmetry is why this is not a violation of "configuration must not name
    a model" — a forbid-list names a model to EXCLUDE it, and an excluded model
    cannot be the one that runs.
    """
    raw = os.environ.get(forbid_key(family), "")
    return {item.strip().lower() for item in raw.split(",") if item.strip()}


def _accelerator() -> tuple[bool, int]:
    """Free accelerator memory, or (False, 0) when it cannot be READ.

    Presence of a driver is not free memory. An accelerator is only claimed when
    its USABLE free bytes can actually be read — the same discipline
    ``services/tts/tts_server.py`` applies — because claiming one whose capacity
    is unknown would admit a model against a number nobody has.

    Absence of an accelerator is a legitimate MEASURED state, not a failed
    measurement: it makes ``requires_accelerator`` entries report
    ``unsupported_configuration`` (a distinct reason with a distinct remedy)
    rather than making the whole host unmeasurable.
    """
    try:
        import torch  # noqa: PLC0415

        if torch.cuda.is_available():
            free, _total = torch.cuda.mem_get_info(0)
            return True, int(free)
    except Exception:
        pass

    smi = shutil.which("nvidia-smi")
    if not smi:
        return False, 0
    try:
        out = subprocess.run(
            [smi, "--query-gpu=memory.free", "--format=csv,noheader,nounits"],
            capture_output=True,
            text=True,
            timeout=15,
            check=True,
        ).stdout.strip()
    except (OSError, subprocess.SubprocessError):
        return False, 0
    first = out.splitlines()[0].strip() if out else ""
    if not first.isdigit():
        return False, 0
    # nvidia-smi reports MiB.
    return True, int(first) * 1024 * 1024


def measure_host(weights_dir: str | None = None) -> gate.HostMeasurement:
    """Measure THIS host.

    An unreadable system-memory or disk figure yields ``measured=False``, which
    causes a refusal to choose rather than a guess (FR-056). It is deliberately
    not softened into a zero: an absent measurement is not a small one.
    """
    probe_dir = weights_dir or os.environ.get("HELIXLLM_WEIGHTS_DIR", "/")
    try:
        system_free = os.sysconf("SC_PAGE_SIZE") * os.sysconf("SC_AVPHYS_PAGES")
        st = os.statvfs(probe_dir)
        free_disk = st.f_bavail * st.f_frsize
    except (ValueError, OSError, AttributeError):
        return gate.HostMeasurement(measured=False)

    has_accel, accel_free = _accelerator()
    return gate.HostMeasurement(
        measured=True,
        has_accelerator=has_accel,
        accelerator_free_bytes=accel_free,
        system_free_bytes=int(system_free),
        free_disk_bytes=int(free_disk),
    )


def load_entries(paths: list[str] | None = None) -> list[gate.CatalogueEntry]:
    """Read the candidate catalogue.

    A path that does not exist is an ERROR, not an empty result. Silently
    skipping a missing catalogue file is how a family becomes unofferable while
    looking merely unpopulated — the exact defect this task was opened on.
    """
    resolved = paths if paths is not None else catalogue_paths()
    missing = [p for p in resolved if not os.path.exists(p)]
    if missing:
        raise gate.CatalogueError(
            "catalogue file(s) not found: "
            + ", ".join(missing)
            + " — nothing is chosen without candidates to choose from"
        )
    return gate.load_catalogue(*resolved)


def choose(
    family: str,
    entries: list[gate.CatalogueEntry] | None = None,
    host: gate.HostMeasurement | None = None,
    purpose: str | None = None,
) -> gate.Selection:
    """Decide which model runs for one family.

    Raises ``gate.CannotChoose`` carrying WHICH of the reasons applies. There is
    no fallback to a fixed model on any path through this function.
    """
    if family not in SERVED_FAMILIES:
        raise ValueError(f"family {family!r} is not served here ({', '.join(SERVED_FAMILIES)})")

    resolved_entries = entries if entries is not None else load_entries()
    resolved_host = host if host is not None else measure_host()
    resolved_purpose = purpose if purpose is not None else declared_purpose()[0]

    # The forbid-list is applied BEFORE selection so a forbidden option is never
    # the one chosen, and is reported as an operator act rather than silently
    # vanishing into "not offered".
    forbid = forbidden(family)
    if forbid:
        kept = []
        for e in resolved_entries:
            if e.model_id.lower() in forbid or e.key.lower() in forbid:
                print(
                    f"FORBIDDEN-BY-CONFIG: {e.key} removed by {forbid_key(family)} "
                    "(operator choice, not a measurement)"
                )
                continue
            kept.append(e)
        resolved_entries = kept

    return gate.select(resolved_entries, family=family, purpose=resolved_purpose, host=resolved_host)


# --------------------------------------------------------------------------
# Anti-simulation guard for embeddings
#
# The sibling audio families assert their real output through
# gate.assert_real_waveform / gate.assert_real_scores. Embeddings had no
# equivalent, so a fabricated vector — a zero fill, a constant, a repeating
# pattern — would satisfy any caller that only checked the length. This guard is
# the embedding-shaped member of that family and belongs upstream in
# container/helix_model_gate.py beside the other two; it lives here because
# container/ is outside this change's scope. Reported, not compounded silently.
#
# Thresholds are calibrated against this repository's own shapes rather than
# taken from literature: a real dense embedding of any served width has
# essentially every component distinct, while every fabrication mode this guard
# exists to catch (zeros, a constant fill, a short repeating cycle) collapses the
# distinct-component ratio to near zero. The floor is set far below any plausible
# real vector and far above every degenerate one.
# --------------------------------------------------------------------------

MIN_DISTINCT_COMPONENT_RATIO = 0.5


def assert_real_embedding(vector: Sequence[float], expected_dimensions: int | None = None) -> dict:
    """Raise ``gate.SimulationSuspected`` unless ``vector`` looks generated.

    Deliberately an error rather than a warning: returning a fabricated vector
    with a caveat attached would put a fake in front of a caller, which is the
    exact failure this repository's anti-bluff posture forbids. A vector that
    fails here is not returned at all.
    """
    values = list(vector)
    if not values:
        raise gate.SimulationSuspected("embedding is empty; no vector was produced")

    if expected_dimensions is not None and len(values) != expected_dimensions:
        raise gate.SimulationSuspected(
            f"embedding has {len(values)} components, but the catalogued model records "
            f"{expected_dimensions}; a vector of the wrong width did not come from that model"
        )

    for i, v in enumerate(values):
        if not isinstance(v, (int, float)) or isinstance(v, bool) or not math.isfinite(float(v)):
            raise gate.SimulationSuspected(
                f"embedding component {i} is {v!r}, which is not a finite number"
            )

    norm = math.sqrt(sum(float(v) * float(v) for v in values))
    if norm == 0.0:
        raise gate.SimulationSuspected(
            "embedding is all zeros; a zero vector carries no information and cannot "
            "be the output of a real forward pass"
        )

    distinct = len({float(v) for v in values})
    ratio = distinct / len(values)
    if ratio < MIN_DISTINCT_COMPONENT_RATIO:
        raise gate.SimulationSuspected(
            f"embedding has only {distinct} distinct values across {len(values)} components "
            f"(ratio {ratio:.4f} < {MIN_DISTINCT_COMPONENT_RATIO}); a constant or short "
            "repeating fill is a fabricated vector, not a generated one"
        )

    return {
        "dimensions": len(values),
        "distinct_components": distinct,
        "distinct_ratio": ratio,
        "l2_norm": norm,
    }


def _report_host(host: gate.HostMeasurement) -> None:
    if not host.measured:
        print("MEASURE-INCOMPLETE: this host could not be measured")
        return
    print(
        "MEASURED "
        f"memory_available={host.system_free_bytes // (1024 * 1024)}MiB "
        f"storage_available={host.free_disk_bytes // (1024 * 1024)}MiB "
        f"accelerator={'yes' if host.has_accelerator else 'none'} "
        f"accelerator_free={host.accelerator_free_bytes // (1024 * 1024)}MiB"
    )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="model_choice",
        description=(
            "Decide which vector-graphics or embedding model runs, from the measured "
            "host and the declared usage purpose. There is no default model."
        ),
    )
    parser.add_argument("--family", choices=SERVED_FAMILIES, required=True)
    args = parser.parse_args(argv)
    family = args.family

    try:
        purpose, defaulted = declared_purpose()
    except ValueError as exc:
        print(f"CANNOT-CHOOSE: {exc}")
        return EXIT_USAGE_TERMS
    if defaulted:
        print(
            f"DECLARED-USAGE: {purpose} (default — the narrowest purpose; "
            "set HELIXLLM_USAGE_PURPOSE to declare another)"
        )
    else:
        print(f"DECLARED-USAGE: {purpose} (declared)")

    try:
        entries = load_entries()
    except gate.CatalogueError as exc:
        print(f"CANNOT-CHOOSE: the catalogue of candidates could not be read ({exc}).")
        print("  No model is started without candidates to choose from.")
        return EXIT_CATALOGUE_MISSING

    host = measure_host()
    _report_host(host)

    try:
        selection = choose(family, entries=entries, host=host, purpose=purpose)
    except gate.CannotChoose as exc:
        print(f"CANNOT-CHOOSE ({family}): {exc.detail}")
        for w in exc.withheld:
            print(f"  WITHHELD {w.entry_key}: {w.kind} — {w.detail}")
        print(
            "  No model is started: there is deliberately no default, because a model "
            "that was not chosen from a measurement may not fit this host (FR-056)."
        )
        return _EXIT_FOR_REASON.get(exc.kind, EXIT_INSUFFICIENT_RESOURCES)

    for w in selection.withheld:
        print(f"WITHHELD {w.entry_key}: {w.kind} — {w.detail}")
    print(
        f"DECIDED {family}: {selection.entry.key} "
        f"(memory={selection.entry.memory_required_bytes} B, "
        f"storage={selection.entry.storage_required_bytes} B, "
        f"licence={selection.entry.usage_terms.license_id}, purpose={selection.purpose})"
    )
    print(f"  considered: {', '.join(selection.considered)}")
    return EXIT_DECIDED


if __name__ == "__main__":  # pragma: no cover - CLI entry point
    raise SystemExit(main())
