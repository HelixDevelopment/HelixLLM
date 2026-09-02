package selection_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/capability/testdata/fixtures"
	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
	"github.com/stretchr/testify/require"
)

// FR-056: which model runs is decided from the measured host, never from a
// static preconfigured value.
//
// This is a regression guard against a defect that is live in the artifact
// this feature replaces: the boot paths pick their model out of the
// environment (VISIONGEN_MODEL_GGUF / VISIONGEN_MODEL_DIR in
// cmd/visiongen-boot, AGENTGEN_MODEL_DIR in cmd/agentgen-boot). Selection must
// never grow that behaviour, so the guard is both behavioural — the same
// answer with the variables set as without — and structural: this package
// reads no environment at all, so there is nothing to honour.

// modelNamingEnvironment is the set of variables that name a model today, plus
// the shapes a future one would plausibly take. Selection must be indifferent
// to every one of them.
func modelNamingEnvironment() map[string]string {
	return map[string]string{
		// Honoured by the artifact this feature replaces.
		"VISIONGEN_MODEL_GGUF": "/models/statically-named-model.gguf",
		"VISIONGEN_MODEL_DIR":  "/models/statically-named",
		"AGENTGEN_MODEL_DIR":   "/models/statically-named",
		// Shapes a regression would plausibly take here.
		"HELIXLLM_MODEL":         "statically-named-model",
		"HELIXLLM_DEFAULT_MODEL": "statically-named-model",
		"HELIXLLM_MODEL_ID":      "statically-named-model",
	}
}

const staticallyNamedModel = "statically-named-model"

// TestEnvironmentDoesNotNameTheModel. The same request answered twice — once
// with every model-naming variable set, once with none — must produce the same
// answer. A difference means the environment reached the decision.
func TestEnvironmentDoesNotNameTheModel(t *testing.T) {
	for name, host := range fixtures.All() {
		t.Run(name, func(t *testing.T) {
			req := request(host, catalogue.UsageCommercial)

			clean, cleanErr := selection.Select(req)

			for k, v := range modelNamingEnvironment() {
				t.Setenv(k, v)
			}
			configured, configuredErr := selection.Select(req)

			require.Equal(t, cleanErr, configuredErr,
				"the environment changed whether selection succeeded")
			require.Equal(t, clean, configured,
				"the environment changed the selected options; the model was named statically")
		})
	}
}

// TestStaticallyNamedModelIsNeverOffered. The variables name a model that is
// not in the catalogue. It must appear nowhere in the answer — not as an offer,
// not as a withheld candidate.
func TestStaticallyNamedModelIsNeverOffered(t *testing.T) {
	for k, v := range modelNamingEnvironment() {
		t.Setenv(k, v)
	}

	res, err := selection.Select(request(fixtures.SingleAccelerator(), catalogue.UsageCommercial))
	require.NoError(t, err)

	for _, fr := range res.Families {
		for _, o := range fr.Offered {
			require.NotContains(t, o.ModelID, staticallyNamedModel,
				"a model named only by the environment was offered")
			require.NotContains(t, o.Identity, staticallyNamedModel)
		}
		for _, w := range fr.Withheld {
			require.NotContains(t, w.ModelID, staticallyNamedModel,
				"a model named only by the environment entered the candidate set")
		}
	}
}

// TestNoFixedDefaultWhenMeasurementIsUnavailable. FR-056's second half: with
// the environment naming a model AND the host unmeasurable, the answer is still
// a refusal. Falling back to the configured name here is exactly the failure
// mode the rule exists to prevent — starting an arbitrary model that may not
// fit.
func TestNoFixedDefaultWhenMeasurementIsUnavailable(t *testing.T) {
	for k, v := range modelNamingEnvironment() {
		t.Setenv(k, v)
	}

	res, err := selection.Select(request(fixtures.Unmeasurable(), catalogue.UsageCommercial))

	require.ErrorIs(t, err, selection.ErrHostNotMeasured)
	require.NotNil(t, res.Refusal)
	require.Empty(t, res.Families, "a configured name must not stand in for a measurement")
}

// TestPinnedModelStillGoesThroughMeasurement. A pin is the one legitimate way a
// caller names a model, and it is a constraint on the choice, not a bypass: the
// pinned entry is looked up in the catalogue and checked against the
// measurement like any other candidate. A name that is in no catalogue is
// refused rather than started.
func TestPinnedModelStillGoesThroughMeasurement(t *testing.T) {
	for k, v := range modelNamingEnvironment() {
		t.Setenv(k, v)
	}

	req := request(fixtures.SingleAccelerator(), catalogue.UsageCommercial)
	req.Pin = &selection.Pin{ModelID: staticallyNamedModel}

	res, err := selection.Select(req)
	require.NoError(t, err)
	require.Len(t, res.Families, 1)

	fr := res.Families[0]
	require.Empty(t, fr.Offered, "a pin naming a model the catalogue does not record must not be offered")
	require.NotNil(t, fr.Refusal)
	require.Equal(t, selection.ReasonUnsupportedConfiguration, fr.Refusal.Reason)
	require.Contains(t, fr.Refusal.Missing(), string(selection.RequirementCatalogueEntry))
}

// TestSelectionPackageReadsNoEnvironment is the structural half of the guard.
//
// The behavioural tests above can only catch a variable they happen to name.
// This one catches any environment read at all in the package's production
// sources, including one added later under a name nobody anticipated.
func TestSelectionPackageReadsNoEnvironment(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, sources, "the package must have sources to scan")

	forbidden := []string{"os.Getenv", "os.LookupEnv", "os.Environ", "syscall.Getenv"}

	scanned := 0
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		scanned++

		content, readErr := os.ReadFile(path)
		require.NoError(t, readErr)

		for _, call := range forbidden {
			require.NotContainsf(t, string(content), call,
				"%s reads the environment (%s); selection is decided from the measured host (FR-056)",
				path, call)
		}
	}

	require.Positive(t, scanned, "no production sources were scanned; the guard proved nothing")
}
