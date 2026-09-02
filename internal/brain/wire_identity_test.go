package brain

import (
	"encoding/json"
	"strings"
	"testing"
)

// The model-listing contract requires each listed model to carry BOTH the
// derived, charset-safe identifier AND the human-readable identity
// helixllm/<host>/<model>. The identifier travels as `id`; the identity needs a
// field of its own on the wire, or a consumer reading only the JSON cannot tell a
// locally-served HelixLLM model from a remote vendor's — which is the single
// thing FR-014 asks the identity to make obvious.
func TestModelIdentityIsCarriedOnTheWire(t *testing.T) {
	opts := []ModelOption{{
		Identifier: "helixllm-gpu-01-llama3-39b96cf9c493",
		Identity:   "helixllm/gpu-01/llama3",
		OwnedBy:    "llamacpp",
		Available:  true,
	}}

	m := modelsFromOptions(opts)
	if len(m) != 1 {
		t.Fatalf("got %d models, want 1", len(m))
	}

	blob, err := json.Marshal(m[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(blob)

	if !strings.Contains(got, `"model_identity":"helixllm/gpu-01/llama3"`) {
		t.Fatalf("serialised model does not carry the identity on the wire:\n  %s", got)
	}
	if !strings.Contains(got, `"id":"helixllm-gpu-01-llama3-39b96cf9c493"`) {
		t.Fatalf("id must remain the derived charset-safe identifier:\n  %s", got)
	}
}

// A remote vendor model must NOT wear the helixllm identity: stamping it on a
// vendor's model destroys the distinction the identity exists to draw.
func TestRemoteModelCarriesNoHelixLLMIdentity(t *testing.T) {
	opts := []ModelOption{{
		Identifier: "gpt-4o",
		Identity:   "",
		OwnedBy:    "openai",
		Available:  true,
	}}

	blob, _ := json.Marshal(modelsFromOptions(opts)[0])
	if strings.Contains(string(blob), "model_identity") {
		t.Fatalf("a remote model must not carry a model_identity field:\n  %s", string(blob))
	}
}

// HelixAgent decodes `host` and `availability` from this listing (see
// helix_agent/internal/adapters/helixllm/types.go). Until HelixLLM emits them, a
// legacy payload arrives there as "listed but availability unreported", which
// that side correctly treats as NOT usable. Emitting them is what makes the
// downstream light up.
//
// `availability: "serving"` is not redundant with mere presence: "the serving
// layer affirmatively said it is serving" is a different claim from "the serving
// layer said nothing", and the consumer already distinguishes the two.
func TestServingModelCarriesHostAndAffirmativeAvailability(t *testing.T) {
	opts := []ModelOption{{
		Identifier: "helixllm-gpu-01-llama3-39b96cf9c493",
		Identity:   "helixllm/gpu-01/llama3",
		OwnedBy:    "llamacpp",
		Host:       "gpu-01",
		Available:  true,
	}}

	blob, err := json.Marshal(modelsFromOptions(opts)[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(blob)

	if !strings.Contains(got, `"host":"gpu-01"`) {
		t.Fatalf("serving model must name its host (FR-023):\n  %s", got)
	}
	if !strings.Contains(got, `"availability":"serving"`) {
		t.Fatalf("a listed model must affirmatively report that it is serving:\n  %s", got)
	}
}

// A remote vendor model has no HelixLLM host, and inventing one would be a
// fabricated finding.
func TestRemoteModelClaimsNoHost(t *testing.T) {
	opts := []ModelOption{{Identifier: "gpt-4o", OwnedBy: "openai", Available: true}}
	blob, _ := json.Marshal(modelsFromOptions(opts)[0])
	if strings.Contains(string(blob), `"host"`) {
		t.Fatalf("a remote model must not claim a serving host:\n  %s", string(blob))
	}
}
