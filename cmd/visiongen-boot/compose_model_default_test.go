package main

import (
	"os"
	"strings"
	"testing"
)

// T076, the layer below the binary.
//
// cmd/visiongen-boot was migrated onto measured selection: it measures the host,
// asks the catalogue what fits, and REFUSES — starting nothing — when the answer
// is "nothing" (exitNoOptionOffered, exitHostNotMeasured). measured_selection_test.go
// guards that.
//
// None of it helps if the compose file one layer down supplies its own answer.
// This lane shipped
//
//   - /models/${VISIONGEN_MODEL_GGUF:-Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf}
//
// which means the variable is never actually absent: `podman compose up` on this
// file, run directly rather than through the binary, serves that model on a host
// nothing ever measured. The refusal path is not weakened, it is UNREACHABLE.
//
// This is the imagegen defect, exactly. That lane's binary was migrated while
// imagegen_server.py kept `MODEL_ID = _env("IMAGEGEN_MODEL", "black-forest-labs/
// FLUX.1-schnell")` and its Containerfile baked the variable; the fix removed
// both, and services/imagegen/Containerfile now states the reasoning in full.
// cmd/imagegen-boot/compose.imagegen.yml's bare `${IMAGEGEN_MODEL}` is the form
// this lane now follows.
//
// SCOPE, stated rather than implied: this reads THIS lane's compose file only.
// cmd/agentgen-boot/compose.agent.yml carries the same defaulted shape
// (`${AGENTGEN_MODEL_GGUF:-Mistral-Nemo-Instruct-2407-Q4_K_M.gguf}`) and that
// lane has no modelchoice.go at all — it has not been migrated, so it is not
// this test's business and is reported rather than silently swept in.
const composeFile = "compose.vision.yml"

// composeBody returns the file with YAML comments removed.
//
// Comments are stripped because the assertions below look for model names and
// default-interpolation syntax, and the header comment now QUOTES both while
// explaining why neither may appear in the configuration itself. A scanner that
// could not tell an explanation from a setting would forbid documenting the
// rule it enforces.
func composeBody(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatalf("read %s: %v", composeFile, err)
	}

	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		// YAML starts a comment at a '#' that opens the line or follows
		// whitespace; a '#' inside a token is part of the token.
		if i := strings.Index(line, " #"); i >= 0 {
			line = line[:i]
		}
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "#") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// The two variables that name the model must be interpolated with NO default.
func TestComposeSuppliesNoModelDefault(t *testing.T) {
	body := composeBody(t)

	for _, key := range []string{weightsKey, projectorKey} {
		// Present at all: a rule about how a variable is interpolated proves
		// nothing if the interpolation was deleted.
		if !strings.Contains(body, "${"+key+"}") {
			t.Errorf("%s does not interpolate ${%s} at all; measured selection writes that "+
				"variable (applyChoice) and the container must read it", composeFile, key)
		}

		// Every form of default the shell-style interpolation compose uses
		// supports: :- and - (use default), := and = (assign default).
		for _, form := range []string{":-", "-", ":=", "="} {
			if !strings.Contains(body, "${"+key+form) {
				continue
			}
			t.Errorf(`%s defaults ${%s} with %q.

A default here makes the refusal path unreachable. visiongen-boot measures this
host and starts nothing when no catalogued option fits — but a default means the
variable is never absent, so bringing this file up directly serves whatever model
is written here on a host that was never measured against it (FR-056).

The fix is to remove the default, not to pick a safer one: any literal here is a
model that nothing chose. See cmd/imagegen-boot/compose.imagegen.yml, which
interpolates a bare ${IMAGEGEN_MODEL} for the same reason.`,
				composeFile, key, form)
		}
	}
}

// The name-agnostic half: no model artefact may be named in the configuration
// at all, whatever variable it hides behind.
//
// The assertion above can be satisfied by moving the literal — writing it into a
// second variable, or straight into the command with no interpolation. This one
// cannot: a .gguf or .safetensors filename appearing anywhere in the served
// configuration IS a model that no measurement chose.
func TestComposeNamesNoModelArtefact(t *testing.T) {
	body := composeBody(t)

	for _, ext := range []string{".gguf", ".safetensors", ".bin"} {
		if !strings.Contains(body, ext) {
			continue
		}
		for _, line := range strings.Split(body, "\n") {
			if strings.Contains(line, ext) {
				t.Errorf("%s names a model artefact in its configuration:\n    %s\n\n"+
					"Which weights run is an OUTPUT of measured selection, written into "+
					"${%s}/${%s} by visiongen-boot. A filename here serves a model no host "+
					"was measured against (FR-056).",
					composeFile, strings.TrimSpace(line), weightsKey, projectorKey)
			}
		}
	}
}

// The whole file, not just the two known keys: any variable whose NAME says it
// selects a model must be undefaulted too, so the next model-selecting variable
// added to this lane inherits the rule instead of quietly escaping it.
//
// Deliberately narrow. VISIONGEN_MEM_LIMIT and VISIONGEN_SHM_SIZE do carry
// defaults and must keep them: they are §12.6 host-safety bounds, not a claim
// about which model fits.
func TestComposeDefaultsNothingThatSelectsAModel(t *testing.T) {
	selectsAModel := func(name string) bool {
		for _, marker := range []string{"MODEL", "GGUF", "MMPROJ", "WEIGHTS", "CHECKPOINT"} {
			if strings.Contains(name, marker) {
				return true
			}
		}
		return false
	}

	body := composeBody(t)
	for _, chunk := range strings.Split(body, "${")[1:] {
		end := strings.IndexByte(chunk, '}')
		if end < 0 {
			continue
		}
		expr := chunk[:end]

		name, rest, defaulted := expr, "", false
		if i := strings.IndexAny(expr, ":-="); i >= 0 {
			name, rest, defaulted = expr[:i], expr[i:], true
		}
		if !defaulted || !selectsAModel(name) {
			continue
		}
		// VISIONGEN_MODEL_DIR is a LOCATION — where the artefacts live — not a
		// model. Defaulting a search path does not decide what runs.
		if strings.HasSuffix(name, "_DIR") || strings.HasSuffix(name, "_PATH") {
			continue
		}
		t.Errorf("%s defaults ${%s%s}, a variable that names a model. Which model runs is "+
			"decided by measurement, never by a literal in this file (FR-056).",
			composeFile, name, rest)
	}
}
