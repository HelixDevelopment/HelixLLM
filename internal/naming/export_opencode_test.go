package naming

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestOpenCodeRuleset_ForbidsTheReferenceSeparator is the FR-014a test for the
// OpenCode consumer. OpenCode resolves a model reference by splitting on the
// FIRST "/" (`indexOf("/")`, then `slice(0,i)` / `slice(i+1)` — read out of the
// installed opencode binary, v1.18.x), so a "/" inside a provider key silently
// re-points the reference at a provider that does not exist. The ruleset must
// forbid it, and must never be widened to admit a richer name.
func TestOpenCodeRuleset_ForbidsTheReferenceSeparator(t *testing.T) {
	if err := OpenCode.Validate(); err != nil {
		t.Fatalf("OpenCode ruleset is not self-consistent: %v", err)
	}
	for _, r := range []rune{'/', ':', ' ', '"', '\\'} {
		if OpenCode.Allow(r) {
			t.Errorf("OpenCode ruleset admits %q, which breaks reference parsing or JSON safety", r)
		}
	}
}

// TestExportOpenCode_KeysAreReferenceSafe: both halves of the `provider/model`
// reference a user types must survive OpenCode's split.
func TestExportOpenCode_KeysAreReferenceSafe(t *testing.T) {
	cfg, err := ExportOpenCode(gpuInstance())
	if err != nil {
		t.Fatalf("ExportOpenCode: %v", err)
	}
	keys := append([]string{cfg.ProviderID}, nil...)
	for _, m := range cfg.Models {
		keys = append(keys, m.Identifier)
	}
	for _, k := range keys {
		if k == "" {
			t.Fatalf("empty key in %v", keys)
		}
		for _, r := range k {
			if !OpenCode.Allow(r) {
				t.Errorf("key %q contains %q, which the consumer's rules forbid", k, r)
			}
		}
	}
	// The reference round-trips through OpenCode's own split-on-first-slash.
	for _, m := range cfg.Models {
		ref := cfg.ProviderID + "/" + m.Identifier
		i := strings.Index(ref, "/")
		if ref[:i] != cfg.ProviderID || ref[i+1:] != m.Identifier {
			t.Errorf("reference %q does not split back into %q and %q", ref, cfg.ProviderID, m.Identifier)
		}
	}
}

// TestExportOpenCode_DocumentShape checks the entry against OpenCode's real
// schema: the adapter name it dispatches on, the baseURL its openai-compatible
// adapter concatenates "/chat/completions" onto, the per-model `id` that
// becomes the wire model, and the `name` that is only ever displayed.
func TestExportOpenCode_DocumentShape(t *testing.T) {
	cfg, err := ExportOpenCode(gpuInstance())
	if err != nil {
		t.Fatalf("ExportOpenCode: %v", err)
	}

	var doc struct {
		Provider map[string]struct {
			NPM     string `json:"npm"`
			Name    string `json:"name"`
			Options struct {
				BaseURL string `json:"baseURL"`
				APIKey  string `json:"apiKey"`
			} `json:"options"`
			Models map[string]struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(cfg.Document, &doc); err != nil {
		t.Fatalf("document is not valid JSON: %v\n%s", err, cfg.Document)
	}
	entry, ok := doc.Provider[cfg.ProviderID]
	if !ok {
		t.Fatalf("document has no entry for %q: %s", cfg.ProviderID, cfg.Document)
	}
	if entry.NPM != "@ai-sdk/openai-compatible" {
		t.Errorf("npm = %q, want the openai-compatible adapter", entry.NPM)
	}
	if entry.Options.BaseURL != "http://gpu-01:18434/v1" {
		t.Errorf("baseURL = %q, want the versioned base the adapter appends onto", entry.Options.BaseURL)
	}
	if entry.Options.APIKey != "" {
		t.Errorf("an apiKey reached the exported configuration: %q", entry.Options.APIKey)
	}
	if len(entry.Models) != 2 {
		t.Fatalf("got %d models, want 2 (the unavailable one is withheld)", len(entry.Models))
	}
	for key, m := range entry.Models {
		if strings.Contains(key, "/") {
			t.Errorf("model key %q contains the reference separator", key)
		}
		if m.ID == "" {
			t.Errorf("model %q has no wire id; OpenCode would send the key upstream", key)
		}
		if !strings.HasPrefix(m.Name, IdentityPrefix+"/") {
			t.Errorf("model %q name %q is not the human-readable identity", key, m.Name)
		}
		if m.Name == key {
			t.Errorf("model %q uses its identity as the key", key)
		}
	}
	// The served name that carries a "/" survives as a VALUE, not as a key.
	var sawPath bool
	for _, m := range entry.Models {
		if m.ID == "/models/Qwen3-Coder.gguf" {
			sawPath = true
		}
	}
	if !sawPath {
		t.Errorf("the served model name was not carried through as the wire id: %s", cfg.Document)
	}
}

// TestMergeOpenCode_IdempotentAndAdditive holds contract invariant 3 and the
// established OpenCode sync rule that a pre-existing provider is never
// clobbered.
func TestMergeOpenCode_IdempotentAndAdditive(t *testing.T) {
	cfg, err := ExportOpenCode(gpuInstance())
	if err != nil {
		t.Fatalf("ExportOpenCode: %v", err)
	}
	existing := []byte(`{
  "$schema": "https://opencode.ai/config.json",
  "provider": {"anthropic": {"options": {"apiKey": "operator-key"}}},
  "mcp": {"codegraph": {"type": "local", "enabled": true}}
}`)

	once, err := MergeOpenCode(existing, cfg)
	if err != nil {
		t.Fatalf("first merge: %v", err)
	}
	twice, err := MergeOpenCode(once, cfg)
	if err != nil {
		t.Fatalf("second merge: %v", err)
	}
	if string(once) != string(twice) {
		t.Errorf("merge is not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(twice, &got); err != nil {
		t.Fatalf("merged document is not valid JSON: %v", err)
	}
	for _, key := range []string{"$schema", "mcp", "provider"} {
		if _, ok := got[key]; !ok {
			t.Errorf("merge dropped top-level key %q", key)
		}
	}
	var providers map[string]json.RawMessage
	if err := json.Unmarshal(got["provider"], &providers); err != nil {
		t.Fatalf("provider is not an object: %v", err)
	}
	if _, ok := providers["anthropic"]; !ok {
		t.Errorf("merge clobbered the operator's own provider entry")
	}
	if !strings.Contains(string(providers["anthropic"]), "operator-key") {
		t.Errorf("merge rewrote the operator's own provider entry: %s", providers["anthropic"])
	}
	if _, ok := providers[cfg.ProviderID]; !ok {
		t.Errorf("merge did not add %q", cfg.ProviderID)
	}

	// A later export with a smaller model set REPLACES our entry rather than
	// accumulating stale models beside it.
	shrunk := gpuInstance()
	shrunk.Offers = shrunk.Offers[:1]
	small, err := ExportOpenCode(shrunk)
	if err != nil {
		t.Fatalf("ExportOpenCode(shrunk): %v", err)
	}
	updated, err := MergeOpenCode(twice, small)
	if err != nil {
		t.Fatalf("update merge: %v", err)
	}
	if strings.Contains(string(updated), "Qwen3-Coder") {
		t.Errorf("update left a stale model behind:\n%s", updated)
	}
}

// TestExportOpenCode_UnavailableIsWithheld holds invariant 4: an unavailable
// model is absent from `models`, so OpenCode's picker cannot offer it.
func TestExportOpenCode_UnavailableIsWithheld(t *testing.T) {
	cfg, err := ExportOpenCode(gpuInstance())
	if err != nil {
		t.Fatalf("ExportOpenCode: %v", err)
	}
	if strings.Contains(string(cfg.Document), "mistral") {
		t.Errorf("an unavailable model reached the document:\n%s", cfg.Document)
	}
	if len(cfg.Withheld) != 1 || cfg.Withheld[0].Reason == "" {
		t.Errorf("withheld set is %v, want one entry carrying a reason", cfg.Withheld)
	}

	down := gpuInstance()
	down.Healthy = false
	down.Reason = "unreachable"
	cfg, err = ExportOpenCode(down)
	if err != nil {
		t.Fatalf("ExportOpenCode(unhealthy): %v", err)
	}
	if len(cfg.Models) != 0 {
		t.Errorf("unreachable instance exported %d usable models", len(cfg.Models))
	}
}

// TestExportOpenCode_SecretNeverReachesTheConfiguration holds invariant 5.
func TestExportOpenCode_SecretNeverReachesTheConfiguration(t *testing.T) {
	inst := gpuInstance()
	inst.BaseURL = "http://helix:s3cr3t@gpu-01:18434"
	cfg, err := ExportOpenCode(inst)
	if err == nil {
		t.Fatalf("credentials in the base URL were accepted:\n%s", cfg.Document)
	}
	if strings.Contains(err.Error(), "s3cr3t") {
		t.Errorf("the error message leaks the secret: %v", err)
	}
}
