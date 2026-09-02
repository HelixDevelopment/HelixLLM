package naming

import (
	"strings"
	"testing"
	"unicode"
)

// gpuInstance is the fixture both export suites lean on: one healthy instance
// offering three models, one of which is not being served.
func gpuInstance() Instance {
	must := func(host, model, variant string) Identity {
		id, err := NewIdentity(host, model, variant)
		if err != nil {
			panic(err)
		}
		return id
	}
	return Instance{
		Host:    "gpu-01",
		BaseURL: "http://gpu-01:18434",
		Healthy: true,
		Offers: []Offer{
			{Identity: must("gpu-01", "llama3", "8b"), Available: true},
			{Identity: must("gpu-01", "/models/Qwen3-Coder.gguf", ""), Available: true},
			{Identity: must("gpu-01", "mistral", "7b"), Available: false, Reason: "model-not-loaded"},
		},
	}
}

// TestExportHelixCode_IdentifierIsTheResolvableOne is the load-bearing test for
// T049. HelixCode itself imposes no charset rule on the model field — it passes
// the string verbatim to the upstream body, gguf paths and all. The binding
// constraint is on the OTHER side: Brain.ResolveModelName maps a published
// identifier back to a served model name using the ClaudeToolkit ruleset ONLY,
// so an identifier derived under any other ruleset would arrive at HelixLLM
// unresolvable and silently misroute. The export must therefore publish exactly
// the identifier that resolver can map, and it must satisfy that ruleset as it
// stands (FR-014a).
func TestExportHelixCode_IdentifierIsTheResolvableOne(t *testing.T) {
	cfg, err := ExportHelixCode(gpuInstance())
	if err != nil {
		t.Fatalf("ExportHelixCode: %v", err)
	}
	if len(cfg.Models) != 2 {
		t.Fatalf("got %d exported models, want 2", len(cfg.Models))
	}
	for _, m := range cfg.Models {
		id, err := ParseIdentity(m.Identity)
		if err != nil {
			t.Fatalf("exported identity %q does not parse: %v", m.Identity, err)
		}
		want, err := Derive(id, ClaudeToolkit)
		if err != nil {
			t.Fatalf("Derive(%q): %v", m.Identity, err)
		}
		if m.Identifier != want {
			t.Errorf("identifier %q is not the resolvable one; want %q", m.Identifier, want)
		}
		for _, r := range m.Identifier {
			if !ClaudeToolkit.Allow(r) {
				t.Errorf("identifier %q contains %q, which the consumer's rules forbid", m.Identifier, r)
			}
		}
		if !unicode.IsLetter(rune(m.Identifier[0])) {
			t.Errorf("identifier %q does not start with a letter", m.Identifier)
		}
		if len(m.Identifier) > ClaudeToolkit.MaxLength {
			t.Errorf("identifier %q is %d bytes, over the %d cap", m.Identifier, len(m.Identifier), ClaudeToolkit.MaxLength)
		}
	}
}

// TestExportHelixCode_IdentityTravelsAsAValue holds contract invariant 2: the
// `helixllm/...` string is data, never the thing a consumer keys on.
func TestExportHelixCode_IdentityTravelsAsAValue(t *testing.T) {
	cfg, err := ExportHelixCode(gpuInstance())
	if err != nil {
		t.Fatalf("ExportHelixCode: %v", err)
	}
	for _, m := range cfg.Models {
		if m.Identifier == m.Identity {
			t.Errorf("identifier %q IS the identity — the two must stay separate", m.Identifier)
		}
		if strings.ContainsAny(m.Identifier, "/:") {
			t.Errorf("identifier %q carries an identity separator", m.Identifier)
		}
	}
	// The env assignment carries the endpoint, never an identity.
	for _, line := range strings.Split(cfg.EnvFile, "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if strings.Contains(line, IdentityPrefix+"/") {
			t.Errorf("assignment line %q carries a human-readable identity", line)
		}
	}
	if !strings.Contains(cfg.EnvFile, HelixCodeEndpointEnv+"=") {
		t.Errorf("env file does not assign %s:\n%s", HelixCodeEndpointEnv, cfg.EnvFile)
	}
}

// TestExportHelixCode_MergeIsIdempotent holds contract invariant 3.
func TestExportHelixCode_MergeIsIdempotent(t *testing.T) {
	cfg, err := ExportHelixCode(gpuInstance())
	if err != nil {
		t.Fatalf("ExportHelixCode: %v", err)
	}
	existing := "# operator's own settings\nHELIX_OTHER=1\n"

	once, err := MergeHelixCodeEnv(existing, cfg)
	if err != nil {
		t.Fatalf("first merge: %v", err)
	}
	twice, err := MergeHelixCodeEnv(once, cfg)
	if err != nil {
		t.Fatalf("second merge: %v", err)
	}
	if once != twice {
		t.Errorf("merge is not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
	if n := strings.Count(twice, HelixCodeEndpointEnv+"="); n != 1 {
		t.Errorf("re-merge duplicated the assignment %d times", n)
	}
	if !strings.Contains(twice, "HELIX_OTHER=1") {
		t.Errorf("merge dropped the operator's own line:\n%s", twice)
	}
}

// TestExportHelixCode_UnavailableIsNeverExportedAsUsable holds invariant 4.
func TestExportHelixCode_UnavailableIsNeverExportedAsUsable(t *testing.T) {
	cfg, err := ExportHelixCode(gpuInstance())
	if err != nil {
		t.Fatalf("ExportHelixCode: %v", err)
	}
	for _, m := range cfg.Models {
		if strings.Contains(m.Identity, "mistral") {
			t.Fatalf("unavailable model %q was exported as usable", m.Identity)
		}
	}
	if len(cfg.Withheld) != 1 {
		t.Fatalf("got %d withheld, want 1", len(cfg.Withheld))
	}
	if cfg.Withheld[0].Reason == "" {
		t.Errorf("withheld option %q carries no reason", cfg.Withheld[0].Identity)
	}

	down := gpuInstance()
	down.Healthy = false
	down.Reason = "unreachable"
	cfg, err = ExportHelixCode(down)
	if err != nil {
		t.Fatalf("ExportHelixCode(unhealthy): %v", err)
	}
	if len(cfg.Models) != 0 {
		t.Errorf("unreachable instance exported %d usable models", len(cfg.Models))
	}
	if len(cfg.Withheld) != 3 {
		t.Errorf("got %d withheld from an unreachable instance, want 3", len(cfg.Withheld))
	}
	for _, w := range cfg.Withheld {
		if w.Reason != "unreachable" {
			t.Errorf("withheld %q reason %q, want the instance's own reason", w.Identity, w.Reason)
		}
	}
}

// TestExportHelixCode_SecretNeverReachesTheConfiguration holds invariant 5.
func TestExportHelixCode_SecretNeverReachesTheConfiguration(t *testing.T) {
	for _, bad := range []string{
		"http://helix:s3cr3t@gpu-01:18434",
		"http://gpu-01:18434?token=s3cr3t",
		"http://gpu-01:18434#s3cr3t",
	} {
		inst := gpuInstance()
		inst.BaseURL = bad
		cfg, err := ExportHelixCode(inst)
		if err == nil {
			t.Errorf("BaseURL %q was accepted; env file:\n%s", bad, cfg.EnvFile)
			continue
		}
		if strings.Contains(err.Error(), "s3cr3t") {
			t.Errorf("the error message for %q leaks the secret: %v", bad, err)
		}
	}
}

// TestMergeHelixCodeEnv_ForeignAssignmentSurfaces: a second assignment of the
// same variable outside our block would silently win or lose depending on load
// order. Surfacing it is the honest answer; silently rewriting the operator's
// line is not.
func TestMergeHelixCodeEnv_ForeignAssignmentSurfaces(t *testing.T) {
	cfg, err := ExportHelixCode(gpuInstance())
	if err != nil {
		t.Fatalf("ExportHelixCode: %v", err)
	}
	_, err = MergeHelixCodeEnv("export "+HelixCodeEndpointEnv+"=http://elsewhere:1\n", cfg)
	if err == nil {
		t.Fatalf("a conflicting foreign assignment was merged silently")
	}
}
