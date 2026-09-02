package naming_test

import (
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/naming"
)

func TestIdentityString(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		host, model, variant string
		want                 string
	}{
		{"host and model", "gpu-01", "llama3", "", "helixllm/gpu-01/llama3"},
		{"with variant", "gpu-01", "llama3", "8b", "helixllm/gpu-01/llama3:8b"},
		{"dotted host", "node.local", "qwen2.5", "14b", "helixllm/node.local/qwen2.5:14b"},
		// A ':' inside the model would otherwise be indistinguishable from the
		// variant separator, so it is escaped.
		{"colon in model", "gpu-01", "llama3:8b", "", `helixllm/gpu-01/llama3\:8b`},
		// A '/' inside the model would otherwise look like a field boundary.
		{"slash in model", "gpu-01", "org/llama3", "", `helixllm/gpu-01/org\/llama3`},
		{"slash in host", "a/b", "m", "", `helixllm/a\/b/m`},
		{"backslash in model", "h", `a\b`, "", `helixllm/h/a\\b`},
		{"colon in variant", "h", "m", "a:b", `helixllm/h/m:a\:b`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, err := naming.NewIdentity(tc.host, tc.model, tc.variant)
			if err != nil {
				t.Fatalf("NewIdentity(%q,%q,%q): %v", tc.host, tc.model, tc.variant, err)
			}
			if got := id.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestIdentityCarriesItsProvenanceAndHost is SC-015 stated as a test: the
// identity alone says HelixLLM-served, and names the serving host.
func TestIdentityCarriesItsProvenanceAndHost(t *testing.T) {
	id, err := naming.NewIdentity("gpu-01", "llama3", "8b")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	s := id.String()
	if !strings.HasPrefix(s, naming.IdentityPrefix+"/") {
		t.Errorf("identity %q does not open with the %q provenance prefix", s, naming.IdentityPrefix)
	}
	if !strings.Contains(s, "gpu-01") {
		t.Errorf("identity %q does not name its serving host", s)
	}
}

// TestIdentityRoundTrips — escaping is only defensible if it is reversible;
// otherwise an awkward name is silently corrupted rather than handled.
func TestIdentityRoundTrips(t *testing.T) {
	for _, want := range []naming.Identity{
		{Host: "gpu-01", Model: "llama3"},
		{Host: "gpu-01", Model: "llama3", Variant: "8b"},
		{Host: "node.local", Model: "qwen2.5", Variant: "14b"},
		{Host: "gpu-01", Model: "llama3:8b"},
		{Host: "gpu-01", Model: "org/llama3", Variant: "q4_K_M"},
		{Host: "a/b", Model: "c:d", Variant: "e/f:g"},
		{Host: "h", Model: `back\slash`},
		{Host: "höst", Model: "модель", Variant: "большой"},
		{Host: "my host", Model: "my model"},
		{Host: "h", Model: "$(whoami)"},
		{Host: "h", Model: "a;b|c&d"},
	} {
		t.Run(want.Model, func(t *testing.T) {
			id, err := naming.NewIdentity(want.Host, want.Model, want.Variant)
			if err != nil {
				t.Fatalf("NewIdentity: %v", err)
			}
			got, err := naming.ParseIdentity(id.String())
			if err != nil {
				t.Fatalf("ParseIdentity(%q): %v", id.String(), err)
			}
			if got != id {
				t.Errorf("round trip lost data: %#v -> %q -> %#v", id, id.String(), got)
			}
		})
	}
}

// TestIdentityDistinguishesTheVariantBoundary — a model literally named
// "llama3:8b" is a different option from model "llama3" variant "8b", and the
// identity string must keep them apart.
func TestIdentityDistinguishesTheVariantBoundary(t *testing.T) {
	withVariant, err := naming.NewIdentity("gpu-01", "llama3", "8b")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	colonInModel, err := naming.NewIdentity("gpu-01", "llama3:8b", "")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	if withVariant.String() == colonInModel.String() {
		t.Errorf("both identities render as %q; the variant boundary is ambiguous",
			withVariant.String())
	}
}

func TestNewIdentityNormalises(t *testing.T) {
	// Surrounding whitespace is an input artefact, never part of a name.
	id, err := naming.NewIdentity("  gpu-01  ", "  llama3  ", "  8b  ")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	if want := "helixllm/gpu-01/llama3:8b"; id.String() != want {
		t.Errorf("String() = %q, want %q", id.String(), want)
	}

	// Hostnames are case-insensitive (RFC 4343), so one machine is one option
	// however the operator happened to type it.
	upper, err := naming.NewIdentity("GPU-01", "llama3", "")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	lower, err := naming.NewIdentity("gpu-01", "llama3", "")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	if upper.String() != lower.String() {
		t.Errorf("host case is significant: %q vs %q", upper.String(), lower.String())
	}

	// Model names are NOT case-folded: upstream registries treat them as
	// case-sensitive, so folding would merge two genuinely different models.
	mixed, err := naming.NewIdentity("h", "Llama3", "")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	plain, err := naming.NewIdentity("h", "llama3", "")
	if err != nil {
		t.Fatalf("NewIdentity: %v", err)
	}
	if mixed.String() == plain.String() {
		t.Errorf("model case was folded; %q and %q became one identity",
			"Llama3", "llama3")
	}
}

func TestNewIdentityRejectsUnusableInput(t *testing.T) {
	for name, tc := range map[string]struct{ host, model, variant string }{
		"empty host":            {"", "llama3", ""},
		"empty model":           {"gpu-01", "", ""},
		"whitespace-only host":  {"   ", "llama3", ""},
		"whitespace-only model": {"gpu-01", "\t", ""},
		"newline in host":       {"gpu\n01", "llama3", ""},
		"newline in model":      {"gpu-01", "llama\n3", ""},
		"newline in variant":    {"gpu-01", "llama3", "8\nb"},
		"control byte in model": {"gpu-01", "llama\x003", ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := naming.NewIdentity(tc.host, tc.model, tc.variant); err == nil {
				t.Errorf("expected an error, got identity %q", got.String())
			}
		})
	}
}

func TestParseIdentityRejectsMalformedInput(t *testing.T) {
	for name, s := range map[string]string{
		"empty":           "",
		"wrong prefix":    "openai/gpu-01/llama3",
		"no prefix":       "gpu-01/llama3",
		"missing model":   "helixllm/gpu-01",
		"empty host":      "helixllm//llama3",
		"empty model":     "helixllm/gpu-01/",
		"trailing escape": `helixllm/gpu-01/llama3\`,
		"too many fields": "helixllm/a/b/c",
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := naming.ParseIdentity(s); err == nil {
				t.Errorf("ParseIdentity(%q): expected an error, got %#v", s, got)
			}
		})
	}
}

func TestIdentityValidate(t *testing.T) {
	if err := (naming.Identity{Host: "h", Model: "m"}).Validate(); err != nil {
		t.Errorf("a well-formed identity failed Validate: %v", err)
	}
	for name, id := range map[string]naming.Identity{
		"zero":                   {},
		"no host":                {Model: "m"},
		"no model":               {Host: "h"},
		"newline":                {Host: "h\n", Model: "m"},
		"unnormalised host case": {Host: "H", Model: "m"},
		"untrimmed":              {Host: " h ", Model: "m"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := id.Validate(); err == nil {
				t.Errorf("Validate(%#v) = nil, want an error", id)
			}
		})
	}
}
