"""Tests for the audio-classification service (T081).

Audio classification is the one audio family that is mature, cheap and
processor-viable on EVERY host class, down to a machine with no accelerator and
8 GB of RAM. Two things follow, and both are tested here.

First, it must not be gated behind an accelerator. A test that only passed on a
GPU host would be testing the wrong service.

Second, it must stay SEPARATE from audio GENERATION. Those are two distinct
families on purpose: classification runs anywhere, and generation has no
processor-viable option at all at any RAM size. Merging them would either offer
a machine with no accelerator something it cannot run, or hide generation
entirely behind a family that looks available. Several tests below exist only
to keep that separation from quietly eroding.
"""

import os
import sys
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(os.path.dirname(HERE))
sys.path.insert(0, HERE)
sys.path.insert(0, os.path.join(REPO_ROOT, "container"))

import audio_engine  # noqa: E402
import helix_model_gate as gate  # noqa: E402

SERVICE_CATALOGUE = os.path.join(HERE, "models.yaml")
REPO_CATALOGUE = os.path.join(
    REPO_ROOT, "internal", "catalogue", "data", "speech_audio.yaml"
)


def _host(**kw):
    base = dict(
        measured=True,
        has_accelerator=False,
        accelerator_free_bytes=0,
        system_free_bytes=8 << 30,
        free_disk_bytes=20 << 30,
    )
    base.update(kw)
    return gate.HostMeasurement(**base)


class ProcessorViabilityTest(unittest.TestCase):
    """This family must serve the smallest supported host."""

    def setUp(self):
        self.entries = gate.load_catalogue(SERVICE_CATALOGUE)

    def test_no_entry_requires_an_accelerator(self):
        for e in self.entries:
            self.assertFalse(
                e.requires_accelerator,
                f"{e.key} requires an accelerator; this family must not be gated behind one",
            )

    def test_selection_succeeds_on_a_no_accelerator_8gb_host(self):
        sel = gate.select(
            self.entries,
            gate.FAMILY_AUDIO_CLASSIFICATION,
            gate.USAGE_COMMERCIAL,
            _host(has_accelerator=False, system_free_bytes=8 << 30),
        )
        self.assertTrue(sel.entry.model_id)
        self.assertFalse(sel.entry.requires_accelerator)

    def test_selection_still_succeeds_on_a_very_small_host(self):
        # 1 GiB free RAM, 1 GiB free disk — well below the family's stated
        # smallest tier, and it must still find something.
        sel = gate.select(
            self.entries,
            gate.FAMILY_AUDIO_CLASSIFICATION,
            gate.USAGE_COMMERCIAL,
            _host(system_free_bytes=1 << 30, free_disk_bytes=1 << 30),
        )
        self.assertTrue(sel.entry.model_id)

    def test_the_commercial_default_is_permissively_licensed(self):
        sel = gate.select(
            self.entries, gate.FAMILY_AUDIO_CLASSIFICATION, gate.USAGE_COMMERCIAL, _host()
        )
        self.assertTrue(sel.entry.usage_terms.permits(gate.USAGE_COMMERCIAL))
        self.assertIsNone(sel.entry.usage_terms.restriction_for(gate.USAGE_COMMERCIAL))


class FamilySeparationTest(unittest.TestCase):
    """Classification and generation must not be conflated."""

    def setUp(self):
        self.entries = gate.load_catalogue(SERVICE_CATALOGUE)

    def test_the_service_catalogue_contains_no_generation_entry(self):
        for e in self.entries:
            self.assertEqual(e.family, gate.FAMILY_AUDIO_CLASSIFICATION, e.key)

    def test_a_generation_request_is_not_answered_from_classification(self):
        with self.assertRaises(gate.CannotChoose) as ctx:
            gate.select(
                self.entries, gate.FAMILY_AUDIO_GENERATION, gate.USAGE_COMMERCIAL, _host()
            )
        self.assertEqual(ctx.exception.kind, gate.REASON_UNSUPPORTED_CONFIGURATION)

    def test_the_engine_refuses_a_generation_entry_by_name(self):
        gen = gate.CatalogueEntry(
            model_id="ace-step-2b",
            family=gate.FAMILY_AUDIO_GENERATION,
            usage_terms=gate.UsageTerms("Apache-2.0", (gate.USAGE_COMMERCIAL,)),
            memory_required_bytes=4294967296,
            storage_required_bytes=1,
            requires_accelerator=True,
        )
        with self.assertRaises(audio_engine.EngineUnsupported) as ctx:
            audio_engine.resolve(gen)
        message = str(ctx.exception)
        self.assertIn(gate.FAMILY_AUDIO_GENERATION, message)
        # A bare "wrong family" refusal is NOT enough, and this assertion was
        # tightened because a paired mutation proved it: deleting the
        # generation-specific branch left the generic family check to raise, so
        # the test stayed green while the reason a processor-only host is owed
        # had silently disappeared. The refusal must SAY why generation is not
        # served here — that it needs an accelerator this service does not
        # assume — because that statement is what such a host receives INSTEAD
        # of an offer it could never run.
        self.assertIn(
            "accelerator",
            message.lower(),
            "the generation refusal must explain the accelerator requirement, "
            "not merely report a family mismatch",
        )

    def test_generation_on_a_processor_host_is_unsupported_not_underprovisioned(self):
        # The remedy differs and must not be reported as one generic failure:
        # more RAM does not make an accelerator-only model runnable.
        gen = gate.CatalogueEntry(
            model_id="ace-step-2b",
            family=gate.FAMILY_AUDIO_GENERATION,
            usage_terms=gate.UsageTerms("Apache-2.0", (gate.USAGE_COMMERCIAL,)),
            memory_required_bytes=4294967296,
            storage_required_bytes=1,
            requires_accelerator=True,
        )
        d = gate.evaluate(gen, gate.USAGE_COMMERCIAL, _host(system_free_bytes=1 << 40))
        self.assertFalse(d.allowed)
        self.assertEqual(d.kind, gate.REASON_UNSUPPORTED_CONFIGURATION)


class HonestFigureTest(unittest.TestCase):
    """A number that was not measured must not be dressed up as one."""

    def setUp(self):
        self.entries = gate.load_catalogue(SERVICE_CATALOGUE)

    def test_every_resource_figure_declares_how_it_was_arrived_at(self):
        for e in self.entries:
            with self.subTest(entry=e.key):
                self.assertIn("memory_basis", e.annotations, "memory basis must be stated")
                self.assertIn("storage_basis", e.annotations, "storage basis must be stated")
                self.assertIn(
                    e.annotations["memory_basis"],
                    ("measured", "sourced", "conservative-ceiling"),
                )
                self.assertIn(
                    e.annotations["storage_basis"], ("measured", "sourced")
                )

    def test_an_unmeasured_memory_figure_is_labelled_a_ceiling_and_says_so(self):
        for e in self.entries:
            if e.annotations.get("memory_basis") != "conservative-ceiling":
                continue
            joined = " ".join(e.notes).lower()
            self.assertIn("not a measurement", joined, f"{e.key} must say so plainly")


class EngineResolutionTest(unittest.TestCase):
    def setUp(self):
        self.entries = gate.load_catalogue(SERVICE_CATALOGUE)

    def test_every_catalogued_entry_resolves_or_is_refused_by_name(self):
        for e in self.entries:
            with self.subTest(entry=e.key):
                try:
                    plan = audio_engine.resolve(e)
                except audio_engine.EngineUnsupported as exc:
                    self.assertIn(e.model_id, str(exc))
                else:
                    self.assertTrue(plan.engine)
                    self.assertGreater(plan.sample_rate, 0)
                    self.assertGreater(plan.expected_class_count, 0)

    def test_plan_for_host_yields_a_runnable_plan_without_an_accelerator(self):
        plan = audio_engine.plan_for_host(self.entries, _host(), gate.USAGE_COMMERCIAL)
        self.assertTrue(plan.engine)

    def test_unmeasured_host_refuses_rather_than_defaulting(self):
        with self.assertRaises(gate.CannotChoose) as ctx:
            audio_engine.plan_for_host(
                self.entries, _host(measured=False), gate.USAGE_COMMERCIAL
            )
        self.assertEqual(ctx.exception.kind, gate.REASON_HOST_UNMEASURED)


class ClassMapTest(unittest.TestCase):
    def test_the_bundled_class_map_has_the_expected_shape(self):
        path = audio_engine.class_map_path()
        if not os.path.exists(path):
            self.skipTest(
                f"SKIP-REASON: class map not present at {path}. Obtain it via the "
                ".gitignore-meta regeneration manifest."
            )
        names = audio_engine.load_class_map(path)
        self.assertEqual(
            len(names),
            audio_engine.YAMNET_CLASS_COUNT,
            "the class map length is what the anti-simulation guard checks scores against",
        )
        self.assertTrue(all(n.strip() for n in names))

    def test_a_truncated_class_map_is_refused(self):
        import tempfile

        with tempfile.NamedTemporaryFile("w", suffix=".csv", delete=False) as fh:
            fh.write("index,mid,display_name\n0,/m/09x0r,Speech\n")
            bad = fh.name
        try:
            with self.assertRaises(audio_engine.ClassMapError):
                audio_engine.load_class_map(bad)
        finally:
            os.unlink(bad)


class RealInferenceTest(unittest.TestCase):
    """Runs the ACTUAL interpreter over REAL weights, or skips saying why."""

    def setUp(self):
        try:
            import ai_edge_litert  # noqa: F401
        except Exception:
            try:
                import tflite_runtime  # noqa: F401
            except Exception as exc:
                self.skipTest(
                    f"SKIP-REASON: no LiteRT/TFLite runtime on this host ({exc}). It ships "
                    "in the service container; run this suite inside the built image to "
                    "exercise real inference."
                )
        if not os.path.exists(audio_engine.model_path()):
            self.skipTest(
                f"SKIP-REASON: model weights absent at {audio_engine.model_path()}. "
                "This test will not fabricate scores in their absence."
            )
        if not os.path.exists(audio_engine.class_map_path()):
            self.skipTest("SKIP-REASON: class map absent; cannot attribute a score to a class.")

    def test_classification_of_real_audio_produces_a_real_score_vector(self):
        import math

        interp = audio_engine.load_engine()
        names = audio_engine.load_class_map(audio_engine.class_map_path())
        sr = audio_engine.YAMNET_SAMPLE_RATE
        # A real signal, not silence: one second of a 440 Hz tone. What class
        # it lands on is not asserted — only that a real forward pass produced
        # a real distribution over the loaded class map.
        wave = [0.5 * math.sin(2 * math.pi * 440 * t / sr) for t in range(sr)]
        scores = audio_engine.classify(interp, wave)
        stats = gate.assert_real_scores(scores, len(names))
        self.assertGreater(stats["spread"], 0.0)
        self.assertLess(stats["top_index"], len(names))

    def test_different_audio_produces_different_scores(self):
        # A hardcoded response returns the same vector for any input.
        import math
        import random

        interp = audio_engine.load_engine()
        sr = audio_engine.YAMNET_SAMPLE_RATE
        tone = [0.5 * math.sin(2 * math.pi * 440 * t / sr) for t in range(sr)]
        rng = random.Random(11)
        noise = [rng.uniform(-0.5, 0.5) for _ in range(sr)]
        a = audio_engine.classify(interp, tone)
        b = audio_engine.classify(interp, noise)
        self.assertNotEqual(list(a), list(b))


if __name__ == "__main__":
    unittest.main(verbosity=2)
