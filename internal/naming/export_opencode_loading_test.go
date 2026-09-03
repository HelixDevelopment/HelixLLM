package naming

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// A merge run while the backend is LOADING must not empty the user's entry.
//
// THE DEFECT, REPRODUCED. A host that is up but not yet serving reports itself
// unavailable while still listing its models, so every option is withheld and
// ExportOpenCode produces an entry whose `models` is `{}` — a correct
// description of the moment. MergeOpenCode then replaced the user's entry with
// it, because the replacement rule read withheld as WITHDRAWN. A user who
// merged during a restart lost every model they had been selecting, under a
// successful return, and the document they got back said nothing about why.
//
// Withheld options carry reasons now, so the two cases are separable and the
// assumption is no longer needed.
//
// RED_MODE is the §11.4.115 polarity switch: RED_MODE=1 asserts the defect on
// the pre-fix tree — the merge succeeds and the models are gone — and
// RED_MODE=0 is the standing guard.
func namingRedMode() bool { return os.Getenv("RED_MODE") == "1" }

// loadingInstance is gpuInstance mid-restart: same host, same offered models,
// none of them servable yet.
func loadingInstance() Instance {
	inst := gpuInstance()
	inst.Healthy = false
	inst.Reason = "provider-unavailable"
	return inst
}

// yesterdaysFile is the user's opencode.json from when the host was serving:
// their own provider, and ours carrying the models they select from.
func yesterdaysFile(t *testing.T, providerID string) []byte {
	t.Helper()
	doc := map[string]any{
		"theme": "tokyonight",
		"provider": map[string]any{
			"my-own-openrouter": map[string]any{
				"npm":     openCodeAdapterNPM,
				"name":    "mine",
				"options": map[string]any{"baseURL": "https://openrouter.ai/api/v1"},
				"models": map[string]any{
					"anthropic-claude": map[string]any{"id": "anthropic/claude-3.5", "name": "Claude"},
				},
			},
			providerID: map[string]any{
				"npm":     openCodeAdapterNPM,
				"name":    IdentityPrefix + "/gpu-01",
				"options": map[string]any{"baseURL": "http://gpu-01:18434/v1"},
				"models": map[string]any{
					"helixllm-gpu-01-llama3-8b-aaaaaaaaaaaa": map[string]any{
						"id": "llama3:8b", "name": "helixllm/gpu-01/llama3:8b"},
					"helixllm-gpu-01-mistral-7b-bbbbbbbbbbbb": map[string]any{
						"id": "mistral:7b", "name": "helixllm/gpu-01/mistral:7b"},
				},
			},
		},
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("building the user's existing file: %v", err)
	}
	return raw
}

// modelsUnder returns the `models` map of one provider entry in a document.
func modelsUnder(t *testing.T, document []byte, providerID string) map[string]any {
	t.Helper()
	var root struct {
		Provider map[string]struct {
			Models map[string]any `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(document, &root); err != nil {
		t.Fatalf("merged document does not parse: %v\n%s", err, document)
	}
	entry, ok := root.Provider[providerID]
	if !ok {
		t.Fatalf("merged document has no `provider.%s`:\n%s", providerID, document)
	}
	return entry.Models
}

func TestMergeOpenCode_LoadingBackendDoesNotEmptyTheUsersEntry(t *testing.T) {
	cfg, err := ExportOpenCode(loadingInstance())
	if err != nil {
		t.Fatalf("ExportOpenCode(loading): %v", err)
	}
	// The premise: the export genuinely describes nothing servable. If this
	// ever stops holding, the test below proves nothing.
	if len(cfg.Models) != 0 {
		t.Fatalf("a loading instance exported %d usable models — this test no "+
			"longer exercises the state it is about", len(cfg.Models))
	}
	if len(cfg.Withheld) == 0 {
		t.Fatal("a loading instance withheld nothing, so it carries no reason to act on")
	}

	existing := yesterdaysFile(t, cfg.ProviderID)
	merged, err := MergeOpenCode(existing, cfg)

	if namingRedMode() {
		if err != nil {
			t.Fatalf("RED_MODE=1 expects the pre-fix merge to succeed, got: %v", err)
		}
		if got := modelsUnder(t, merged, cfg.ProviderID); len(got) != 0 {
			t.Fatalf("RED_MODE=1 expects the user's models to be erased, but %d "+
				"survived — the defect is gone; re-run with RED_MODE=0", len(got))
		}
		return
	}

	if err == nil {
		t.Fatalf("merging a loading instance's empty export succeeded and left "+
			"%d models in the user's entry:\n%s",
			len(modelsUnder(t, merged, cfg.ProviderID)), merged)
	}
	if !errors.Is(err, ErrNothingServable) {
		t.Errorf("refused with %v, want ErrNothingServable — the caller has to be "+
			"able to tell a wait-and-retry from a malformed configuration", err)
	}
	// The refusal has to SAY why, or the caller is back to guessing.
	if !strings.Contains(err.Error(), "provider-unavailable") {
		t.Errorf("the refusal does not carry the withheld reason: %v", err)
	}
	if merged != nil {
		t.Errorf("a refused merge still returned a document, which a caller "+
			"could write:\n%s", merged)
	}
}

// The other half of the trade: refusing must not strand an instance that
// genuinely has nothing to offer. It has no offers, so it holds no withheld
// reasons, so it does not meet the refusal's condition — its empty entry merges
// through and clears whatever was there. Without this, the fix would have
// replaced data loss with a configuration that can never be updated.
func TestMergeOpenCode_InstanceWithNoOffersStillConverges(t *testing.T) {
	empty := Instance{Host: "gpu-01", BaseURL: "http://gpu-01:18434", Healthy: true}
	cfg, err := ExportOpenCode(empty)
	if err != nil {
		t.Fatalf("ExportOpenCode(no offers): %v", err)
	}
	if len(cfg.Models) != 0 || len(cfg.Withheld) != 0 {
		t.Fatalf("an instance with no offers produced %d models and %d withheld",
			len(cfg.Models), len(cfg.Withheld))
	}

	merged, err := MergeOpenCode(yesterdaysFile(t, cfg.ProviderID), cfg)
	if namingRedMode() {
		return // unchanged by the fix; nothing to assert about the old tree
	}
	if err != nil {
		t.Fatalf("an instance that genuinely offers nothing cannot converge: %v", err)
	}
	if got := modelsUnder(t, merged, cfg.ProviderID); len(got) != 0 {
		t.Errorf("the stale models were not cleared: %v", got)
	}
	// And the user's own provider is still untouched, as it always was.
	if got := modelsUnder(t, merged, "my-own-openrouter"); len(got) != 1 {
		t.Errorf("the operator's own provider lost its models: %v", got)
	}
}

// Vacuity guard: a merge that refused everything would satisfy the test above
// while being useless. A serving instance must still merge.
func TestMergeOpenCode_ServingInstanceStillMerges(t *testing.T) {
	cfg, err := ExportOpenCode(gpuInstance())
	if err != nil {
		t.Fatalf("ExportOpenCode: %v", err)
	}
	merged, err := MergeOpenCode(yesterdaysFile(t, cfg.ProviderID), cfg)
	if err != nil {
		t.Fatalf("a serving instance was refused: %v", err)
	}
	if got := modelsUnder(t, merged, cfg.ProviderID); len(got) != len(cfg.Models) {
		t.Errorf("merged entry holds %d models, want the %d exported", len(got), len(cfg.Models))
	}
}

// WithheldReasons is what the refusal and the endpoint both say out loud, so
// its output has to be stable and free of duplicates.
func TestWithheldReasons_DistinctAndSorted(t *testing.T) {
	got := WithheldReasons([]WithheldOption{
		{Identity: "a", Reason: "provider-unavailable"},
		{Identity: "b", Reason: "model-unavailable"},
		{Identity: "c", Reason: "provider-unavailable"},
		{Identity: "d", Reason: ""},
	})
	if got != "model-unavailable, provider-unavailable" {
		t.Errorf("WithheldReasons = %q, want the distinct reasons sorted", got)
	}
}
