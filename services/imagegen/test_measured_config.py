"""The imagegen service must not substitute a model no measurement chose.

The boot binary was migrated onto measured selection, but this service kept a
hardcoded fallback one layer down:

    MODEL_ID = _env("IMAGEGEN_MODEL", "black-forest-labs/FLUX.1-schnell")

So a boot path that correctly refused to choose, or an operator running the
service directly, still got a model — silently, and one no host was ever
measured against. Fixing the binary while leaving this fixes only the half that
is easy to see.

WHY THIS IS A SOURCE-STRUCTURE TEST, NOT A BEHAVIOURAL ONE: importing this
module requires the web framework and diffusion stack, which live in the service
image and not on this host. Stubbing enough of them to reach the configuration
lines means stubbing a class hierarchy, and a test that elaborate is testing the
stub. Reading the assignment itself is narrower and cannot drift from the code —
the thing under test IS the source line.

Mirrors services/videogen/test_measured_config.py, which pins the same rule for
the sibling service.
"""
import ast
import os
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
SERVER = os.path.join(HERE, "imagegen_server.py")

# Values that are OUTPUTS of the upstream measured selection. None may carry a
# default: a default here is a model, or a memory footprint, that no host was
# measured against.
DECIDED = ("MODEL_ID", "PRECISION")


def module_assignments():
    """Every module-level `NAME = <expr>` in the server, as {name: expr node}."""
    with open(SERVER, encoding="utf-8") as fh:
        tree = ast.parse(fh.read(), filename=SERVER)
    out = {}
    for node in tree.body:
        if isinstance(node, ast.Assign):
            for t in node.targets:
                if isinstance(t, ast.Name):
                    out[t.id] = node.value
    return out


class MeasuredConfig(unittest.TestCase):
    def test_decided_values_carry_no_default(self):
        assigns = module_assignments()
        for name in DECIDED:
            with self.subTest(name=name):
                self.assertIn(name, assigns, f"{name} is not assigned at module level")
                expr = assigns[name]
                self.assertIsInstance(
                    expr, ast.Call,
                    f"{name} must be read through a helper that permits no default",
                )
                fn = expr.func.id if isinstance(expr.func, ast.Name) else ""
                self.assertNotEqual(
                    fn, "_env",
                    f"{name} is read via _env(), which takes a default — a value "
                    f"invented here is a model no measurement chose (FR-056)",
                )
                self.assertEqual(
                    len(expr.args), 1,
                    f"{name} is read with {len(expr.args)} arguments; a second "
                    f"argument is a fallback, and there must be none",
                )

    def test_no_decided_value_is_defaulted_from_a_literal(self):
        """No MODULE-LEVEL assignment may hold a model repository literal.

        Deliberately scoped to module level, and deliberately NOT a line grep.
        Two things in this file legitimately name repositories and must survive:

          - the module docstring and comments explaining WHICH repositories are
            gated and why, which a grep would force out to go green;
          - `_default_nvfp4_transformer(model_id)`, which DERIVES the matching
            quantised transformer FROM the chosen model. That is a mapping, not
            a default — it cannot produce a model when none was decided, because
            it takes the decided one as its argument.

        The defect class is narrower than "a repo string appears somewhere": it
        is a value standing ready to be used when nothing chose one.
        """
        for name, expr in module_assignments().items():
            for node in ast.walk(expr):
                if isinstance(node, ast.Constant) and isinstance(node.value, str):
                    parts = node.value.split("/")
                    if len(parts) == 2 and all(parts) and " " not in node.value:
                        self.fail(
                            f"module-level {name} holds the repository literal "
                            f"{node.value!r}: a value invented here is a model no "
                            f"measurement chose (FR-056)"
                        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
