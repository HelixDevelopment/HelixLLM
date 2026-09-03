package selection_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Go's sortOffered and the Python gate's select() are two independent
// statements of ONE selection policy: which admissible option a host reaches
// for first. They have already disagreed once. Go's boot lanes ranked
// most-capable-first while the gate ranked cheapest-first, so the same host
// served an f16 build through one path and a q4_k_m build through the other —
// and the largest-that-fits choice is exactly what leaves a co-resident vision
// or coder model on the same accelerator with nothing (see internal/vrambroker).
//
// That was resolved by moving the ordering into Go at internal/selection and
// giving it the gate's key. Three places now ASSERT the two agree in prose —
// sortOffered's doc comment, offer_order_test.go's header, and the gate's
// module docstring — and until this test nothing MECHANICALLY held them
// together. A prose promise does not fail a build when someone edits one side.
//
// GO IS AUTHORITATIVE, per the gate's own header ("Where Go and this module
// differ, Go is the source of truth and this module is the defect"). So when
// this test fails, the gate is what changes — unless the change to Go was
// itself the defect, which is the one case worth reading the ordering rationale
// in sortOffered before touching either side.
//
// It compares the ordered comparison AXES and, on each axis, the DIRECTION.
// Direction is the load-bearing half: most-capable-first is not a different set
// of axes, it is the same axes with the inequality flipped. A test that checked
// only that both sides sort on memory-then-storage-then-identity would have
// passed throughout the original defect.
func TestGoOfferOrderingAgreesWithThePythonGate(t *testing.T) {
	root := selectionRepoRoot(t)

	goSrc := readSourceFile(t, filepath.Join(root, "internal", "selection", "family.go"))
	pySrc := readSourceFile(t, filepath.Join(root, "container", "helix_model_gate.py"))

	goAxes := parseGoOfferOrdering(t, goSrc)
	pyAxes := parsePythonGateOrdering(t, pySrc)

	if len(goAxes) == 0 {
		t.Fatal("parsed no comparison axes from sortOffered — the parser has drifted from family.go")
	}
	if len(pyAxes) == 0 {
		t.Fatal("parsed no sort axes from the gate's select() — the parser has drifted from helix_model_gate.py")
	}

	if renderOrdering(goAxes) == renderOrdering(pyAxes) {
		t.Logf("go sortOffered=%s  python gate select()=%s  (agree)",
			renderOrdering(goAxes), renderOrdering(pyAxes))
	} else {
		t.Errorf("the Go offer ordering and the Python gate's ranking disagree.\n"+
			"  Go   internal/selection/family.go sortOffered(): %s\n"+
			"  Py   container/helix_model_gate.py select():     %s\n"+
			"Both decide which admissible option a host serves, so a host now answers "+
			"differently through the Go path and the Python path — the same defect that "+
			"once served f16 through one and q4_k_m through the other. Go is authoritative "+
			"(see the gate's module docstring): bring the gate's sort key back to Go's, or, "+
			"if the change to Go was the mistake, read the cheapest-first rationale in "+
			"sortOffered before flipping either side.",
			renderOrdering(goAxes), renderOrdering(pyAxes))
	}
}

// orderingAxis is one comparison step: what is compared, and which way.
type orderingAxis struct {
	axis      string // canonical: "memory", "storage", "identity"
	ascending bool
}

func renderOrdering(axes []orderingAxis) string {
	parts := make([]string, 0, len(axes))
	for _, a := range axes {
		dir := "asc"
		if !a.ascending {
			dir = "desc"
		}
		parts = append(parts, a.axis+":"+dir)
	}
	return "[" + strings.Join(parts, " then ") + "]"
}

// canonicalAxis maps either language's spelling of a cost axis onto the shared
// vocabulary. An expression naming no known axis returns "", which the callers
// treat as a parser drift rather than silently skipping a comparison.
func canonicalAxis(expr string) string {
	switch {
	case strings.Contains(expr, "MemoryRequiredBytes"), strings.Contains(expr, "memory_required_bytes"):
		return "memory"
	case strings.Contains(expr, "StorageRequiredBytes"), strings.Contains(expr, "storage_required_bytes"):
		return "storage"
	case strings.Contains(expr, "catalogueKey"), strings.Contains(expr, ".key"):
		return "identity"
	default:
		return ""
	}
}

// parseGoOfferOrdering reads the comparison chain out of sortOffered.
//
// Each `return <lhs> <op> <rhs>` inside the less-function is one axis, in the
// order the chain applies them; `<` is ascending and `>` descending.
func parseGoOfferOrdering(t *testing.T, src string) []orderingAxis {
	t.Helper()

	block := regexp.MustCompile(`(?s)func sortOffered\(offered \[\]Option\) \{(.*?)\n\}`).FindStringSubmatch(src)
	if block == nil {
		t.Fatal("sortOffered not found in internal/selection/family.go")
	}

	var out []orderingAxis
	// A comparison named inside an explanatory comment must not read as a
	// declaration — the same care the precision agreement test takes.
	returnCmp := regexp.MustCompile(`^return\s+(.+?)\s*([<>])\s*(.+)$`)
	for _, line := range strings.Split(block[1], "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		m := returnCmp.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		axis := canonicalAxis(m[1])
		if axis == "" {
			t.Fatalf("sortOffered compares %q, which names no axis this test knows; "+
				"either a new cost axis was added and this parser must learn it, or the "+
				"comparison has drifted", m[1])
		}
		out = append(out, orderingAxis{axis: axis, ascending: m[2] == "<"})
	}
	return out
}

// parsePythonGateOrdering reads the sort key out of the gate's select().
//
// The key is a tuple, so its axes are ordered left to right. Direction is
// ascending unless the field is negated (`-e.memory_required_bytes`) or the
// call passes reverse=True — the two ways Python spells most-capable-first.
func parsePythonGateOrdering(t *testing.T, src string) []orderingAxis {
	t.Helper()

	call := regexp.MustCompile(`(?m)^\s*admissible\.sort\((.*)\)\s*$`).FindStringSubmatch(src)
	if call == nil {
		t.Fatal("admissible.sort(...) not found in container/helix_model_gate.py select()")
	}
	args := call[1]

	reversed := regexp.MustCompile(`reverse\s*=\s*True`).MatchString(args)

	tuple := regexp.MustCompile(`key\s*=\s*lambda\s+\w+\s*:\s*\((.*?)\)`).FindStringSubmatch(args)
	if tuple == nil {
		t.Fatalf("could not read the sort key tuple out of %q; the gate's ranking is no "+
			"longer a lambda over a tuple and this parser must learn its new shape rather "+
			"than silently reporting agreement", args)
	}

	var out []orderingAxis
	for _, field := range strings.Split(tuple[1], ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		axis := canonicalAxis(field)
		if axis == "" {
			t.Fatalf("the gate sorts on %q, which names no axis this test knows; either a "+
				"new cost axis was added and this parser must learn it, or the key has drifted", field)
		}
		// A negated numeric field reverses that axis alone; reverse=True
		// reverses every axis.
		ascending := !strings.HasPrefix(field, "-")
		if reversed {
			ascending = !ascending
		}
		out = append(out, orderingAxis{axis: axis, ascending: ascending})
	}
	return out
}

func selectionRepoRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolved repo root %s has no go.mod; the guard is looking in the wrong place", root)
	}
	return root
}

func readSourceFile(t *testing.T, p string) string {
	t.Helper()

	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}
