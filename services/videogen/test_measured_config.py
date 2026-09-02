#!/usr/bin/env python3
"""FR-056 unit guard for the video-gen service's configuration surface.

WHAT THIS PROVES
    The service carries NO default model, and refuses rather than substituting
    one; the clip it serves is the shape the upstream measured selection
    decided; and a request may not ask for a shape larger than the one the
    broker admitted VRAM for.

POLARITY (§11.4.115) — one source, two roles, switched by RED_MODE:
    RED_MODE=1  assert the pre-fix defect is PRESENT: the module falls back to
                a hardcoded model when VIDEOGEN_MODEL is unset.
    RED_MODE=0  the standing guard (default): no fallback exists.

HONEST BOUNDARY (§11.4.6)
    This is a UNIT test. `fastapi`, `pydantic` and `uvicorn` live in the service
    image, not on the host, so they are stubbed to let the module import — the
    REAL configuration and shape-resolution code is then executed unmodified.
    It does NOT exercise the container, the diffusion pipeline, or a generation:
    those remain the PENDING runtime proof.

Run:  python3 test_measured_config.py          # standing guard
      RED_MODE=1 python3 test_measured_config.py
"""

import ast
import os
import sys
import types
import unittest

MODULE = "videogen_server"
DECISION_VARS = (
    "VIDEOGEN_MODEL",
    "VIDEOGEN_BACKEND",
    "VIDEOGEN_PRECISION",
    "VIDEOGEN_SIZE",
    "VIDEOGEN_NUM_FRAMES",
    "VIDEOGEN_FPS",
)
HARDCODED_MODEL_MARKERS = ("Wan-AI/", "Lightricks/", "huggingface.co/")


def red_mode() -> bool:
    return os.environ.get("RED_MODE") == "1"


def _install_stubs() -> None:
    """Stand in for the web framework that lives in the service image.

    Only the absent third-party imports are stubbed. Everything under test is
    the real module's own code.
    """
    if "fastapi" not in sys.modules:
        fastapi = types.ModuleType("fastapi")

        class HTTPException(Exception):
            def __init__(self, status_code=500, detail=""):
                super().__init__(detail)
                self.status_code = status_code
                self.detail = detail

        class FastAPI:
            def __init__(self, *a, **kw):
                pass

            def get(self, *a, **kw):
                return lambda fn: fn

            def post(self, *a, **kw):
                return lambda fn: fn

        fastapi.FastAPI = FastAPI
        fastapi.HTTPException = HTTPException
        sys.modules["fastapi"] = fastapi

    if "pydantic" not in sys.modules:
        pydantic = types.ModuleType("pydantic")

        class BaseModel:
            def __init__(self, **kw):
                for k, v in kw.items():
                    setattr(self, k, v)

        def Field(default=None, **kw):
            return default

        pydantic.BaseModel = BaseModel
        pydantic.Field = Field
        sys.modules["pydantic"] = pydantic

    if "uvicorn" not in sys.modules:
        uvicorn = types.ModuleType("uvicorn")
        uvicorn.run = lambda *a, **kw: None
        sys.modules["uvicorn"] = uvicorn


def load(**environment):
    """Import the service module fresh under a given environment."""
    _install_stubs()
    for name in DECISION_VARS:
        os.environ.pop(name, None)
    for k, v in environment.items():
        os.environ[k] = v
    sys.modules.pop(MODULE, None)
    return __import__(MODULE)


DECIDED = {
    "VIDEOGEN_MODEL": "decided-owner/Decided-Model",
    "VIDEOGEN_BACKEND": "wan",
    "VIDEOGEN_PRECISION": "fp8",
    "VIDEOGEN_SIZE": "832x480",
    "VIDEOGEN_NUM_FRAMES": "49",
    "VIDEOGEN_FPS": "16",
}


class NoDefaultModel(unittest.TestCase):
    def test_no_hardcoded_model_in_source(self):
        """A model literal in EXECUTABLE code is a model that can be served
        when nothing measured the host.

        The oracle is the AST, not the file text: a repository named in prose
        cannot be served, one in a string literal can. Docstrings are excluded
        for exactly that reason, and because the module must be free to explain
        the rule it enforces.
        """
        with open(f"{MODULE}.py", encoding="utf-8") as fh:
            tree = ast.parse(fh.read())

        docstrings = set()
        for node in ast.walk(tree):
            if isinstance(node, (ast.Module, ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
                body = getattr(node, "body", None)
                if body and isinstance(body[0], ast.Expr) and isinstance(body[0].value, ast.Constant) \
                        and isinstance(body[0].value.value, str):
                    docstrings.add(id(body[0].value))

        found = []
        for node in ast.walk(tree):
            if isinstance(node, ast.Constant) and isinstance(node.value, str) and id(node) not in docstrings:
                for marker in HARDCODED_MODEL_MARKERS:
                    if marker in node.value:
                        found.append(f"line {node.lineno}: {marker}")
        if red_mode():
            self.assertTrue(found, "RED_MODE=1: expected a hardcoded model repository in the source")
            return
        self.assertEqual(found, [], f"a model repository is hardcoded in the service source: {found}")

    def test_unset_model_is_refused_not_defaulted(self):
        mod = load()  # nothing decided
        if red_mode():
            self.assertIsNotNone(mod.MODEL_ID, "RED_MODE=1: expected a fallback model to be substituted")
            return
        self.assertIsNone(mod.MODEL_ID, "a model was substituted when none was decided")
        reason = mod._unconfigured_reason()
        self.assertIsNotNone(reason, "an undecided container reported itself configured")
        for name in DECISION_VARS:
            self.assertIn(name, reason, f"the refusal does not name the missing {name}")

    def test_partial_decision_is_still_refused(self):
        """A model without its clip shape is not a decision."""
        if red_mode():
            self.skipTest("SKIP-OK: no pre-fix counterpart — the pre-fix module had defaults for every field")
        mod = load(VIDEOGEN_MODEL=DECIDED["VIDEOGEN_MODEL"], VIDEOGEN_BACKEND="wan")
        reason = mod._unconfigured_reason()
        self.assertIsNotNone(reason, "a half-configured container reported itself configured")
        self.assertIn("VIDEOGEN_SIZE", reason)

    def test_malformed_number_is_absence_not_a_guess(self):
        if red_mode():
            self.skipTest("SKIP-OK: no pre-fix counterpart — the pre-fix module coerced with int() and crashed")
        env = dict(DECIDED, VIDEOGEN_FPS="23.976")
        mod = load(**env)
        self.assertIsNone(mod.DECIDED_FPS)
        self.assertIn("VIDEOGEN_FPS", mod._unconfigured_reason())

    def test_decided_values_are_what_is_served(self):
        if red_mode():
            self.skipTest("SKIP-OK: no pre-fix counterpart — the pre-fix module served its own defaults")
        mod = load(**DECIDED)
        self.assertIsNone(mod._unconfigured_reason())
        self.assertEqual(mod.MODEL_ID, DECIDED["VIDEOGEN_MODEL"])
        self.assertEqual(mod.BACKEND, "wan")
        self.assertEqual(mod.PRECISION, "fp8")
        self.assertEqual((mod.DECIDED_SIZE, mod.DECIDED_NUM_FRAMES, mod.DECIDED_FPS), ("832x480", 49, 16))


class AdmittedShapeIsTheCeiling(unittest.TestCase):
    def setUp(self):
        if red_mode():
            self.skipTest("SKIP-OK: no pre-fix counterpart — the pre-fix module had no admitted shape to bound")
        self.mod = load(**DECIDED)

    def request(self, **kw):
        fields = {"prompt": "p", "size": None, "num_frames": None, "fps": None, "steps": None, "seed": None}
        fields.update(kw)
        return self.mod.VideoRequest(**fields)

    def test_default_is_the_decided_shape(self):
        self.assertEqual(self.mod._resolve_shape(self.request()), (832, 480, 49, 16))

    def test_smaller_request_is_allowed(self):
        self.assertEqual(
            self.mod._resolve_shape(self.request(size="640x384", num_frames=25)),
            (640, 384, 25, 16),
        )

    def test_larger_frame_size_is_refused(self):
        with self.assertRaises(self.mod.HTTPException) as caught:
            self.mod._resolve_shape(self.request(size="1280x704"))
        self.assertEqual(caught.exception.status_code, 400)
        self.assertIn("admitted", caught.exception.detail)

    def test_more_frames_is_refused(self):
        with self.assertRaises(self.mod.HTTPException) as caught:
            self.mod._resolve_shape(self.request(num_frames=201))
        self.assertEqual(caught.exception.status_code, 400)

    def test_nonpositive_shape_is_refused(self):
        with self.assertRaises(self.mod.HTTPException):
            self.mod._resolve_shape(self.request(num_frames=0))


if __name__ == "__main__":
    print(f"RED_MODE={os.environ.get('RED_MODE', '0')}")
    unittest.main(verbosity=2)
