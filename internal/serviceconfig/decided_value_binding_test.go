package serviceconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// EX-11, closed structurally rather than by a name heuristic.
//
// EX-11's fix gave the imagegen service a `_decided()` helper that takes NO
// default: a value it cannot read is absent, and the service refuses instead of
// substituting one. videogen has the same helper. The helper's whole guarantee
// is "if nothing decided this, nothing runs".
//
// A Containerfile `ENV <THAT_VARIABLE>=<value>` voids the guarantee completely
// and leaves no trace: the process starts with the variable already populated,
// `_decided()` reads it happily, and the refusal path is unreachable. The Python
// guard cannot see this — it reads the .py — and a name-based Containerfile
// heuristic only catches variables somebody thought to name.
//
// This binds the TWO ARTIFACTS TOGETHER instead. The forbidden set is not a list
// anyone maintains: it is derived, per service, from that service's own
// `_decided("X")` / `_decided_int("X")` call sites. Adding a new decided value
// automatically extends what its image may not bake — the pairing cannot drift,
// because neither side is written twice.

// decidedCall matches the no-default reads whose contract this enforces.
var decidedCall = regexp.MustCompile(`_decided(?:_int)?\(\s*"([A-Za-z_][A-Za-z0-9_]*)"\s*\)`)

func TestContainerfileDoesNotBakeADecidedValue(t *testing.T) {
	root := repoRoot(t)

	serviceDirs, err := filepath.Glob(filepath.Join(root, "services", "*"))
	if err != nil {
		t.Fatalf("glob services: %v", err)
	}

	pairs := 0
	found := map[string]string{}

	for _, dir := range serviceDirs {
		info, statErr := os.Stat(dir)
		if statErr != nil || !info.IsDir() {
			continue
		}
		containerfile := filepath.Join(dir, "Containerfile")
		if _, statErr := os.Stat(containerfile); statErr != nil {
			continue
		}

		decided := decidedValues(t, dir)
		if len(decided) == 0 {
			continue // this service declares no no-default reads; nothing to bind
		}
		pairs++

		rel, relErr := filepath.Rel(root, containerfile)
		if relErr != nil {
			t.Fatalf("relativise %s: %v", containerfile, relErr)
		}

		for _, a := range dockerfileAssignments(t, containerfile) {
			if !decided[a.name] || a.value == "" {
				continue
			}
			found[fmt.Sprintf("%s:%s", rel, a.name)] = a.value
		}
		t.Logf("%s declares %d decided values: %v", rel, len(decided), sortedKeys(decided))
	}

	if pairs == 0 {
		t.Fatal("no service pairs a Containerfile with a _decided() reader; the guard would prove nothing")
	}

	for key, value := range found {
		want, pinned := knownUnfixed[key]
		if !pinned {
			t.Errorf("%s = %q, but the service reads that variable through a no-default helper.\n"+
				"  `_decided()` exists so an undecided container REFUSES rather than substituting a "+
				"model nothing measured (EX-11). Baking the value into the image makes the refusal "+
				"path unreachable: the process starts with it already set, the helper reads it, and "+
				"the container serves a value no host was measured against.\n"+
				"  Remedy: delete the assignment. The boot binary writes these from the measured decision.",
				key, value)
			continue
		}
		if want != value {
			t.Errorf("%s = %q, pinned as known-unfixed with value %q — the pin no longer describes "+
				"the tree; re-investigate before updating it", key, value, want)
		}
	}
}

// decidedValues returns every environment variable a service reads through a
// no-default helper, across every Python source in its directory.
func decidedValues(t *testing.T, dir string) map[string]bool {
	t.Helper()

	sources, err := filepath.Glob(filepath.Join(dir, "*.py"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}

	out := map[string]bool{}
	for _, path := range sources {
		if strings.HasPrefix(filepath.Base(path), "test_") {
			continue // a test naming the variable is not the service reading it
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		for _, m := range decidedCall.FindAllStringSubmatch(string(raw), -1) {
			out[m[1]] = true
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
