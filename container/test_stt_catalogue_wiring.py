"""Tests for the speech-to-text catalogue wiring (T079).

Speech-to-text is NOT greenfield here. `container/whisper_stt_server.py` and
`container/Containerfile.whisper` already serve a real faster-whisper CPU
engine with VAD and no_speech_prob hallucination guards. What did not exist
was the path from a CATALOGUE ENTRY to that running engine — the server chose
its model from a `WHISPER_MODEL` environment default, which is precisely the
static selection FR-056 forbids as the selection mechanism.

So these tests are about REACHABILITY through the catalogue, not about
building an engine. The live-engine test transcribes real audio through the
running server and SKIPS with a stated reason when the server is not up — it
never fabricates a transcript to go green.
"""

import json
import os
import sys
import unittest
import urllib.error
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import helix_model_gate as gate  # noqa: E402
import stt_catalogue_wiring as wiring  # noqa: E402

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CATALOGUE = os.path.join(REPO_ROOT, "internal", "catalogue", "data", "speech_audio.yaml")


def _host(**kw):
    base = dict(
        measured=True,
        has_accelerator=False,
        accelerator_free_bytes=0,
        system_free_bytes=16 << 30,
        free_disk_bytes=200 << 30,
    )
    base.update(kw)
    return gate.HostMeasurement(**base)


class CataloguePathTest(unittest.TestCase):
    """The repo catalogue's speech-to-text entries must reach the engine."""

    def setUp(self):
        if not os.path.exists(CATALOGUE):
            self.skipTest(f"SKIP-REASON: repo catalogue absent at {CATALOGUE}")
        self.entries = gate.load_catalogue(CATALOGUE)
        self.stt = [e for e in self.entries if e.family == gate.FAMILY_SPEECH_TO_TEXT]

    def test_the_catalogue_actually_carries_speech_to_text_entries(self):
        self.assertGreaterEqual(len(self.stt), 1)

    def test_every_catalogued_stt_entry_is_resolved_or_refused_with_a_reason(self):
        # Not every catalogued entry is servable by THIS container's engine —
        # faster-whisper runs Whisper weights, not Parakeet. What is forbidden
        # is an entry that neither resolves nor explains itself.
        for e in self.stt:
            with self.subTest(entry=e.key):
                try:
                    plan = wiring.resolve(e, _host())
                except wiring.EngineUnsupported as exc:
                    self.assertTrue(str(exc).strip(), "refusal must state a reason")
                    self.assertIn(e.model_id, str(exc))
                else:
                    self.assertTrue(plan.model_name)
                    self.assertIn(plan.device, ("cpu", "cuda"))
                    self.assertTrue(plan.compute_type)

    def test_a_whisper_entry_resolves_to_the_faster_whisper_size_name(self):
        medium = next((e for e in self.stt if e.model_id == "whisper-medium"), None)
        if medium is None:
            self.skipTest("SKIP-REASON: whisper-medium is not a loaded catalogue entry")
        plan = wiring.resolve(medium, _host())
        self.assertEqual(plan.model_name, "medium")
        self.assertEqual(plan.catalogue_key, "whisper-medium")

    def test_selection_through_the_gate_yields_a_servable_plan(self):
        # The end-to-end catalogue path: measured host + declared purpose ->
        # selected entry -> engine plan. This is the wiring T079 is about.
        plan = wiring.plan_for_host(self.entries, _host(), purpose=gate.USAGE_COMMERCIAL)
        self.assertTrue(plan.model_name)
        self.assertEqual(plan.family, gate.FAMILY_SPEECH_TO_TEXT)


class SelectionDisciplineTest(unittest.TestCase):
    def test_no_selection_means_refusal_not_a_default_model(self):
        # The pre-wiring server started `base` when nothing was configured.
        # That is the exact FR-056 violation this wiring closes: an unmeasured
        # host must report that it cannot choose, not start an arbitrary model.
        with self.assertRaises(gate.CannotChoose) as ctx:
            wiring.plan_for_host([], _host(measured=False), purpose=gate.USAGE_COMMERCIAL)
        self.assertEqual(ctx.exception.kind, gate.REASON_HOST_UNMEASURED)

    def test_no_catalogue_entries_reports_unsupported_not_a_default(self):
        with self.assertRaises(gate.CannotChoose):
            wiring.plan_for_host([], _host(), purpose=gate.USAGE_COMMERCIAL)

    def test_device_follows_the_measured_host_not_a_configured_name(self):
        cpu_plan = wiring.resolve(_whisper_entry(), _host(has_accelerator=False))
        self.assertEqual(cpu_plan.device, "cpu")
        self.assertEqual(cpu_plan.compute_type, "int8")

        gpu_plan = wiring.resolve(
            _whisper_entry(), _host(has_accelerator=True, accelerator_free_bytes=24 << 30)
        )
        self.assertEqual(gpu_plan.device, "cuda")
        self.assertNotEqual(gpu_plan.compute_type, "int8")

    def test_a_non_whisper_engine_entry_is_refused_by_name(self):
        parakeet = gate.CatalogueEntry(
            model_id="parakeet-tdt-0.6b-v3",
            variant="int8",
            family=gate.FAMILY_SPEECH_TO_TEXT,
            usage_terms=gate.UsageTerms("CC-BY-4.0", (gate.USAGE_COMMERCIAL,)),
            memory_required_bytes=1,
            storage_required_bytes=1,
        )
        with self.assertRaises(wiring.EngineUnsupported) as ctx:
            wiring.resolve(parakeet, _host())
        self.assertIn("parakeet", str(ctx.exception).lower())
        self.assertIn("faster-whisper", str(ctx.exception).lower())


def _whisper_entry():
    return gate.CatalogueEntry(
        model_id="whisper-medium",
        family=gate.FAMILY_SPEECH_TO_TEXT,
        usage_terms=gate.UsageTerms("MIT", (gate.USAGE_COMMERCIAL,)),
        memory_required_bytes=1,
        storage_required_bytes=1,
    )


class LiveEngineTest(unittest.TestCase):
    """Reaches the RUNNING server. Skips honestly when it is not up.

    A transcript is never fabricated to make this pass. If the server is down,
    or the weights are absent, the test says so and skips — a green result
    here means real audio really went through a real decoder.
    """

    BASE = os.environ.get("HELIXLLM_STT_BASE_URL", "http://127.0.0.1:8000")

    def _get(self, path):
        try:
            with urllib.request.urlopen(self.BASE + path, timeout=3) as r:
                return json.loads(r.read().decode())
        except (urllib.error.URLError, OSError, TimeoutError) as exc:
            self.skipTest(
                f"SKIP-REASON: speech-to-text server not reachable at {self.BASE} ({exc}). "
                "Boot it via the containers submodule to run this test against real weights."
            )

    def test_health_reports_the_catalogue_selection_it_is_serving(self):
        body = self._get("/health")
        self.assertIn("catalogue_key", body)
        self.assertIn("engine_ready", body)
        if not body["engine_ready"]:
            self.assertTrue(
                body.get("reason"), "a not-ready engine MUST carry the exact reason"
            )

    def test_real_audio_transcribes_through_the_catalogue_selected_model(self):
        health = self._get("/health")
        if not health.get("engine_ready"):
            self.skipTest(
                "SKIP-REASON: engine not ready — " + str(health.get("reason"))
            )
        fixture = os.environ.get("HELIXLLM_STT_FIXTURE_WAV")
        if not fixture or not os.path.exists(fixture):
            self.skipTest(
                "SKIP-REASON: no real speech fixture. Set HELIXLLM_STT_FIXTURE_WAV to a "
                "WAV of real speech; this test will not synthesise or assert a transcript "
                "it did not obtain from the decoder."
            )
        boundary = "----helixllmsttfixture"
        with open(fixture, "rb") as fh:
            audio = fh.read()
        body = (
            f"--{boundary}\r\n"
            f'Content-Disposition: form-data; name="file"; filename="fixture.wav"\r\n'
            "Content-Type: audio/wav\r\n\r\n"
        ).encode() + audio + f"\r\n--{boundary}--\r\n".encode()
        req = urllib.request.Request(
            self.BASE + "/v1/audio/transcriptions",
            data=body,
            headers={"Content-Type": f"multipart/form-data; boundary={boundary}"},
        )
        with urllib.request.urlopen(req, timeout=300) as r:
            out = json.loads(r.read().decode())
        self.assertIn("text", out)
        self.assertIn("silence_guard", out)
        self.assertGreater(out.get("duration", 0), 0.0)


if __name__ == "__main__":
    unittest.main(verbosity=2)
