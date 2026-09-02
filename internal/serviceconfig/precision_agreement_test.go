package serviceconfig

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The Go boot lane and the Python service each hold a list saying which
// precisions can be served. They are two independent statements about the same
// question, and they disagreed: Go's servingPrecisions declared "gguf-q4"
// servable while the service refused it in _UNIMPLEMENTED_PRECISIONS.
//
// Nothing noticed, because ordering happened to rank a servable fp8 build first.
// When offer ordering changed to cheapest-first, the cheaper gguf-q4 entry moved
// to the front and the lane began choosing a build the runtime refuses — admit
// the VRAM, compose up, 503 for the full health timeout, exit 4, holding the
// lease throughout. A correct-looking `plan` all the way to a dead container.
//
// This test binds the two lists so they cannot drift apart again. The SERVICE is
// authoritative: it is the thing that actually loads weights.
func TestGoServingPrecisionsAgreeWithTheService(t *testing.T) {
	root := repoRoot(t)

	goSrc := readFile(t, filepath.Join(root, "cmd", "videogen-boot", "modelchoice.go"))
	pySrc := readFile(t, filepath.Join(root, "services", "videogen", "videogen_server.py"))

	goServable := parseGoServingPrecisions(t, goSrc)
	pyRefused := parsePyUnimplementedPrecisions(t, pySrc)

	if len(goServable) == 0 {
		t.Fatal("parsed no precisions from servingPrecisions — the parser has drifted from the source")
	}
	if len(pyRefused) == 0 {
		t.Fatal("parsed no precisions from _UNIMPLEMENTED_PRECISIONS — the parser has drifted from the source")
	}

	for p := range goServable {
		if _, refused := pyRefused[p]; refused {
			t.Errorf("the boot lane declares precision %q servable, but the service refuses it in "+
				"_UNIMPLEMENTED_PRECISIONS. The lane can select a build the runtime cannot load: it "+
				"admits the memory, starts the container, and health-checks a service that will never "+
				"become ready. The service is authoritative — remove %q from servingPrecisions, or add "+
				"the load path to the service in the same change.", p, p)
		}
	}
	t.Logf("go servable=%v  service refuses=%v  (disjoint)", keys(goServable), keys(pyRefused))
}

func parseGoServingPrecisions(t *testing.T, src string) map[string]struct{} {
	t.Helper()
	block := regexp.MustCompile(`(?s)var servingPrecisions = map\[string\]string\{(.*?)\n\}`).FindStringSubmatch(src)
	if block == nil {
		t.Fatal("servingPrecisions not found in modelchoice.go")
	}
	// Only real entries, never a commented-out one: a precision named inside an
	// explanatory comment must not read as a declaration.
	out := map[string]struct{}{}
	for _, line := range regexp.MustCompile(`\n`).Split(block[1], -1) {
		trimmed := regexp.MustCompile(`^\s+`).ReplaceAllString(line, "")
		if trimmed == "" || regexp.MustCompile(`^//`).MatchString(trimmed) {
			continue
		}
		if m := regexp.MustCompile(`^"([a-z0-9-]+)":`).FindStringSubmatch(trimmed); m != nil {
			out[m[1]] = struct{}{}
		}
	}
	return out
}

func parsePyUnimplementedPrecisions(t *testing.T, src string) map[string]struct{} {
	t.Helper()
	block := regexp.MustCompile(`(?s)_UNIMPLEMENTED_PRECISIONS = \{(.*?)\n\}`).FindStringSubmatch(src)
	if block == nil {
		t.Fatal("_UNIMPLEMENTED_PRECISIONS not found in videogen_server.py")
	}
	out := map[string]struct{}{}
	for _, m := range regexp.MustCompile(`(?m)^\s+"([a-z0-9-]+)":`).FindAllStringSubmatch(block[1], -1) {
		out[m[1]] = struct{}{}
	}
	return out
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}
