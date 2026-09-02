// Package serviceconfig holds guards over the DEPLOYED configuration surface —
// the Containerfiles and service configuration that surround the Go code.
//
// It is deliberately test-only. Nothing here is imported by the binaries: these
// are repository lints whose true home is a build-time check, implemented as Go
// tests so that they RUN in the ordinary `go test ./internal/...` sweep rather
// than in a script somebody remembers to invoke. A guard nothing runs is not a
// guard.
package serviceconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// EX-14: `Containerfile.whisper` carried `WHISPER_MODEL=base`.
//
// That is a configuration value naming the model to run — exactly the static
// selection FR-056 replaces. An unmeasured host silently started `base`: no
// error, no refusal, just a model nothing chose, serving requests as though it
// had been selected. It was fixed by deleting the line, and the ONLY thing
// standing between that fix and its quiet return was a comment in the file
// asking the next person not to.
//
// A comment is not a guard. This is.
//
// WHY THE LAYER MATTERS. EX-11 fixed the same class one level down, in the
// imagegen SERVICE, by removing the Python default so MODEL_ID is read through a
// helper that permits no fallback. But a Containerfile `ENV IMAGEGEN_MODEL=...`
// defeats that fix completely: the variable is already set when the process
// starts, so the no-default read succeeds and returns a model no host was
// measured against. A default deleted from the code and re-added to the image is
// the same defect wearing a different hat, which is why this guard reads the
// image definition and not just the source.
//
// SCOPE, stated rather than implied: this reads Containerfiles only. Compose
// files and runtime `-e` flags can set the same variables and are NOT covered
// here — cmd/*-boot's own tests cover the values those binaries write.

// modelNamingVariable reports whether an environment variable NAMES A MODEL, as
// opposed to describing where models live or how many may run.
//
// The distinction is the whole test. `HELIX_MODELS_MAX=3` is a count,
// `HELIXLLM_WEIGHTS_DIR=/models` is a location, `HF_HOME=/models/hf` is a cache
// — none of them can start a model nobody chose. `WHISPER_MODEL=base` can.
func modelNamingVariable(name string) bool {
	upper := strings.ToUpper(name)

	// Names a model's identity, or the precision the model is loaded at —
	// EX-11: precision "determines the memory footprint admission was granted
	// for", so a baked precision serves a shape nobody admitted VRAM for.
	identity := false
	for _, needle := range []string{"MODEL", "CHECKPOINT", "PRECISION", "WEIGHTS_REPO"} {
		if strings.Contains(upper, needle) {
			identity = true
			break
		}
	}
	if !identity {
		return false
	}

	// ...unless the name says it is a location, a count, or a limit.
	for _, suffix := range []string{
		"_DIR", "_DIRS", "_PATH", "_PATHS", "_HOME", "_ROOT", "_CACHE",
		"_MAX", "_MIN", "_LIMIT", "_COUNT", "_TIMEOUT", "_PORT",
	} {
		if strings.HasSuffix(upper, suffix) {
			return false
		}
	}
	return true
}

// knownUnfixed records violations that are LIVE in the tree today and are NOT
// this task's to fix (§11.4.124: a defect found while doing something else gets
// its own investigation and its own commit).
//
// It is a pin, not an exemption. Each entry is asserted to be STILL PRESENT, so
// the day one is fixed this test FAILS and demands the pin be removed — the list
// cannot silently outlive the defect it describes. Anything NOT on this list
// fails immediately, which is what makes EX-14's return impossible to miss.
//
// FINDING (2026-09-02, T094): services/imagegen/Containerfile bakes the model
// and precision that EX-11 removed from imagegen_server.py. Running that image
// directly — without cmd/imagegen-boot, which overwrites both — starts
// FLUX.1-dev at nvfp4 on a host nothing measured. The file's own comment on the
// line above claims "no hardcoded model/precision literal baked in as
// behaviour", which is the claim this contradicts.
// FINDING (2026-09-02, T094), second instance: services/videogen/Containerfile
// bakes the same pair for the sibling lane. videogen_server.py reads every one
// of these through `_decided()`, which permits no default precisely so an
// undecided container refuses — and the image hands it a decided-looking value
// anyway. The file's comment likewise claims "no hardcoded model/precision
// literal baked in as behaviour".
var knownUnfixed = map[string]string{
	// Empty, and it must stay that way unless a NEW live defect is deliberately
	// deferred with a reason.
	//
	// This list held eight assignments — IMAGEGEN_MODEL/_PRECISION and
	// VIDEOGEN_MODEL/_PRECISION/_BACKEND/_SIZE/_NUM_FRAMES/_FPS — pinned as
	// live-but-not-this-task's-to-fix. They have since been removed from both
	// Containerfiles, and TestKnownUnfixedPinsAreStillLive failed until this list
	// was emptied: a pin cannot outlive the defect it describes, or the guard
	// quietly stops covering that assignment forever.

}

func TestContainerfilesNameNoModel(t *testing.T) {
	root := repoRoot(t)
	files := containerfiles(t, root)
	if len(files) < 2 {
		t.Fatalf("found %d Containerfiles under %s; the guard would prove nothing", len(files), root)
	}

	found := map[string]string{}
	for _, path := range files {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("relativise %s: %v", path, err)
		}
		for _, a := range dockerfileAssignments(t, path) {
			if !modelNamingVariable(a.name) || a.value == "" {
				continue
			}
			found[fmt.Sprintf("%s:%s", rel, a.name)] = a.value
		}
	}

	for key, value := range found {
		want, pinned := knownUnfixed[key]
		if !pinned {
			t.Errorf("%s = %q names a model in the image definition.\n"+
				"  A model named in configuration is a model nothing measured: the container "+
				"starts it on any host, and a service that reads it through a no-default helper "+
				"will find it already set and serve it as though it had been chosen (FR-056, EX-14).\n"+
				"  Remedy: delete the assignment. The model is decided by measuring the host and "+
				"applying the declared usage over the catalogue; the boot binary writes the result.",
				key, value)
			continue
		}
		if want != value {
			t.Errorf("%s = %q, but it is pinned as a known-unfixed violation with value %q. "+
				"The pin no longer describes the tree; re-investigate before updating it.",
				key, value, want)
		}
	}

	// Staleness of the pins is checked once, across BOTH detectors, in
	// TestKnownUnfixedPinsAreStillLive — a key this heuristic cannot see may
	// still be live and seen by the derived binding.
	t.Logf("scanned %d Containerfiles", len(files))
}

// The pins must not outlive the defects. A fixed violation still listed in
// knownUnfixed would silently exempt that file from BOTH guards forever, so the
// day a pinned violation is fixed this test FAILs and demands the pin be
// removed. It runs over the union of what both detectors see, because each sees
// violations the other cannot.
func TestKnownUnfixedPinsAreStillLive(t *testing.T) {
	live := allContainerfileViolations(t, repoRoot(t))

	var stale []string
	for key := range knownUnfixed {
		if _, still := live[key]; !still {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		t.Errorf("%s is pinned as a known-unfixed violation but no longer violates: "+
			"remove it from knownUnfixed so the guards cover that assignment again", key)
	}

	for _, key := range sortedStringKeys(knownUnfixed) {
		t.Logf("KNOWN-UNFIXED (a live defect, not this task's to fix — §11.4.124): %s = %q",
			key, knownUnfixed[key])
	}
}

// allContainerfileViolations is the union of both detectors: name-heuristic
// model naming, and any variable the paired service reads through a no-default
// helper.
func allContainerfileViolations(t *testing.T, root string) map[string]string {
	t.Helper()

	out := map[string]string{}
	for _, path := range containerfiles(t, root) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("relativise %s: %v", path, err)
		}
		decided := decidedValues(t, filepath.Dir(path))
		for _, a := range dockerfileAssignments(t, path) {
			if a.value == "" {
				continue
			}
			if modelNamingVariable(a.name) || decided[a.name] {
				out[fmt.Sprintf("%s:%s", rel, a.name)] = a.value
			}
		}
	}
	return out
}

func sortedStringKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- Dockerfile reading -----------------------------------------------------

type assignment struct{ name, value string }

// dockerfileAssignments returns every ENV/ARG assignment in a Containerfile.
//
// It handles the two shapes that make a naive line grep wrong: backslash
// continuations, and full-line `#` comments INSIDE a continuation (which
// Dockerfile permits and which this repo's files use heavily to explain what the
// variables deliberately do not do).
func dockerfileAssignments(t *testing.T, path string) []assignment {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(string(raw), "\n")

	var out []assignment
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		upper := strings.ToUpper(trimmed)
		if !strings.HasPrefix(upper, "ENV ") && !strings.HasPrefix(upper, "ARG ") {
			continue
		}

		var block []string
		continues := true
		for continues && i < len(lines) {
			line := strings.TrimSpace(lines[i])
			if strings.HasPrefix(line, "#") {
				// A comment inside the block neither contributes text nor ends
				// the continuation.
				i++
				continue
			}
			continues = strings.HasSuffix(line, "\\")
			block = append(block, strings.TrimSuffix(line, "\\"))
			if continues {
				i++
			}
		}

		text := strings.Join(block, " ")
		text = strings.TrimPrefix(strings.TrimPrefix(text, "ENV"), "ARG")
		for _, field := range splitAssignments(text) {
			out = append(out, field)
		}
	}
	return out
}

// splitAssignments pulls `NAME=value` pairs out of an ENV/ARG body, honouring
// double-quoted values (which may contain spaces).
func splitAssignments(text string) []assignment {
	var out []assignment
	i := 0
	for i < len(text) {
		for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
			i++
		}
		start := i
		for i < len(text) && text[i] != '=' && text[i] != ' ' && text[i] != '\t' {
			i++
		}
		name := text[start:i]
		if i >= len(text) || text[i] != '=' || name == "" {
			// Not an assignment (ENV's legacy `ENV key value` form, or trailing
			// text). Skip to the next whitespace.
			for i < len(text) && text[i] != ' ' && text[i] != '\t' {
				i++
			}
			continue
		}
		i++ // consume '='

		var value string
		if i < len(text) && text[i] == '"' {
			i++
			vs := i
			for i < len(text) && text[i] != '"' {
				i++
			}
			value = text[vs:i]
			if i < len(text) {
				i++
			}
		} else {
			vs := i
			for i < len(text) && text[i] != ' ' && text[i] != '\t' {
				i++
			}
			value = text[vs:i]
		}
		out = append(out, assignment{name: name, value: value})
	}
	return out
}

func containerfiles(t *testing.T, root string) []string {
	t.Helper()

	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "vendor", "__pycache__":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(info.Name(), "Containerfile") || strings.HasPrefix(info.Name(), "Dockerfile") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}

// repoRoot resolves the repository root from this package's directory, and
// proves it found the right place rather than assuming a relative depth.
func repoRoot(t *testing.T) string {
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
