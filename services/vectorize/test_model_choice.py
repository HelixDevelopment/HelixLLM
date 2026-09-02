"""Tests for measured selection of the VECTOR-GRAPHICS and EMBEDDING lanes.

RED-FIRST POLARITY (§11.4.115)
------------------------------
The tests marked below carry a single polarity switch, ``RED_MODE``:

    RED_MODE=1  reproduce the defect on the PRE-FIX artifact and assert it is
                PRESENT. For the catalogue-coverage tests the pre-fix artifact
                is real and still reachable: it is the catalogue as it stood
                before this change, i.e. the same data/ directory with
                embedding.yaml and vector.yaml absent. Those runs load exactly
                the files that existed before and assert the genuine pre-fix
                failure — a declared family refused on every host for want of a
                single candidate.

    RED_MODE=0  (the default) the standing GREEN regression guard: the same
                assertions inverted, run against the shipped catalogue.

One source, two roles. The bug-catcher IS the regression guard; there is no
separate happy-path test standing in for the RED, which is the substitution
§11.4.43/§11.4.115 exist to forbid.

WHAT IS REAL AND WHAT SKIPS
---------------------------
Everything about the DECISION is exercised for real: the real shipped catalogue,
the real shared gate, the real host measurement. Nothing is stubbed.

Real INFERENCE needs weights, and no weight file has been downloaded on this
host — correctly, since both catalogued models are refused by
``Entry.ValidateForAcquisition`` while no digest has been captured. The
inference test therefore SKIPS WITH A STATED REASON rather than asserting
anything about a model that never ran. The analyzer that inference test would
use is NOT skipped: it is self-validated here against golden-good and
golden-bad vectors (§11.4.107(10)), so the guard is proven to have teeth even on
a host that cannot run the model.
"""

from __future__ import annotations

import contextlib
import json
import os
import subprocess
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(os.path.dirname(HERE))
sys.path.insert(0, HERE)
sys.path.insert(0, os.path.join(REPO_ROOT, "container"))

import helix_model_gate as gate  # noqa: E402
import model_choice  # noqa: E402

DATA_DIR = os.path.join(REPO_ROOT, "internal", "catalogue", "data")

# The catalogue files this change ADDED.
NEW_CATALOGUE_FILES = {
    model_choice.FAMILY_EMBEDDING: os.path.join(DATA_DIR, "embedding.yaml"),
    model_choice.FAMILY_VECTOR: os.path.join(DATA_DIR, "vector.yaml"),
}

# The catalogue files that existed BEFORE this change. Loading exactly these is
# a faithful reconstruction of the pre-fix artifact, not a synthetic stand-in.
PRE_FIX_CATALOGUE_FILES = [
    os.path.join(DATA_DIR, name)
    for name in ("text.yaml", "vision_image.yaml", "video.yaml", "speech_audio.yaml")
]

RED_MODE = os.environ.get("RED_MODE", "0") == "1"

# Env vars the module reads. Every one of them is cleared before a decision so a
# developer's shell can never be the reason a test passes.
_MODULE_ENV = (
    "HELIXLLM_CATALOGUE_PATHS",
    "HELIXLLM_USAGE_PURPOSE",
    "HELIXLLM_WEIGHTS_DIR",
    "HELIXLLM_GATE_DIR",
    "HELIXLLM_VECTOR_FORBID_MODELS",
    "HELIXLLM_EMBEDDING_FORBID_MODELS",
)


@contextlib.contextmanager
def clean_env(**overrides):
    saved = {k: os.environ.get(k) for k in _MODULE_ENV}
    try:
        for k in _MODULE_ENV:
            os.environ.pop(k, None)
        for k, v in overrides.items():
            os.environ[k] = v
        yield
    finally:
        for k, v in saved.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v


def host(**kw) -> gate.HostMeasurement:
    """A measured host, generous by default so a test that means to probe one
    axis is never accidentally refused on another."""
    base = dict(
        measured=True,
        has_accelerator=True,
        accelerator_free_bytes=48 << 30,
        system_free_bytes=64 << 30,
        free_disk_bytes=512 << 30,
    )
    base.update(kw)
    return gate.HostMeasurement(**base)


def run_cli(family: str, **env) -> subprocess.CompletedProcess:
    child = dict(os.environ)
    for k in _MODULE_ENV:
        child.pop(k, None)
    child.update(env)
    return subprocess.run(
        [sys.executable, os.path.join(HERE, "model_choice.py"), "--family", family],
        capture_output=True,
        text=True,
        env=child,
        timeout=180,
    )


# ---------------------------------------------------------------------------
# RED 1 — the defect this task was opened on.
#
# ASSERTS: the embedding and vector families have candidates at all.
#
# RED_MODE=1 loads the pre-fix catalogue and asserts the REAL pre-fix failure:
# each declared family is refused with "no catalogued model serves the <family>
# capability on any host" — a refusal that had nothing to do with the host and
# so was returned identically to every host, forever.
# ---------------------------------------------------------------------------
class DeclaredFamiliesHaveCandidatesTest(unittest.TestCase):
    def test_embedding_and_vector_are_offerable(self):
        paths = PRE_FIX_CATALOGUE_FILES if RED_MODE else list(NEW_CATALOGUE_FILES.values())
        with clean_env():
            entries = model_choice.load_entries(paths)

        for family in (model_choice.FAMILY_EMBEDDING, model_choice.FAMILY_VECTOR):
            in_family = [e for e in entries if e.family == family]
            if RED_MODE:
                self.assertEqual(
                    in_family,
                    [],
                    f"RED expected the pre-fix defect: {family} had no candidates",
                )
                with self.assertRaises(gate.CannotChoose) as caught:
                    gate.select(entries, family=family, purpose=gate.USAGE_COMMERCIAL, host=host())
                self.assertEqual(caught.exception.kind, gate.REASON_UNSUPPORTED_CONFIGURATION)
                self.assertIn("no catalogued model serves", caught.exception.detail)
            else:
                self.assertTrue(
                    in_family,
                    f"{family} is a declared family with no catalogue entry: it is refused "
                    "identically on every host, whatever that host could run",
                )

    def test_every_new_entry_states_both_resource_axes_and_a_licence(self):
        """An entry that omits a figure is not a cheap entry, it is an unusable one.

        The gate refuses to construct one, so this also proves the added YAML is
        readable by the SAME loader the audio families use — not only by Go.
        """
        if RED_MODE:
            self.skipTest("RED reproduces absence; there is nothing to validate on the pre-fix artifact")
        with clean_env():
            for family, path in NEW_CATALOGUE_FILES.items():
                entries = model_choice.load_entries([path])
                self.assertTrue(entries, f"{path} contributed no entries")
                for e in entries:
                    self.assertEqual(e.family, family)
                    self.assertGreater(e.memory_required_bytes, 0)
                    self.assertGreater(e.storage_required_bytes, 0)
                    self.assertTrue(e.usage_terms.license_id)
                    self.assertTrue(e.usage_terms.permitted)


# ---------------------------------------------------------------------------
# RED 2 — THE MOST LOAD-BEARING INVARIANT (FR-056).
#
# ASSERTS: an unmeasured host refuses, names why, and starts nothing. There is
# no fixed default anywhere on this path.
#
# This is the invariant the paired mutation targets: neuter it and a host that
# could not be measured is handed a model regardless.
# ---------------------------------------------------------------------------
class UnmeasuredHostRefusesTest(unittest.TestCase):
    def test_unmeasured_host_never_yields_a_default_model(self):
        with clean_env():
            entries = model_choice.load_entries(list(NEW_CATALOGUE_FILES.values()))
            for family in (model_choice.FAMILY_EMBEDDING, model_choice.FAMILY_VECTOR):
                with self.assertRaises(gate.CannotChoose) as caught:
                    model_choice.choose(
                        family,
                        entries=entries,
                        host=gate.HostMeasurement(measured=False),
                        purpose=gate.USAGE_COMMERCIAL,
                    )
                self.assertEqual(caught.exception.kind, gate.REASON_HOST_UNMEASURED)
                self.assertIn("no default", caught.exception.detail)

    def test_failed_measurement_is_not_softened_into_zero(self):
        """measured=False must stay a distinct state.

        Reported as zero free memory it would look like insufficient_resources —
        remedy "buy memory" — for a host whose real problem is that nobody knows
        what it has. Different remedies must not collapse.
        """
        with clean_env():
            entries = model_choice.load_entries(list(NEW_CATALOGUE_FILES.values()))
            with self.assertRaises(gate.CannotChoose) as unmeasured:
                model_choice.choose(
                    model_choice.FAMILY_EMBEDDING,
                    entries=entries,
                    host=gate.HostMeasurement(measured=False),
                    purpose=gate.USAGE_COMMERCIAL,
                )
            with self.assertRaises(gate.CannotChoose) as starved:
                model_choice.choose(
                    model_choice.FAMILY_EMBEDDING,
                    entries=entries,
                    host=host(has_accelerator=False, accelerator_free_bytes=0, system_free_bytes=1),
                    purpose=gate.USAGE_COMMERCIAL,
                )
            self.assertNotEqual(unmeasured.exception.kind, starved.exception.kind)
            self.assertEqual(starved.exception.kind, gate.REASON_INSUFFICIENT_RESOURCES)

    def test_cli_exits_non_zero_and_names_no_model_when_unmeasurable(self):
        """End to end, through the process boundary.

        HELIXLLM_WEIGHTS_DIR points at a path that cannot be stat'd, which is a
        real measurement failure rather than an injected flag.
        """
        result = run_cli(
            model_choice.FAMILY_EMBEDDING,
            HELIXLLM_WEIGHTS_DIR="/nonexistent-path-for-measurement-failure",
            HELIXLLM_CATALOGUE_PATHS=os.pathsep.join(NEW_CATALOGUE_FILES.values()),
        )
        self.assertNotEqual(result.returncode, 0, "an unmeasurable host must not exit success")
        self.assertEqual(result.returncode, model_choice.EXIT_HOST_NOT_MEASURED)
        self.assertNotIn("DECIDED", result.stdout)
        self.assertIn("MEASURE-INCOMPLETE", result.stdout)
        # No model may be named as a choice on a refusal path.
        self.assertNotIn("nomic-embed-text-v1.5:", result.stdout.split("CANNOT-CHOOSE")[0])


# ---------------------------------------------------------------------------
# RED 3 — configuration may say WHERE, never WHICH (FR-056).
# ---------------------------------------------------------------------------
class ConfigurationNeverNamesTheModelTest(unittest.TestCase):
    def test_no_env_var_can_introduce_a_model(self):
        """Set a model-shaped value into every env var the module reads.

        None of them may become the decision. The forbid-list is included on
        purpose: it names a model, but only ever to REMOVE it, and a removed
        model cannot be the one that runs.
        """
        planted = "attacker-supplied-model:v9"
        for var in _MODULE_ENV:
            if var in ("HELIXLLM_CATALOGUE_PATHS", "HELIXLLM_GATE_DIR", "HELIXLLM_WEIGHTS_DIR"):
                continue  # these are PATHS; a bogus path is covered by the loader test
            with clean_env(**{var: planted}), self.subTest(var=var):
                entries = model_choice.load_entries(list(NEW_CATALOGUE_FILES.values()))
                try:
                    chosen = model_choice.choose(
                        model_choice.FAMILY_EMBEDDING,
                        entries=entries,
                        host=host(),
                        purpose=gate.USAGE_COMMERCIAL,
                    ).entry.key
                except (gate.CannotChoose, ValueError):
                    continue  # refusing is always an acceptable answer; inventing is not
                self.assertNotEqual(chosen, planted)
                self.assertIn(chosen, {e.key for e in entries})

    def test_a_catalogue_path_that_does_not_exist_is_an_error_not_an_empty_result(self):
        """Silently skipping a missing catalogue file is how a family becomes
        unofferable while merely looking unpopulated — this task's own defect."""
        with clean_env():
            with self.assertRaises(gate.CatalogueError):
                model_choice.load_entries([os.path.join(DATA_DIR, "no-such-file.yaml")])

    def test_the_decision_is_always_an_entry_from_the_catalogue(self):
        with clean_env():
            entries = model_choice.load_entries(list(NEW_CATALOGUE_FILES.values()))
            keys = {e.key for e in entries}
            for family in (model_choice.FAMILY_EMBEDDING, model_choice.FAMILY_VECTOR):
                chosen = model_choice.choose(
                    family, entries=entries, host=host(), purpose=gate.USAGE_COMMERCIAL
                )
                self.assertIn(chosen.entry.key, keys)
                self.assertEqual(chosen.entry.family, family)


# ---------------------------------------------------------------------------
# RED 4 — the three reasons stay distinct (FR-055), all the way to the shell.
# ---------------------------------------------------------------------------
class ThreeReasonsStayDistinctTest(unittest.TestCase):
    def _research_only_entry(self) -> gate.CatalogueEntry:
        return gate.entry_from_mapping(
            {
                "model_id": "research-only-vectoriser",
                "family": model_choice.FAMILY_VECTOR,
                "memory_required_bytes": 1024,
                "storage_required_bytes": 1024,
                "requires_accelerator": False,
                "usage_terms": {
                    "license_id": "research-only-1.0",
                    "permitted": ["research", "evaluation"],
                    "restrictions": [
                        {
                            "term": "non-commercial",
                            "excludes": ["commercial"],
                            "reference": "test-fixture",
                        }
                    ],
                },
            },
            source_path="<test-fixture>",
        )

    def test_each_reason_is_reported_with_its_own_kind(self):
        with clean_env():
            catalogued = model_choice.load_entries(list(NEW_CATALOGUE_FILES.values()))

            # (a) insufficient resources — an accelerator exists but is too small.
            with self.assertRaises(gate.CannotChoose) as starved:
                model_choice.choose(
                    model_choice.FAMILY_VECTOR,
                    entries=catalogued,
                    host=host(accelerator_free_bytes=1 << 20),
                    purpose=gate.USAGE_COMMERCIAL,
                )

            # (b) unsupported configuration — no accelerator at all. More memory
            #     cannot substitute for one, so the remedy is different.
            with self.assertRaises(gate.CannotChoose) as unsupported:
                model_choice.choose(
                    model_choice.FAMILY_VECTOR,
                    entries=catalogued,
                    host=host(has_accelerator=False, accelerator_free_bytes=0),
                    purpose=gate.USAGE_COMMERCIAL,
                )

            # (c) excluded by usage terms — the host could serve it.
            with self.assertRaises(gate.CannotChoose) as licensed:
                model_choice.choose(
                    model_choice.FAMILY_VECTOR,
                    entries=[self._research_only_entry()],
                    host=host(),
                    purpose=gate.USAGE_COMMERCIAL,
                )

            kinds = [
                starved.exception.kind,
                unsupported.exception.kind,
                licensed.exception.kind,
            ]
            self.assertEqual(
                kinds,
                [
                    gate.REASON_INSUFFICIENT_RESOURCES,
                    gate.REASON_UNSUPPORTED_CONFIGURATION,
                    gate.REASON_USAGE_TERMS,
                ],
            )
            self.assertEqual(len(set(kinds)), 3, "the three reasons must not collapse")

    def test_each_reason_carries_its_own_exit_code(self):
        """A shell script must be able to act on the remedy without parsing prose."""
        codes = {
            model_choice._EXIT_FOR_REASON[gate.REASON_INSUFFICIENT_RESOURCES],
            model_choice._EXIT_FOR_REASON[gate.REASON_UNSUPPORTED_CONFIGURATION],
            model_choice._EXIT_FOR_REASON[gate.REASON_USAGE_TERMS],
            model_choice._EXIT_FOR_REASON[gate.REASON_HOST_UNMEASURED],
        }
        self.assertEqual(len(codes), 4, "distinct reasons must not share an exit code")
        self.assertNotIn(model_choice.EXIT_DECIDED, codes, "no refusal may exit success")

    def test_a_licence_excluded_option_is_reported_as_licence_not_as_hardware(self):
        """The wrong reason sends the operator to buy hardware they do not need."""
        with clean_env():
            with self.assertRaises(gate.CannotChoose) as caught:
                model_choice.choose(
                    model_choice.FAMILY_VECTOR,
                    entries=[self._research_only_entry()],
                    host=host(),
                    purpose=gate.USAGE_COMMERCIAL,
                )
            self.assertEqual(caught.exception.kind, gate.REASON_USAGE_TERMS)
            self.assertTrue(caught.exception.withheld)
            self.assertEqual(caught.exception.withheld[0].term, "non-commercial")

    def test_widening_the_declared_purpose_admits_what_the_licence_allows(self):
        """The negative control for the test above: the exclusion must be the
        licence discriminating, not the entry being unusable in every case."""
        with clean_env():
            chosen = model_choice.choose(
                model_choice.FAMILY_VECTOR,
                entries=[self._research_only_entry()],
                host=host(),
                purpose=gate.USAGE_RESEARCH,
            )
            self.assertEqual(chosen.entry.model_id, "research-only-vectoriser")


# ---------------------------------------------------------------------------
# RED 5 — the anti-simulation guard has teeth (§11.4.107(10)).
#
# The analyzer is self-validated against a golden-good and golden-bad pair. This
# runs on a host with no weights, so the guard the inference test depends on is
# proven correct even when the inference itself cannot run.
# ---------------------------------------------------------------------------
class EmbeddingGuardSelfValidationTest(unittest.TestCase):
    # golden-good: a real-shaped dense vector — every component distinct.
    GOOD = [((i * 37 % 991) / 991.0) - 0.5 for i in range(768)]

    def test_golden_good_passes(self):
        stats = model_choice.assert_real_embedding(self.GOOD, expected_dimensions=768)
        self.assertEqual(stats["dimensions"], 768)
        self.assertGreater(stats["distinct_ratio"], model_choice.MIN_DISTINCT_COMPONENT_RATIO)
        self.assertGreater(stats["l2_norm"], 0.0)

    def test_golden_bad_fixtures_all_fail(self):
        """If any of these passes, the guard is decoration and the inference test
        that leans on it proves nothing."""
        cases = {
            "all zeros": [0.0] * 768,
            "constant fill": [0.5] * 768,
            "short repeating cycle": [0.1, 0.2, 0.3, 0.4] * 192,
            "empty": [],
            "wrong width": [0.1 * i for i in range(64)],
            "non-finite": [float("nan")] + list(self.GOOD[1:]),
            "infinite": [float("inf")] + list(self.GOOD[1:]),
        }
        for name, vector in cases.items():
            with self.subTest(fixture=name):
                with self.assertRaises(gate.SimulationSuspected):
                    model_choice.assert_real_embedding(vector, expected_dimensions=768)


# ---------------------------------------------------------------------------
# RED 6 — REAL inference, or an honest skip.
#
# No fabricated vector is ever asserted on. When the weights are absent this
# states exactly why and skips; it never substitutes a synthetic result.
# ---------------------------------------------------------------------------
class RealEmbeddingInferenceTest(unittest.TestCase):
    def test_real_forward_pass_produces_a_real_vector(self):
        import shutil as _shutil

        gguf = os.environ.get("HELIXLLM_EMBED_GGUF", "")
        server = _shutil.which("llama-server")

        if not server:
            self.skipTest(
                "SKIP-REASON: llama-server is not on PATH, so the real embedding "
                "runtime cannot be started here. No synthetic vector is substituted."
            )
        if not gguf or not os.path.exists(gguf):
            self.skipTest(
                "SKIP-REASON: no embedding weight file is present on this host. "
                "internal/catalogue/data/embedding.yaml records "
                "nomic-embed-text-v1.5.Q4_K_M.gguf (84106624 B, HF revision "
                "0188c9bf409793f810680a5a431e7b899c46104c) but no download has been "
                "performed, and the entry is CORRECTLY refused by "
                "Entry.ValidateForAcquisition while no digest has been captured. "
                "Set HELIXLLM_EMBED_GGUF to a real .gguf to run this for real. "
                "No fabricated embedding is asserted on in place of the model."
            )

        with clean_env():
            entries = model_choice.load_entries(
                [NEW_CATALOGUE_FILES[model_choice.FAMILY_EMBEDDING]]
            )
            chosen = model_choice.choose(
                model_choice.FAMILY_EMBEDDING,
                entries=entries,
                host=model_choice.measure_host(),
                purpose=gate.USAGE_COMMERCIAL,
            )
        dims = int(chosen.entry.annotations.get("embedding_dimensions", 0)) or None

        port = "18455"
        proc = subprocess.Popen(
            [server, "-m", gguf, "--embedding", "--port", port, "--host", "127.0.0.1"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        try:
            import time
            import urllib.error
            import urllib.request

            body = json.dumps({"content": "helix measured selection embedding probe"}).encode()
            vector = None
            deadline = time.time() + 120
            last = None
            while time.time() < deadline:
                try:
                    req = urllib.request.Request(
                        f"http://127.0.0.1:{port}/embedding",
                        data=body,
                        headers={"Content-Type": "application/json"},
                    )
                    with urllib.request.urlopen(req, timeout=30) as resp:
                        payload = json.loads(resp.read().decode())
                    got = payload[0] if isinstance(payload, list) else payload
                    vector = got.get("embedding")
                    if isinstance(vector, list) and vector and isinstance(vector[0], list):
                        vector = vector[0]
                    break
                except (urllib.error.URLError, OSError, ValueError, KeyError, IndexError) as exc:
                    last = exc
                    time.sleep(2)

            self.assertIsNotNone(vector, f"llama-server returned no embedding (last error: {last})")
            # The real assertion: a REAL vector from a REAL forward pass.
            stats = model_choice.assert_real_embedding(vector, expected_dimensions=dims)
            self.assertGreater(stats["l2_norm"], 0.0)
        finally:
            proc.terminate()
            with contextlib.suppress(subprocess.TimeoutExpired):
                proc.wait(timeout=30)


if __name__ == "__main__":
    unittest.main(verbosity=2)
